package config

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	qqbot "github.com/xiaobaitu/soloqueue/internal/channel/qq"
	"github.com/xiaobaitu/soloqueue/internal/channel/wechat"
	"github.com/xiaobaitu/soloqueue/internal/tasktype"
	"gopkg.in/yaml.v3"
)

// ─── Agent (L1 orchestrator) ──────────────────────────────────────────────────

// AgentConfig holds per-agent-type settings. Currently only the L1 orchestrator.
type AgentConfig struct {
	// BuiltinMCPServers is the L1 orchestrator's built-in MCP server whitelist.
	// nil (unset) = load all built-in servers.
	// [] = explicit empty = load nothing.
	// ["builtin-lsp"] = whitelist, only load named servers.
	BuiltinMCPServers []string `json:"builtinMcpServers" yaml:"builtin_mcp_servers,omitempty"`

	// ExternalMCPServers is the L1 orchestrator's external MCP server whitelist.
	// nil (unset) = load all external servers.
	// [] = explicit empty = load nothing.
	// ["server1"] = whitelist, only load named servers.
	ExternalMCPServers []string `json:"externalMcpServers" yaml:"external_mcp_servers,omitempty"`
}

// ─── Simulation ──────────────────────────────────────────────────────────

// SimulationConfig holds default simulation settings.
type SimulationConfig struct {
	DefaultModelID        string `json:"defaultModelId" yaml:"default_model_id,omitempty"`
	DefaultProviderID     string `json:"defaultProviderId" yaml:"default_provider_id,omitempty"`
	DBPath                string `json:"dbPath" yaml:"db_path,omitempty"`
	DefaultMaxWallClockMs int    `json:"defaultMaxWallClockMs" yaml:"default_max_wall_clock_ms,omitempty"`

	// Generative Agents mode
	EnableReflection bool   `json:"enableReflection" yaml:"enable_reflection,omitempty"`
	SimulatedHours   int    `json:"simulatedHours" yaml:"simulated_hours,omitempty"`
	TickIntervalMs   int    `json:"tickIntervalMs" yaml:"tick_interval_ms,omitempty"`
	TimeScale        int    `json:"timeScale" yaml:"time_scale,omitempty"`
	Language         string `json:"language" yaml:"language,omitempty"`
}

// ─── Speech ───────────────────────────────────────────────────────────────

// SpeechConfig controls local speech-to-text via whisper.cpp.
type SpeechConfig struct {
	Enabled  bool   `json:"enabled" yaml:"enabled,omitempty"`
	Model    string `json:"model"   yaml:"model,omitempty"`      // tiny / base / small / medium
	ModelDir string `json:"modelDir" yaml:"model_dir,omitempty"` // "" = ~/.soloqueue/models
}

type Settings struct {
	Session     SessionConfig     `json:"session" yaml:"session,omitempty"`
	Log         LogConfig         `json:"log" yaml:"log,omitempty"`
	Tools       ToolsConfig       `json:"tools" yaml:"tools,omitempty"`
	Providers   []LLMProvider     `json:"providers" yaml:"providers,omitempty"`
	Models      []LLMModel        `json:"models" yaml:"models,omitempty"`
	Embedding   EmbeddingConfig   `json:"embedding" yaml:"embedding,omitempty"`
	ModelRoutes ModelRoutesConfig `json:"modelRoutes" yaml:"model_routes,omitempty"`
	QQBots      []QQBotConfig     `json:"qqbots" yaml:"qqbots,omitempty"`
	WechatBots  []WechatBotConfig `json:"wechatBots" yaml:"wechat_bots,omitempty"`
	Agent       AgentConfig       `json:"agent" yaml:"agent,omitempty"`
	LSPMCP      LSPMCPConfig      `json:"lspmcp" yaml:"lspmcp,omitempty"`
	Simulation  SimulationConfig  `json:"simulation" yaml:"simulation,omitempty"`
	Speech      SpeechConfig      `json:"speech" yaml:"speech,omitempty"`
}

// ─── WeChat Bot ─────────────────────────────────────────────────────────────

