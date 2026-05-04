// Package embedding provides embedder interfaces and implementations.
package embedding

import "context"

// Result holds the embedding vector and token usage for one input text.
type Result struct {
	Embedding []float32
	Tokens    int
}

// Embedder generates vector embeddings for text.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([]Result, error)
	Dimension() int
}
