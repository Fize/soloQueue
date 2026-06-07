package memoryengine

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
)

// PageRankConfig holds PPR algorithm parameters.
type PageRankConfig struct {
	Damping   float64 // default 0.85
	MaxIters  int     // default 20
	Tolerance float64 // default 1e-8
}

// DefaultPageRankConfig returns sensible defaults.
func DefaultPageRankConfig() PageRankConfig {
	return PageRankConfig{
		Damping:   0.85,
		MaxIters:  20,
		Tolerance: 1e-8,
	}
}

// pprNodeScore maps a node ID to its PPR score.
type pprNodeScore struct {
	nodeID int64
	score  float64
}

// PPR computes Personalized PageRank from seed node IDs.
// Returns node scores sorted by descending PPR value.
func PPR(ctx context.Context, db *sql.DB, seedIDs []int64, config PageRankConfig) ([]pprNodeScore, error) {
	if len(seedIDs) == 0 {
		return nil, nil
	}

	// Build adjacency from kg_edges (undirected for PPR)
	edges, err := loadEdgesForPPR(ctx, db, seedIDs)
	if err != nil {
		return nil, fmt.Errorf("ppr: load edges: %w", err)
	}

	if len(edges) == 0 {
		// No edges — each seed gets uniform score
		scores := make([]pprNodeScore, len(seedIDs))
		for i, id := range seedIDs {
			scores[i] = pprNodeScore{nodeID: id, score: 1.0 / float64(len(seedIDs))}
		}
		return scores, nil
	}

	// Build adjacency map
	adj := make(map[int64][]int64)
	nodeSet := make(map[int64]bool)
	for _, e := range edges {
		adj[e.from] = append(adj[e.from], e.to)
		adj[e.to] = append(adj[e.to], e.from)
		nodeSet[e.from] = true
		nodeSet[e.to] = true
	}

	nodes := make([]int64, 0, len(nodeSet))
	for id := range nodeSet {
		nodes = append(nodes, id)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i] < nodes[j] })

	n := len(nodes)
	if n == 0 {
		return nil, nil
	}

	// Build index for fast lookup
	idx := make(map[int64]int, n)
	for i, id := range nodes {
		idx[id] = i
	}

	// Teleport vector: uniform over seeds
	teleport := make([]float64, n)
	seedSet := make(map[int64]bool)
	for _, sid := range seedIDs {
		seedSet[sid] = true
	}
	for i, id := range nodes {
		if seedSet[id] {
			teleport[i] = 1.0 / float64(len(seedIDs))
		}
	}

	// Out-degree
	outDeg := make([]int, n)
	for i, id := range nodes {
		outDeg[i] = len(adj[id])
	}

	// Power iteration
	pr := make([]float64, n)
	for i := range pr {
		pr[i] = 1.0 / float64(n)
	}

	damping := config.Damping
	for iter := 0; iter < config.MaxIters; iter++ {
		newPR := make([]float64, n)

		// Teleport component
		for i := range newPR {
			newPR[i] = (1.0 - damping) * teleport[i]
		}

		// Transition component
		for i, id := range nodes {
			if pr[i] == 0 || outDeg[i] == 0 {
				// Dangling node: distribute to all nodes uniformly
				if outDeg[i] == 0 && pr[i] > 0 {
					share := damping * pr[i] / float64(n)
					for j := range newPR {
						newPR[j] += share
					}
				}
				continue
			}
			share := damping * pr[i] / float64(outDeg[i])
			for _, neighbor := range adj[id] {
				j := idx[neighbor]
				newPR[j] += share
			}
		}

		// Check convergence
		var maxDiff float64
		for i := range pr {
			diff := math.Abs(newPR[i] - pr[i])
			if diff > maxDiff {
				maxDiff = diff
			}
		}
		pr = newPR
		if maxDiff < config.Tolerance {
			break
		}
	}

	// Collect results
	scores := make([]pprNodeScore, 0, n)
	for i, id := range nodes {
		if pr[i] > 0 {
			scores = append(scores, pprNodeScore{nodeID: id, score: pr[i]})
		}
	}
	sort.Slice(scores, func(i, j int) bool { return scores[i].score > scores[j].score })

	return scores, nil
}

type pprEdge struct {
	from int64
	to   int64
}

