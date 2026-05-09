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
	HTTPServer    *http.Server       // Embedded HTTP API server (TUI mode)
	HTTPListener  net.Listener       // Listener for the HTTP server

	// compactorInstance stores the concrete type for internal use.
	compactorInstance *compactor.LLMCompactor
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

// SetSystemPrompt updates the compiled system prompt (concurrency-safe).
func (s *Stack) SetSystemPrompt(prompt string) {
	s.CfgMu.Lock()
	defer s.CfgMu.Unlock()
	s.SystemPrompt = prompt
}
