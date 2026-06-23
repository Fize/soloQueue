// Package tools collects the built-in business tools available to an agent.
//
// Design principles:
//   - All tools are "configuration-driven value objects": main.go creates a Config
//     at startup and calls Build(cfg), returning a []Tool that can be passed directly
//     to agent.WithTools.
//   - The tools use a flat layout (one .go file per tool plus a *_test.go file); no subpackages.
//     If the tool count exceeds ~30 or a domain-based split becomes more meaningful,
//     a refactor can be done then.
//   - Shared configuration and helpers (sandbox checks, atomic write) are centralized in
//     exec.go and helpers.go.
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
	"time"

	"github.com/xiaobaitu/soloqueue/internal/cron"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/memoryengine"
)

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

	// ── Logging ──────────────────────────────────────────────────
	// Logger is an optional logger instance (nil disables logging).
	Logger *logger.Logger

	// ── Sandbox executor ──────────────────────────────────────────────
	// Sandbox is the execution backend for all tools; it handles all host-system interactions.
	// If nil, Build automatically injects NewSandbox (useful for tests and local development).
	Sandbox *Sandbox

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
	// MemoryEngine is the long-term memory engine (nil means disabled).
	// Remember / RecallMemory and related memory tools only take effect when non-nil.
	MemoryEngine *memoryengine.Engine
	// ── Cron tasks ───────────────────────────────────────────────────
	CronStore     *cron.DBStore
	CronScheduler *cron.Scheduler

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

// ensureSandbox ensures cfg.Sandbox is never nil by injecting the default implementation.
func ensureSandbox(cfg *Config) {
	if cfg.Sandbox == nil {
		cfg.Sandbox = NewSandbox()
	}
}

// Build returns all tools enabled for the current Config.
//
// The returned slice preserves declaration order (useful for debugging).
// If cfg.Sandbox is nil, it is injected automatically.
func Build(cfg Config) []Tool {
	ensureSandbox(&cfg)
	tools := []Tool{
		newFileReadTool(cfg),
		newGrepTool(cfg),
		newGlobTool(cfg),
		newWriteFileTool(cfg),
		newReplaceTool(cfg),
		newMultiReplaceTool(cfg),
		newMultiWriteTool(cfg),
		newHTTPFetchTool(cfg),
		newShellExecTool(cfg),
		newWebSearchTool(cfg),
		newRememberTool(cfg),
		newRecallMemoryTool(cfg),
		newSendFileTool(cfg),
	}
	if cfg.MemoryEngine != nil {
		tools = append(tools,
			newKGIndexTool(cfg),
			newRecallEntityTool(cfg),
			newConnectEntitiesTool(cfg),
			newMemoryTimelineTool(cfg),
			newConsolidateMemoriesTool(cfg),
		)
	}
	if cfg.CronStore != nil && cfg.CronScheduler != nil {
		tools = append(tools,
			newScheduleTaskTool(cfg),
			newModifyScheduledTaskTool(cfg),
			newDeleteScheduledTaskTool(cfg),
		)
	}
	hasImgModel := false
	for _, m := range cfg.ImageModels {
		if m.Enabled {
			hasImgModel = true
			break
		}
	}
	if hasImgModel {
		tools = append(tools, newImageGenTool(cfg), newImageEditTool(cfg))
	}
	return tools
}

// ─── Default Config ─────────────────────────────────────────────────────

// DefaultConfig returns a set of recommended defaults that main.go can override.
//
// The values come from plan §5.3 and are safe for most local-development scenarios.
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
