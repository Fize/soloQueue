// Package runtime provides the shared runtime dependency container (Stack).
package runtime

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/memory/conversation"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/agenttools/mcp/lsp"
	"github.com/xiaobaitu/soloqueue/internal/agenttools/mcp"
	"github.com/xiaobaitu/soloqueue/internal/memory/engine"
	"github.com/xiaobaitu/soloqueue/internal/memory/engine/embedding"
	"github.com/xiaobaitu/soloqueue/internal/memory/engine/vectorstore"
	"github.com/xiaobaitu/soloqueue/internal/prompt"
	"github.com/xiaobaitu/soloqueue/internal/router"
	"github.com/xiaobaitu/soloqueue/internal/simulation"
	"github.com/xiaobaitu/soloqueue/internal/agenttools/skill"
	"github.com/xiaobaitu/soloqueue/internal/infra/db"
	"github.com/xiaobaitu/soloqueue/internal/tasktype"
	"github.com/xiaobaitu/soloqueue/internal/team/store"
	"github.com/xiaobaitu/soloqueue/internal/infra/telemetry"
	"github.com/xiaobaitu/soloqueue/internal/agenttools/tools"
	"github.com/xiaobaitu/soloqueue/internal/workflow"
)

// Stack holds runtime dependencies, initialized once by Build to avoid duplication.
type Stack struct {
	CfgMu          sync.RWMutex
	LLMClient      agent.LLMClient
	ToolsCfg       tools.Config
	RuntimeManager *tools.RuntimeManager
	DefaultModel   *config.LLMModel
	Settings       *config.GlobalService
	Log            *logger.Logger

	AgentRegistry *agent.Registry
	AgentFactory  *agent.DefaultFactory
	Supervisors   []*agent.Supervisor
	Leaders       []prompt.LeaderInfo
	AllTemplates  []agent.AgentTemplate
	Groups        map[string]prompt.GroupFile
	SystemPrompt  string
	PromptCfg     *prompt.PromptConfig
	Tokenizer     *ctxwin.Tokenizer
	Compactor     ctxwin.Compactor // context compression engine
	RulesCreated  bool
	TaskRouter    *router.Router
	SkillRegistry *skill.SkillRegistry
	MemoryManager *conversation.Manager // Short-term memory manager
	MemoryEngine  *engine.Engine     // Memory engine (BM25 + KG + optional vector)
	SharedDB      *db.DB             // Shared SQLite connection
	MCPManager    *mcp.Manager             // MCP server manager
	LSPManager    *lsp.Manager             // Built-in LSP MCP server manager

	BypassConfirm bool // --bypass flag: all agents skip tool confirmations

	TeamStore *store.Store // DB-backed team/agent store (nil = disabled)

	// L1Channels and L1NotifyChannel hold the L1 main agent's channel bindings,
	// loaded from ~/.soloqueue/agents/main.md at startup.
	L1Channels      map[string]string
	L1NotifyChannel string

	SimulationEngine *simulation.SimulationEngine // multi-agent simulation engine (nil = disabled)

	// Workflow support (v1)
	WorkflowStore  *workflow.Store
	WorkflowEngine *workflow.Engine
	WorkflowRuns   *workflow.RunManager

	// compactorInstance stores the concrete type for internal use.
	compactorInstance *LLMCompactor

	// promptRebuildFuncs holds callbacks to rebuild the L1 system prompt.
	promptRebuildFuncs []func() error
	promptRebuildMu    sync.Mutex
	promptWatcherClose func()
	skillsSyncCancel   func()
}

// ReloadFromTeamStore reloads groups, leaders, and templates from the DB-backed
// team store. If TeamStore is nil, this is a no-op.
func (s *Stack) ReloadFromTeamStore() error {
	if s.TeamStore == nil {
		return nil
	}
	dbGroups, dbLeaders, dbTemplates, err := loadFromTeamStore(s.TeamStore)
	if err != nil {
		return err
	}
	if s.AgentFactory != nil {
		s.AgentFactory.ReplaceTeamCatalog(dbTemplates, dbGroups)
	}
	s.CfgMu.Lock()
	s.Groups = dbGroups
	s.Leaders = dbLeaders
	s.AllTemplates = dbTemplates
	s.CfgMu.Unlock()
	return nil
}

