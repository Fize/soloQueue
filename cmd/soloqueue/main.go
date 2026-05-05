package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/compactor"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/embedding"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/llm/deepseek"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/memory"
	"github.com/xiaobaitu/soloqueue/internal/permanent"
	"github.com/xiaobaitu/soloqueue/internal/prompt"
	"github.com/xiaobaitu/soloqueue/internal/vectorstore"
	"github.com/xiaobaitu/soloqueue/internal/server"
	"github.com/xiaobaitu/soloqueue/internal/router"
	"github.com/xiaobaitu/soloqueue/internal/sandbox"
	"github.com/xiaobaitu/soloqueue/internal/session"
	"github.com/xiaobaitu/soloqueue/internal/skill"
	"github.com/xiaobaitu/soloqueue/internal/timeline"
	"github.com/xiaobaitu/soloqueue/internal/tools"
	"github.com/xiaobaitu/soloqueue/internal/team"
	"github.com/xiaobaitu/soloqueue/internal/tui"
)

const version = "0.1.0"

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "soloqueue",
		Short: "SoloQueue — AI multi-agent collaboration tool",
		Long: `SoloQueue is an AI multi-agent collaboration tool built on the Actor model.

Run without subcommands for interactive TUI mode.
Use 'soloqueue serve' to start the local HTTP/WebSocket server.`,
		SilenceUsage:   true,
		SilenceErrors:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			workDir, err := config.DefaultWorkDir()
			if err != nil {
				return err
			}
			cfg, err := config.Init(workDir)
			if err != nil {
				return fmt.Errorf("init config: %w", err)
			}
			defer cfg.Close()

			log, err := initLogger(workDir, cfg, false)
			if err != nil {
				return fmt.Errorf("init logger: %w", err)
			}
			defer log.Close()

			cfg.SetLogger(log)

			// promptProfileQuestions は TUI モードのみ必要なので、
			// profileSetup コールバックとして渡す
			profileSetup := func(cfg *prompt.PromptConfig) error {
				answers := promptProfileQuestions()
				return cfg.WriteProfile(answers)
			}

			rt, err := buildRuntimeStack(workDir, cfg, log, profileSetup)
			if err != nil {
				return err
			}
			defer rt.shutdown()

			log.Info(logger.CatApp, "soloqueue tui starting",
				"version", version, "model", rt.readDefaultModel().ID)

			agentFactory := buildSessionFactory(rt, workDir, cfg, false /* TUI: no console log */)
			mgr := session.NewSessionManager(agentFactory, log)
			mgr.SetRouter(buildRouterFunc(rt))
			mgr.SetMemoryHook(buildMemoryHook(rt))

			defer mgr.Shutdown(5 * time.Second)

			// Start TUI immediately; sandbox + session init run in background.
			// The TUI shows a loading indicator until the session is delivered.
			sandboxCh := make(chan tui.SandboxInitMsg, 1)

			go func() {
				sb, executor, err := startSandbox(context.Background(), rt.sandboxMounts, log)
				if err != nil {
					sandboxCh <- tui.SandboxInitMsg{Err: err}
					return
				}
				rt.dockerSandbox = sb
				rt.cfgMu.Lock()
				rt.toolsCfg.Executor = executor
				rt.cfgMu.Unlock()

				sess, err := mgr.Init(context.Background(), "")
				sandboxCh <- tui.SandboxInitMsg{Sess: sess, Err: err}
			}()

			return tui.Run(tui.Config{
				Session:       nil,
				SandboxInitCh: sandboxCh,
				ModelID:       rt.readDefaultModel().ID,
				Version:       version,
				RulesCreated:  rt.rulesCreated,
				RulesPath:     rt.promptCfg.RulesPath(),
				Registry:      rt.agentRegistry,
				Supervisors:   rt.supervisors,
				Skills:        rt.skillRegistry,
				NotifyCh:      rt.permNotifyCh,
			})
		},
	}

	root.AddCommand(versionCmd())
	root.AddCommand(serveCmd())
	root.AddCommand(cleanupCmd())

	return root
}

// ─── runtimeStack ─────────────────────────────────────────────────────────────

// runtimeStack 保存两种模式（TUI / serve）共享的运行时依赖，
// 由 buildRuntimeStack 统一初始化，避免重复代码。
type runtimeStack struct {
	// 配置派生字段，受 cfgMu 保护（用于热重载）。
	// 构造期间可直接赋值；构造完成后必须通过 read* / OnChange 写锁访问。
	cfgMu        sync.RWMutex
	llmClient    agent.LLMClient
	toolsCfg     tools.Config
	defaultModel *config.LLMModel

	agentRegistry *agent.Registry
	agentFactory  *agent.DefaultFactory
	supervisors   []*agent.Supervisor
	leaders       []prompt.LeaderInfo
	allTemplates  []agent.AgentTemplate
	systemPrompt  string
	promptCfg     *prompt.PromptConfig
	tokenizer     *ctxwin.Tokenizer
	compactor     ctxwin.Compactor // context compression engine
	rulesCreated  bool
	taskRouter    *router.Router // 任务路由分类器（TUI + serve 共用）
	skillRegistry *skill.SkillRegistry
	dockerSandbox  sandbox.Sandbox   // Docker 沙盒（L3 工具执行隔离底座）
	sandboxMounts  []sandbox.Mount // 沙盒挂载列表（延迟启动用）
	memoryManager   *memory.Manager   // 短期记忆管理器
	permanentMemory *permanent.Manager // 长期记忆管理器
	permScheduler   *permanent.Scheduler
	permNotifyCh    chan string
	permCancel      context.CancelFunc // 取消 permanent scheduler 的 context
}

