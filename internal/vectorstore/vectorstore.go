// Package vectorstore provides a local file-backed vector store for permanent memory.
package vectorstore

import (
	"context"
	"math"
	"time"
)

// MemoryEntry is one permanently stored memory item.
type MemoryEntry struct {
	ID        string    `json:"id"`
	Content   string    `json:"content"`
	Embedding []float32 `json:"embedding"`
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source"` // original short-term memory filename
}

// VectorStore persists and queries embedded memory entries.
type VectorStore interface {
	Upsert(ctx context.Context, entry MemoryEntry) error
	Query(ctx context.Context, embedding []float32, topK int, minSimilarity float32) ([]MemoryEntry, error)
	Count(ctx context.Context) (int, error)
}

// CosineSimilarity returns the cosine similarity between two float32 vectors.
// Both must have the same length. Returns 0 if either vector has zero norm.
func CosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(normA) * math.Sqrt(normB)))
}
