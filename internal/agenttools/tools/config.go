// Package tools collects the built-in business tools available to an agent.
//
// Design principles:
//   - All tools are "configuration-driven value objects": main.go creates a Config
//     at startup and calls Build(cfg), returning a []Tool that can be passed directly
//     to agent.WithTools.
//   - Business tools use a flat layout (one .go file per tool plus a *_test.go file).
//   - Shared configuration and helpers are centralized in exec.go and helpers.go.
//   - Tool Execute always returns a JSON string (easy for the LLM to parse) or a structured error;
//     the agent layer formats errors as "error: ..." and sends them back to the LLM without
//     interrupting the loop.
//
// Typical usage:
//
//	cfg := tools.Config{
//	    MaxFileSize:  1 << 20,
//	    MaxWriteSize: 1 << 20,
//	}
//	all := tools.Build(cfg)
//	a := agent.NewAgent(def, llm, log, agent.WithTools(all...))
package tools

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/cron"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/memory/engine"
	"github.com/xiaobaitu/soloqueue/internal/team/store"
)

func memoryToolError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	switch {
	case errors.Is(err, engine.ErrMemoryOwnerInvalid):
		return fmt.Errorf("memory_owner_invalid")
	case errors.Is(err, engine.ErrMemoryAccessDenied):
		return fmt.Errorf("memory_access_denied")
	default:
		return fmt.Errorf("memory_unavailable")
	}
}

// ─── Config ──────────────────────────────────────────────────────────────────

// Config is the shared configuration for all built-in tools.
//
// Zero-value semantics: Build can still be called when all fields are left at zero value, but
// filesystem/network-related tools will be handled with the strictest behavior during Execute.
// Production code should populate this explicitly in main.go.
type Config struct {
	// ── Read limits ────────────────────────────────────────────────────

	// MaxFileSize is the maximum size of a single file for Read (in bytes).
	MaxFileSize int64

	// MaxMatches is the maximum number of grep matches; exceeding it truncates and returns truncated=true.
	MaxMatches int

	// MaxLineLen is the maximum line length for grep; exceeding it truncates the line.
	MaxLineLen int

	// MaxGlobItems is the maximum number of glob matches to return.
	MaxGlobItems int

	// ── Write limits ───────────────────────────────────────────────────

	// MaxWriteSize is the maximum size of a single write for Write / Edit / MultiEdit.
	MaxWriteSize int64

	// MaxMultiWriteBytes is the maximum total size of all Content values for MultiWrite.
	MaxMultiWriteBytes int64

	// MaxMultiWriteFiles is the maximum number of files in a single MultiWrite call.
	MaxMultiWriteFiles int

	// MaxReplaceEdits is the maximum number of edits in a single MultiEdit call.
	MaxReplaceEdits int

	// ── WebFetch ─────────────────────────────────────────────────────

	// HTTPAllowedHosts, if non-empty, only allows URLs whose host matches one of them.
	HTTPAllowedHosts []string

	// HTTPMaxBody is the maximum response body size (in bytes).
	HTTPMaxBody int64

	// HTTPTimeout is the HTTP request timeout.
	HTTPTimeout time.Duration

	// HTTPBlockPrivate controls whether to block private, loopback, or link-local addresses (recommended true by default).
	HTTPBlockPrivate bool

	// ── Bash ─────────────────────────────────────────────────────────

	// ShellBlockRegexes are command blocklist regexes; any match is rejected.
	ShellBlockRegexes []string

	// ShellConfirmRegexes are command confirmation regexes; any match requires user confirmation.
	ShellConfirmRegexes []string

	// ShellMaxOutput is the maximum output size for shell execution; stdout/stderr are truncated independently.
	ShellMaxOutput int64

	// ── WebSearch ─────────────────────────────────────────────
	// WebSearchTimeout is the web search request timeout.
	WebSearchTimeout time.Duration

	// TavilyAPIKey, when non-empty, routes WebSearch through the Tavily API instead of DuckDuckGo Lite.
	TavilyAPIKey string

	// ── Logging ──────────────────────────────────────────────────
	// Logger is an optional logger instance (nil disables logging).
	Logger *logger.Logger

	// ── Executor ──────────────────────────────────────────────────────
	// Executor is the direct execution engine for process, filesystem, and network ops.
	Executor *Executor

	// ── Work directory ────────────────────────────────────────────
	// WorkDir is the agent's working directory for tool execution.
	// When non-empty, tools like Bash use this as the default working
	// directory for commands. Set by the factory during agent creation.
	WorkDir string

	// ── Plan Directory ─────────────────────────────────────────────
	// PlanDir is the absolute path to the plan directory (~/.soloqueue/plan/).
	// When non-empty, writeFileImpl will auto-create intermediate directories
	// under this prefix (instead of returning ErrParentDirMissing).
	// Set by main.go via config.PlanDir().
	PlanDir string

	// ── Long-term memory ──────────────────────────────────────────────
	// MemoryEngine is privileged runtime configuration used only to create
	// server-bound capabilities and administrative operations. BuildBase strips
	// it before constructing agent tools.
	MemoryEngine *engine.Engine

	// MemoryAccess is an immutable owner-bound capability. Agent-facing
	// memory tools are registered only when this value is non-nil.
	MemoryAccess engine.Access

	// ── Team store ──────────────────────────────────────────────────────
	// TeamStore is the project/team/agent persistence store.
	// When non-nil, resolve_project and related tools are registered.
	TeamStore *store.Store

	// ── Cron tasks ───────────────────────────────────────────────────
	CronStore     *cron.DBStore
	CronScheduler *cron.Scheduler
	CronScope     CronAccessScope

	// ── Image generation ─────────────────────────────────────
	// ImageModels lists image generation models. If any model has Enabled set, the ImageGenerate tool is registered.
	ImageModels []ImgModelCfg
}

