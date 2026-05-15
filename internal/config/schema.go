package config

import (
	"os"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/qqbot"
)

// ─── Agent (L1 orchestrator) ──────────────────────────────────────────────────

// AgentConfig holds per-agent-type settings. Currently only the L1 orchestrator.
type AgentConfig struct {
	// BuiltinMCPServers is the L1 orchestrator's built-in MCP server whitelist.
	// nil (unset) = load all built-in servers.
	// [] = explicit empty = load nothing.
	// ["builtin-lsp"] = whitelist, only load named servers.
	BuiltinMCPServers []string `json:"builtinMcpServers" toml:"builtin_mcp_servers,omitempty"`

	// ExternalMCPServers is the L1 orchestrator's external MCP server whitelist.
	// nil (unset) = load all external servers.
	// [] = explicit empty = load nothing.
	// ["server1"] = whitelist, only load named servers.
	ExternalMCPServers []string `json:"externalMcpServers" toml:"external_mcp_servers,omitempty"`
}

// ─── Top-level Settings ───────────────────────────────────────────────────────

// Settings is the complete structure for global configuration
// Corresponds to ~/.soloqueue/settings.toml
//
// Note: UI-specific states (theme / language, etc.) are persisted by the frontend itself (localStorage /
// Tauri Store), not in backend settings file —— backend does not do i18n, logs
// are uniformly output in English, no need to help frontend manage storage.
type Settings struct {
	Session       SessionConfig       `json:"session" toml:"session,omitempty"`
	Log           LogConfig           `json:"log" toml:"log,omitempty"`
	Tools         ToolsConfig         `json:"tools" toml:"tools,omitempty"`
	Providers     []LLMProvider       `json:"providers" toml:"providers,omitempty"`
	Models        []LLMModel          `json:"models" toml:"models,omitempty"`
	Embedding     EmbeddingConfig     `json:"embedding" toml:"embedding,omitempty"`
	DefaultModels DefaultModelsConfig `json:"defaultModels" toml:"default_models,omitempty"`
	QQBot         QQBotConfig         `json:"qqbot" toml:"qqbot,omitempty"`
	Agent         AgentConfig         `json:"agent" toml:"agent,omitempty"`
	Sandbox       SandboxConfig       `json:"sandbox" toml:"sandbox,omitempty"`
	LSPMCP        LSPMCPConfig        `json:"lspmcp" toml:"lspmcp,omitempty"`
}

// ─── QQ Bot ──────────────────────────────────────────────────────────────────

// QQBotConfig is the configuration for QQ Bot WebSocket Gateway integration.
type QQBotConfig struct {
	Enabled   bool   `json:"enabled"   toml:"enabled,omitempty"`
	AppID     string `json:"appId"     toml:"app_id,omitempty"`
	AppSecret string `json:"appSecret" toml:"app_secret,omitempty"`
	Intents   int    `json:"intents,omitempty"   toml:"intents,omitempty"`
	Sandbox   bool   `json:"sandbox,omitempty"   toml:"sandbox,omitempty"`
}

// ToQQBotConfig converts config.QQBotConfig to qqbot.Config.
func (c QQBotConfig) ToQQBotConfig() qqbot.Config {
	return qqbot.Config{
		Enabled:   c.Enabled,
		AppID:     c.AppID,
		AppSecret: c.AppSecret,
		Intents:   c.Intents,
		Sandbox:   c.Sandbox,
	}
}

// ─── Session ──────────────────────────────────────────────────────────────────

type SessionConfig struct {
	TimelineMaxFileMB int `json:"timelineMaxFileMB" toml:"timeline_max_file_mb,omitempty"`
}

// ─── Log ──────────────────────────────────────────────────────────────────────

type LogConfig struct {
	Level   string `json:"level"   toml:"level,omitempty"`
	Console bool   `json:"console" toml:"console,omitempty"`
	File    bool   `json:"file"    toml:"file,omitempty"`
}

// ─── Tools ────────────────────────────────────────────────────────────────────