// loadEdgesForPPR queries the subgraph reachable from seeds (2-hop neighborhood).
func loadEdgesForPPR(ctx context.Context, db *sql.DB, seedIDs []int64) ([]pprEdge, error) {
	if len(seedIDs) == 0 {
		return nil, nil
	}

	// Build IN clause
	query := `SELECT DISTINCT e.source, e.target FROM kg_edges e
		WHERE (e.valid_until IS NULL OR e.valid_until > datetime('now'))
		AND (e.source IN (` + int64Placeholders(seedIDs) + `) OR e.target IN (` + int64Placeholders(seedIDs) + `))`

	args := make([]interface{}, 0, len(seedIDs)*2)
	for _, id := range seedIDs {
		args = append(args, id)
	}
	for _, id := range seedIDs {
		args = append(args, id)
	}

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var edges []pprEdge
	for rows.Next() {
		var e pprEdge
		if err := rows.Scan(&e.from, &e.to); err != nil {
			return nil, err
		}
		edges = append(edges, e)
	}

	// Also query 2-hop: edges where source or target is connected to seed nodes
	if len(edges) > 0 {
		// Collect all nodes from first query
		nodeSet := make(map[int64]bool)
		for _, e := range edges {
			nodeSet[e.from] = true
			nodeSet[e.to] = true
		}
		nodeIDs := make([]int64, 0, len(nodeSet))
		for id := range nodeSet {
			nodeIDs = append(nodeIDs, id)
		}

		query2 := `SELECT DISTINCT e.source, e.target FROM kg_edges e
			WHERE (e.valid_until IS NULL OR e.valid_until > datetime('now'))
			AND (e.source IN (` + int64Placeholders(nodeIDs) + `) OR e.target IN (` + int64Placeholders(nodeIDs) + `))`

		args2 := make([]interface{}, 0, len(nodeIDs)*2)
		for _, id := range nodeIDs {
			args2 = append(args2, id)
		}
		for _, id := range nodeIDs {
			args2 = append(args2, id)
		}

		rows2, err := db.QueryContext(ctx, query2, args2...)
		if err != nil {
			return nil, err
		}
		defer rows2.Close()

		for rows2.Next() {
			var e pprEdge
			if err := rows2.Scan(&e.from, &e.to); err != nil {
				return nil, err
			}
			edges = append(edges, e)
		}
	}

	return edges, nil
}

// PPRToMemories maps PPR node scores to memory search results via incident edges.
func PPRToMemories(ctx context.Context, db *sql.DB, nodeScores []pprNodeScore, limit int) ([]SearchResult, error) {
	if len(nodeScores) == 0 {
		return nil, nil
	}

	// For each high-scoring node, fetch incident edges and accumulate source_hash scores
	scoresByHash := make(map[string]float64)
	topN := limit * 2
	if topN > len(nodeScores) {
		topN = len(nodeScores)
	}

	for i := 0; i < topN; i++ {
		ns := nodeScores[i]
		edges, err := queryEdgesForNode(ctx, db, ns.nodeID)
		if err != nil {
			continue
		}
		for _, e := range edges {
			if e.sourceHash != "" {
				scoresByHash[e.sourceHash] += e.weight * ns.score
			}
		}
	}

	// Also check the 1-hop neighbors for shared memories
	if len(scoresByHash) < limit {
		for i := 0; i < topN && len(scoresByHash) < limit; i++ {
			ns := nodeScores[i]
			edges, err := queryEdgesForNode(ctx, db, ns.nodeID)
			if err != nil {
				continue
			}
			for _, e := range edges {
				neighborID := e.target
				if neighborID == ns.nodeID {
					neighborID = e.source
				}
				neighborEdges, _ := queryEdgesForNode(ctx, db, neighborID)
				for _, ne := range neighborEdges {
					if ne.sourceHash != "" {
						if _, exists := scoresByHash[ne.sourceHash]; !exists {
							scoresByHash[ne.sourceHash] = ne.weight * ns.score * 0.5
						}
					}
				}
			}
		}
	}

	// Convert to sorted results
	type hashScore struct {
		hash  string
		score float64
	}
	var sorted []hashScore
	for hash, score := range scoresByHash {
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

type simpleEdge struct {
	sourceHash string
	weight     float64
	source     int64
	target     int64
}

func queryEdgesForNode(ctx context.Context, db *sql.DB, nodeID int64) ([]simpleEdge, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT source_hash, weight, source, target FROM kg_edges
		 WHERE (source = ? OR target = ?) AND (valid_until IS NULL OR valid_until > datetime('now'))`,
		nodeID, nodeID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var edges []simpleEdge
	for rows.Next() {
		var e simpleEdge
		if err := rows.Scan(&e.sourceHash, &e.weight, &e.source, &e.target); err != nil {
			return nil, err
		}
		edges = append(edges, e)
	}
	return edges, rows.Err()
}

func int64Placeholders(ids []int64) string {
	if len(ids) == 0 {
		return ""
	}
	s := "?"
	for i := 1; i < len(ids); i++ {
		s += ",?"
	}
	return s
}
