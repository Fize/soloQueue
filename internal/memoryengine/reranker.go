package memoryengine

import "sort"

// RRFConfig holds Reciprocal Rank Fusion parameters.
type RRFConfig struct {
	K     int // rank constant, default 60
	Limit int // top-N to return
}

// DefaultRRFConfig returns sensible defaults.
func DefaultRRFConfig() RRFConfig {
	return RRFConfig{K: 60, Limit: 10}
}

// Fuse merges multiple ranked result lists using Reciprocal Rank Fusion.
// Each result list contributes 1/(K + rank_pos + 1) to the score of its items.
// Items are deduplicated by content_hash (with fallback to content text).
func Fuse(resultLists [][]SearchResult, cfg RRFConfig) []SearchResult {
	if len(resultLists) == 0 {
		return nil
	}

	// Accumulate RRF scores by content_hash
	type scored struct {
		result    SearchResult
		totalScore float64
		sources    []string
	}
	accum := make(map[string]*scored)

	for _, list := range resultLists {
		for rank, item := range list {
			rrfScore := 1.0 / (float64(cfg.K) + float64(rank) + 1.0)
			key := item.ContentHash
			if key == "" {
				key = item.Content // fallback
			}

			if existing, ok := accum[key]; ok {
				existing.totalScore += rrfScore
				existing.sources = append(existing.sources, item.Source)
				// Keep the result with more content
				if item.Content != "" && existing.result.Content == "" {
					existing.result = item
				}
			} else {
				accum[key] = &scored{
					result:     item,
					totalScore: rrfScore,
					sources:    []string{item.Source},
				}
			}
		}
	}

	// Sort by descending score
	var sorted []scored
	for _, s := range accum {
		sorted = append(sorted, *s)
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].totalScore > sorted[j].totalScore
	})

	// Return top N
	limit := cfg.Limit
	if limit <= 0 || limit > len(sorted) {
		limit = len(sorted)
	}

	results := make([]SearchResult, limit)
	for i := 0; i < limit; i++ {
		results[i] = sorted[i].result
		results[i].Score = sorted[i].totalScore
	}

	return results
}
