package memoryengine

import (
	"context"

	"github.com/xiaobaitu/soloqueue/internal/memoryengine/embedding"
	"github.com/xiaobaitu/soloqueue/internal/memoryengine/vectorstore"
)

// VectorSearcher wraps embedding-based vector search. No-op when embedder is nil.
type VectorSearcher struct {
	embedder embedding.Embedder
	vecStore vectorstore.VectorStore
}

// NewVectorSearcher creates a vector searcher. Both params may be nil (no vector search).
func NewVectorSearcher(embedder embedding.Embedder, vecStore vectorstore.VectorStore) *VectorSearcher {
	return &VectorSearcher{embedder: embedder, vecStore: vecStore}
}

// Enabled returns true if vector search is available.
func (v *VectorSearcher) Enabled() bool {
	return v.embedder != nil && v.vecStore != nil
}

// Search embeds the query and queries the vector store.
// Returns results with normalized scores or nil if disabled.
func (v *VectorSearcher) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if !v.Enabled() {
		return nil, nil
	}

	results, err := v.embedder.Embed(ctx, []string{"query: " + query})
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}

	entries, err := v.vecStore.Query(ctx, results[0].Embedding, limit, 0.0)
	if err != nil {
		return nil, err
	}

	// Normalize vector scores to [0, 1]
	var maxSim float32
	for _, e := range entries {
		// Compute cosine similarity for scoring (Query returns entries sorted by similarity)
		if len(results[0].Embedding) > 0 && len(e.Embedding) > 0 {
			sim := vectorstore.CosineSimilarity(results[0].Embedding, e.Embedding)
			if sim > maxSim {
				maxSim = sim
			}
		}
	}

	searchResults := make([]SearchResult, 0, len(entries))
	for _, e := range entries {
		sim := vectorstore.CosineSimilarity(results[0].Embedding, e.Embedding)
		score := float64(sim)
		if maxSim > 0 {
			score /= float64(maxSim)
		}
		searchResults = append(searchResults, SearchResult{
			ContentHash: e.ID,
			Content:     e.Content,
			Score:       score,
			Source:      "vector",
		})
	}
	return searchResults, nil
}
