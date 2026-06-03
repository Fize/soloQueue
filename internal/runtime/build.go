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
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/compactor"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/llm/deepseek"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/mcp"
	lspmcp "github.com/xiaobaitu/soloqueue/internal/mcp/lsp"
	"github.com/xiaobaitu/soloqueue/internal/memory"
	"github.com/xiaobaitu/soloqueue/internal/permanent"
	"github.com/xiaobaitu/soloqueue/internal/prompt"
	"github.com/xiaobaitu/soloqueue/internal/router"
	"github.com/xiaobaitu/soloqueue/internal/skill"
	"github.com/xiaobaitu/soloqueue/internal/sqlitedb"
	"github.com/xiaobaitu/soloqueue/internal/teamstore"
	"github.com/xiaobaitu/soloqueue/internal/todo"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// ProfileSetupFn writes the user profile on first startup.
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

	bc := &buildContext{
		workDir:       workDir,
		cfg:           cfg,
		settings:      settings,
		log:           log,
		profileSetup:  profileSetup,
		bypassConfirm: bypassConfirm,
	}

	// Phase 1: Shared DB + TeamStore (must happen first so config layer is DB-backed)
	if err := bc.initSharedDB(); err != nil {
		return nil, err
	}
	bc.teamstore = teamstore.NewStore(filepath.Join(bc.workDir, "groups"), filepath.Join(bc.workDir, "agents"), bc.sharedDB)
	// Migrate direct workspaces to projects table
	if err := bc.teamstore.MigrateWorkspacesToProjects(context.Background()); err != nil {
		bc.log.Warn(logger.CatApp, "failed to migrate team workspaces to projects", "err", err.Error())
	}
	if err := bc.teamstore.EnsureBuiltinTechTeam(context.Background()); err != nil {
		bc.log.Warn(logger.CatApp, "failed to ensure built-in technical team", "err", err.Error())
	}

	// Wire DB to Config and load DB-backed settings
	if err := bc.cfg.SetDB(bc.sharedDB); err != nil {
		return nil, fmt.Errorf("failed to wire DB to config: %w", err)
	}

	// Phase 2: Validate & resolve config (now fully DB-backed)
	if err := bc.resolveConfig(); err != nil {
		return nil, err
	}

	// Phase 2.5: LLM Client (critical path)
	if err := bc.buildLLMClient(); err != nil {
		return nil, err
	}

	// Phase 3: Independent subsystems (no cross-deps)
	bc.buildMCP()
	if err := bc.buildPrompt(); err != nil {
		return nil, err
	}
	if err := bc.buildMemory(); err != nil {
		return nil, err
	}
	bc.buildSkills()

	// Phase 4: Build agent infra (depends on Phase 2+3)
	bc.buildAgentInfra()

	// Phase 5: Assemble Stack
	rt := bc.assembleStack()

	// Phase 6: Post-build hooks (hot-reload wiring)
	bc.registerHotReload(rt)

	log.Debug(logger.CatApp, "build: total", "duration", time.Since(buildStart).String())
	return rt, nil
}

