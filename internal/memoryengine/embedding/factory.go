package embedding

import "fmt"

// Config configures an embedder from the app config.
type Config struct {
	Provider  string // "none", "onnx", "openai"
	ModelPath string // ONNX model path (empty = auto-download)
	ModelName string // model name for download/API
	BaseURL   string // OpenAI-compatible base URL
	APIKey    string // API key for OpenAI
	Dimension int    // embedding dimension
}

// NewFromConfig creates an Embedder based on provider config.
// Returns (nil, nil) when provider is "none" or empty.
func NewFromConfig(cfg Config) (Embedder, error) {
	switch cfg.Provider {
	case "none", "":
		return nil, nil
	case "openai":
		return NewOpenAI(OpenAIConfig{
			BaseURL:   cfg.BaseURL,
			APIKey:    cfg.APIKey,
			ModelID:   cfg.ModelName,
			Dimension: cfg.Dimension,
		})
	case "onnx":
		return newONNXEmbedder(cfg)
	default:
		return nil, fmt.Errorf("embedding: unknown provider %q", cfg.Provider)
	}
}