// ReloadAgentTemplates re-reads agent templates and groups from disk, updates
// the factory cache, and propagates changes to running L2 supervisors.
// Called by the hot-reload watcher when agents/ or groups/ files change.
func (s *Stack) ReloadAgentTemplates(log *logger.Logger, agentsDir, groupsDir string) error {
	if agentsDir == "" {
		return nil
	}
	// 1. Re-read templates and groups from disk
	newTemplates, err := agent.LoadAgentTemplates(agentsDir)
	if err != nil {
		return fmt.Errorf("reload agent templates: %w", err)
	}
	var newGroups map[string]prompt.GroupFile
	if groupsDir != "" {
		newGroups, err = prompt.LoadGroups(groupsDir)
		if err != nil {
			log.Warn(logger.CatApp, "reload agent templates: reload groups failed, using cached", "err", err.Error())
		}
	}
	if newGroups == nil {
		s.CfgMu.RLock()
		newGroups = s.Groups
		s.CfgMu.RUnlock()
	}

	// 2. Update factory cache (ensures future Create() calls use fresh templates)
	if s.AgentFactory != nil {
		s.AgentFactory.ReplaceTeamCatalog(newTemplates, newGroups)
	}

	// 3. Update stack's own cache
	s.CfgMu.Lock()
	s.AllTemplates = newTemplates
	if newGroups != nil {
		s.Groups = newGroups
	}
	supervisors := make([]*agent.Supervisor, len(s.Supervisors))
	copy(supervisors, s.Supervisors)
	s.CfgMu.Unlock()

	// 4. Build leader index from templates for lookup
	leaders := make(map[string]agent.AgentTemplate)
	for _, tmpl := range newTemplates {
		if tmpl.IsLeader {
			leaders[tmpl.ID] = tmpl
		}
	}

	// 5. Propagate to running supervisors
	for _, sv := range supervisors {
		if sv.Agent() == nil {
			continue
		}
		// Check if this supervisor's leader template was updated
		leaderID := sv.Agent().Def.ID
		newTmpl, ok := leaders[leaderID]
		if !ok {
			continue
		}
		// Rebuild the L2 system prompt from the updated template
		newPrompt, err := s.AgentFactory.RebuildLeaderPrompt(newTmpl, sv.Agent().WorkDir)
		if err != nil {
			log.Warn(logger.CatApp, "reload agent templates: rebuild leader prompt failed",
				"leader", leaderID, "err", err.Error())
			continue
		}
		sv.UpdateLeaderPrompt(newPrompt, newTemplates)
		log.Info(logger.CatApp, "reload agent templates: updated leader prompt",
			"leader", leaderID, "group", sv.Group())
	}
	return nil
}

// Shutdown gracefully reaps all child Agents managed by L2 Supervisors.
func (s *Stack) Shutdown() {
	if s.promptWatcherClose != nil {
		s.promptWatcherClose()
	}
	if s.skillsSyncCancel != nil {
		s.skillsSyncCancel()
	}
	for _, sv := range s.Supervisors {
		_ = sv.ReapAll(5 * time.Second)
	}
	if s.MCPManager != nil {
		s.MCPManager.Shutdown()
	}
	if s.LSPManager != nil {
		s.LSPManager.Shutdown()
	}
	if s.RuntimeManager != nil {
		if err := s.RuntimeManager.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: sandbox runtime shutdown failed: %v\n", err)
		}
	}
	// Close the shared SQLite DB last so any flush performed by the stores
	// above (e.g. future scheduled writes) can still reach disk.
	if s.SharedDB != nil {
		if err := s.SharedDB.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: shared sqlite db close failed: %v\n", err)
		}
	}
}

// ReadLLMClient returns the current LLM client (concurrency-safe).
func (s *Stack) ReadLLMClient() agent.LLMClient {
	s.CfgMu.RLock()
	defer s.CfgMu.RUnlock()
	return s.LLMClient
}

// ReadToolsCfg returns the current tools config (concurrency-safe).
func (s *Stack) ReadToolsCfg() tools.Config {
	s.CfgMu.RLock()
	defer s.CfgMu.RUnlock()
	return s.ToolsCfg
}

// SetToolsCfg updates the tools config (concurrency-safe).
func (s *Stack) SetToolsCfg(cfg tools.Config) {
	s.CfgMu.Lock()
	defer s.CfgMu.Unlock()
	s.ToolsCfg = cfg
}

// ReadDefaultModel returns the current default model (concurrency-safe).
func (s *Stack) ReadDefaultModel() *config.LLMModel {
	s.CfgMu.RLock()
	defer s.CfgMu.RUnlock()
	return s.DefaultModel
}