// shutdown 优雅回收所有 L2 Supervisor 管理的子 Agent，并销毁 Docker 沙盒。
func (rt *runtimeStack) shutdown() {
	for _, sv := range rt.supervisors {
		_ = sv.ReapAll(5 * time.Second)
	}
	if rt.dockerSandbox != nil {
		destroyCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := rt.dockerSandbox.Destroy(destroyCtx); err != nil {
			fmt.Fprintf(os.Stderr, "warning: docker sandbox destroy failed: %v\n", err)
		}
	}
}

// readLLMClient 返回当前 LLM 客户端（并发安全，读取配置热重载后的最新值）。
func (rt *runtimeStack) readLLMClient() agent.LLMClient {
	rt.cfgMu.RLock()
	defer rt.cfgMu.RUnlock()
	return rt.llmClient
}

// readToolsCfg 返回当前工具配置（并发安全，读取配置热重载后的最新值）。
func (rt *runtimeStack) readToolsCfg() tools.Config {
	rt.cfgMu.RLock()
	defer rt.cfgMu.RUnlock()
	return rt.toolsCfg
}

// readDefaultModel 返回当前默认模型（并发安全，读取配置热重载后的最新值）。
func (rt *runtimeStack) readDefaultModel() *config.LLMModel {
	rt.cfgMu.RLock()
	defer rt.cfgMu.RUnlock()
	return rt.defaultModel
}

// profileSetupFn 在首次启动时写入用户 profile（TUI 用交互式问卷，serve 用默认值）
type profileSetupFn func(cfg *prompt.PromptConfig) error

