package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/compactor"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/embedding"
	"github.com/xiaobaitu/soloqueue/internal/mcp"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/llm/deepseek"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/memory"
	"github.com/xiaobaitu/soloqueue/internal/permanent"
	"github.com/xiaobaitu/soloqueue/internal/prompt"
	"github.com/xiaobaitu/soloqueue/internal/router"
	"github.com/xiaobaitu/soloqueue/internal/sandbox"
	"github.com/xiaobaitu/soloqueue/internal/skill"
	"github.com/xiaobaitu/soloqueue/internal/sqlitedb"
	"github.com/xiaobaitu/soloqueue/internal/todo"
	"github.com/xiaobaitu/soloqueue/internal/vectorstore"
)

// ProfileSetupFn writes the user profile on first startup (interactive questionnaire for TUI, defaults for serve).
type ProfileSetupFn func(cfg *prompt.PromptConfig) error

// Build initializes the runtime stack shared by both modes:
//
//  1. LLM client (DeepSeek)
//  2. Prompt system (EnsureFiles + BuildPrompt)
//  3. Agent Registry + DefaultFactory
//  4. L2 Supervisor list (one per IsLeader template)
func Build(
	workDir string,
	cfg *config.GlobalService,
	log *logger.Logger,
	profileSetup ProfileSetupFn,
	bypassConfirm bool,
) (*Stack, error) {
	buildStart := time.Now()
	settings := cfg.Get()
	provider := cfg.DefaultProvider()
	if provider == nil {
		return nil, errors.New("no default provider configured")
	}
	defaultModel := cfg.DefaultModelByRole("fast")
	if defaultModel == nil {
		return nil, errors.New("no default model configured (fast role)")
	}

	// ── LLM Client ──────────────────────────────────────────────────────────────
	llmClient, err := BuildLLMClient(provider, log)
	if err != nil {
		return nil, fmt.Errorf("build llm client: %w", err)
	}
	log.Debug(logger.CatApp, "build: LLM client ready", "duration", time.Since(buildStart).String())

	// ── Tools Config ───────────────────────────────────────────────────────────
	toolsCfg := settings.Tools.ToToolsConfig()

	// ── MCP Manager ──────────────────────────────────────────────────────────
	mcpConfigPath := filepath.Join(workDir, "mcp.json")
	mcpLoader, mcpLoaderErr := mcp.NewLoader(mcpConfigPath, log)
	if mcpLoaderErr != nil {
		log.Warn(logger.CatMCP, "failed to create MCP config loader", "err", mcpLoaderErr)
	}
	var mcpMgr *mcp.Manager
	if mcpLoader != nil {
		if err := mcpLoader.Load(); err != nil {
			log.Warn(logger.CatMCP, "failed to load mcp.json, creating default", "err", err)
		}
		mcpMgr = mcp.NewManager(mcpLoader, log)
	}

	// ── Prompt System ──────────────────────────────────────────────────────────
	promptStart := time.Now()
	promptCfg := &prompt.PromptConfig{
		RolesDir:  filepath.Join(workDir, "roles"),
		GlobalDir: filepath.Join(workDir, "prompts", "global"),
	}
	rulesCreated, err := promptCfg.EnsureFiles()
	if err != nil {
		var profileErr *prompt.SoulNeededError
		if errors.As(err, &profileErr) {
			if writeErr := profileSetup(promptCfg); writeErr != nil {
				return nil, fmt.Errorf("write soul: %w", writeErr)
			}
			rulesCreated, err = promptCfg.EnsureFiles()
			if err != nil {
				return nil, fmt.Errorf("ensure prompt files: %w", err)
			}
		} else {
			return nil, fmt.Errorf("ensure prompt files: %w", err)
		}
	}
	log.Debug(logger.CatApp, "build: prompt system ready", "duration", time.Since(promptStart).String())

	// ── Groups ─────────────────────────────────────────────────────────────
	groups, err := prompt.LoadGroups(filepath.Join(workDir, "groups"))
	if err != nil {
		log.Warn(logger.CatApp, "failed to load groups", "err", err)
		groups = nil
	}

	// ── Leaders + Agent Templates ────────────────────────────────────────────────
	leaders, err := prompt.LoadLeaders(filepath.Join(workDir, "agents"), groups)
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

	// ── Shared SQLite DB ──────────────────────────────────────────────────
	embStart := time.Now()
	sharedDBPath := filepath.Join(workDir, "permanent_memory", "entries.db")
	sharedDB, sharedDBErr := sqlitedb.Open(sharedDBPath)
	if sharedDBErr != nil {
		return nil, fmt.Errorf("open shared sqlite db: %w", sharedDBErr)
	}
	log.Debug(logger.CatApp, "build: sqlite opened", "duration", time.Since(embStart).String())

	// ── Permanent Memory Manager ──────────────────────────────────────────
	var permanentMgr *permanent.Manager
	var permScheduler *permanent.Scheduler
	var permCancel context.CancelFunc
	permNotifyCh := make(chan string, 8)

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
					store := vectorstore.NewSQLiteStoreFromDB(sharedDB.DB, &sharedDB.WMu)
					{
						permBuildStart := time.Now()
						permanentMgr = permanent.NewManager(store, embClient, nil, "", memoryDir, log)
						permScheduler = permanent.NewScheduler(permanentMgr, log, func(msg string) {
							log.Error(logger.CatApp, msg)
							select {
							case permNotifyCh <- msg:
							default:
							}
						})
						permCtx, cancel := context.WithCancel(context.Background())
						permCancel = cancel
						go func() {
							defer func() {
								if r := recover(); r != nil {
									log.Error(logger.CatApp, "permScheduler goroutine panic recovered",
										"panic", fmt.Sprintf("%v", r))
								}
							}()
							permScheduler.Run(permCtx)
						}()

						toolsCfg.PermanentManager = permanentMgr
						log.Debug(logger.CatApp, "build: permanent memory ready", "duration", time.Since(permBuildStart).String())
					}
				} else {
					log.Warn(logger.CatApp, "permanent memory: failed to create embedder", "err", embErr)
				}
			}
		}
	}

	// ── Plan Directory ───────────────────────────────────────────────────
	planDir, planErr := config.PlanDir()
	if planErr != nil {
		log.Warn(logger.CatApp, "failed to create plan directory", "err", planErr)
	} else {
		toolsCfg.PlanDir = planDir
	}

	// ── Todo Store ──────────────────────────────────────────────────────
	todoStore := todo.NewStoreFromDB(sharedDB.DB, &sharedDB.WMu)
	toolsCfg.TodoStore = todoStore

	systemPrompt, err := promptCfg.BuildPrompt(leaders, memoryDir, memoryDir, planDir)
	if err != nil {
		return nil, fmt.Errorf("build system prompt: %w", err)
	}

	// ── Agent Registry + Factory ──────────────────────────────────────────────
	agentRegistry := agent.NewRegistry(log)

	// Build model resolver: validates agent model IDs against settings.toml
	modelResolver := BuildModelResolver(cfg)

	agentFactory := agent.NewDefaultFactory(
		agentRegistry, llmClient, toolsCfg,
		filepath.Join(workDir, "skills"), log,
		agent.WithModelResolver(modelResolver),
		agent.WithDefaultModelID(defaultModel.ID),
		agent.WithTemplates(allTemplates),
		agent.WithGroups(groups),
		agent.WithWorkDir(workDir),
		agent.WithBypassConfirm(bypassConfirm),
		agent.WithMCPManager(mcpMgr),
	)

	// ── L2 Supervisors ────────────────────────────────────────────────────────
	var supervisors []*agent.Supervisor

	// ── Compactor (context compression engine) ────────────────────────────
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

	// Load global skill registry (shared by TUI slash commands and sessions)
	skillStart := time.Now()
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
	log.Debug(logger.CatApp, "build: skills loaded", "duration", time.Since(skillStart).String())

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

	rt := &Stack{
		LLMClient:       llmClient,
		AgentRegistry:   agentRegistry,
		AgentFactory:    agentFactory,
		Supervisors:     supervisors,
		Leaders:         leaders,
		AllTemplates:    allTemplates,
		Groups:          groups,
		SystemPrompt:    systemPrompt,
		PromptCfg:       promptCfg,
		DefaultModel:    defaultModel,
		Tokenizer:       tok,
		Compactor:       llmCompactor,
		ToolsCfg:        toolsCfg,
		RulesCreated:    rulesCreated,
		TaskRouter:      taskRouter,
		SkillRegistry:   skillReg,
		DockerSandbox:   nil,
		SandboxMounts:   sandboxMounts,
		MemoryManager:   memoryMgr,
		PermanentMemory: permanentMgr,
		PermScheduler:   permScheduler,
		PermNotifyCh:    permNotifyCh,
		PermCancel:      permCancel,
		TodoStore:       todoStore,
		SharedDB:        sharedDB,
		BypassConfirm:   bypassConfirm,
			MCPManager:      mcpMgr,
		}

	// Register config hot-reload callback
	RegisterHotReload(rt, cfg, log, workDir)

	// MCP config hot-reload: reload manager when mcp.json changes.
	if mcpLoader != nil && mcpMgr != nil {
		mcpLoader.OnChange(func(_ mcp.Config) {
			if err := mcpMgr.Reload(context.Background()); err != nil {
				log.Error(logger.CatMCP, "MCP hot-reload failed", "err", err)
			}
		})
	}

	log.Debug(logger.CatApp, "build: total", "duration", time.Since(buildStart).String())
	return rt, nil
}

