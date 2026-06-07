package memoryengine

import (
	"context"
	"sort"
	"strings"
)

// GraphSearcher performs KG-based memory retrieval.
type GraphSearcher struct {
	store *GraphStore
}

// NewGraphSearcher creates a graph searcher.
func NewGraphSearcher(store *GraphStore) *GraphSearcher {
	return &GraphSearcher{store: store}
}

// Search retrieves memories via the knowledge graph.
// If entities are provided, uses PPR from those entities.
// Otherwise falls back to token-matching against KG node names.
func (gs *GraphSearcher) Search(ctx context.Context, query SearchQuery) ([]SearchResult, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = 10
	}

	if len(query.Entities) > 0 {
		return gs.entitySearch(ctx, query.Entities, limit)
	}
	return gs.tokenFallbackSearch(ctx, query.Text, limit)
}

// entitySearch resolves entity names and runs PPR to find related memories.
func (gs *GraphSearcher) entitySearch(ctx context.Context, entities []string, limit int) ([]SearchResult, error) {
	// Resolve entities to node IDs
	nodeMap := make(map[int64]bool)
	for _, name := range entities {
		canon, err := gs.store.ResolveAlias(ctx, name)
		if err != nil {
			canon = name
		}
		node, err := gs.store.GetNode(ctx, canon)
		if err != nil {
			continue
		}
		nodeMap[node.ID] = true
	}

	if len(nodeMap) == 0 {
		return nil, nil
	}

	seedIDs := make([]int64, 0, len(nodeMap))
	for id := range nodeMap {
		seedIDs = append(seedIDs, id)
	}

	// Run PPR. We need access to the db.
	// The GraphStore has access via gs.store.db, but it's not exported.
	// We'll traverse through GraphStore's BFS and collect connected memories.
	// For a full PPR implementation, see pagerank.go's PPR/PPRToMemories.

	// Simpler approach for entity search: BFS from seeds, collect edges, score by weight
	scoreByHash := make(map[string]float64)
	seen := make(map[int64]bool)

	for _, seedID := range seedIDs {
		edges, err := gs.store.GetEdgesFrom(ctx, seedID, false)
		if err != nil {
			continue
		}
		seen[seedID] = true
		for _, e := range edges {
			if e.SourceHash != "" {
				scoreByHash[e.SourceHash] += e.Weight
			}
			// 1-hop
			if !seen[e.Target] {
				seen[e.Target] = true
				neighborEdges, _ := gs.store.GetEdgesFrom(ctx, e.Target, false)
				for _, ne := range neighborEdges {
					if ne.SourceHash != "" {
						scoreByHash[ne.SourceHash] += ne.Weight * 0.5
					}
				}
			}
		}
		// Incoming edges
		inEdges, _ := gs.store.GetEdgesTo(ctx, seedID, false)
		for _, e := range inEdges {
			if e.SourceHash != "" {
				scoreByHash[e.SourceHash] += e.Weight * 0.7
			}
		}
	}

	// Convert to sorted results
	type hashScore struct {
		hash  string
		score float64
	}
	var sorted []hashScore
	for hash, score := range scoreByHash {
		sorted = append(sorted, hashScore{hash: hash, score: score})
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].score > sorted[j].score })

	results := make([]SearchResult, 0, limit)
	for i := 0; i < len(sorted) && i < limit; i++ {
		results = append(results, SearchResult{
			ContentHash: sorted[i].hash,
			Score:       sorted[i].score,
			Source:      "kg",
		})
	}
	return results, nil
}

// tokenFallbackSearch tokenizes the query, matches entity names, traverses, and collects memories.
func (gs *GraphSearcher) tokenFallbackSearch(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	// Tokenize and filter stopwords
	tokens := tokenize(query)
	var keywords []string
	for _, tok := range tokens {
		tok = strings.ToLower(strings.TrimSpace(tok))
		if len(tok) < 2 || isStopword(tok) {
			continue
		}
		keywords = append(keywords, tok)
	}

	if len(keywords) == 0 {
		return nil, nil
	}

	// Match against entity names
	var matchedNodes []GraphNode
	seen := make(map[int64]bool)
	for _, kw := range keywords {
		nodes, err := gs.store.SearchNodesByName(ctx, kw, 10)
		if err != nil {
			continue
		}
		for _, n := range nodes {
			if !seen[n.ID] {
				seen[n.ID] = true
				matchedNodes = append(matchedNodes, n)
			}
		}
	}

	if len(matchedNodes) == 0 {
		return nil, nil
	}

	// Exclude self-entity (highest mention_count node)
	selfID := gs.findSelfEntity(matchedNodes)
	filtered := make([]int64, 0, len(matchedNodes))
	for _, n := range matchedNodes {
		if n.ID != selfID {
			filtered = append(filtered, n.ID)
		}
	}

	// BFS from matched nodes to collect memories
	scoreByHash := make(map[string]float64)
	visited := make(map[int64]bool)

	for _, nodeID := range filtered {
		if visited[nodeID] {
			continue
		}
		visited[nodeID] = true

		edges, err := gs.store.GetEdgesFrom(ctx, nodeID, false)
		if err != nil {
			continue
		}
		for _, e := range edges {
			if e.SourceHash != "" {
				scoreByHash[e.SourceHash] += e.Weight
			}
		}
		inEdges, _ := gs.store.GetEdgesTo(ctx, nodeID, false)
		for _, e := range inEdges {
			if e.SourceHash != "" {
				scoreByHash[e.SourceHash] += e.Weight * 0.7
			}
		}
	}

	type hashScore struct {
		hash  string
		score float64
	}
	var sorted []hashScore
	for hash, score := range scoreByHash {
		sorted = append(sorted, hashScore{hash: hash, score: score})
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].score > sorted[j].score })

	results := make([]SearchResult, 0, limit)
	for i := 0; i < len(sorted) && i < limit; i++ {
		results = append(results, SearchResult{
			ContentHash: sorted[i].hash,
			Score:       sorted[i].score,
			Source:      "kg",
		})
	}
	return results, nil
}

// findSelfEntity returns the ID of the node with the highest mention_count.
func (gs *GraphSearcher) findSelfEntity(nodes []GraphNode) int64 {
	var maxCount int
	var selfID int64
	for _, n := range nodes {
		if n.MentionCount > maxCount {
			maxCount = n.MentionCount
			selfID = n.ID
		}
	}
	return selfID
}

// tokenize splits text into word tokens.
func tokenize(text string) []string {
	var tokens []string
	var current []rune
	for _, r := range text {
		if r == ' ' || r == '\t' || r == '\n' || r == ',' || r == '.' || r == '!' || r == '?' || r == ';' || r == ':' {
			if len(current) > 0 {
				tokens = append(tokens, string(current))
				current = current[:0]
			}
		} else {
			current = append(current, r)
		}
	}
	if len(current) > 0 {
		tokens = append(tokens, string(current))
	}
	// Also try bigrams for CJK characters
	var bigrams []string
	for _, tok := range tokens {
		runes := []rune(tok)
		if len(runes) > 1 {
			for i := 0; i < len(runes)-1; i++ {
				bigrams = append(bigrams, string(runes[i:i+2]))
			}
		}
	}
	return append(tokens, bigrams...)
}