// WechatBotConfig configures an official WeChat iLink bot account.
type WechatBotConfig struct {
	ID               string   `json:"id" yaml:"id,omitempty"`
	Name             string   `json:"name" yaml:"name,omitempty"`
	Enabled          bool     `json:"enabled" yaml:"enabled,omitempty"`
	BotToken         string   `json:"-" yaml:"bot_token,omitempty"`
	BotID            string   `json:"-" yaml:"bot_id,omitempty"`
	BaseURL          string   `json:"baseUrl" yaml:"base_url,omitempty"`
	BotAgent         string   `json:"botAgent" yaml:"bot_agent,omitempty"`
	BindType         string   `json:"bind_type" yaml:"bind_type,omitempty"`
	BindAgent        string   `json:"bind_agent" yaml:"bind_agent,omitempty"`
	WhitelistEnabled bool     `json:"whitelist_enabled" yaml:"whitelist_enabled,omitempty"`
	Whitelist        []string `json:"whitelist" yaml:"whitelist,omitempty"`
}

func (c WechatBotConfig) ToWechatConfig(version string) wechat.Config {
	return wechat.Config{
		Enabled:  c.Enabled,
		Token:    c.BotToken,
		BotID:    c.BotID,
		BaseURL:  c.BaseURL,
		Version:  version,
		BotAgent: c.BotAgent,
	}
}

// ─── QQ Bot ──────────────────────────────────────────────────────────────────

// QQBotConfig is the configuration for QQ Bot WebSocket Gateway integration.
type QQBotConfig struct {
	ID               string   `json:"id"        yaml:"id,omitempty"`
	Name             string   `json:"name"      yaml:"name,omitempty"`
	Enabled          bool     `json:"enabled"   yaml:"enabled,omitempty"`
	AppID            string   `json:"appId"     yaml:"app_id,omitempty"`
	AppSecret        string   `json:"appSecret" yaml:"app_secret,omitempty"`
	Intents          int      `json:"intents,omitempty"   yaml:"intents,omitempty"`
	Sandbox          bool     `json:"sandbox,omitempty"   yaml:"sandbox,omitempty"`
	BindType         string   `json:"bind_type"  yaml:"bind_type,omitempty"`  // "l1" or "l2"
	BindAgent        string   `json:"bind_agent" yaml:"bind_agent,omitempty"` // Agent Template ID (for l2)
	WhitelistEnabled bool     `json:"whitelist_enabled" yaml:"whitelist_enabled,omitempty"`
	Whitelist        []string `json:"whitelist" yaml:"whitelist,omitempty"`
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
	TimelineMaxFileMB int `json:"timelineMaxFileMB" yaml:"timeline_max_file_mb,omitempty"`
}

// ─── Log ──────────────────────────────────────────────────────────────────────

type LogConfig struct {
	Level   string `json:"level"   yaml:"level,omitempty"`
	Console bool   `json:"console" yaml:"console"`
	File    bool   `json:"file"    yaml:"file"`
}

// ─── Tools ────────────────────────────────────────────────────────────────────