// buildRuntimeStack 初始化两种模式共用的运行时栈：
//
//  1. LLM 客户端（DeepSeek）
//  2. Prompt 系统（EnsureFiles + BuildPrompt）
//  3. Agent Registry + DefaultFactory
//  4. L2 Supervisor 列表（IsLeader 模板各一个）
func buildRuntimeStack(
	workDir string,
	cfg *config.GlobalService,
	log *logger.Logger,
	profileSetup profileSetupFn,
) (*runtimeStack, error) {
	settings := cfg.Get()
	provider := cfg.DefaultProvider()
	if provider == nil {
		return nil, errors.New("no default provider configured")
	}
	defaultModel := cfg.DefaultModelByRole("fast")
	if defaultModel == nil {
		return nil, errors.New("no default model configured (fast role)")
	}

	// ── LLM 客户端 ───────────────────────────────────────────────────────────
	llmClient, err := buildLLMClient(provider, log)
	if err != nil {
		return nil, fmt.Errorf("build llm client: %w", err)
	}

	// ── Tools 配置 ────────────────────────────────────────────────────────────
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get working directory: %w", err)
	}
	allowedDirs := append([]string{workDir, cwd}, settings.Tools.AllowedDirs...)
	toolsCfg := settings.Tools.ToToolsConfig(allowedDirs)

	// ── Prompt 系统 ───────────────────────────────────────────────────────────
	promptCfg := &prompt.PromptConfig{
		RoleID:  "main_assistant",
		BaseDir: filepath.Join(workDir, "prompts"),
	}
	rulesCreated, err := promptCfg.EnsureFiles()
	if err != nil {
		var profileErr *prompt.ProfileNeededError
		if errors.As(err, &profileErr) {
			if writeErr := profileSetup(promptCfg); writeErr != nil {
				return nil, fmt.Errorf("write profile: %w", writeErr)
			}
			rulesCreated, err = promptCfg.EnsureFiles()
			if err != nil {
				return nil, fmt.Errorf("ensure prompt files: %w", err)
			}
		} else {
			return nil, fmt.Errorf("ensure prompt files: %w", err)
		}
	}

	// ── Groups ─────────────────────────────────────────────────────────────
	groups, err := prompt.LoadGroups(filepath.Join(workDir, "groups"))
	if err != nil {
		log.Warn(logger.CatApp, "failed to load groups", "err", err)
		groups = nil
	}

	// ── Leaders + Agent 模板 ──────────────────────────────────────────────────
	leaders, err := prompt.LoadLeaders(filepath.Join(workDir, "agents"), groups, cwd)
	if err != nil {
		log.Warn(logger.CatApp, "failed to load leaders", "err", err)
		leaders = nil
	}
	allTemplates, err := agent.LoadAgentTemplates(filepath.Join(workDir, "agents"))
	if err != nil {
		log.Warn(logger.CatApp, "failed to load agent templates", "err", err)
		allTemplates = nil
	}

	// ── Short-term Memory Manager ─────────────────────────────────
	memoryDir := filepath.Join(workDir, "memory")
	fastModel := cfg.DefaultModelByRole("fast")
	fastModelID := ""
	if fastModel != nil {
		fastModelID = fastModel.ID
	}
	memoryMgr := memory.NewManager(memoryDir, llmClient, fastModelID, log)

		// ── Permanent Memory Manager ──────────────────────────────────────────
		var permanentMgr *permanent.Manager
		var permScheduler *permanent.Scheduler
		var permCancel context.CancelFunc
		permNotifyCh := make(chan string, 8)
		var permContent string

		if settings.Embedding.Enabled {
			embModel := cfg.DefaultEmbeddingModel()
			if embModel != nil && embModel.Enabled {
				embProvider := cfg.EmbeddingProviderByID(embModel.ProviderID)
				if embProvider != nil && embProvider.Enabled {
					apiKey := os.Getenv(embProvider.APIKeyEnv)
					embClient, embErr := embedding.NewOpenAI(embedding.OpenAIConfig{
						BaseURL:   embProvider.BaseURL,
						APIKey:    apiKey,
						ModelID:   embModel.Name,
						Dimension: embModel.Dimension,
					})
					if embErr == nil {
						store, storeErr := vectorstore.NewSQLiteStore(filepath.Join(workDir, "permanent_memory", "entries.db"))
						if storeErr == nil {
							permanentMgr = permanent.NewManager(store, embClient, memoryDir, log)
							permScheduler = permanent.NewScheduler(permanentMgr, log, func(msg string) {
								log.Error(logger.CatApp, msg)
								select {
								case permNotifyCh <- msg:
								default:
								}
							})
							permCtx, cancel := context.WithCancel(context.Background())
							permCancel = cancel
							go permScheduler.Run(permCtx)

							recentText, _ := memoryMgr.ReadRecentMemory(7)
							permContent, _ = permanentMgr.QueryForPrompt(context.Background(), recentText)
						toolsCfg.PermanentManager = permanentMgr
						} else {
							log.Warn(logger.CatApp, "permanent memory: failed to create vector store", "err", storeErr)
						}
					} else {
						log.Warn(logger.CatApp, "permanent memory: failed to create embedder", "err", embErr)
					}
				}
			}
		}


	systemPrompt, err := promptCfg.BuildPrompt(leaders, memoryDir, permContent)
	if err != nil {
		return nil, fmt.Errorf("build system prompt: %w", err)
	}

	// ── Agent Registry + Factory ──────────────────────────────────────────────
	agentRegistry := agent.NewRegistry(log)

	// Build model resolver: validates agent model IDs against settings.toml
	modelResolver := buildModelResolver(cfg)

	agentFactory := agent.NewDefaultFactory(
		agentRegistry, llmClient, toolsCfg,
		filepath.Join(workDir, "skills"), log,
		agent.WithModelResolver(modelResolver),
		agent.WithDefaultModelID(defaultModel.ID),
		agent.WithTemplates(allTemplates),
		agent.WithGroups(groups),
	)

	// ── L2 Supervisors ────────────────────────────────────────────────────────
	// L2 agents are created dynamically on first delegation (via SpawnFn).
	// Each dynamically-created L2 is wrapped in a SelfReapableAdapter so it is
	// reaped immediately when the delegation completes.
	var supervisors []*agent.Supervisor

	// ── Compactor (context compression engine) ────────────────────────────
	// Use "fast" role model, fallback to default model
	compactorModel := cfg.DefaultModelByRole("fast")
	if compactorModel == nil {
		compactorModel = defaultModel
	}
	compactorModelID := compactorModel.APIModel
	if compactorModelID == "" {
		compactorModelID = compactorModel.ID
	}
	llmCompactor := compactor.NewLLMCompactor(
		compactor.NewAgentChatClient(llmClient),
		compactorModelID,
		compactor.WithLogger(log),
	)

	tok := ctxwin.NewTokenizer()

	// ── Task Router Classifier ───────────────────────────────────────────────
	classifierModel := defaultModel.APIModel
	if classifierModel == "" {
		classifierModel = defaultModel.ID
	}
	classifierConfig := router.DefaultClassifierConfig()
	classifier := router.NewDefaultClassifier(classifierConfig, llmClient, classifierModel, log)
	taskRouter := router.NewRouter(classifier, cfg, log)

	// 加载全局 skill registry（TUI slash 命令和 session 共用）
	// 支持多目录优先级：plugin < user < project
	skill.SetPackageLogger(log)
	skillDirs := map[string]string{
		"user": filepath.Join(workDir, "skills"),
	}
	skillReg := skill.NewSkillRegistry()
	if skills, err := skill.LoadSkillsFromDirs(skillDirs); err == nil {
		for _, s := range skills {
			_ = skillReg.Register(s)
		}
	}

	// ── Docker Sandbox mounts (sandbox is started asynchronously by caller) ──
	var sandboxMounts []sandbox.Mount
	seen := make(map[string]bool)
	seen[workDir] = true
	sandboxMounts = append(sandboxMounts, sandbox.Mount{HostPath: workDir})
	for _, gf := range groups {
		for _, ws := range gf.Frontmatter.Workspaces {
			p := ws.Path
			if p == "" || p == "@default" {
				continue
			}
			// Expand ~ prefix for Docker mounts
			if strings.HasPrefix(p, "~/") || p == "~" {
				if home, err := os.UserHomeDir(); err == nil {
					p = filepath.Join(home, p[1:])
				}
			}
			if seen[p] {
				continue
			}
			seen[p] = true
			sandboxMounts = append(sandboxMounts, sandbox.Mount{HostPath: p})
		}
	}

	rt := &runtimeStack{
		llmClient:     llmClient,
		agentRegistry: agentRegistry,
		agentFactory:  agentFactory,
		supervisors:   supervisors,
		leaders:       leaders,
		allTemplates:  allTemplates,
		systemPrompt:  systemPrompt,
		promptCfg:     promptCfg,
		defaultModel:  defaultModel,
		tokenizer:     tok,
		compactor:     llmCompactor,
		toolsCfg:      toolsCfg,
		rulesCreated:  rulesCreated,
		taskRouter:    taskRouter,
		skillRegistry: skillReg,
		dockerSandbox: nil,
		sandboxMounts: sandboxMounts,
		memoryManager: memoryMgr,
		permanentMemory: permanentMgr,
		permScheduler:   permScheduler,
		permNotifyCh:    permNotifyCh,
		permCancel:      permCancel,
	}

	// 注册配置热重载回调，使 runtimeStack 中的缓存配置与 cfg 保持同步。
	cfg.OnChange(func(old, new config.Settings) {
		rt.cfgMu.Lock()
		defer rt.cfgMu.Unlock()

		// 1. Tools 配置
		newAllowedDirs := append([]string{workDir, cwd}, new.Tools.AllowedDirs...)
		newToolsCfg := new.Tools.ToToolsConfig(newAllowedDirs)
		newToolsCfg.PermanentManager = rt.toolsCfg.PermanentManager
		newToolsCfg.Logger = rt.toolsCfg.Logger
		newToolsCfg.Executor = rt.toolsCfg.Executor
		rt.toolsCfg = newToolsCfg
		if rt.agentFactory != nil {
			rt.agentFactory.SetToolsConfig(newToolsCfg)
		}

		// 2. 默认模型
		if newModel := cfg.DefaultModelByRole("fast"); newModel != nil {
			rt.defaultModel = newModel
			rt.agentFactory.SetDefaultModelID(newModel.ID)
		}

		// 3. LLM 客户端（仅在 default provider 变更时重建）
		oldProv := findDefaultProvider(old.Providers)
		newProv := findDefaultProvider(new.Providers)
		if providerChanged(oldProv, newProv) {
			if newClient, err := buildLLMClient(newProv, log); err == nil {
				rt.llmClient = newClient
				rt.agentFactory.SetLLMClient(newClient)
			} else {
				log.Warn(logger.CatConfig, "hot-reload: failed to rebuild LLM client", "err", err)
			}
		}

		// 4. 日志级别
		if new.Log.Level != old.Log.Level {
			log.SetLevel(logger.ParseLogLevel(new.Log.Level))
			log.Info(logger.CatConfig, "log level hot-reloaded", "level", new.Log.Level)
		}

		// 5. Embedding / Permanent memory
		if embeddingConfigChanged(old.Embedding, new.Embedding) {
			handleEmbeddingChange(rt, new.Embedding, cfg, log, workDir)
		}

		log.Info(logger.CatConfig, "config hot-reload applied")
	})

	return rt, nil
}

