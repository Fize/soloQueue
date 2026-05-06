package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// OpenAIConfig configures an OpenAI-compatible embedding client.
type OpenAIConfig struct {
	BaseURL    string            // e.g., "https://api.deepseek.com/v1"
	APIKey     string            // Bearer token
	ModelID    string            // e.g., "deepseek-embed"
	Dimension  int               // expected embedding dimension
	Headers    map[string]string // extra HTTP headers
	TimeoutMs  int               // HTTP timeout in ms
	HTTPClient *http.Client      // nil = use default
}

// OpenAIClient calls an OpenAI-compatible embeddings endpoint.
type OpenAIClient struct {
	baseURL   string
	apiKey    string
	modelID   string
	dimension int
	headers   map[string]string
	http      *http.Client
}

// NewOpenAI creates an OpenAIClient.
func NewOpenAI(cfg OpenAIConfig) (*OpenAIClient, error) {
	if cfg.BaseURL == "" || cfg.ModelID == "" {
		return nil, fmt.Errorf("openai embed: BaseURL and ModelID are required")
	}
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		timeout := 10 * time.Minute
		if cfg.TimeoutMs > 0 {
			timeout = time.Duration(cfg.TimeoutMs) * time.Millisecond
		}
		httpClient = &http.Client{Timeout: timeout}
	}
	return &OpenAIClient{
		baseURL:   baseURL,
		apiKey:    cfg.APIKey,
		modelID:   cfg.ModelID,
		dimension: cfg.Dimension,
		headers:   cfg.Headers,
		http:      httpClient,
	}, nil
}

// Dimension returns the expected embedding dimension.
func (c *OpenAIClient) Dimension() int { return c.dimension }

type embedRequest struct {
	Model          string   `json:"model"`
	Input          []string `json:"input"`
	EncodingFormat string   `json:"encoding_format,omitempty"`
}

type embedResponse struct {
	Data  []embedData `json:"data"`
	Usage *embedUsage `json:"usage,omitempty"`
}

type embedData struct {
	Embedding []float32 `json:"embedding"`
	Index     int       `json:"index"`
}

type embedUsage struct {
	PromptTokens int `json:"prompt_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// Embed generates embeddings for the given texts.
func (c *OpenAIClient) Embed(ctx context.Context, texts []string) ([]Result, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	reqBody := embedRequest{
		Model:          c.modelID,
		Input:          texts,
		EncodingFormat: "float",
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("openai embed: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai embed: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("openai embed: HTTP %d", resp.StatusCode)
	}

	var parsed embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("openai embed: decode: %w", err)
	}

	results := make([]Result, len(parsed.Data))
	for _, d := range parsed.Data {
		results[d.Index] = Result{
			Embedding: d.Embedding,
		}
	}
	if parsed.Usage != nil {
		tokensPer := parsed.Usage.TotalTokens / len(texts)
		for i := range results {
			results[i].Tokens = tokensPer
		}
	}
	return results, nil
}