// ToolsConfig is the runtime configuration for agent built-in tools
//
// File system limits / write limits / external tools (http / shell / web search) policies are all here.
// main.go will use these fields to construct internal/tools.Config and call tools.Build(cfg).
type ToolsConfig struct {
	// Read limits (0 = use compile-time built-in defaults)
	MaxFileSize  int64 `json:"maxFileSize"  toml:"max_file_size,omitempty"`
	MaxMatches   int   `json:"maxMatches"   toml:"max_matches,omitempty"`
	MaxLineLen   int   `json:"maxLineLen"   toml:"max_line_len,omitempty"`
	MaxGlobItems int   `json:"maxGlobItems" toml:"max_glob_items,omitempty"`

	// Write limits
	MaxWriteSize       int64 `json:"maxWriteSize"       toml:"max_write_size,omitempty"`
	MaxMultiWriteBytes int64 `json:"maxMultiWriteBytes" toml:"max_multi_write_bytes,omitempty"`
	MaxMultiWriteFiles int   `json:"maxMultiWriteFiles" toml:"max_multi_write_files,omitempty"`
	MaxReplaceEdits    int   `json:"maxReplaceEdits"    toml:"max_replace_edits,omitempty"`

	// WebFetch
	HTTPAllowedHosts []string `json:"httpAllowedHosts,omitempty" toml:"http_allowed_hosts,omitempty"`
	HTTPMaxBody      int64    `json:"httpMaxBody"      toml:"http_max_body,omitempty"`
	HTTPTimeoutMs    int      `json:"httpTimeoutMs"    toml:"http_timeout_ms,omitempty"`
	HTTPBlockPrivate bool     `json:"httpBlockPrivate" toml:"http_block_private,omitempty"`

	// Bash
	ShellBlockRegexes   []string `json:"shellBlockRegexes"   toml:"shell_block_regexes,omitempty"`
	ShellConfirmRegexes []string `json:"shellConfirmRegexes" toml:"shell_confirm_regexes,omitempty"`
	ShellMaxOutput      int64    `json:"shellMaxOutput"      toml:"shell_max_output,omitempty"`

	// WebSearch
	WebSearchTimeoutMs int `json:"webSearchTimeoutMs" toml:"web_search_timeout_ms,omitempty"`

	// ImageGen
	ImageModels []ImageModelConfig `json:"imageModels,omitempty" toml:"image_models,omitempty"`
}

// ─── LLM Provider ─────────────────────────────────────────────────────────────

type RetryConfig struct {
	MaxRetries        int     `json:"maxRetries"        toml:"max_retries,omitempty"`
	InitialDelayMs    int     `json:"initialDelayMs"    toml:"initial_delay_ms,omitempty"`
	MaxDelayMs        int     `json:"maxDelayMs"        toml:"max_delay_ms,omitempty"`
	BackoffMultiplier float64 `json:"backoffMultiplier" toml:"backoff_multiplier,omitempty"`
}

// ResolveAPIKey reads the environment variable specified by LLMProvider.APIKeyEnv
func (p LLMProvider) ResolveAPIKey() string {
	return os.Getenv(p.APIKeyEnv)
}

type LLMProvider struct {
	ID        string            `json:"id"        toml:"id,omitempty"`
	Name      string            `json:"name"      toml:"name,omitempty"`
	BaseURL   string            `json:"baseUrl"   toml:"base_url,omitempty"`
	APIKeyEnv string            `json:"apiKeyEnv" toml:"api_key_env,omitempty"`
	Enabled   bool              `json:"enabled"   toml:"enabled,omitempty"`
	IsDefault bool              `json:"isDefault" toml:"is_default,omitempty"`
	TimeoutMs int               `json:"timeoutMs" toml:"timeout_ms,omitempty"`
	Retry     RetryConfig       `json:"retry"     toml:"retry,omitempty"`
	Headers   map[string]string `json:"headers,omitempty" toml:"headers,omitempty"`
}

// ─── LLM Model ────────────────────────────────────────────────────────────────

// GenerationParams model generation parameters (sampling control)
type GenerationParams struct {
	Temperature float64 `json:"temperature" toml:"temperature,omitempty"`
	MaxTokens   int     `json:"maxTokens"   toml:"max_tokens,omitempty"`
}

// ThinkingConfig thinking/reasoning configuration (DeepSeek V4 thinking mode)
type ThinkingConfig struct {
	Enabled         bool   `json:"enabled"         toml:"enabled,omitempty"`
	ReasoningEffort string `json:"reasoningEffort" toml:"reasoning_effort,omitempty"`
}

type LLMModel struct {
	ID            string           `json:"id"            toml:"id,omitempty"`
	ProviderID    string           `json:"providerId"    toml:"provider_id,omitempty"`
	Name          string           `json:"name"          toml:"name,omitempty"`
	APIModel      string           `json:"apiModel,omitempty"      toml:"api_model,omitempty"`
	ContextWindow int              `json:"contextWindow" toml:"context_window,omitempty"`
	Enabled       bool             `json:"enabled"       toml:"enabled,omitempty"`
	Generation    GenerationParams `json:"generation"    toml:"generation,omitempty"`
	Thinking      ThinkingConfig   `json:"thinking"      toml:"thinking,omitempty"`
}