// registerSkillHotReload watches the skills directory and rebuilds the registry on file changes.
func registerSkillHotReload(reg *skill.SkillRegistry, dirs map[string]string, log *logger.Logger) {
	var dirToWatch string
	for _, d := range dirs {
		dirToWatch = d
		break
	}
	if dirToWatch == "" {
		return
	}

	if err := os.MkdirAll(dirToWatch, 0o755); err != nil {
		log.Warn(logger.CatApp, "skills hot-reload: cannot create skills dir", "err", err.Error())
		return
	}

	w, err := fsnotify.NewWatcher()
	if err != nil {
		log.Warn(logger.CatApp, "skills hot-reload: cannot create watcher", "err", err.Error())
		return
	}
	if err := w.Add(dirToWatch); err != nil {
		_ = w.Close()
		log.Warn(logger.CatApp, "skills hot-reload: cannot watch skills dir", "err", err.Error())
		return
	}

	var debounceMu sync.Mutex
	var debounceTimer *time.Timer

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Error(logger.CatApp, "skills hot-reload goroutine panic recovered", "panic", fmt.Sprintf("%v", r))
			}
		}()
		for {
			select {
			case evt, ok := <-w.Events:
				if !ok {
					return
				}
				if !evt.Has(fsnotify.Write) && !evt.Has(fsnotify.Create) && !evt.Has(fsnotify.Rename) && !evt.Has(fsnotify.Remove) {
					continue
				}
				debounceMu.Lock()
				if debounceTimer != nil {
					debounceTimer.Stop()
				}
				debounceTimer = time.AfterFunc(200*time.Millisecond, func() {
					if err := reg.Rebuild(dirs); err != nil {
						log.Warn(logger.CatApp, "skills hot-reload: rebuild failed", "err", err.Error())
					} else {
						log.Info(logger.CatApp, "skills hot-reload completed")
					}
				})
				debounceMu.Unlock()
			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				log.Warn(logger.CatApp, "skills hot-reload watch error", "err", err.Error())
			}
		}
	}()
	log.Debug(logger.CatApp, "skills hot-reload: watching directory", "path", dirToWatch)
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
			ProviderID:      m.ProviderID,
			APIModel:        m.APIModel,
			ContextWindow:   m.ContextWindow,
			Temperature:     m.Generation.Temperature,
			MaxTokens:       m.Generation.MaxTokens,
			ThinkingEnabled: m.Thinking.Enabled,
			ReasoningEffort: m.Thinking.ReasoningEffort,
		}, nil
	}
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

// buildContext holds intermediate build state during initialization.
// Kept unexported as it is only used internally by the Build process.
type buildContext struct {
	workDir       string
	cfg           *config.GlobalService
	settings      config.Settings
	log           *logger.Logger
	profileSetup  ProfileSetupFn
	bypassConfirm bool

	// Resolved config
	provider            *config.LLMProvider
	defaultModel        *config.LLMModel
	fastModelID         string
	fastModelProviderID string

	// Constructed values
	llmClient         agent.LLMClient
	toolsCfg          tools.Config
	mcpLoader         *mcp.Loader
	mcpMgr            *mcp.Manager
	lspMgr            *lspmcp.Manager
	promptCfg         *prompt.PromptConfig
	rulesCreated      bool
	groups            map[string]prompt.GroupFile
	leaders           []prompt.LeaderInfo
	allTemplates      []agent.AgentTemplate
	memoryDir         string
	memoryMgr         *memory.Manager
	sharedDB          *sqlitedb.DB
	permanentMgr      *permanent.Manager
	permScheduler     *permanent.Scheduler
	permCancel        context.CancelFunc
	planDir           string
	todoStore         *todo.Store
	mcpServers        []string
	systemPrompt      string
	agentRegistry     *agent.Registry
	modelResolver     agent.ModelResolver
	skillReg          *skill.SkillRegistry
	skillDirs         map[string]string
	exploreDir        string
	agentFactory      *agent.DefaultFactory
	teamstore         *teamstore.Store
	supervisors       []*agent.Supervisor
	tokenizer         *ctxwin.Tokenizer
	compactorInstance *compactor.LLMCompactor
	taskRouter        *router.Router
}

func (bc *buildContext) resolveConfig() error {
	provider := bc.cfg.DefaultProvider()
	if provider == nil {
		return errors.New("no default provider configured")
	}
	bc.provider = provider

	defaultModel := bc.cfg.DefaultModelByRole("fast")
	if defaultModel == nil {
		return errors.New("no default model configured (fast role)")
	}
	bc.defaultModel = defaultModel

	fastModel := bc.cfg.DefaultModelByRole("fast")
	if fastModel != nil {
		bc.fastModelID = fastModel.ID
		bc.fastModelProviderID = fastModel.ProviderID
	}

	bc.memoryDir = filepath.Join(bc.workDir, "memory")

	planDir, planErr := config.PlanDir()
	if planErr != nil {
		bc.log.Warn(logger.CatApp, "failed to create plan directory", "err", planErr)
	} else {
		bc.planDir = planDir
	}

	return nil
}

