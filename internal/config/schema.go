package config

import (
	"os"
	"strings"
)

// ─── Top-level Settings ───────────────────────────────────────────────────────

// Settings is the complete structure for global configuration
// Corresponds to ~/.soloqueue/settings.json
//
// Note: UI-specific states (theme / language, etc.) are persisted by the frontend itself (localStorage /
// Tauri Store), not in backend settings.json —— backend does not do i18n, logs
// are uniformly output in English, no need to help frontend manage storage.
type Settings struct {
	Session       SessionConfig       `json:"session"`
	Log           LogConfig           `json:"log"`
	Tools         ToolsConfig         `json:"tools"`
	Providers     []LLMProvider       `json:"providers"`
	Models        []LLMModel          `json:"models"`
	Embedding     EmbeddingConfig     `json:"embedding"`
	DefaultModels DefaultModelsConfig `json:"defaultModels"`
}

// ─── Session ──────────────────────────────────────────────────────────────────

type SessionConfig struct {
	ReplaySegments    int `json:"replaySegments"`    // Number of replay segments, default 3
	TimelineMaxFileMB int `json:"timelineMaxFileMB"` // Single file limit MB, default 50
	TimelineMaxFiles  int `json:"timelineMaxFiles"`  // Number of rotating files, default 5
}

// ─── Log ──────────────────────────────────────────────────────────────────────

type LogConfig struct {
	Level   string `json:"level"` // "debug" | "info" | "warn" | "error"
	Console bool   `json:"console"`
	File    bool   `json:"file"`
}

// ─── Tools ────────────────────────────────────────────────────────────────────

// ToolsConfig is the runtime configuration for agent built-in tools
//
// File system limits / write limits / external tools (http / shell / Tavily) policies are all here.
// main.go will use these fields to construct internal/tools.Config and call tools.Build(cfg).
type ToolsConfig struct {
	// AllowedDirs sandbox whitelist (empty = prohibit all file operations)
	AllowedDirs []string `json:"allowedDirs"`

	// Read limits (0 = use compile-time built-in defaults)
	MaxFileSize  int64 `json:"maxFileSize"`
	MaxMatches   int   `json:"maxMatches"`
	MaxLineLen   int   `json:"maxLineLen"`
	MaxGlobItems int   `json:"maxGlobItems"`

	// Write limits
	MaxWriteSize       int64 `json:"maxWriteSize"`
	MaxMultiWriteBytes int64 `json:"maxMultiWriteBytes"`
	MaxMultiWriteFiles int   `json:"maxMultiWriteFiles"`
	MaxReplaceEdits    int   `json:"maxReplaceEdits"`

	// WebFetch
	HTTPAllowedHosts []string `json:"httpAllowedHosts,omitempty"`
	HTTPMaxBody      int64    `json:"httpMaxBody"`
	HTTPTimeoutMs    int      `json:"httpTimeoutMs"`
	HTTPBlockPrivate bool     `json:"httpBlockPrivate"`

	// Bash
	ShellBlockRegexes   []string `json:"shellBlockRegexes"`
	ShellConfirmRegexes []string `json:"shellConfirmRegexes"`
	ShellTimeoutMs      int      `json:"shellTimeoutMs"`
	ShellMaxOutput      int64    `json:"shellMaxOutput"`

	// WebSearch
	WebSearchTimeoutMs int `json:"webSearchTimeoutMs"`
}

// ─── LLM Provider ─────────────────────────────────────────────────────────────

type RetryConfig struct {
	MaxRetries        int     `json:"maxRetries"`
	InitialDelayMs    int     `json:"initialDelayMs"`
	MaxDelayMs        int     `json:"maxDelayMs"`
	BackoffMultiplier float64 `json:"backoffMultiplier"`
}

// ResolveAPIKey reads the environment variable specified by LLMProvider.APIKeyEnv
func (p LLMProvider) ResolveAPIKey() string {
	return os.Getenv(p.APIKeyEnv)
}

type LLMProvider struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	BaseURL   string            `json:"baseUrl"`
	APIKeyEnv string            `json:"apiKeyEnv"`
	Enabled   bool              `json:"enabled"`
	IsDefault bool              `json:"isDefault"`
	TimeoutMs int               `json:"timeoutMs"`
	Retry     RetryConfig       `json:"retry"`
	Headers   map[string]string `json:"headers,omitempty"`
}

// ─── LLM Model ────────────────────────────────────────────────────────────────

// GenerationParams model generation parameters (sampling control)
type GenerationParams struct {
	Temperature float64 `json:"temperature"`
	MaxTokens   int     `json:"maxTokens"`
}

// ThinkingConfig thinking/reasoning configuration (DeepSeek V4 thinking mode)
type ThinkingConfig struct {
	Enabled         bool   `json:"enabled"`
	ReasoningEffort string `json:"reasoningEffort"` // "high" | "max" | "" (used by V4 models)
}

type LLMModel struct {
	ID            string           `json:"id"`
	ProviderID    string           `json:"providerId"`
	Name          string           `json:"name"`
	APIModel      string           `json:"apiModel,omitempty"` // Actual API model name, empty uses ID
	ContextWindow int              `json:"contextWindow"`
	Enabled       bool             `json:"enabled"`
	Generation    GenerationParams `json:"generation"`
	Thinking      ThinkingConfig   `json:"thinking"`
}

// ─── Embedding ────────────────────────────────────────────────────────────────

type EmbeddingProvider struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	BaseURL   string `json:"baseUrl"`
	APIKeyEnv string `json:"apiKeyEnv"`
	Enabled   bool   `json:"enabled"`
}

type EmbeddingModel struct {
	ID         string `json:"id"`
	ProviderID string `json:"providerId"`
	Name       string `json:"name"`
	Dimension  int    `json:"dimension"`
	BatchSize  int    `json:"batchSize"`
	Normalize  bool   `json:"normalize"`
	Enabled    bool   `json:"enabled"`
	IsDefault  bool   `json:"isDefault"`
}

type EmbeddingConfig struct {
	Enabled   bool                `json:"enabled"`
	Providers []EmbeddingProvider `json:"providers"`
	Models    []EmbeddingModel    `json:"models"`
}

// ─── Default Models ────────────────────────────────────────────────────────────

// DefaultModelsConfig configures default models by role
//
// Config value format is "provider:id", provider and id must exist in the config file's
// Providers[] and Models[]. effort follows the model's own definition.
//
// Resolution priority: role field value → Fallback → hardcoded default value.
type DefaultModelsConfig struct {
	Expert    string `json:"expert"`    // Expert model
	Superior  string `json:"superior"`  // Secondary expert model
	Universal string `json:"universal"` // Universal model
	Fast      string `json:"fast"`      // Fast model
	Fallback  string `json:"fallback"`  // Fallback default model (empty=no config)
}

// parseProviderModelID parses config value in "provider:id" format
func parseProviderModelID(s string) (providerID, modelID string, ok bool) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}