// ─── Hot-Reload Helpers ─────────────────────────────────────────────────────

// buildLLMClient creates a DeepSeek LLM client from provider configuration.
func buildLLMClient(provider *config.LLMProvider, log *logger.Logger) (agent.LLMClient, error) {
	apiKey := provider.ResolveAPIKey()
	if apiKey == "" {
		log.Warn(logger.CatApp, "LLM API key not set", "env", provider.APIKeyEnv)
	}
	baseURL := provider.BaseURL
	if v := os.Getenv("DEEPSEEK_BASE_URL"); v != "" && baseURL == "" {
		baseURL = v
	}
	return deepseek.NewClient(deepseek.Config{
		BaseURL:   baseURL,
		APIKey:    apiKey,
		Headers:   provider.Headers,
		TimeoutMs: provider.TimeoutMs,
		Retry: llm.RetryPolicy{
			MaxRetries:   provider.Retry.MaxRetries,
			InitialDelay: time.Duration(provider.Retry.InitialDelayMs) * time.Millisecond,
			MaxDelay:     time.Duration(provider.Retry.MaxDelayMs) * time.Millisecond,
			Multiplier:   provider.Retry.BackoffMultiplier,
		},
		Log: log,
	})
}

// findDefaultProvider returns the first provider with IsDefault=true from a slice.
func findDefaultProvider(providers []config.LLMProvider) *config.LLMProvider {
	for i := range providers {
		if providers[i].IsDefault {
			p := providers[i]
			return &p
		}
	}
	return nil
}

// providerChanged returns true if the default provider configuration changed
// in a way that requires recreating the LLM client.
func providerChanged(old, new *config.LLMProvider) bool {
	if old == nil || new == nil {
		return old != new
	}
	return old.BaseURL != new.BaseURL ||
		old.APIKeyEnv != new.APIKeyEnv ||
		old.TimeoutMs != new.TimeoutMs ||
		old.Retry.MaxRetries != new.Retry.MaxRetries ||
		old.Retry.InitialDelayMs != new.Retry.InitialDelayMs ||
		old.Retry.MaxDelayMs != new.Retry.MaxDelayMs ||
		old.Retry.BackoffMultiplier != new.Retry.BackoffMultiplier ||
		!stringMapsEqual(old.Headers, new.Headers)
}

func stringMapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// embeddingConfigChanged returns true if the embedding configuration changed meaningfully.
func embeddingConfigChanged(old, new config.EmbeddingConfig) bool {
	if old.Enabled != new.Enabled {
		return true
	}
	oldModel := findDefaultEmbeddingModel(old.Models)
	newModel := findDefaultEmbeddingModel(new.Models)
	if (oldModel == nil) != (newModel == nil) {
		return true
	}
	if oldModel != nil && newModel != nil {
		if oldModel.ID != newModel.ID || oldModel.ProviderID != newModel.ProviderID {
			return true
		}
	}
	return false
}

func findDefaultEmbeddingModel(models []config.EmbeddingModel) *config.EmbeddingModel {
	for i := range models {
		if models[i].IsDefault {
			m := models[i]
			return &m
		}
	}
	return nil
}

// handleEmbeddingChange handles enabling/disabling/changing the embedding subsystem at runtime.
func handleEmbeddingChange(rt *runtimeStack, emb config.EmbeddingConfig, cfg *config.GlobalService, log *logger.Logger, workDir string) {
	if !emb.Enabled && rt.permanentMemory != nil {
		// 禁用 embedding：停止 scheduler 并从 toolsCfg 中移除 PermanentManager。
		log.Info(logger.CatConfig, "embedding disabled at runtime — stopping permanent memory scheduler")
		if rt.permCancel != nil {
			rt.permCancel()
		}
		rt.permScheduler = nil
		rt.permanentMemory = nil
		rt.toolsCfg.PermanentManager = nil
		if rt.agentFactory != nil {
			rt.agentFactory.SetToolsConfig(rt.toolsCfg)
		}
		return
	}

	if emb.Enabled && rt.permanentMemory == nil {
		// 从禁用变为启用：完整热启动 embedding 子系统比较复杂，建议重启。
		log.Info(logger.CatConfig, "embedding enabled at runtime — restart required to activate")
		return
	}

	// 模型或 provider 变更：同样建议重启。
	log.Info(logger.CatConfig, "embedding model/provider changed at runtime — restart required for full effect")
}

// ─── Sandbox ─────────────────────────────────────────────────────────────────

