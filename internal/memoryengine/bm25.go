package memoryengine

import "context"

// BM25Searcher wraps FTS5 BM25 search with score normalization.
type BM25Searcher struct {
	store *MemoryStore
}

// NewBM25Searcher creates a BM25 searcher.
func NewBM25Searcher(store *MemoryStore) *BM25Searcher {
	return &BM25Searcher{store: store}
}

// Search runs BM25 and returns normalized results.
func (b *BM25Searcher) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	results, _, err := b.store.BM25Search(ctx, query, limit)
	return results, err
}
