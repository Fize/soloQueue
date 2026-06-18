package config

import (
	"encoding/json"
	"fmt"
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

// ─── Simulation ──────────────────────────────────────────────────────────

// SimulationConfig holds default simulation settings.
type SimulationConfig struct {
	DefaultModelID         string `json:"defaultModelId" toml:"default_model_id,omitempty"`
	DefaultProviderID      string `json:"defaultProviderId" toml:"default_provider_id,omitempty"`
	DBPath                 string `json:"dbPath" toml:"db_path,omitempty"`
	DefaultMaxWallClockMs  int    `json:"defaultMaxWallClockMs" toml:"default_max_wall_clock_ms,omitempty"`

	// Generative Agents mode
	EnableReflection       bool  `json:"enableReflection" toml:"enable_reflection,omitempty"`
	SimulatedHours         int   `json:"simulatedHours" toml:"simulated_hours,omitempty"`
	TickIntervalMs         int   `json:"tickIntervalMs" toml:"tick_interval_ms,omitempty"`
	TimeScale              int   `json:"timeScale" toml:"time_scale,omitempty"`
}

// ─── Top-level Settings ───────────────────────────────────────────────────────

// Settings is the complete structure for global configuration
// Corresponds to ~/.soloqueue/settings.toml
//
// Note: UI-specific states (theme / language, etc.) are persisted by the frontend itself (localStorage /
// Tauri Store), not in backend settings file —— backend does not do i18n, logs
// are uniformly output in English, no need to help frontend manage storage.
type Settings struct {
	Session       SessionConfig       `json:"session" toml:"-"`
	Auth          AuthConfig          `json:"auth" toml:"auth,omitempty"`
	Log           LogConfig           `json:"log" toml:"log,omitempty"`
	Tools         ToolsConfig         `json:"tools" toml:"-"`
	Providers     []LLMProvider       `json:"providers" toml:"-"`
	Models        []LLMModel          `json:"models" toml:"-"`
	Embedding     EmbeddingConfig     `json:"embedding" toml:"-"`
	DefaultModels DefaultModelsConfig `json:"defaultModels" toml:"-"`
	QQBot         QQBotConfig         `json:"qqbot" toml:"-"`
	Agent         AgentConfig         `json:"agent" toml:"agent,omitempty"`
	LSPMCP        LSPMCPConfig        `json:"lspmcp" toml:"-"`
	Simulation    SimulationConfig    `json:"simulation" toml:"-"`
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

// ─── Auth ─────────────────────────────────────────────────────────────────────

// AuthConfig configures HTTP authentication.
// If User is empty, authentication is disabled.
type AuthConfig struct {
	User     string `json:"user" toml:"user,omitempty"`
	Password string `json:"-" toml:"password,omitempty"`
}

// ─── Session ──────────────────────────────────────────────────────────────────

type SessionConfig struct {
	TimelineMaxFileMB int `json:"timelineMaxFileMB" toml:"timeline_max_file_mb,omitempty"`
}

// ─── Log ──────────────────────────────────────────────────────────────────────

type LogConfig struct {
	Level   string `json:"level"   toml:"level,omitempty"`
	Console bool   `json:"console" toml:"console"`
	File    bool   `json:"file"    toml:"file"`
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

// ResolveAPIKey returns the API key.
// If APIKey is set directly it takes priority; otherwise falls back to os.Getenv(APIKeyEnv).
func (p LLMProvider) ResolveAPIKey() string {
	if p.APIKey != "" {
		return p.APIKey
	}
	return os.Getenv(p.APIKeyEnv)
}

type LLMProvider struct {
	ID        string            `json:"id"        toml:"id,omitempty"`
	Name      string            `json:"name"      toml:"name,omitempty"`
	BaseURL   string            `json:"baseUrl"   toml:"base_url,omitempty"`
	APIKey    string            `json:"apiKey"    toml:"api_key,omitempty"`
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
	APIKey    string `json:"apiKey"    toml:"api_key,omitempty"`
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
	Provider      string              `json:"provider"      toml:"provider,omitempty"`   // "none", "openai"
	ModelName     string              `json:"modelName"     toml:"model_name,omitempty"` // model name for API
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

// ImageModelConfig configures the image generation model.
type ImageModelConfig struct {
	ID           string `json:"id"           toml:"id,omitempty"`
	Name         string `json:"name"         toml:"name,omitempty"`
	Provider     string `json:"provider"     toml:"provider,omitempty"`
	SecretId     string `json:"secretId"     toml:"secret_id,omitempty"`
	SecretIdEnv  string `json:"secretIdEnv"  toml:"secret_id_env,omitempty"`
	SecretKey    string `json:"secretKey"    toml:"secret_key,omitempty"`
	SecretKeyEnv string `json:"secretKeyEnv" toml:"secret_key_env,omitempty"`
	APIKey       string `json:"apiKey"       toml:"api_key,omitempty"`
	APIKeyEnv    string `json:"apiKeyEnv"    toml:"api_key_env,omitempty"`
	APIBaseHost  string `json:"apiBaseHost"  toml:"api_base_host,omitempty"`
	Region       string `json:"region"       toml:"region,omitempty"`
	IsDefault    bool   `json:"isDefault"    toml:"is_default,omitempty"`
	Enabled      bool   `json:"enabled"      toml:"enabled,omitempty"`
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

// MarshalTOML implements toml.Marshaler to customize TOML serialization.
// It ensures that migrated settings (Providers, Models, DefaultModels, Tools, QQBot, LSPMCP, Embedding, Session)
// are NOT written back to settings.toml, leaving only process/environment specific settings (Auth, Log, Agent).
func (s Settings) MarshalTOML() (interface{}, error) {
	type FileSettings struct {
		Auth  AuthConfig  `toml:"auth,omitempty"`
		Log   LogConfig   `toml:"log,omitempty"`
		Agent AgentConfig `toml:"agent,omitempty"`
	}
	return FileSettings{
		Auth:  s.Auth,
		Log:   s.Log,
		Agent: s.Agent,
	}, nil
}

// MarshalTOMLWithComments serializes Settings into a TOML byte slice with detailed comments explaining each option.
func (s Settings) MarshalTOMLWithComments() ([]byte, error) {
	var sb strings.Builder
	sb.WriteString("# SoloQueue Configuration File\n")
	sb.WriteString("# This file stores only base configurations related to the underlying process, system environment, and local authorization.\n")
	sb.WriteString("# All migrated configuration items (such as LLM providers, models, tools limits, QQ Bot, LSP MCP, embedding, and simulation settings)\n")
	sb.WriteString("# have been migrated to the SQLite database (entries.db) for persistence and dynamic management, and are no longer read from here at startup.\n\n")

	sb.WriteString("[auth]\n")
	sb.WriteString("# Username for HTTP Basic Authentication. If empty, authentication is disabled.\n")
	sb.WriteString(fmt.Sprintf("user = %q\n", s.Auth.User))
	sb.WriteString("# Password for HTTP Basic Authentication.\n")
	sb.WriteString(fmt.Sprintf("password = %q\n\n", s.Auth.Password))

	sb.WriteString("[log]\n")
	sb.WriteString("# Log level: debug, info, warn, error.\n")
	sb.WriteString(fmt.Sprintf("level = %q\n", s.Log.Level))
	sb.WriteString("# Print structured logs to stderr/console.\n")
	sb.WriteString(fmt.Sprintf("console = %t\n", s.Log.Console))
	sb.WriteString("# Save structured logs to ~/.soloqueue/logs/system/app-YYYY-MM-DD.json.\n")
	sb.WriteString(fmt.Sprintf("file = %t\n\n", s.Log.File))

	sb.WriteString("[agent]\n")
	sb.WriteString("# Whitelist of built-in MCP servers to load. If null (or omitted), all built-in servers are loaded. If [], no built-in servers are loaded. List like [\"builtin-lsp\"] to load only specified servers.\n")
	if s.Agent.BuiltinMCPServers != nil {
		bytes, err := json.Marshal(s.Agent.BuiltinMCPServers)
		if err != nil {
			return nil, err
		}
		sb.WriteString(fmt.Sprintf("builtin_mcp_servers = %s\n", string(bytes)))
	} else {
		sb.WriteString("# builtin_mcp_servers = [\"builtin-lsp\"]\n")
	}
	sb.WriteString("# Whitelist of external custom MCP servers to load. Rules are the same as above.\n")
	if s.Agent.ExternalMCPServers != nil {
		bytes, err := json.Marshal(s.Agent.ExternalMCPServers)
		if err != nil {
			return nil, err
		}
		sb.WriteString(fmt.Sprintf("external_mcp_servers = %s\n", string(bytes)))
	} else {
		sb.WriteString("# external_mcp_servers = [\"server1\", \"server2\"]\n")
	}

	return []byte(sb.String()), nil
}