// startSandbox creates and starts a Docker sandbox, returning it along with
// a configured DockerExecutor. It is called asynchronously so the TUI can
// start immediately while the sandbox initializes in the background.
func startSandbox(ctx context.Context, mounts []sandbox.Mount, log *logger.Logger) (sandbox.Sandbox, *sandbox.DockerExecutor, error) {
	dockerSandbox, err := sandbox.NewDockerSandbox(mounts)
	if err != nil {
		return nil, nil, fmt.Errorf("docker sandbox init failed: is Docker running? %w", err)
	}
	dockerSandbox.SetLogger(log)
	if err := dockerSandbox.Start(ctx); err != nil {
		return nil, nil, fmt.Errorf("docker sandbox start failed: is Docker running? %w", err)
	}
	log.Info(logger.CatApp, "docker sandbox started",
		"image", "debian:bookworm-slim", "mounts", len(mounts))

	executor := sandbox.NewDockerExecutor(dockerSandbox)
	executor.SetLogger(log)
	return dockerSandbox, executor, nil
}

// --- Session factory ---

// formatCtxwinMessages converts ctxwin messages to a plain-text representation
// suitable for memory summarization. Skips system messages.
func formatCtxwinMessages(msgs []ctxwin.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		switch m.Role {
		case ctxwin.RoleSystem:
			continue
		case ctxwin.RoleUser:
			b.WriteString("User: ")
		case ctxwin.RoleAssistant:
			b.WriteString("Assistant: ")
		case ctxwin.RoleTool:
			b.WriteString("Tool(" + m.Name + "): ")
		default:
			b.WriteString(string(m.Role) + ": ")
		}
		content := m.Content
		if len(content) > 2000 {
			content = content[:2000] + "...(truncated)"
		}
		b.WriteString(content)
		b.WriteString("\n\n")
	}
	return b.String()
}

// sessionBuilder encapsulates session creation logic, replacing the
// 140-line closure in buildSessionFactory with a testable struct.
type sessionBuilder struct {
	rt         *runtimeStack
	workDir    string
	cfg        *config.GlobalService // 每次 Build() 时读最新配置，支持热重载
	consoleLog bool
}

func newSessionBuilder(
	rt *runtimeStack,
	workDir string,
	cfg *config.GlobalService,
	consoleLog bool,
) *sessionBuilder {
	return &sessionBuilder{
		rt:         rt,
		workDir:    workDir,
		cfg:        cfg,
		consoleLog: consoleLog,
	}
}

