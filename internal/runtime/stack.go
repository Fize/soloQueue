// Package runtime provides the shared runtime dependency container (Stack)
// used by both TUI and serve modes of the soloqueue application.
package runtime

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/compactor"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/mcp"
	"github.com/xiaobaitu/soloqueue/internal/mcp/lsp"
	"github.com/xiaobaitu/soloqueue/internal/memory"
	"github.com/xiaobaitu/soloqueue/internal/permanent"
	"github.com/xiaobaitu/soloqueue/internal/prompt"
	"github.com/xiaobaitu/soloqueue/internal/router"
	"github.com/xiaobaitu/soloqueue/internal/sandbox"
	"github.com/xiaobaitu/soloqueue/internal/skill"
	"github.com/xiaobaitu/soloqueue/internal/sqlitedb"
	"github.com/xiaobaitu/soloqueue/internal/todo"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// Stack holds runtime dependencies shared by both modes (TUI / serve),
// initialized once by Build to avoid duplication.
type Stack struct {
	// Configuration-derived fields, protected by CfgMu (for hot-reload).
	CfgMu        sync.RWMutex
	LLMClient    agent.LLMClient
	ToolsCfg     tools.Config
	DefaultModel *config.LLMModel
	Settings     *config.GlobalService // hot-reloaded global config

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
	TaskRouter    *router.Router // Task router classifier (shared by TUI + serve)
	SkillRegistry *skill.SkillRegistry
	DockerSandbox sandbox.Sandbox    // Docker sandbox (L3 tool execution isolation base)
	SandboxMounts []sandbox.Mount    // Sandbox mount list (for deferred startup)
	MemoryManager *memory.Manager    // Short-term memory manager
	PermanentMemory *permanent.Manager // Permanent memory manager
	PermScheduler *permanent.Scheduler
	PermNotifyCh  chan string
	PermCancel    context.CancelFunc // Cancel function for permanent scheduler context
	TodoStore     *todo.Store        // Todo plan/task store
	SharedDB      *sqlitedb.DB       // Shared SQLite connection reused by vectorstore + todo stores
	MCPManager    *mcp.Manager       // MCP server manager
	LSPManager    *lsp.Manager       // Built-in LSP MCP server manager
	HTTPServer    *http.Server       // Embedded HTTP API server (TUI mode)
	HTTPListener  net.Listener       // Listener for the HTTP server

	BypassConfirm bool // --bypass flag: all agents skip tool confirmations

	// compactorInstance stores the concrete type for internal use.
	compactorInstance *compactor.LLMCompactor

	// promptRebuildFuncs holds callbacks to rebuild the L1 system prompt.
	promptRebuildFuncs []func() error
	promptRebuildMu    sync.Mutex
}

// Shutdown gracefully reaps all child Agents managed by L2 Supervisors and destroys the Docker sandbox.
func (s *Stack) Shutdown() {
	for _, sv := range s.Supervisors {
		_ = sv.ReapAll(5 * time.Second)
	}
	if s.DockerSandbox != nil {
		destroyCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.DockerSandbox.Destroy(destroyCtx); err != nil {
			fmt.Fprintf(os.Stderr, "warning: docker sandbox destroy failed: %v\n", err)
		}
	}
	if s.HTTPServer != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.HTTPServer.Shutdown(shutdownCtx)
	}
	if s.MCPManager != nil {
		s.MCPManager.Shutdown()
	}
	if s.LSPManager != nil {
		s.LSPManager.Shutdown()
	}
	// Close the shared SQLite DB last so any flush performed by the stores
	// above (e.g. future scheduled writes) can still reach disk.
	if s.SharedDB != nil {
		if err := s.SharedDB.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: shared sqlite db close failed: %v\n", err)
		}
	}
}

// ReadLLMClient returns the current LLM client (concurrency-safe, reads the latest hot-reloaded config value).
func (s *Stack) ReadLLMClient() agent.LLMClient {
	s.CfgMu.RLock()
	defer s.CfgMu.RUnlock()
	return s.LLMClient
}

// ReadToolsCfg returns the current tools config (concurrency-safe, reads the latest hot-reloaded config value).
func (s *Stack) ReadToolsCfg() tools.Config {
	s.CfgMu.RLock()
	defer s.CfgMu.RUnlock()
	return s.ToolsCfg
}

// ReadDefaultModel returns the current default model (concurrency-safe, reads the latest hot-reloaded config value).
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
// Called by the hot-reload subsystem when settings.toml changes.
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

// L1MCPServers returns the MCP server names available to the L1 orchestrator,
// filtered by the agent.mcpServers whitelist (empty = all enabled).
func (s *Stack) L1MCPServers() []string {
	if s.MCPManager == nil {
		return nil
	}
	cfg := s.MCPManager.Loader().Get()
	allowedSet := s.allowedMCPSet()
	var names []string
	for _, srv := range cfg.Servers {
		if !srv.Enabled {
			continue
		}
		if allowedSet == nil || allowedSet[srv.Name] {
			names = append(names, srv.Name)
		}
	}
	return names
}

// allowedMCPSet returns the set of allowed MCP server names from settings,
// or nil if no whitelist is configured (meaning all enabled servers are allowed).
func (s *Stack) allowedMCPSet() map[string]bool {
	if s.Settings == nil {
		return nil
	}
	allowed := s.Settings.Get().Agent.MCPServers
	if len(allowed) == 0 {
		return nil
	}
	set := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		set[name] = true
	}
	return set
}
