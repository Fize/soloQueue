package engine

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
		results, err := h.bm25.Search(gCtx, query, fetchLimit)
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
			results, err := h.vector.Search(gCtx, query, fetchLimit)
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

	// Hydrate authoritative content and lifecycle metadata for every result.
	hashes := make([]string, 0, len(fused))
	for _, r := range fused {
		if r.ContentHash != "" {
			hashes = append(hashes, r.ContentHash)
		}
	}
	if len(hashes) > 0 {
		entries, err := h.store.GetByContentHashesOwned(ctx, hashes, query.OwnerType, query.OwnerID)
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
					fused[i].MemoryType = e.MemoryType
					fused[i].ScopeType = e.ScopeType
					fused[i].ScopeID = e.ScopeID
					fused[i].Status = e.Status
					fused[i].ExpiresAt = e.ExpiresAt
					fused[i].SubjectKey = e.SubjectKey
					fused[i].ValidFrom = e.ValidFrom
					fused[i].ValidUntil = e.ValidUntil
					fused[i].SupersedesHash = e.SupersedesHash
				}
			}
		}
	}

	fused = h.filterByLifecycleAndScope(fused, query)

	// Apply salience boost
	fused = h.applySalience(ctx, fused, query.OwnerType, query.OwnerID)

	// Trim to limit
	if len(fused) > limit {
		fused = fused[:limit]
	}

	// Graph context edges for entity queries
	var graphEdges []GraphEdge
	if query.IncludeGraphContext && len(query.Entities) > 0 {
		graphEdges = h.collectGraphContext(ctx, query, limit-len(fused))
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

func (h *HybridSearcher) filterByLifecycleAndScope(results []SearchResult, query SearchQuery) []SearchResult {
	referenceTime := time.Now().UTC()
	if query.AsOf != "" {
		parsed, ok := parseMemoryTime(query.AsOf)
		if !ok {
			return nil
		}
		referenceTime = parsed
	}
	filtered := make([]SearchResult, 0, len(results))
	for _, result := range results {
		if query.AsOf != "" {
			if result.Status != "" && result.Status != StatusActive && result.Status != StatusSuperseded {
				continue
			}
			if result.ValidUntil != "" {
				validUntil, ok := parseMemoryTime(result.ValidUntil)
				if !ok || !referenceTime.Before(validUntil) {
					continue
				}
			}
		} else if result.Status != "" && result.Status != StatusActive {
			continue
		}
		if result.ValidFrom != "" {
			validFrom, ok := parseMemoryTime(result.ValidFrom)
			if !ok || validFrom.After(referenceTime) {
				continue
			}
		}
		if result.ExpiresAt != "" {
			expiresAt, ok := parseMemoryTime(result.ExpiresAt)
			if !ok || !referenceTime.Before(expiresAt) {
				continue
			}
		}
		if query.ScopeType == "" {
			filtered = append(filtered, result)
			continue
		}
		if result.ScopeType == query.ScopeType && result.ScopeID == query.ScopeID {
			filtered = append(filtered, result)
			continue
		}
		if query.IncludeGlobal && result.ScopeType == ScopeGlobal {
			filtered = append(filtered, result)
		}
	}
	return filtered
}

// filterByTime filters results based on date range or as_of point.
func (h *HybridSearcher) filterByTime(results []SearchResult, query SearchQuery) []SearchResult {
	asOf, asOfOK := parseOptionalMemoryTime(query.AsOf)
	dateFrom, dateFromOK := parseOptionalMemoryTime(query.DateFrom)
	dateTo, dateToOK := parseOptionalMemoryTime(query.DateTo)
	if !asOfOK || !dateFromOK || !dateToOK {
		return nil
	}
	var filtered []SearchResult
	for _, r := range results {
		eventTime := r.EventTime
		if eventTime == "" {
			eventTime = r.Date // fallback to date
		}
		event, ok := parseMemoryTime(eventTime)
		if !ok {
			continue
		}
		if query.AsOf != "" && event.After(asOf) {
			continue
		}
		if query.DateFrom != "" && event.Before(dateFrom) {
			continue
		}
		if query.DateTo != "" && event.After(dateTo) {
			continue
		}
		filtered = append(filtered, r)
	}
	return filtered
}

func parseOptionalMemoryTime(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, true
	}
	return parseMemoryTime(value)
}

// applySalience boosts scores based on Ebbinghaus salience.
func (h *HybridSearcher) applySalience(ctx context.Context, results []SearchResult, ownerType, ownerID string) []SearchResult {
	hashes := make([]string, 0, len(results))
	for _, r := range results {
		if r.ContentHash != "" {
			hashes = append(hashes, r.ContentHash)
		}
	}
	if len(hashes) == 0 {
		return results
	}

	entries, err := h.store.GetByContentHashesOwned(ctx, hashes, ownerType, ownerID)
	if err != nil {
		return results
	}
	salienceByHash := make(map[string]float64, len(entries))
	for _, e := range entries {
		referenceTime := e.LastUsedAt
		if referenceTime == "" {
			referenceTime = e.CreatedAt
		}
		reference, ok := parseMemoryTime(referenceTime)
		if !ok {
			salienceByHash[e.ContentHash] = e.Salience
			continue
		}
		days := time.Since(reference).Hours() / 24
		salienceByHash[e.ContentHash] = EbbinghausSalience(e.Salience, days, 30)
	}

	for i, r := range results {
		if s, ok := salienceByHash[r.ContentHash]; ok && s > 0 {
			results[i].Score *= s
		}
	}
	return results
}

func parseMemoryTime(value string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

// collectGraphContext fetches graph edges for entities not already covered by search results.
func (h *HybridSearcher) collectGraphContext(ctx context.Context, query SearchQuery, maxExtra int) []GraphEdge {
	if maxExtra <= 0 {
		return nil
	}

	var edges []GraphEdge
	for _, name := range query.Entities {
		n, err := h.kg.store.GetNodeOwned(ctx, query.OwnerType, query.OwnerID, name)
		if err != nil {
			canon, _ := h.kg.store.ResolveAliasOwned(ctx, query.OwnerType, query.OwnerID, name)
			n, err = h.kg.store.GetNodeOwned(ctx, query.OwnerType, query.OwnerID, canon)
			if err != nil {
				continue
			}
		}
		outEdges, _ := h.kg.store.GetEdgesFromOwned(ctx, query.OwnerType, query.OwnerID, n.ID, false)
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

	hashes := make([]string, 0, len(unique))
	for _, edge := range unique {
		if edge.SourceHash != "" {
			hashes = append(hashes, edge.SourceHash)
		}
	}
	active := h.store.ContentHashesVisibleAtOwned(
		ctx, hashes, query.OwnerType, query.OwnerID, query.ScopeType, query.ScopeID, query.IncludeGlobal, query.AsOf,
	)
	filteredEdges := unique[:0]
	for _, edge := range unique {
		if edge.SourceHash == "" && query.ScopeType == "" {
			filteredEdges = append(filteredEdges, edge)
			continue
		}
		if active[edge.SourceHash] {
			filteredEdges = append(filteredEdges, edge)
		}
	}
	if len(filteredEdges) > maxExtra {
		filteredEdges = filteredEdges[:maxExtra]
	}
	return filteredEdges
}

// SearchText is a convenience wrapper that does a simple text search.
func (h *HybridSearcher) SearchText(ctx context.Context, text string, limit int) (*SearchResultSet, error) {
	return h.Search(ctx, SearchQuery{
		Text:  strings.TrimSpace(text),
		Limit: limit,
	})
}