// BuildLLMClient creates a DeepSeek LLM client from provider configuration.
func BuildLLMClient(provider *config.LLMProvider, log *logger.Logger) (agent.LLMClient, error) {
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

// BuildModelResolver creates a ModelResolver that validates agent model IDs
// against the settings model registry.
func BuildModelResolver(cfg *config.GlobalService) agent.ModelResolver {
	return func(modelID string) (agent.ModelInfo, error) {
		m := cfg.ModelByID(modelID)
		if m == nil {
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

// StartSandbox creates and starts a Docker sandbox, returning it along with
// a configured DockerExecutor.
func StartSandbox(ctx context.Context, mounts []sandbox.Mount, log *logger.Logger) (sandbox.Sandbox, *sandbox.DockerExecutor, error) {
	dockerSandbox, err := sandbox.NewDockerSandbox(mounts)
	if err != nil {
		return nil, nil, fmt.Errorf("docker sandbox init failed: is Docker running? %w", err)
	}
	dockerSandbox.SetLogger(log)
	if err := dockerSandbox.Start(ctx); err != nil {
		return nil, nil, fmt.Errorf("docker sandbox start failed: %w", err)
	}
	log.Info(logger.CatApp, "docker sandbox started",
		"image", "debian:bookworm-slim", "mounts", len(mounts))

	executor := sandbox.NewDockerExecutor(dockerSandbox)
	executor.SetLogger(log)
	return dockerSandbox, executor, nil
}

// InitLogger creates a system-level Logger based on the current configuration.
func InitLogger(workDir string, cfg *config.GlobalService, console bool) (*logger.Logger, error) {
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

// NewAgentID returns a short random ID for an agent instance.
func NewAgentID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("crypto/rand.Read failed: %v", err))
	}
	return "agent-" + hex.EncodeToString(b[:])
}
