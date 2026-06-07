package memoryengine

import (
	"context"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"
)

// HybridSearcher orchestrates BM25 + KG [+ Vector if enabled] search.
type HybridSearcher struct {
	bm25   *BM25Searcher
	kg     *GraphSearcher
	vector *VectorSearcher
	store  *MemoryStore
}

// NewHybridSearcher creates a hybrid searcher. vector may be nil.
func NewHybridSearcher(bm25 *BM25Searcher, kg *GraphSearcher, vector *VectorSearcher, store *MemoryStore) *HybridSearcher {
	return &HybridSearcher{bm25: bm25, kg: kg, vector: vector, store: store}
}

// Search runs the full hybrid pipeline:
// 1. Launch BM25, KG, and Vector searches concurrently
// 2. RRF fuse results
// 3. Apply temporal filter
// 4. Hydrate content
// 5. Optionally fetch graph context edges
func (h *HybridSearcher) Search(ctx context.Context, query SearchQuery) (*SearchResultSet, error) {
	start := time.Now()

	limit := query.Limit
	if limit <= 0 {
		limit = 10
	}
	// Fetch more from each pipeline for better RRF fusion
	fetchLimit := limit * 3

	var (
		bm25Results   []SearchResult
		kgResults     []SearchResult
		vectorResults []SearchResult
	)

	g, gCtx := errgroup.WithContext(ctx)

	// BM25 pipeline
	g.Go(func() error {
		results, err := h.bm25.Search(gCtx, query.Text, fetchLimit)
		if err != nil {
			return err
		}
		bm25Results = results
		return nil
	})

	// KG pipeline
	g.Go(func() error {
		results, err := h.kg.Search(gCtx, query)
		if err != nil {
			return err
		}
		kgResults = results
		return nil
	})

	// Vector pipeline (if enabled)
	if h.vector != nil && h.vector.Enabled() {
		g.Go(func() error {
			results, err := h.vector.Search(gCtx, query.Text, fetchLimit)
			if err != nil {
				return err
			}
			vectorResults = results
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	// RRF fusion
	resultLists := [][]SearchResult{bm25Results, kgResults}
	if len(vectorResults) > 0 {
		resultLists = append(resultLists, vectorResults)
	}

	rrfCfg := DefaultRRFConfig()
	rrfCfg.Limit = limit * 2 // Fetch extra for temporal filtering
	fused := Fuse(resultLists, rrfCfg)

	// Temporal filter
	if query.DateFrom != "" || query.DateTo != "" || query.AsOf != "" {
		fused = h.filterByTime(fused, query)
	}

	// Hydrate content for results that only have content_hash
	hashes := make([]string, 0, len(fused))
	for _, r := range fused {
		if r.Content == "" && r.ContentHash != "" {
			hashes = append(hashes, r.ContentHash)
		}
	}
	if len(hashes) > 0 {
		entries, err := h.store.GetByContentHashes(ctx, hashes)
		if err == nil {
			entryMap := make(map[string]MemoryEntry, len(entries))
			for _, e := range entries {
				entryMap[e.ContentHash] = e
			}
			for i, r := range fused {
				if e, ok := entryMap[r.ContentHash]; ok {
					fused[i].Content = e.Content
					fused[i].Date = e.Date
					fused[i].Tags = e.Tags
					fused[i].EventTime = e.EventTime
				}
			}
		}
	}

	// Apply salience boost
	fused = h.applySalience(ctx, fused)

	// Trim to limit
	if len(fused) > limit {
		fused = fused[:limit]
	}

	// Graph context edges for entity queries
	var graphEdges []GraphEdge
	if query.IncludeGraphContext && len(query.Entities) > 0 {
		graphEdges = h.collectGraphContext(ctx, query.Entities, limit-len(fused))
	}

	return &SearchResultSet{
		Results:      fused,
		BM25Count:    len(bm25Results),
		KGCount:      len(kgResults),
		VectorCount:  len(vectorResults),
		GraphEdges:   graphEdges,
		QueryLatency: time.Since(start),
	}, nil
}

// filterByTime filters results based on date range or as_of point.
func (h *HybridSearcher) filterByTime(results []SearchResult, query SearchQuery) []SearchResult {
	var filtered []SearchResult
	for _, r := range results {
		eventTime := r.EventTime
		if eventTime == "" {
			eventTime = r.Date // fallback to date
		}
		if query.AsOf != "" && eventTime > query.AsOf {
			continue
		}
		if query.DateFrom != "" && eventTime < query.DateFrom {
			continue
		}
		if query.DateTo != "" && eventTime > query.DateTo {
			continue
		}
		filtered = append(filtered, r)
	}
	return filtered
}

// applySalience boosts scores based on Ebbinghaus salience.
func (h *HybridSearcher) applySalience(ctx context.Context, results []SearchResult) []SearchResult {
	hashes := make([]string, 0, len(results))
	for _, r := range results {
		if r.ContentHash != "" {
			hashes = append(hashes, r.ContentHash)
		}
	}
	if len(hashes) == 0 {
		return results
	}

	entries, err := h.store.GetByContentHashes(ctx, hashes)
	if err != nil {
		return results
	}
	salienceByHash := make(map[string]float64, len(entries))
	for _, e := range entries {
		salienceByHash[e.ContentHash] = e.Salience
	}

	for i, r := range results {
		if s, ok := salienceByHash[r.ContentHash]; ok && s > 0 {
			results[i].Score *= s
		}
	}
	return results
}

// collectGraphContext fetches graph edges for entities not already covered by search results.
func (h *HybridSearcher) collectGraphContext(ctx context.Context, entities []string, maxExtra int) []GraphEdge {
	if maxExtra <= 0 {
		return nil
	}

	var edges []GraphEdge
	for _, name := range entities {
		n, err := h.kg.store.GetNode(ctx, name)
		if err != nil {
			canon, _ := h.kg.store.ResolveAlias(ctx, name)
			n, err = h.kg.store.GetNode(ctx, canon)
			if err != nil {
				continue
			}
		}
		outEdges, _ := h.kg.store.GetEdgesFrom(ctx, n.ID, false)
		edges = append(edges, outEdges...)
	}

	// Deduplicate by edge ID
	seen := make(map[int64]bool)
	var unique []GraphEdge
	for _, e := range edges {
		if !seen[e.ID] {
			seen[e.ID] = true
			unique = append(unique, e)
		}
	}

	if len(unique) > maxExtra {
		unique = unique[:maxExtra]
	}
	return unique
}

// SearchText is a convenience wrapper that does a simple text search.
func (h *HybridSearcher) SearchText(ctx context.Context, text string, limit int) (*SearchResultSet, error) {
	return h.Search(ctx, SearchQuery{
		Text:  strings.TrimSpace(text),
		Limit: limit,
	})
}