// ImgModelCfg contains runtime image model configuration.
type ImgModelCfg struct {
	ID           string
	Name         string
	Provider     string
	SecretId     string
	SecretIdEnv  string
	SecretKey    string
	SecretKeyEnv string
	APIKey       string
	APIKeyEnv    string
	APIBaseHost  string
	Region       string
	IsDefault    bool
	Enabled      bool
}

// ─── Build ────────────────────────────────────────────────────────────────

// ensureExecutor ensures that Executor is initialized and logged.
func ensureExecutor(cfg *Config) {
	if cfg.Executor == nil {
		cfg.Executor = NewExecutor()
	}
	if cfg.Logger != nil {
		cfg.Executor.SetLogger(cfg.Logger)
	}
}

// Build returns all tools enabled for the current Config.
//
// The returned slice preserves declaration order (useful for debugging).
func Build(cfg Config) []Tool {
	base := BuildBase(cfg)
	return append(base, BuildMemory(cfg, cfg.MemoryAccess)...)
}

// BuildBase returns built-in tools without durable-memory capabilities.
func BuildBase(cfg Config) []Tool {
	// Base tools must not retain either the privileged engine or a bound
	// capability. This keeps L3 and skill-fork tool values memoryless.
	cfg.MemoryEngine = nil
	cfg.MemoryAccess = nil
	ensureExecutor(&cfg)
	tools := []Tool{
		newFileReadTool(cfg),
		newGrepTool(cfg),
		newGlobTool(cfg),
		newWriteFileTool(cfg),
		newReplaceTool(cfg),
		newHTTPFetchTool(cfg),
		newShellExecTool(cfg),
		newWebSearchTool(cfg),
		newSendFileTool(cfg),
	}
	if cfg.TeamStore != nil {
		tools = append(tools, newResolveProjectTool(cfg))
	}

	if cfg.CronStore != nil && cfg.CronScheduler != nil && cfg.CronScope.Enabled() {
		tools = append(tools, newManageCronTool(cfg))
	}
	hasImgModel := false
	for _, m := range cfg.ImageModels {
		if m.Enabled {
			hasImgModel = true
			break
		}
	}
	if hasImgModel {
		tools = append(tools, newImageTool(cfg))
	}
	return tools
}

// BuildMemory returns durable-memory tools for an already bound capability.
func BuildMemory(cfg Config, access engine.Access) []Tool {
	if access == nil {
		return nil
	}
	cfg.MemoryEngine = nil
	cfg.MemoryAccess = access
	return []Tool{newRememberTool(cfg), newRecallMemoryTool(cfg)}
}

// ─── Default Config ─────────────────────────────────────────────────────

// DefaultConfig returns a set of recommended defaults that main.go can override.
func DefaultConfig() Config {
	return Config{
		MaxFileSize:  1 << 20,
		MaxMatches:   100,
		MaxLineLen:   500,
		MaxGlobItems: 1000,

		MaxWriteSize:       1 << 20,
		MaxMultiWriteBytes: 10 << 20,
		MaxMultiWriteFiles: 50,
		MaxReplaceEdits:    50,

		HTTPMaxBody:      5 << 20,
		HTTPTimeout:      10 * time.Minute,
		HTTPBlockPrivate: true,

		ShellMaxOutput: 256 << 10,

		WebSearchTimeout: 10 * time.Minute,
	}
}