// Build creates a new session with its own agent, context window, and
// timeline writer. Each call produces an independent session.
func (sb *sessionBuilder) Build(ctx context.Context, teamID string) (*agent.Agent, *ctxwin.ContextWindow, *timeline.Writer, error) {
	agentID := newAgentID()

	// 快照配置派生字段（并发安全，每次 Build 使用最新热重载值）。
	defModel := sb.rt.readDefaultModel()
	llmClient := sb.rt.readLLMClient()
	toolsCfg := sb.rt.readToolsCfg()

	effectiveModelID := defModel.APIModel
	if effectiveModelID == "" {
		effectiveModelID = defModel.ID
	}
	def := agent.Definition{
		ID:              agentID,
		Kind:            agent.KindChat,
		ModelID:         effectiveModelID,
		Temperature:     defModel.Generation.Temperature,
		MaxTokens:       defModel.Generation.MaxTokens,
		ReasoningEffort: defModel.Thinking.ReasoningEffort,
		ThinkingEnabled: defModel.Thinking.Enabled,
		MaxIterations:   1000,
		ContextWindow:   defModel.ContextWindow,
		SystemPrompt:    sb.rt.systemPrompt,
	}

	effectiveTeam := teamID
	if effectiveTeam == "" {
		effectiveTeam = "default"
	}
	settings := sb.cfg.Get()
	sessLog, err := logger.Session(sb.workDir, effectiveTeam, agentID,
		logger.WithLevel(logger.ParseLogLevel(settings.Log.Level)),
		logger.WithConsole(sb.consoleLog),
		logger.WithFile(settings.Log.File),
	)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build session logger: %w", err)
	}

	// Tools: built-in tools (fallback-only for L1) + DelegateTool (async mode: L1 -> L2)
	sessionToolsCfg := toolsCfg
	sessionToolsCfg.Logger = sessLog
	baseTools := tools.Build(sessionToolsCfg)

	// Auto-reload: wrap file-writing tools so writes to agents/ or groups/ dirs
	// trigger automatic parsing and instantiation.
	autoReloadCfg := &team.AutoReloadConfig{
		AgentsDir:    filepath.Join(sb.workDir, "agents"),
		GroupsDir:    filepath.Join(sb.workDir, "groups"),
		AgentFactory: sb.rt.agentFactory,
		Logger:       sessLog,
		OnWorkerCreated: func(ctx context.Context, name, group string, ag *agent.Agent) {
			for _, sv := range sb.rt.supervisors {
				if sv.Group() == group {
					sv.AdoptChild(ag)
					sessLog.Info(logger.CatActor, "auto-reload: worker adopted",
						"name", name, "group", group)
					return
				}
			}
		},
	}
	for i, t := range baseTools {
		switch t.Name() {
		case "Write", "Edit", "MultiWrite", "MultiEdit":
			baseTools[i] = team.WrapWithAutoReload(t, autoReloadCfg)
		}
	}

	allTools := tools.WithFallbackPrefix(baseTools)
		for _, l := range sb.rt.leaders {
			leader := l // capture loop variable

			// Find the AgentTemplate matching this leader for dynamic creation.
			var leaderTmpl *agent.AgentTemplate
			for i := range sb.rt.allTemplates {
				if sb.rt.allTemplates[i].IsLeader && sb.rt.allTemplates[i].ID == leader.Name {
					leaderTmpl = &sb.rt.allTemplates[i]
					break
				}
			}

			dt := tools.NewDelegateTool(leader.Name, leader.Description, 0, sb.rt.agentRegistry, sessLog)
			dt.SpawnFn = func(ctx context.Context, task string) (iface.Locatable, error) {
				// Prefer an idle instance to avoid cold-start latency.
				if loc, ok := sb.rt.agentRegistry.LocateIdle(leader.Name); ok {
					return loc, nil
				}
				// No idle instance — create a new one with a unique InstanceID.
				if leaderTmpl != nil {
					child, _, err := sb.rt.agentFactory.Create(ctx, *leaderTmpl)
					if err != nil {
						return nil, fmt.Errorf("spawn leader %q: %w", leader.Name, err)
					}

					sv := agent.NewSupervisor(child, sb.rt.agentFactory, sessLog)
					sv.WireSpawnFns(sb.rt.allTemplates)
					sv.SetGroup(leaderTmpl.Group)
					sb.rt.supervisors = append(sb.rt.supervisors, sv)

					sessLog.Info(logger.CatActor, "dynamic L2 supervisor created",
						"instance_id", child.InstanceID,
						"name", leader.Name,
					)
					return agent.NewSelfReapableAdapter(child, sv), nil
				}
				// Fallback: any existing instance (busy but functional).
				if loc, ok := sb.rt.agentRegistry.Locate(leader.Name); ok {
					return loc, nil
				}
				return nil, fmt.Errorf("leader %q not found", leader.Name)
			}
			allTools = append(allTools, dt)
		}

	// Skills: 使用全局 skillRegistry
	skillList := sb.rt.skillRegistry.Skills()

	// SkillTool: 仅在有 skill 时注册
	if sb.rt.skillRegistry.Len() > 0 {
		// Fork spawn 函数：创建临时子 agent 执行 fork 模式的 skill
		forkSpawn := func(ctx context.Context, s *skill.Skill, content, args string) (iface.Locatable, func(), error) {
			forkDef := agent.Definition{
				ID:           fmt.Sprintf("skill-fork-%s", s.ID),
				ModelID:      def.ModelID,
				SystemPrompt: content,
			}
			forkTools := tools.Build(toolsCfg)
			if len(s.AllowedTools) > 0 {
				forkTools = skill.FilterTools(forkTools, s.AllowedTools)
			}
			child := agent.NewAgent(forkDef, llmClient, sessLog,
				agent.WithTools(forkTools...),
				agent.WithParallelTools(true),
			)
			if err := child.Start(ctx); err != nil {
				return nil, nil, fmt.Errorf("start fork agent: %w", err)
			}
			cleanup := func() { child.Stop(5) }
			return &agent.LocatableAdapter{Agent: child}, cleanup, nil
		}

		skillTool := skill.NewSkillTool(sb.rt.skillRegistry, forkSpawn,
			skill.WithSkillLogger(sessLog))
		allTools = append(allTools, skillTool)
	}

	a := agent.NewAgent(def, llmClient, sessLog,
		agent.WithTools(allTools...),
		agent.WithSkills(skillList...),
		agent.WithParallelTools(true),
		agent.WithPriorityMailbox(),
		agent.WithToolTimeout("shell_exec", 30*time.Second),
		agent.WithToolTimeout("http_fetch", 10*time.Second),
		agent.WithToolTimeout("web_search", 15*time.Second),
	)
	sb.rt.agentRegistry.Register(a)

	// Set the OnLeaderCreated hook after agent construction so the closure
	// can reference 'a'. The hook fires when a leader agent file is written
	// and auto-instantiated — it dynamically registers a delegate_* tool on L1.
	autoReloadCfg.OnLeaderCreated = func(ctx context.Context, name, group string, ag *agent.Agent) {
		sv := agent.NewSupervisor(ag, sb.rt.agentFactory, sessLog)
		sv.WireSpawnFns(sb.rt.allTemplates)
		sv.SetGroup(group)
		sb.rt.supervisors = append(sb.rt.supervisors, sv)
		sessLog.Info(logger.CatActor, "auto-reload: leader supervisor created",
			"name", name, "group", group)

		dt := tools.NewDelegateTool(name, name+" team leader", 0, sb.rt.agentRegistry, sessLog)
		dt.SpawnFn = func(ctx context.Context, task string) (iface.Locatable, error) {
			// Prefer an idle instance to enable parallel delegation.
			if loc, ok := sb.rt.agentRegistry.LocateIdle(name); ok {
				return loc, nil
			}
			// Fallback: any instance (even if busy).
			if loc, ok := sb.rt.agentRegistry.Locate(name); ok {
				return loc, nil
			}
			return nil, fmt.Errorf("leader %q not found in registry", name)
		}
		if err := a.RegisterTool(dt); err != nil {
			sessLog.Error(logger.CatActor, "register delegate tool for new leader failed",
				"leader", name, "err", err)
		}
	}

	// Timeline writer + push hook
	tlDir := filepath.Join(sb.workDir, "logs", "timelines", effectiveTeam)
	tlMaxBytes := int64(config.DefaultInt(settings.Session.TimelineMaxFileMB, 50)) * 1024 * 1024
	tlMaxFiles := config.DefaultInt(settings.Session.TimelineMaxFiles, 5)
	tl, err := timeline.NewWriter(tlDir, "timeline", tlMaxBytes, tlMaxFiles,
		timeline.WithWriterLogger(sessLog))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build timeline writer: %w", err)
	}
	summaryHook := func(summary string, msgs []ctxwin.Message) {
		if err := tl.AppendControl(&timeline.ControlPayload{
			Action:  "summary",
			Reason:  "auto_compact",
			Content: summary,
		}); err != nil {
			sessLog.Error(logger.CatActor, "timeline summary append failed",
				"err", err, "agent_id", agentID)
		}
		// Record to short-term memory (fire-and-forget, non-blocking)
		if sb.rt.memoryManager != nil {
			go func() {
				text := formatCtxwinMessages(msgs)
				_ = sb.rt.memoryManager.Record(context.Background(), text)
			}()
		}
	}

	pushHook := func(msg ctxwin.Message) {
		var toolCalls []timeline.ToolCallRec
		for _, tc := range msg.ToolCalls {
			toolCalls = append(toolCalls, timeline.ToolCallRec{
				ID:        tc.ID,
				Type:      tc.Type,
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			})
		}
		if err := tl.AppendMessage(&timeline.MessagePayload{
			Role:             string(msg.Role),
			Content:          msg.Content,
			ReasoningContent: msg.ReasoningContent,
			Name:             msg.Name,
			ToolCallID:       msg.ToolCallID,
			ToolCalls:        toolCalls,
			IsEphemeral:      msg.IsEphemeral,
			AgentID:          agentID,
		}); err != nil {
			sessLog.Error(logger.CatActor, "timeline append failed",
				"err", err, "role", string(msg.Role), "agent_id", agentID)
		}
	}

	// ContextWindow + system prompt
	cw := ctxwin.NewContextWindow(
		defModel.ContextWindow,
		defModel.ContextWindow/10,
		0,
		sb.rt.tokenizer,
		ctxwin.WithPushHook(pushHook),
		ctxwin.WithSummaryHook(summaryHook),
		ctxwin.WithCompactor(sb.rt.compactor),
	)
	if def.SystemPrompt != "" {
		cw.Push(ctxwin.RoleSystem, def.SystemPrompt)
	}

	// Replay history segments (always enabled)
	segments, err := timeline.ReadLastSegments(tlDir, "timeline")
	if err == nil && len(segments) > 0 {
		cw.SetReplayMode(true)
		timeline.ReplayInto(cw, segments)
		cw.SetReplayMode(false)
	}

	if err := a.Start(context.Background()); err != nil {
		tl.Close()
		return nil, nil, nil, err
	}
	return a, cw, tl, nil
}