// ─── Embedding ────────────────────────────────────────────────────────────────

type EmbeddingProvider struct {
	ID        string `json:"id"        toml:"id,omitempty"`
	Name      string `json:"name"      toml:"name,omitempty"`
	BaseURL   string `json:"baseUrl"   toml:"base_url,omitempty"`
	APIKeyEnv string `json:"apiKeyEnv" toml:"api_key_env,omitempty"`
	Enabled   bool   `json:"enabled"   toml:"enabled,omitempty"`
}

type EmbeddingModel struct {
	ID         string `json:"id"         toml:"id,omitempty"`
	ProviderID string `json:"providerId" toml:"provider_id,omitempty"`
	Name       string `json:"name"       toml:"name,omitempty"`
	Dimension  int    `json:"dimension"  toml:"dimension,omitempty"`
	BatchSize  int    `json:"batchSize"  toml:"batch_size,omitempty"`
	Normalize  bool   `json:"normalize"  toml:"normalize,omitempty"`
	Enabled    bool   `json:"enabled"    toml:"enabled,omitempty"`
	IsDefault  bool   `json:"isDefault"  toml:"is_default,omitempty"`
}

type EmbeddingConfig struct {
	Enabled       bool                `json:"enabled"       toml:"enabled,omitempty"`
	MinSimilarity float32             `json:"minSimilarity" toml:"min_similarity,omitempty"`
	Providers     []EmbeddingProvider `json:"providers"     toml:"providers,omitempty"`
	Models        []EmbeddingModel    `json:"models"        toml:"models,omitempty"`
}

// ─── Default Models ────────────────────────────────────────────────────────────

// DefaultModelsConfig configures default models by role
//
// Config value format is "provider:id", provider and id must exist in the config file's
// Providers[] and Models[]. effort follows the model's own definition.
//
// Resolution priority: role field value → Fallback → hardcoded default value.
type DefaultModelsConfig struct {
	Expert    string `json:"expert"    toml:"expert,omitempty"`
	Superior  string `json:"superior"  toml:"superior,omitempty"`
	Universal string `json:"universal" toml:"universal,omitempty"`
	Fast      string `json:"fast"      toml:"fast,omitempty"`
	Fallback  string `json:"fallback"  toml:"fallback,omitempty"`
}

// parseProviderModelID parses config value in "provider:id" format
func parseProviderModelID(s string) (providerID, modelID string, ok bool) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// ─── Image Model ─────────────────────────────────────────────────────────────

// ImageModelConfig 图片生成模型配置
type ImageModelConfig struct {
	ID           string `json:"id"           toml:"id,omitempty"`
	Name         string `json:"name"         toml:"name,omitempty"`
	Provider     string `json:"provider"     toml:"provider,omitempty"`
	SecretIdEnv  string `json:"secretIdEnv"  toml:"secret_id_env,omitempty"`
	SecretKeyEnv string `json:"secretKeyEnv" toml:"secret_key_env,omitempty"`
	APIKeyEnv    string `json:"apiKeyEnv"    toml:"api_key_env,omitempty"`
	APIBaseHost  string `json:"apiBaseHost"  toml:"api_base_host,omitempty"`
	Region       string `json:"region"       toml:"region,omitempty"`
	IsDefault    bool   `json:"isDefault"    toml:"is_default,omitempty"`
	Enabled      bool   `json:"enabled"      toml:"enabled,omitempty"`
}

// ─── Sandbox (container isolation) ─────────────────────────────────────────

// SandboxConfig configures the container sandbox environment.
type SandboxConfig struct {
	// Env is a list of environment variables to inject into the sandbox.
	// Each entry is either "KEY" (resolved from host process via os.Getenv)
	// or "KEY=VALUE" (used literally).
	Env []string `json:"env" toml:"env"`
}

// ─── LSP MCP (built-in LSP-based MCP server) ───────────────────────────────

// LSPMCPConfig configures the built-in LSP-based MCP server.
type LSPMCPConfig struct {
	Servers []LSPMCPEntry `json:"servers,omitempty" toml:"servers"`
}

// LSPMCPEntry is a single LSP server entry in settings.toml.
// When the servers list is empty, all built-in servers are used.
type LSPMCPEntry struct {
	ID         string   `json:"id" toml:"id"`
	Command    string   `json:"command" toml:"command"`
	Args       []string `json:"args" toml:"args"`
	Languages  []string `json:"languages" toml:"languages"`
	Extensions []string `json:"extensions" toml:"extensions"`
	Disabled   bool     `json:"disabled" toml:"disabled"`
}