func (bc *buildContext) buildLLMClient() error {
	buildStart := time.Now()

	settings := bc.cfg.Get()
	clients := make(map[string]agent.LLMClient)

	for _, prov := range settings.Providers {
		if !prov.Enabled {
			continue
		}

		client, err := BuildLLMClient(&prov, bc.log)
		if err != nil {
			return fmt.Errorf("build llm client for provider %q: %w", prov.ID, err)
		}
		clients[prov.ID] = client
	}

	if len(clients) == 0 {
		return fmt.Errorf("no LLM client could be initialized")
	}

	bc.llmClient = agent.NewRoutingClient(clients)
	bc.log.Debug(logger.CatApp, "build: LLM multi-client ready", "duration", time.Since(buildStart).String(), "count", len(clients))
	return nil
}

func (bc *buildContext) initSharedDB() error {
	sharedDBPath := filepath.Join(bc.workDir, "permanent_memory", "entries.db")
	if err := os.MkdirAll(filepath.Dir(sharedDBPath), 0o755); err != nil {
		return fmt.Errorf("create permanent_memory dir: %w", err)
	}
	sharedDB, err := sqlitedb.Open(sharedDBPath)
	if err != nil {
		return fmt.Errorf("open shared sqlite db: %w", err)
	}
	bc.sharedDB = sharedDB
	return nil
}

func (bc *buildContext) assembleStack() *Stack {
	return &Stack{
		Settings:          bc.cfg,
		LLMClient:         bc.llmClient,
		Log:               bc.log,
		AgentRegistry:     bc.agentRegistry,
		AgentFactory:      bc.agentFactory,
		Supervisors:       bc.supervisors,
		Leaders:           bc.leaders,
		AllTemplates:      bc.allTemplates,
		Groups:            bc.groups,
		SystemPrompt:      bc.systemPrompt,
		PromptCfg:         bc.promptCfg,
		DefaultModel:      bc.defaultModel,
		Tokenizer:         bc.tokenizer,
		Compactor:         bc.compactorInstance,
		ToolsCfg:          bc.toolsCfg,
		RulesCreated:      bc.rulesCreated,
		TaskRouter:        bc.taskRouter,
		SkillRegistry:     bc.skillReg,
		MemoryManager:     bc.memoryMgr,
		PermanentMemory:   bc.permanentMgr,
		PermScheduler:     bc.permScheduler,
		PermCancel:        bc.permCancel,
		TodoStore:         bc.todoStore,
		SharedDB:          bc.sharedDB,
		BypassConfirm:     bc.bypassConfirm,
		MCPManager:        bc.mcpMgr,
		LSPManager:        bc.lspMgr,
		TeamStore:         bc.teamstore,
		compactorInstance: bc.compactorInstance,
	}
}

func (bc *buildContext) registerHotReload(rt *Stack) {
	if bc.mcpLoader != nil && bc.mcpMgr != nil {
		bc.mcpLoader.OnChange(func(_ mcp.Config) {
			if err := bc.mcpMgr.Reload(context.Background()); err != nil {
				bc.log.Error(logger.CatMCP, "MCP hot-reload failed", "err", err.Error())
			}
		})
	}

	registerSkillHotReload(bc.skillReg, bc.skillDirs, bc.log)
	
	groupsDir := filepath.Join(bc.workDir, "groups")
	agentsDir := filepath.Join(bc.workDir, "agents")
	registerPromptHotReload(rt, bc.log, groupsDir, agentsDir)
}

