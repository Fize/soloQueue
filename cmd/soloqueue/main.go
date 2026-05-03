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
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/compactor"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/llm/deepseek"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/prompt"
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

			settings := cfg.Get()

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
				"version", version, "model", rt.defaultModel.ID)

			agentFactory := buildSessionFactory(rt, workDir, settings, false /* TUI: no console log */)
			mgr := session.NewSessionManager(agentFactory, log)
			mgr.SetRouter(buildRouterFunc(rt))

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
				rt.toolsCfg.Executor = executor

				sess, err := mgr.Init(context.Background(), "")
				sandboxCh <- tui.SandboxInitMsg{Sess: sess, Err: err}
			}()

			return tui.Run(tui.Config{
				Session:       nil,
				SandboxInitCh: sandboxCh,
				ModelID:       rt.defaultModel.ID,
				Version:       version,
				RulesCreated:  rt.rulesCreated,
				RulesPath:     rt.promptCfg.RulesPath(),
				Registry:      rt.agentRegistry,
				Supervisors:   rt.supervisors,
				Skills:        rt.skillRegistry,
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
	llmClient     agent.LLMClient
	agentRegistry *agent.Registry
	agentFactory  *agent.DefaultFactory
	supervisors   []*agent.Supervisor
	leaders       []prompt.LeaderInfo
	allTemplates  []agent.AgentTemplate
	systemPrompt  string
	promptCfg     *prompt.PromptConfig
	defaultModel  *config.LLMModel
	tokenizer     *ctxwin.Tokenizer
	compactor     ctxwin.Compactor // context compression engine
	toolsCfg      tools.Config
	rulesCreated  bool
	taskRouter    *router.Router // 任务路由分类器（TUI + serve 共用）
	skillRegistry *skill.SkillRegistry
	dockerSandbox sandbox.Sandbox   // Docker 沙盒（L3 工具执行隔离底座）
	sandboxMounts []sandbox.Mount // 沙盒挂载列表（延迟启动用）
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
	apiKey := provider.ResolveAPIKey()
	if apiKey == "" {
		log.Warn(logger.CatApp, "LLM API key not set", "env", provider.APIKeyEnv)
	}
	baseURL := provider.BaseURL
	if v := os.Getenv("DEEPSEEK_BASE_URL"); v != "" && baseURL == "" {
		baseURL = v
	}
	llmClient, err := deepseek.NewClient(deepseek.Config{
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

	systemPrompt, err := promptCfg.BuildPrompt(leaders)
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
	// 为每个 IsLeader 模板创建 L2 Agent 并用 Supervisor 管理其 L3 子 Agent 生命周期。
	// Supervisor 在 runtimeStack.shutdown() 时负责回收所有 L3 子 Agent。
	var supervisors []*agent.Supervisor
	for _, tmpl := range allTemplates {
		if !tmpl.IsLeader {
			continue
		}
		l2Agent, _, err := agentFactory.Create(context.Background(), tmpl)
		if err != nil {
			log.Warn(logger.CatApp, "failed to create L2 agent", "name", tmpl.Name, "err", err)
			continue
		}
		sv := agent.NewSupervisor(l2Agent, agentFactory, log)
		// Wire L2's DelegateTools to spawn L3 through Supervisor.SpawnChild
		// so L3 children are tracked and visible in the TUI sidebar.
		sv.WireSpawnFns(allTemplates)
		supervisors = append(supervisors, sv)
	}

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
			if p == "" || p == "@default" || seen[p] {
				continue
			}
			seen[p] = true
			sandboxMounts = append(sandboxMounts, sandbox.Mount{HostPath: p})
		}
	}

	return &runtimeStack{
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
	}, nil
}

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

// sessionBuilder encapsulates session creation logic, replacing the
// 140-line closure in buildSessionFactory with a testable struct.
type sessionBuilder struct {
	rt         *runtimeStack
	workDir    string
	settings   config.Settings
	consoleLog bool
	tlMaxBytes int64
	tlMaxFiles int
}

func newSessionBuilder(
	rt *runtimeStack,
	workDir string,
	settings config.Settings,
	consoleLog bool,
) *sessionBuilder {
	return &sessionBuilder{
		rt:         rt,
		workDir:    workDir,
		settings:   settings,
		consoleLog: consoleLog,
		tlMaxBytes: int64(config.DefaultInt(settings.Session.TimelineMaxFileMB, 50)) * 1024 * 1024,
		tlMaxFiles: config.DefaultInt(settings.Session.TimelineMaxFiles, 5),
	}
}

// Build creates a new session with its own agent, context window, and
// timeline writer. Each call produces an independent session.
func (sb *sessionBuilder) Build(ctx context.Context, teamID string) (*agent.Agent, *ctxwin.ContextWindow, *timeline.Writer, error) {
	agentID := newAgentID()

	effectiveModelID := sb.rt.defaultModel.APIModel
	if effectiveModelID == "" {
		effectiveModelID = sb.rt.defaultModel.ID
	}
	def := agent.Definition{
		ID:              agentID,
		Kind:            agent.KindChat,
		ModelID:         effectiveModelID,
		Temperature:     sb.rt.defaultModel.Generation.Temperature,
		MaxTokens:       sb.rt.defaultModel.Generation.MaxTokens,
		ReasoningEffort: sb.rt.defaultModel.Thinking.ReasoningEffort,
		ThinkingEnabled: sb.rt.defaultModel.Thinking.Enabled,
		MaxIterations:   1000,
		ContextWindow:   sb.rt.defaultModel.ContextWindow,
		SystemPrompt:    sb.rt.systemPrompt,
	}

	effectiveTeam := teamID
	if effectiveTeam == "" {
		effectiveTeam = "default"
	}
	sessLog, err := logger.Session(sb.workDir, effectiveTeam, agentID,
		logger.WithLevel(logger.ParseLogLevel(sb.settings.Log.Level)),
		logger.WithConsole(sb.consoleLog),
		logger.WithFile(sb.settings.Log.File),
	)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build session logger: %w", err)
	}

	// Tools: built-in tools (fallback-only for L1) + DelegateTool (async mode: L1 -> L2)
	sessionToolsCfg := sb.rt.toolsCfg
	sessionToolsCfg.Logger = sessLog
	baseTools := tools.Build(sessionToolsCfg)

	// Auto-reload: wrap file-writing tools so writes to agents/ or groups/ dirs
	// trigger automatic parsing and instantiation.
	autoReloadCfg := &team.AutoReloadConfig{
		AgentsDir:    filepath.Join(sb.workDir, "agents"),
		GroupsDir:    filepath.Join(sb.workDir, "groups"),
		AgentFactory: sb.rt.agentFactory,
		Logger:       sessLog,
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
			dt := tools.NewDelegateTool(leader.Name, leader.Description, 5*time.Minute, sb.rt.agentRegistry, sessLog)
			dt.SpawnFn = func(ctx context.Context, task string) (iface.Locatable, error) {
				a, ok := sb.rt.agentRegistry.Get(leader.Name)
				if !ok {
					return nil, fmt.Errorf("leader %q not found", leader.Name)
				}
				return &agent.LocatableAdapter{Agent: a}, nil
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
			forkTools := tools.Build(sb.rt.toolsCfg)
			if len(s.AllowedTools) > 0 {
				forkTools = skill.FilterTools(forkTools, s.AllowedTools)
			}
			child := agent.NewAgent(forkDef, sb.rt.llmClient, sessLog,
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

	a := agent.NewAgent(def, sb.rt.llmClient, sessLog,
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
	autoReloadCfg.OnLeaderCreated = func(ctx context.Context, name string, ag *agent.Agent) {
		dt := tools.NewDelegateTool(name, name+" team leader", 5*time.Minute, sb.rt.agentRegistry, sessLog)
		dt.SpawnFn = func(ctx context.Context, task string) (iface.Locatable, error) {
			a, ok := sb.rt.agentRegistry.Get(name)
			if !ok {
				return nil, fmt.Errorf("leader %q not found in registry", name)
			}
			return &agent.LocatableAdapter{Agent: a}, nil
		}
		if err := a.RegisterTool(dt); err != nil {
			sessLog.Error(logger.CatActor, "register delegate tool for new leader failed",
				"leader", name, "err", err)
		}
	}

	// Timeline writer + push hook
	tlDir := filepath.Join(sb.workDir, "logs", "timelines", effectiveTeam)
	tl, err := timeline.NewWriter(tlDir, "timeline", sb.tlMaxBytes, sb.tlMaxFiles,
		timeline.WithWriterLogger(sessLog))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build timeline writer: %w", err)
	}
	summaryHook := func(summary string) {
		if err := tl.AppendControl(&timeline.ControlPayload{
			Action:  "summary",
			Reason:  "auto_compact",
			Content: summary,
		}); err != nil {
			sessLog.Error(logger.CatActor, "timeline summary append failed",
				"err", err, "agent_id", agentID)
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
		sb.rt.defaultModel.ContextWindow,
		sb.rt.defaultModel.ContextWindow/10,
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
	settings config.Settings,
	consoleLog bool,
) session.AgentFactory {
	sb := newSessionBuilder(rt, workDir, settings, consoleLog)
	return sb.Build
}

// buildRouterFunc creates a session.TaskRouterFunc from the runtimeStack's task router.
// Returns nil if no router is configured (routing disabled).
func buildRouterFunc(rt *runtimeStack) session.TaskRouterFunc {
	if rt.taskRouter == nil {
		return nil
	}
	rtr := rt.taskRouter
	return func(ctx context.Context, prompt string) (session.RouteResult, error) {
		decision, err := rtr.Route(ctx, prompt)
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
			rt.toolsCfg.Executor = executor

			factory := buildSessionFactory(rt, workDir, settings, settings.Log.Console)
			mgr := session.NewSessionManager(factory, log)
			mgr.SetRouter(buildRouterFunc(rt))

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