// ToolsConfig is the runtime configuration for agent built-in tools
//
// File system limits / write limits / external tools (http / shell / web search) policies are all here.
// main.go will use these fields to construct internal/tools.Config and call tools.Build(cfg).
type ToolsConfig struct {
	// Read limits (0 = use compile-time built-in defaults)
	MaxFileSize  int64 `json:"maxFileSize"  yaml:"max_file_size,omitempty"`
	MaxMatches   int   `json:"maxMatches"   yaml:"max_matches,omitempty"`
	MaxLineLen   int   `json:"maxLineLen"   yaml:"max_line_len,omitempty"`
	MaxGlobItems int   `json:"maxGlobItems" yaml:"max_glob_items,omitempty"`

	// Write limits
	MaxWriteSize       int64 `json:"maxWriteSize"       yaml:"max_write_size,omitempty"`
	MaxMultiWriteBytes int64 `json:"maxMultiWriteBytes" yaml:"max_multi_write_bytes,omitempty"`
	MaxMultiWriteFiles int   `json:"maxMultiWriteFiles" yaml:"max_multi_write_files,omitempty"`
	MaxReplaceEdits    int   `json:"maxReplaceEdits"    yaml:"max_replace_edits,omitempty"`

	// WebFetch
	HTTPAllowedHosts []string `json:"httpAllowedHosts,omitempty" yaml:"http_allowed_hosts,omitempty"`
	HTTPMaxBody      int64    `json:"httpMaxBody"      yaml:"http_max_body,omitempty"`
	HTTPTimeoutMs    int      `json:"httpTimeoutMs"    yaml:"http_timeout_ms,omitempty"`
	HTTPBlockPrivate bool     `json:"httpBlockPrivate" yaml:"http_block_private,omitempty"`

	// Bash
	ShellBlockRegexes   []string `json:"shellBlockRegexes"   yaml:"shell_block_regexes,omitempty"`
	ShellConfirmRegexes []string `json:"shellConfirmRegexes" yaml:"shell_confirm_regexes,omitempty"`
	ShellMaxOutput      int64    `json:"shellMaxOutput"      yaml:"shell_max_output,omitempty"`

	// WebSearch
	WebSearchTimeoutMs int `json:"webSearchTimeoutMs" yaml:"web_search_timeout_ms,omitempty"`
	// TavilyAPIKey enables the Tavily search backend when set; empty falls back to DuckDuckGo.
	TavilyAPIKey string `json:"tavilyApiKey" yaml:"tavily_api_key,omitempty"`
	// TavilyAPIKeyEnv is an env var fallback for TavilyAPIKey (used when the direct key is empty).
	TavilyAPIKeyEnv string `json:"tavilyApiKeyEnv" yaml:"tavily_api_key_env,omitempty"`

	// ImageGen
	ImageModels []ImageModelConfig `json:"imageModels,omitempty" yaml:"image_models,omitempty"`
}

// ─── LLM Provider ─────────────────────────────────────────────────────────────

type RetryConfig struct {
	MaxRetries        int     `json:"maxRetries"        yaml:"max_retries,omitempty"`
	InitialDelayMs    int     `json:"initialDelayMs"    yaml:"initial_delay_ms,omitempty"`
	MaxDelayMs        int     `json:"maxDelayMs"        yaml:"max_delay_ms,omitempty"`
	BackoffMultiplier float64 `json:"backoffMultiplier" yaml:"backoff_multiplier,omitempty"`
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
	ID        string            `json:"id"        yaml:"id,omitempty"`
	Name      string            `json:"name"      yaml:"name,omitempty"`
	BaseURL   string            `json:"baseUrl"   yaml:"base_url,omitempty"`
	APIKey    string            `json:"apiKey"    yaml:"api_key,omitempty"`
	APIKeyEnv string            `json:"apiKeyEnv" yaml:"api_key_env,omitempty"`
	Enabled   bool              `json:"enabled"   yaml:"enabled,omitempty"`
	IsDefault bool              `json:"isDefault" yaml:"is_default,omitempty"`
	TimeoutMs int               `json:"timeoutMs" yaml:"timeout_ms,omitempty"`
	Retry     RetryConfig       `json:"retry"     yaml:"retry,omitempty"`
	Headers   map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
}

// ─── LLM Model ────────────────────────────────────────────────────────────────

// GenerationParams model generation parameters (sampling control)
type GenerationParams struct {
	Temperature float64 `json:"temperature" yaml:"temperature,omitempty"`
	MaxTokens   int     `json:"maxTokens"   yaml:"max_tokens,omitempty"`
}

// ThinkingConfig thinking/reasoning configuration (DeepSeek V4 thinking mode)
type ThinkingConfig struct {
	Enabled         bool   `json:"enabled"         yaml:"enabled,omitempty"`
	ReasoningEffort string `json:"reasoningEffort" yaml:"reasoning_effort,omitempty"`
	// ThinkingType is the value for the thinking.type field sent to the LLM API.
	// Default "enabled" (DeepSeek convention). Use "adaptive" for MiniMax M3 and other
	// OpenAI-compatible providers that expect "adaptive" instead of "enabled".
	ThinkingType string `json:"thinkingType" yaml:"thinking_type,omitempty"`
}

type LLMModel struct {
	ID            string           `json:"id"            yaml:"id,omitempty"`
	ProviderID    string           `json:"providerId"    yaml:"provider_id,omitempty"`
	Name          string           `json:"name"          yaml:"name,omitempty"`
	APIModel      string           `json:"apiModel,omitempty"      yaml:"api_model,omitempty"`
	ContextWindow int              `json:"contextWindow" yaml:"context_window,omitempty"`
	Enabled       bool             `json:"enabled"       yaml:"enabled,omitempty"`
	Generation    GenerationParams `json:"generation"    yaml:"generation,omitempty"`
	Thinking      ThinkingConfig   `json:"thinking"      yaml:"thinking,omitempty"`
	Vision        bool             `json:"vision"        yaml:"vision,omitempty"` // supports multimodal image_url content
}