// AddSupervisor appends a supervisor to the stack's list (concurrency-safe via CfgMu).
func (s *Stack) AddSupervisor(sv *agent.Supervisor) {
	s.CfgMu.Lock()
	defer s.CfgMu.Unlock()
	s.Supervisors = append(s.Supervisors, sv)
}

// RemoveSupervisor removes a supervisor from the stack's list (concurrency-safe via CfgMu).
// Uses pointer identity for comparison.
func (s *Stack) RemoveSupervisor(sv *agent.Supervisor) {
	s.CfgMu.Lock()
	defer s.CfgMu.Unlock()
	for i, v := range s.Supervisors {
		if v == sv {
			s.Supervisors = append(s.Supervisors[:i], s.Supervisors[i+1:]...)
			return
		}
	}
}

// SetSystemPrompt updates the compiled system prompt (concurrency-safe).
func (s *Stack) SetSystemPrompt(prompt string) {
	s.CfgMu.Lock()
	defer s.CfgMu.Unlock()
	s.SystemPrompt = prompt
}

// OnPromptRebuild registers a callback to rebuild the L1 system prompt.
func (s *Stack) OnPromptRebuild(fn func() error) {
	s.promptRebuildMu.Lock()
	defer s.promptRebuildMu.Unlock()
	s.promptRebuildFuncs = append(s.promptRebuildFuncs, fn)
}

// RebuildPrompt executes all registered prompt rebuild callbacks.
func (s *Stack) RebuildPrompt() error {
	s.promptRebuildMu.Lock()
	fns := make([]func() error, len(s.promptRebuildFuncs))
	copy(fns, s.promptRebuildFuncs)
	s.promptRebuildMu.Unlock()
	for _, fn := range fns {
		if err := fn(); err != nil {
			return err
		}
	}
	return nil
}

// L1MCPServers returns the MCP server names available to the L1 orchestrator.
//
// The returned list includes both external MCP servers (from mcp.json) and
// built-in MCP servers (e.g. "builtin-lsp"), each filtered independently by
// the corresponding whitelist in agent config:
//   - nil = not configured → load all available servers of that type
//   - [] = explicit empty → load nothing
//   - ["name"] = whitelist → only load named servers
func (s *Stack) L1MCPServers() []string {
	if s.Settings == nil {
		return nil
	}
	cfg := s.Settings.Get()
	return gatherMCPServerNames(s.MCPManager, s.LSPManager, cfg.Agent.ExternalMCPServers, cfg.Agent.BuiltinMCPServers)
}

// externalMCPSet returns the whitelist for external MCP servers.
// nil = not configured (load all external servers).
// non-nil = explicitly configured (empty map = load none, non-empty = whitelist).

// builtinMCPSet returns the whitelist for built-in MCP servers.
// nil = not configured (load all built-in servers).
// non-nil = explicitly configured (empty map = load none, non-empty = whitelist).