// registerPromptHotReload watches the roles, groups, and agents directories and rebuilds the system prompt when soul.md, rules.md or team/agent markdown files change.
func registerPromptHotReload(rt *Stack, log *logger.Logger, groupsDir, agentsDir string) {
	if rt.PromptCfg == nil {
		return
	}
	rolesDir := rt.PromptCfg.RolesDir
	if rolesDir == "" {
		return
	}

	if err := os.MkdirAll(rolesDir, 0o755); err != nil {
		log.Warn(logger.CatApp, "prompt hot-reload: cannot create roles dir", "err", err.Error())
		return
	}

	w, err := fsnotify.NewWatcher()
	if err != nil {
		log.Warn(logger.CatApp, "prompt hot-reload: cannot create watcher", "err", err.Error())
		return
	}

	if err := w.Add(rolesDir); err != nil {
		_ = w.Close()
		log.Warn(logger.CatApp, "prompt hot-reload: cannot watch roles dir", "err", err.Error())
		return
	}

	if groupsDir != "" {
		_ = os.MkdirAll(groupsDir, 0o755)
		if err := w.Add(groupsDir); err != nil {
			log.Warn(logger.CatApp, "prompt hot-reload: cannot watch groups dir", "err", err.Error())
		}
	}

	if agentsDir != "" {
		_ = os.MkdirAll(agentsDir, 0o755)
		if err := w.Add(agentsDir); err != nil {
			log.Warn(logger.CatApp, "prompt hot-reload: cannot watch agents dir", "err", err.Error())
		}
	}

	var debounceMu sync.Mutex
	var debounceTimer *time.Timer

	rt.promptWatcherClose = func() {
		debounceMu.Lock()
		if debounceTimer != nil {
			debounceTimer.Stop()
		}
		debounceMu.Unlock()
		if err := w.Close(); err != nil {
			log.Warn(logger.CatApp, "prompt hot-reload: failed to close watcher", "err", err.Error())
		}
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Error(logger.CatApp, "prompt hot-reload goroutine panic recovered", "panic", fmt.Sprintf("%v", r))
			}
		}()
		for {
			select {
			case evt, ok := <-w.Events:
				if !ok {
					return
				}
				if !evt.Has(fsnotify.Write) && !evt.Has(fsnotify.Create) && !evt.Has(fsnotify.Rename) && !evt.Has(fsnotify.Remove) {
					continue
				}

				filename := filepath.Base(evt.Name)
				dir := filepath.Dir(evt.Name)

				absDir, _ := filepath.Abs(dir)
				absRoles, _ := filepath.Abs(rolesDir)
				absGroups, _ := filepath.Abs(groupsDir)
				absAgents, _ := filepath.Abs(agentsDir)

				isRolesFile := (absDir == absRoles) && (filename == "soul.md" || filename == "rules.md")
				isGroupsFile := (absDir == absGroups) && strings.HasSuffix(filename, ".md")
				isAgentsFile := (absDir == absAgents) && strings.HasSuffix(filename, ".md")

				if !isRolesFile && !isGroupsFile && !isAgentsFile {
					continue
				}

				debounceMu.Lock()
				if debounceTimer != nil {
					debounceTimer.Stop()
				}
				debounceTimer = time.AfterFunc(200*time.Millisecond, func() {
					if err := rt.RebuildPrompt(); err != nil {
						log.Warn(logger.CatApp, "prompt hot-reload: rebuild failed", "err", err.Error())
					} else {
						log.Info(logger.CatApp, "prompt hot-reload completed", "file", filename)
					}
				})
				debounceMu.Unlock()
			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				log.Warn(logger.CatApp, "prompt hot-reload watch error", "err", err.Error())
			}
		}
	}()
	log.Debug(logger.CatApp, "prompt hot-reload: watching directories", "roles", rolesDir, "groups", groupsDir, "agents", agentsDir)
}