// ─── Embedding ────────────────────────────────────────────────────────────────

type EmbeddingProvider struct {
	ID        string `json:"id"        yaml:"id,omitempty"`
	Name      string `json:"name"      yaml:"name,omitempty"`
	BaseURL   string `json:"baseUrl"   yaml:"base_url,omitempty"`
	APIKey    string `json:"apiKey"    yaml:"api_key,omitempty"`
	APIKeyEnv string `json:"apiKeyEnv" yaml:"api_key_env,omitempty"`
	Enabled   bool   `json:"enabled"   yaml:"enabled,omitempty"`
}

type EmbeddingModel struct {
	ID         string `json:"id"         yaml:"id,omitempty"`
	ProviderID string `json:"providerId" yaml:"provider_id,omitempty"`
	Name       string `json:"name"       yaml:"name,omitempty"`
	Dimension  int    `json:"dimension"  yaml:"dimension,omitempty"`
	BatchSize  int    `json:"batchSize"  yaml:"batch_size,omitempty"`
	Normalize  bool   `json:"normalize"  yaml:"normalize,omitempty"`
	Enabled    bool   `json:"enabled"    yaml:"enabled,omitempty"`
	IsDefault  bool   `json:"isDefault"  yaml:"is_default,omitempty"`
}

type EmbeddingConfig struct {
	Enabled       bool                `json:"enabled"       yaml:"enabled,omitempty"`
	MinSimilarity float32             `json:"minSimilarity" yaml:"min_similarity,omitempty"`
	Provider      string              `json:"provider"      yaml:"provider,omitempty"`   // "none", "openai"
	ModelName     string              `json:"modelName"     yaml:"model_name,omitempty"` // model name for API
	Providers     []EmbeddingProvider `json:"providers"     yaml:"providers,omitempty"`
	Models        []EmbeddingModel    `json:"models"        yaml:"models,omitempty"`
}

// ─── Model Routes ─────────────────────────────────────────────────────────────

// ModelRoutesConfig maps the three user-visible task types to configured models.
// Classifier is an auxiliary non-thinking model, not a task type. Values use the
// existing "provider:id" reference format.
type ModelRoutesConfig struct {
	General     string `json:"general"     yaml:"general,omitempty"`
	Engineering string `json:"engineering" yaml:"engineering,omitempty"`
	Research    string `json:"research"    yaml:"research,omitempty"`
	Classifier  string `json:"classifier"  yaml:"classifier,omitempty"`
	Vision      string `json:"vision"      yaml:"vision,omitempty"`
	Fallback    string `json:"fallback"    yaml:"fallback,omitempty"`
}

// TaskRef returns the configured reference for a task type.
func (c ModelRoutesConfig) TaskRef(t tasktype.TaskType) string {
	switch t {
	case tasktype.General:
		return c.General
	case tasktype.Engineering:
		return c.Engineering
	case tasktype.Research:
		return c.Research
	default:
		return ""
	}
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
	ID           string `json:"id"           yaml:"id,omitempty"`
	Name         string `json:"name"         yaml:"name,omitempty"`
	Provider     string `json:"provider"     yaml:"provider,omitempty"`
	SecretId     string `json:"secretId"     yaml:"secret_id,omitempty"`
	SecretIdEnv  string `json:"secretIdEnv"  yaml:"secret_id_env,omitempty"`
	SecretKey    string `json:"secretKey"    yaml:"secret_key,omitempty"`
	SecretKeyEnv string `json:"secretKeyEnv" yaml:"secret_key_env,omitempty"`
	APIKey       string `json:"apiKey"       yaml:"api_key,omitempty"`
	APIKeyEnv    string `json:"apiKeyEnv"    yaml:"api_key_env,omitempty"`
	APIBaseHost  string `json:"apiBaseHost"  yaml:"api_base_host,omitempty"`
	Region       string `json:"region"       yaml:"region,omitempty"`
	IsDefault    bool   `json:"isDefault"    yaml:"is_default,omitempty"`
	Enabled      bool   `json:"enabled"      yaml:"enabled,omitempty"`
}