// buildSessionFactory constructs the factory function used by SessionManager.
//
// consoleLog controls whether the session logger outputs to stderr
// (TUI=false, serve=settings.Log.Console).
func buildSessionFactory(
	rt *runtimeStack,
	workDir string,
	cfg *config.GlobalService,
	consoleLog bool,
) session.AgentFactory {
	sb := newSessionBuilder(rt, workDir, cfg, consoleLog)
	return sb.Build
}

// buildRouterFunc creates a session.TaskRouterFunc from the runtimeStack's task router.
// Returns nil if no router is configured (routing disabled).
func buildRouterFunc(rt *runtimeStack) session.TaskRouterFunc {
	if rt.taskRouter == nil {
		return nil
	}
	rtr := rt.taskRouter
	return func(ctx context.Context, prompt string, priorLevel string) (session.RouteResult, error) {
		// Parse priorLevel string to router.ClassificationLevel
		var pl router.ClassificationLevel
		switch priorLevel {
		case "L0-Conversation":
			pl = router.LevelConversation
		case "L1-SimpleSingleFile":
			pl = router.LevelSimpleSingleFile
		case "L2-MediumMultiFile":
			pl = router.LevelMediumMultiFile
		case "L3-ComplexRefactoring":
			pl = router.LevelComplexRefactoring
		default:
			pl = router.LevelUnknown
		}
		decision, err := rtr.Route(ctx, prompt, pl)
		if err != nil {
			return session.RouteResult{}, err
		}
		return session.RouteResult{
			ProviderID:      decision.ProviderID,
			ModelID:         decision.ModelID,
			ThinkingEnabled: decision.ThinkingEnabled,
			ReasoningEffort: decision.ReasoningEffort,
			Level:           decision.Level.String(),
		}, nil
	}
}

// buildMemoryHook creates a session.MemoryHook that records conversation segments
// to the short-term memory system using the fast model for summarization.
func buildMemoryHook(rt *runtimeStack) session.MemoryHook {
	if rt.memoryManager == nil {
		return nil
	}
	return func(ctx context.Context, conversationText string) {
		_ = rt.memoryManager.Record(ctx, conversationText)
	}
}

// ─── Commands ──────────────────────────────────────────────────────────────────

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			workDir, err := config.DefaultWorkDir()
			if err != nil {
				return err
			}

			cfg, err := config.Init(workDir)
			if err != nil {
				return fmt.Errorf("init config: %w", err)
			}
			defer cfg.Close()

			log, err := initLogger(workDir, cfg, false)
			if err != nil {
				return fmt.Errorf("init logger: %w", err)
			}
			defer log.Close()

			cfg.SetLogger(log)

			settings := cfg.Get()

			log.Info(logger.CatApp, "soloqueue version info",
				"version", version,
				"work_dir", workDir,
				"log_level", settings.Log.Level,
			)

			p := cfg.DefaultProvider()
			if p != nil {
				log.Info(logger.CatApp, "default provider", "name", p.Name, "id", p.ID)
			}

			m := cfg.DefaultModelByRole("fast")
			if m != nil {
				log.Info(logger.CatApp, "default model", "name", m.Name, "id", m.ID)
			}
			return nil
		},
	}
}

func cleanupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cleanup",
		Short: "Remove all soloqueue sandbox containers",
		RunE: func(cmd *cobra.Command, args []string) error {
			sb, err := sandbox.NewDockerSandbox(nil)
			if err != nil {
				return fmt.Errorf("docker client init failed: is Docker running? %w", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := sb.Cleanup(ctx); err != nil {
				return err
			}
			fmt.Println("cleanup done")
			return nil
		},
	}
}

