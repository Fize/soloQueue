package config

import (
	"os"
	"strings"
)

// ─── Top-level Settings ───────────────────────────────────────────────────────

// Settings 是全局配置的完整结构体
// 对应 ~/.soloqueue/settings.json
//
// 注意：UI 专属状态（theme / language 等）由前端自行持久化（localStorage /
// Tauri Store），不进 backend settings.json —— backend 不做 i18n，日志
// 统一英文输出，没必要帮前端代管存储。
type Settings struct {
	Session   SessionConfig   `json:"session"`
	Log       LogConfig       `json:"log"`
	Tools     ToolsConfig     `json:"tools"`
	Providers     []LLMProvider       `json:"providers"`
	Models        []LLMModel          `json:"models"`
	Embedding     EmbeddingConfig     `json:"embedding"`
	DefaultModels DefaultModelsConfig `json:"defaultModels"`
}

// ─── Session ──────────────────────────────────────────────────────────────────

type SessionConfig struct {
	ReplaySegments    int `json:"replaySegments"`    // 回放段数，默认 3
	TimelineMaxFileMB int `json:"timelineMaxFileMB"` // 单文件上限 MB，默认 50
	TimelineMaxFiles  int `json:"timelineMaxFiles"`  // 轮转文件数，默认 5
}

// ─── Log ──────────────────────────────────────────────────────────────────────

type LogConfig struct {
	Level   string `json:"level"`   // "debug" | "info" | "warn" | "error"
	Console bool   `json:"console"`
	File    bool   `json:"file"`
}

// ─── Tools ────────────────────────────────────────────────────────────────────

// ToolsConfig 是 agent 内置工具的运行时配置
//
// 文件系统上限 / 写入上限 / 外部工具（http / shell / Tavily）的策略都在这里。
// main.go 会用这些字段构造 internal/tools.Config 并调用 tools.Build(cfg)。
type ToolsConfig struct {
	// AllowedDirs 沙箱白名单（empty = 禁止所有文件操作）
	AllowedDirs []string `json:"allowedDirs"`

	// 读类限制（0 = 使用编译内置默认）
	MaxFileSize  int64 `json:"maxFileSize"`
	MaxMatches   int   `json:"maxMatches"`
	MaxLineLen   int   `json:"maxLineLen"`
	MaxGlobItems int   `json:"maxGlobItems"`

	// 写类限制
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

// ResolveAPIKey 读取 LLMProvider.APIKeyEnv 指定的环境变量
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

// GenerationParams 模型生成参数（采样控制）
type GenerationParams struct {
	Temperature float64 `json:"temperature"`
	MaxTokens  int     `json:"maxTokens"`
}

// ThinkingConfig 思考/推理配置（DeepSeek V4 thinking 模式）
type ThinkingConfig struct {
	Enabled         bool   `json:"enabled"`
	ReasoningEffort string `json:"reasoningEffort"` // "high" | "max" | ""（V4 模型使用）
}

type LLMModel struct {
	ID            string          `json:"id"`
	ProviderID    string          `json:"providerId"`
	Name          string          `json:"name"`
	APIModel      string          `json:"apiModel,omitempty"` // 实际 API 模型名，空则用 ID
	ContextWindow int             `json:"contextWindow"`
	Enabled       bool            `json:"enabled"`
	Generation    GenerationParams `json:"generation"`
	Thinking      ThinkingConfig  `json:"thinking"`
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

// DefaultModelsConfig 按角色配置默认模型
//
// 配置值格式为 "provider:id"，provider 和 id 必须存在于配置文件的
// Providers[] 和 Models[] 中。effort 跟随模型自身定义。
//
// 解析优先级：角色字段值 → Fallback → 硬编码默认值。
type DefaultModelsConfig struct {
	Expert    string `json:"expert"`    // 专家模型
	Superior  string `json:"superior"`  // 次级专家模型
	Universal string `json:"universal"` // 通用模型
	Fast      string `json:"fast"`      // 快速模型
	Fallback  string `json:"fallback"`  // 兜底默认模型（空=不配置）
}

// parseProviderModelID 解析 "provider:id" 格式的配置值
func parseProviderModelID(s string) (providerID, modelID string, ok bool) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}