// ─── LSP MCP (built-in LSP-based MCP server) ───────────────────────────────

// LSPMCPConfig configures the built-in LSP-based MCP server.
type LSPMCPConfig struct {
	Servers []LSPMCPEntry `json:"servers,omitempty" yaml:"servers"`
}

// LSPMCPEntry is a single LSP server entry in settings.yaml.
// When the servers list is empty, all built-in servers are used.
type LSPMCPEntry struct {
	ID         string   `json:"id" yaml:"id"`
	Command    string   `json:"command" yaml:"command"`
	Args       []string `json:"args" yaml:"args"`
	Languages  []string `json:"languages" yaml:"languages"`
	Extensions []string `json:"extensions" yaml:"extensions"`
	Disabled   bool     `json:"disabled" yaml:"disabled"`
}

// MarshalYAML customizes AgentConfig YAML output so nil slices are omitted
// while empty slices are preserved as []. This preserves the semantic difference
// between "load all" (nil) and "load none" ([]).
func (a AgentConfig) MarshalYAML() (interface{}, error) {
	if a.BuiltinMCPServers == nil && a.ExternalMCPServers == nil {
		return map[string]interface{}{}, nil
	}
	type raw struct {
		BuiltinMCPServers  []string `yaml:"builtin_mcp_servers"`
		ExternalMCPServers []string `yaml:"external_mcp_servers"`
	}
	return raw{
		BuiltinMCPServers:  a.BuiltinMCPServers,
		ExternalMCPServers: a.ExternalMCPServers,
	}, nil
}

// MarshalYAMLWithComments serializes Settings into a YAML byte slice with section comments.
func (s Settings) MarshalYAMLWithComments() ([]byte, error) {
	var root yaml.Node
	root.Kind = yaml.DocumentNode

	mapping := &yaml.Node{Kind: yaml.MappingNode}
	root.Content = append(root.Content, mapping)

	sections := []struct {
		name    string
		comment string
		value   interface{}
	}{
		{"session", "Session settings", s.Session},
		{"log", "Logging settings", s.Log},
		{"providers", "LLM providers", s.Providers},
		{"models", "LLM models", s.Models},
		{"model_routes", "Task model routing", s.ModelRoutes},
		{"embedding", "Embedding settings", s.Embedding},
		{"qqbots", "QQ bot integrations", s.QQBots},
		{"wechat_bots", "WeChat iLink bot integrations", s.WechatBots},
		{"lspmcp", "Built-in LSP-based MCP servers", s.LSPMCP},
		{"tools", "Tool execution limits", s.Tools},
		{"simulation", "Simulation engine defaults", s.Simulation},
		{"speech", "Local speech-to-text via whisper.cpp", s.Speech},
		{"agent", "MCP server whitelists: nil/omitted = load all; [] = load none", s.Agent},
	}

	for i, sec := range sections {
		key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: sec.name}
		if sec.comment != "" {
			comment := sec.comment
			if i > 0 {
				comment = "\n" + comment
			}
			key.HeadComment = comment
		}
		value, err := valueToYAMLNode(sec.value)
		if err != nil {
			return nil, fmt.Errorf("marshal section %q: %w", sec.name, err)
		}
		mapping.Content = append(mapping.Content, key, value)
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&root); err != nil {
		return nil, fmt.Errorf("encode yaml: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("close yaml encoder: %w", err)
	}

	var out strings.Builder
	out.WriteString("# SoloQueue Configuration File\n")
	out.WriteString("# This file is the single source of truth for SoloQueue configuration.\n")
	out.WriteString("# Direct edits are picked up automatically while the server is running.\n\n")
	out.Write(buf.Bytes())
	return []byte(out.String()), nil
}

// valueToYAMLNode converts a value into a yaml.Node by round-tripping through the
// standard YAML encoder. This lets us assemble a single document where each section
// carries a leading comment without wrapping sections in extra indentation levels.
func valueToYAMLNode(v interface{}) (*yaml.Node, error) {
	data, err := yaml.Marshal(v)
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if len(doc.Content) == 0 {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null"}, nil
	}
	return doc.Content[0], nil
}