// OnConfigChange rebuilds the LLM client and updates the stack's cached configurations
// dynamically when DB settings change.
func (s *Stack) OnConfigChange() error {
	s.CfgMu.Lock()
	defer s.CfgMu.Unlock()

	settings := s.Settings.Get()
	runtimeChanged := false
	if s.RuntimeManager != nil {
		nextRuntime := settings.Sandbox.RuntimeType()
		runtimeChanged = s.RuntimeManager.Desired() != nextRuntime ||
			s.RuntimeManager.NetworkEnabled() != settings.Sandbox.NetworkEnabled
		if err := s.RuntimeManager.SetDesired(nextRuntime); err != nil {
			return fmt.Errorf("update sandbox runtime: %w", err)
		}
		s.RuntimeManager.SetNetworkEnabled(settings.Sandbox.NetworkEnabled)
	}
	if runtimeChanged && s.LSPManager != nil {
		if err := s.LSPManager.Restart(context.Background()); err != nil {
			return fmt.Errorf("restart LSP runtime: %w", err)
		}
	}
	clients := make(map[string]agent.LLMClient)

	for _, prov := range settings.Providers {
		if !prov.Enabled {
			continue
		}
		client, err := BuildLLMClient(&prov, s.Log)
		if err != nil {
			return fmt.Errorf("failed to rebuild LLM client for provider %q: %w", prov.ID, err)
		}
		clients[prov.ID] = telemetry.NewTelemetryClient(client, s.SharedDB)
	}

	if len(clients) == 0 {
		return fmt.Errorf("no LLM client could be initialized on config change")
	}

	// Update existing routing client if it exists, or create a new one
	if rc, ok := s.LLMClient.(*agent.RoutingClient); ok {
		rc.UpdateClients(clients)
	} else {
		// Fallback (e.g. if LLMClient was a FakeLLM (agenttest) or other mock in tests)
		s.LLMClient = agent.NewRoutingClient(clients)
	}

	generalModel := s.Settings.DefaultModelForTask(tasktype.General)
	if generalModel != nil {
		s.DefaultModel = generalModel
		if s.AgentFactory != nil {
			s.AgentFactory.UpdateDefaultModelID(generalModel.ID)
		}
	}
	if classifierModel := s.Settings.DefaultClassifierModel(); classifierModel != nil && s.TaskRouter != nil {
		effectiveModel := classifierModel.APIModel
		if effectiveModel == "" {
			effectiveModel = classifierModel.ID
		}
		s.TaskRouter.UpdateClassifierModel(classifierModel.ProviderID, effectiveModel)
	}

	// Update tools config dynamically, preserving runtime dependencies and fields
	newToolsCfg := settings.Tools.ToToolsConfigWithSandbox(settings.Sandbox)
	newToolsCfg.MemoryEngine = s.ToolsCfg.MemoryEngine
	newToolsCfg.PlanDir = s.ToolsCfg.PlanDir
	newToolsCfg.CronStore = s.ToolsCfg.CronStore
	newToolsCfg.CronScheduler = s.ToolsCfg.CronScheduler
	newToolsCfg.Logger = s.ToolsCfg.Logger
	newToolsCfg.RuntimeManager = s.RuntimeManager
	newToolsCfg.WorkDir = s.ToolsCfg.WorkDir

	s.ToolsCfg = newToolsCfg
	if s.AgentFactory != nil {
		s.AgentFactory.SetToolsConfig(newToolsCfg)
	}

	s.rebuildMemoryEngine(settings.Embedding)

	if s.AgentFactory != nil {
		s.AgentFactory.UpdateLLM(s.LLMClient)
	}

	s.Log.Info(logger.CatConfig, "LLM provider, default model, and tools configurations hot-reloaded successfully from DB")
	return nil
}

func (s *Stack) rebuildMemoryEngine(cfg config.EmbeddingConfig) {
	if s.SharedDB == nil {
		return
	}

	var emb embedding.Embedder
	var vecStore vectorstore.VectorStore

	switch cfg.Provider {
	case "openai":
		embModel := s.Settings.DefaultEmbeddingModel()
		if embModel != nil && embModel.Enabled {
			embProvider := s.Settings.EmbeddingProviderByID(embModel.ProviderID)
			if embProvider != nil && embProvider.Enabled {
				apiKey := embProvider.APIKey
				if apiKey == "" {
					apiKey = os.Getenv(embProvider.APIKeyEnv)
				}
				client, err := embedding.NewOpenAI(embedding.OpenAIConfig{
					BaseURL:   embProvider.BaseURL,
					APIKey:    apiKey,
					ModelID:   embModel.ID,
					Dimension: embModel.Dimension,
				})
				if err != nil {
					s.Log.Warn(logger.CatConfig, "hot-reload: failed to create OpenAI embedder, engine runs without vectors",
						"err", err)
				} else {
					emb = client
				}
			}
		}
	case "none", "":
	default:
		s.Log.Warn(logger.CatConfig, "hot-reload: unknown embedding provider, falling back to none",
			"provider", cfg.Provider)
	}

	if emb != nil {
		vecStore = vectorstore.NewSQLiteStoreFromDB(s.SharedDB.DB, &s.SharedDB.WMu,
			vectorstore.WithTableName("mem_vec"),
			vectorstore.WithLogger(s.Log),
		)
	}

	newEngine := engine.New(s.SharedDB.DB, &s.SharedDB.WMu, emb, vecStore, s.Log)
	s.MemoryEngine = newEngine
	s.ToolsCfg.MemoryEngine = newEngine

	if s.AgentFactory != nil {
		s.AgentFactory.SetToolsConfig(s.ToolsCfg)
	}

	s.Log.Info(logger.CatConfig, "memory engine hot-reloaded",
		"provider", cfg.Provider,
		"has_vector", emb != nil)
}
