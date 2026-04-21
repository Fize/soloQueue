package config

// ─── Top-level Settings ───────────────────────────────────────────────────────

// Settings 是全局配置的完整结构体
// 对应 ~/.soloqueue/settings.json
type Settings struct {
	App       AppConfig       `json:"app"`
	Session   SessionConfig   `json:"session"`
	Log       LogConfig       `json:"log"`
	Providers []LLMProvider   `json:"providers"`
	Models    []LLMModel      `json:"models"`
	Embedding EmbeddingConfig `json:"embedding"`
}

// ─── App ──────────────────────────────────────────────────────────────────────

type AppConfig struct {
	Theme    string `json:"theme"`    // "dark" | "light"
	Language string `json:"language"` // "zh-CN" | "en-US"
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
	Enabled      bool   `json:"enabled"`
	BudgetTokens int    `json:"budgetTokens"` // 0 = 不限
	Type         string `json:"type"`         // "reasoning" | "extended_thinking" | ""
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
	Type          string          `json:"type"`          // "chat" | "code" | "vision"
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