func serveCmd() *cobra.Command {
	var port int
	var host string
	var verbose bool

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the local HTTP/WebSocket server",
		RunE: func(cmd *cobra.Command, args []string) error {
			workDir, err := config.DefaultWorkDir()
			if err != nil {
				return err
			}

			cfg, err := config.Init(workDir)
			if err != nil {
				return fmt.Errorf("init config: %w", err)
			}
			defer cfg.Close()

			log, err := initLogger(workDir, cfg, verbose)
			if err != nil {
				return fmt.Errorf("init logger: %w", err)
			}
			defer log.Close()

			log.Info(logger.CatApp, "soloqueue serve starting",
				"host", host, "port", port, "version", version)

			cfg.SetLogger(log)

			settings := cfg.Get()

			// serve 模式无交互终端，使用默认 profile
			profileSetup := func(cfg *prompt.PromptConfig) error {
				return cfg.WriteProfile(prompt.DefaultProfileAnswers())
			}

			rt, err := buildRuntimeStack(workDir, cfg, log, profileSetup)
			if err != nil {
				return err
			}
			defer rt.shutdown()

			// serve mode: start sandbox synchronously before session init
			sb, executor, err := startSandbox(context.Background(), rt.sandboxMounts, log)
			if err != nil {
				return err
			}
			rt.dockerSandbox = sb
			rt.cfgMu.Lock()
			rt.toolsCfg.Executor = executor
			rt.cfgMu.Unlock()

			factory := buildSessionFactory(rt, workDir, cfg, settings.Log.Console)
			mgr := session.NewSessionManager(factory, log)
			mgr.SetRouter(buildRouterFunc(rt))
			mgr.SetMemoryHook(buildMemoryHook(rt))

			_, err = mgr.Init(context.Background(), "")
			if err != nil {
				return fmt.Errorf("init session: %w", err)
			}

			rootCtx, stop := signal.NotifyContext(context.Background(),
				os.Interrupt, syscall.SIGTERM)
			defer stop()

			mux := server.NewMux(log)
			srv := &http.Server{
				Addr:    fmt.Sprintf("%s:%d", host, port),
				Handler: mux,
			}

			go func() {
				<-rootCtx.Done()
				log.Info(logger.CatApp, "shutdown signal received")
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				_ = srv.Shutdown(shutdownCtx)
				mgr.Shutdown(5 * time.Second)
			}()

			log.Info(logger.CatApp, "server listening", "addr", srv.Addr)

			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				return fmt.Errorf("http listen: %w", err)
			}
			log.Info(logger.CatApp, "soloqueue serve stopped")
			return nil
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", 8765, "HTTP server port")
	cmd.Flags().StringVar(&host, "host", "127.0.0.1", "HTTP server host")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "print logs to console (stderr)")

	return cmd
}

// newAgentID returns a short random ID for an agent instance
func newAgentID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("crypto/rand.Read failed: %v", err))
	}
	return "agent-" + hex.EncodeToString(b[:])
}

// buildModelResolver creates a ModelResolver that validates agent model IDs
// against the settings model registry. The resolver looks up the model by ID,
// checks it is enabled, and returns the resolved model parameters.
func buildModelResolver(cfg *config.GlobalService) agent.ModelResolver {
	return func(modelID string) (agent.ModelInfo, error) {
		m := cfg.ModelByID(modelID)
		if m == nil {
			// List available model IDs for a helpful error message
			settings := cfg.Get()
			var available []string
			for _, model := range settings.Models {
				if model.Enabled {
					available = append(available, model.ID)
				}
			}
			return agent.ModelInfo{}, fmt.Errorf(
				"model %q not found in settings; available models: %v", modelID, available)
		}
		if !m.Enabled {
			return agent.ModelInfo{}, fmt.Errorf("model %q is disabled in settings", modelID)
		}
		return agent.ModelInfo{
			APIModel:        m.APIModel,
			ContextWindow:   m.ContextWindow,
			Temperature:     m.Generation.Temperature,
			MaxTokens:       m.Generation.MaxTokens,
			ThinkingEnabled: m.Thinking.Enabled,
			ReasoningEffort: m.Thinking.ReasoningEffort,
		}, nil
	}
}

// promptProfileQuestions runs the interactive onboarding questionnaire before TUI startup.
// It first shows the preset character list; selecting a preset skips the detailed questionnaire,
// while selecting Custom continues to the original flow.
func promptProfileQuestions() prompt.ProfileAnswers {
	presets := prompt.PresetProfiles()

	fmt.Println(prompt.PresetSelectionPrompt())
	fmt.Println()

	choice := readLineWithDefault("Enter number (1-7)", "7")

	// Parse preset selection
	if choice != "" && choice != "7" {
		for i, p := range presets {
			if choice == fmt.Sprintf("%d", i+1) {
				return prompt.ProfileAnswers{
					Name:   p.Name,
					Gender: p.Gender,
					Preset: p.Name,
				}
			}
		}
	}

	// Custom mode: continue with the original questionnaire
	answers := prompt.DefaultProfileAnswers()

	fmt.Println(prompt.ProfilePromptText())
	fmt.Println()

	answers.Name = readLineWithDefault("1. What should we call your assistant?", answers.Name)
	answers.Gender = readLineWithDefault("2. Assistant gender (male/female)?", answers.Gender)
	answers.Personality = readLineWithDefault("3. Personality (strict/playful/gentle/direct/custom)?", answers.Personality)
	answers.CommStyle = readLineWithDefault("4. Communication style (brief/detailed/casual/formal)?", answers.CommStyle)

	return answers
}

// readLineWithDefault reads a line of input, returning the default if the line is empty.
func readLineWithDefault(prompt, def string) string {
	fmt.Printf("%s [%s] ", prompt, def)
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		input := strings.TrimSpace(scanner.Text())
		if input != "" {
			return input
		}
	}
	return def
}

// initLogger 根据当前配置创建 system 层 Logger。
func initLogger(workDir string, cfg *config.GlobalService, console bool) (*logger.Logger, error) {
	settings := cfg.Get()

	level := logger.ParseLogLevel(settings.Log.Level)
	log, err := logger.System(workDir,
		logger.WithLevel(level),
		logger.WithConsole(console),
		logger.WithFile(settings.Log.File),
	)
	if err != nil {
		return nil, err
	}

	cfg.SetErrorHandler(func(err error) {
		log.Error(logger.CatConfig, "config watcher error", "err", err)
	})

	return log, nil
}
