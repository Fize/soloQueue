package config

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
	Providers []LLMProvider   `json:"providers"`
	Models    []LLMModel      `json:"models"`
	Embedding EmbeddingConfig `json:"embedding"`
}

// ─── Session ──────────────────────────────────────────────────────────────────

type SessionConfig struct {
	TimeoutSecs int  `json:"timeoutSecs"`
	MaxHistory  int  `json:"maxHistory"`
	AutoSave    bool `json:"autoSave"`
}

// ─── Log ──────────────────────────────────────────────────────────────────────

type LogConfig struct {
	Level         string `json:"level"`         // "debug" | "info" | "warn" | "error"
	Console       bool   `json:"console"`
	File          bool   `json:"file"`
	MaxDays       int    `json:"maxDays"`
	MaxFileSizeMB int    `json:"maxFileSizeMB"`
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

	// http_fetch
	HTTPAllowedHosts []string `json:"httpAllowedHosts,omitempty"`
	HTTPMaxBody      int64    `json:"httpMaxBody"`
	HTTPTimeoutMs    int      `json:"httpTimeoutMs"`
	HTTPBlockPrivate bool     `json:"httpBlockPrivate"`

	// shell_exec
	ShellBlockRegexes   []string `json:"shellBlockRegexes"`
	ShellConfirmRegexes []string `json:"shellConfirmRegexes"`
	ShellTimeoutMs      int      `json:"shellTimeoutMs"`
	ShellMaxOutput      int64    `json:"shellMaxOutput"`

	// web_search (Tavily)
	//
	// TavilyAPIKeyEnv 指定读哪个环境变量的值作为 API key（与 provider 对齐）；
	// 空 env 或 env 为空值 → Build 跳过 web_search 注册
	TavilyAPIKeyEnv string `json:"tavilyApiKeyEnv"`
	TavilyEndpoint  string `json:"tavilyEndpoint"`
	TavilyTimeoutMs int    `json:"tavilyTimeoutMs"`
}

// ─── LLM Provider ─────────────────────────────────────────────────────────────

type RetryConfig struct {
	MaxRetries        int     `json:"maxRetries"`
	InitialDelayMs    int     `json:"initialDelayMs"`
	MaxDelayMs        int     `json:"maxDelayMs"`
	BackoffMultiplier float64 `json:"backoffMultiplier"`
}

type LLMProvider struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	BaseURL      string            `json:"baseUrl"`
	APIKeyEnv    string            `json:"apiKeyEnv"`
	Enabled      bool              `json:"enabled"`
	IsDefault    bool              `json:"isDefault"`
	Capabilities []string          `json:"capabilities"`
	TimeoutMs    int               `json:"timeoutMs"`
	Retry        RetryConfig       `json:"retry"`
	Headers      map[string]string `json:"headers,omitempty"`
}

// ─── LLM Model ────────────────────────────────────────────────────────────────

// GenerationParams 模型生成参数（采样控制）
type GenerationParams struct {
	Temperature      float64  `json:"temperature"`
	MaxTokens        int      `json:"maxTokens"`
	TopP             float64  `json:"topP"`
	FrequencyPenalty float64  `json:"frequencyPenalty"`
	PresencePenalty  float64  `json:"presencePenalty"`
	StopSequences    []string `json:"stopSequences"`
}

// ThinkingConfig 思考/推理配置（extended thinking / reasoning 模型）
type ThinkingConfig struct {
	Enabled         bool   `json:"enabled"`
	BudgetTokens    int    `json:"budgetTokens"`    // 旧方式，保留兼容；0 = 不限
	Type            string `json:"type"`            // "reasoning" | "extended_thinking" | ""
	ReasoningEffort string `json:"reasoningEffort"` // "high" | "max" | ""（V4 模型使用）
}

// ModelCapability 模型能力声明
type ModelCapability struct {
	Streaming       bool `json:"streaming"`
	FunctionCalling bool `json:"functionCalling"`
	Vision          bool `json:"vision"`      // 图像生成/理解（生成侧）
	ImageInput      bool `json:"imageInput"`  // 接受图片输入
	Thinking        bool `json:"thinking"`    // 支持思考/推理模式
	JSONMode        bool `json:"jsonMode"`    // 强制 JSON 输出
}

type LLMModel struct {
	ID            string          `json:"id"`
	ProviderID    string          `json:"providerId"`
	Name          string          `json:"name"`
	APIModel      string          `json:"apiModel,omitempty"` // 实际 API 模型名，空则用 ID
	Type          string          `json:"type"`              // "chat" | "code" | "vision"
	ContextWindow int             `json:"contextWindow"`
	Enabled       bool            `json:"enabled"`
	IsDefault     bool            `json:"isDefault"`
	Generation    GenerationParams `json:"generation"`
	Thinking      ThinkingConfig  `json:"thinking"`
	Capabilities  ModelCapability `json:"capabilities"`
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
