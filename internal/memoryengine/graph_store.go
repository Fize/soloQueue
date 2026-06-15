package memoryengine

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// GraphStore manages KG nodes, edges, and aliases.
type GraphStore struct {
	db  *sql.DB
	mu  *sync.Mutex
	log *logger.Logger
}

// NewGraphStore creates a GraphStore backed by the shared database.
func NewGraphStore(db *sql.DB, mu *sync.Mutex, log *logger.Logger) *GraphStore {
	return &GraphStore{db: db, mu: mu, log: log}
}

// UpsertNode inserts or updates an entity node. Returns the node ID and whether it was new.
func (g *GraphStore) UpsertNode(ctx context.Context, name, nodeType string, confidence float64) (int64, bool, error) {
	if confidence <= 0 {
		confidence = 1.0
	}
	now := nowUTC()

	g.mu.Lock()
	defer g.mu.Unlock()

	// Try update first
	res, err := g.db.ExecContext(ctx,
		`UPDATE kg_nodes SET mention_count = mention_count + 1, last_seen = ?,
		 confidence = (confidence + ?) / 2.0, type = CASE WHEN type = 'entity' THEN ? ELSE type END
		 WHERE LOWER(name) = LOWER(?)`,
		now, confidence, nodeType, name,
	)
	if err != nil {
		return 0, false, fmt.Errorf("graph upsert node: %w", err)
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected > 0 {
		var id int64
		err := g.db.QueryRowContext(ctx, `SELECT id FROM kg_nodes WHERE LOWER(name) = LOWER(?)`, name).Scan(&id)
		return id, false, err
	}

	// Insert new
	result, err := g.db.ExecContext(ctx,
		`INSERT INTO kg_nodes (name, type, mention_count, first_seen, last_seen, confidence)
		 VALUES (?, ?, 1, ?, ?, ?)`,
		name, nodeType, now, now, confidence,
	)
	if err != nil {
		return 0, false, fmt.Errorf("graph upsert node: %w", err)
	}
	id, _ := result.LastInsertId()
	return id, true, nil
}

// GetNode returns a node by name.
func (g *GraphStore) GetNode(ctx context.Context, name string) (*GraphNode, error) {
	var n GraphNode
	err := g.db.QueryRowContext(ctx,
		`SELECT id, name, type, mention_count, first_seen, last_seen, confidence
		 FROM kg_nodes WHERE name = ?`, name,
	).Scan(&n.ID, &n.Name, &n.Type, &n.MentionCount, &n.FirstSeen, &n.LastSeen, &n.Confidence)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

// GetNodeByID returns a node by its internal ID.
func (g *GraphStore) GetNodeByID(ctx context.Context, id int64) (*GraphNode, error) {
	var n GraphNode
	err := g.db.QueryRowContext(ctx,
		`SELECT id, name, type, mention_count, first_seen, last_seen, confidence
		 FROM kg_nodes WHERE id = ?`, id,
	).Scan(&n.ID, &n.Name, &n.Type, &n.MentionCount, &n.FirstSeen, &n.LastSeen, &n.Confidence)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

// UpsertEdge inserts or updates a relationship edge. On conflict (same source, target, rel_type),
// the weight is averaged with the old weight.
func (g *GraphStore) UpsertEdge(ctx context.Context, sourceID, targetID int64, relType string, weight float64, evidence, sourceHash, eventTime string, validFrom, validUntil string) error {
	if weight <= 0 {
		weight = 1.0
	}
	now := nowUTC()

	g.mu.Lock()
	defer g.mu.Unlock()

	_, err := g.db.ExecContext(ctx,
		`INSERT INTO kg_edges (source, target, rel_type, weight, evidence, source_hash, event_time, valid_from, valid_until, last_reinforced)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(source, target, rel_type) DO UPDATE SET
		   weight = (weight + excluded.weight) / 2.0,
		   evidence = excluded.evidence,
		   source_hash = excluded.source_hash,
		   last_reinforced = excluded.last_reinforced`,
		sourceID, targetID, relType, weight, evidence, sourceHash, eventTime, validFrom, validUntil, now,
	)
	if err != nil {
		return fmt.Errorf("graph upsert edge: %w", err)
	}
	return nil
}

// GetEdgesFrom returns all outgoing edges from a node, excluding expired ones.
func (g *GraphStore) GetEdgesFrom(ctx context.Context, nodeID int64, includeHistorical bool) ([]GraphEdge, error) {
	query := `SELECT e.id, e.source, e.target, sn.name, tn.name, e.rel_type, e.weight, e.evidence,
		e.source_hash, e.event_time, e.valid_from, e.valid_until
		FROM kg_edges e
		JOIN kg_nodes sn ON e.source = sn.id
		JOIN kg_nodes tn ON e.target = tn.id
		WHERE e.source = ?`
	if !includeHistorical {
		query += ` AND (e.valid_until IS NULL OR e.valid_until > datetime('now'))`
	}
	query += ` ORDER BY e.weight DESC`

	return g.queryEdges(ctx, query, nodeID)
}

// GetEdgesTo returns all incoming edges to a node, excluding expired ones.
func (g *GraphStore) GetEdgesTo(ctx context.Context, nodeID int64, includeHistorical bool) ([]GraphEdge, error) {
	query := `SELECT e.id, e.source, e.target, sn.name, tn.name, e.rel_type, e.weight, e.evidence,
		e.source_hash, e.event_time, e.valid_from, e.valid_until
		FROM kg_edges e
		JOIN kg_nodes sn ON e.source = sn.id
		JOIN kg_nodes tn ON e.target = tn.id
		WHERE e.target = ?`
	if !includeHistorical {
		query += ` AND (e.valid_until IS NULL OR e.valid_until > datetime('now'))`
	}
	query += ` ORDER BY e.weight DESC`

	return g.queryEdges(ctx, query, nodeID)
}

// GetAllEdges returns all edges, optionally filtered to current-valid only.
func (g *GraphStore) GetAllEdges(ctx context.Context, includeHistorical bool) ([]GraphEdge, error) {
	query := `SELECT e.id, e.source, e.target, sn.name, tn.name, e.rel_type, e.weight, e.evidence,
		e.source_hash, e.event_time, e.valid_from, e.valid_until
		FROM kg_edges e
		JOIN kg_nodes sn ON e.source = sn.id
		JOIN kg_nodes tn ON e.target = tn.id`
	if !includeHistorical {
		query += ` WHERE e.valid_until IS NULL OR e.valid_until > datetime('now')`
	}
	query += ` ORDER BY e.weight DESC`

	return g.queryEdges(ctx, query)
}

// GetEdgesBySourceHash returns all edges linked to a memory via source_hash.
func (g *GraphStore) GetEdgesBySourceHash(ctx context.Context, sourceHash string) ([]GraphEdge, error) {
	return g.queryEdges(ctx,
		`SELECT e.id, e.source, e.target, sn.name, tn.name, e.rel_type, e.weight, e.evidence,
		e.source_hash, e.event_time, e.valid_from, e.valid_until
		FROM kg_edges e
		JOIN kg_nodes sn ON e.source = sn.id
		JOIN kg_nodes tn ON e.target = tn.id
		WHERE e.source_hash = ?
		ORDER BY e.weight DESC`,
		sourceHash,
	)
}

// BFS performs breadth-first traversal from a seed entity.
// maxHops controls depth (default 2). maxDegree caps expansion per node (0 = no cap).
const bfsMaxTotalNodes = 200

func (g *GraphStore) BFS(ctx context.Context, seedName string, maxHops, maxDegree int) (nodes []GraphNode, edges []GraphEdge, err error) {
	if maxHops <= 0 {
		maxHops = 2
	}

	seed, err := g.resolveName(ctx, seedName)
	if err != nil {
		return nil, nil, fmt.Errorf("bfs: resolve seed %q: %w", seedName, err)
	}

	visited := map[int64]bool{seed.ID: true}
	queue := []int64{seed.ID}
	allNodes := make([]GraphNode, 0, 16)
	allNodes = append(allNodes, *seed)
	allEdges := make([]GraphEdge, 0, 32)

	for hop := 0; hop < maxHops && len(queue) > 0; hop++ {
		var nextQueue []int64
		for _, nodeID := range queue {
			if len(allNodes)+len(nextQueue) >= bfsMaxTotalNodes {
				return allNodes, allEdges, nil
			}

			neighbors, err := g.GetEdgesFrom(ctx, nodeID, false)
			if err != nil {
				return nil, nil, err
			}

			if maxDegree > 0 && len(neighbors) > maxDegree {
				neighbors = neighbors[:maxDegree]
			}

			for _, e := range neighbors {
				allEdges = append(allEdges, e)
				if !visited[e.Target] {
					visited[e.Target] = true
					n, err := g.GetNodeByID(ctx, e.Target)
					if err == nil {
						allNodes = append(allNodes, *n)
					}
					nextQueue = append(nextQueue, e.Target)
				}
			}
		}
		queue = nextQueue
	}

	return allNodes, allEdges, nil
}

// ShortestPath finds the shortest path between two entities via BFS.
func (g *GraphStore) ShortestPath(ctx context.Context, sourceName, targetName string, maxDepth int) (nodes []GraphNode, edges []GraphEdge, err error) {
	if maxDepth <= 0 {
		maxDepth = 5
	}

	src, err := g.resolveName(ctx, sourceName)
	if err != nil {
		return nil, nil, fmt.Errorf("shortest path: source %q: %w", sourceName, err)
	}
	tgt, err := g.resolveName(ctx, targetName)
	if err != nil {
		return nil, nil, fmt.Errorf("shortest path: target %q: %w", targetName, err)
	}

	if src.ID == tgt.ID {
		return []GraphNode{*src}, nil, nil
	}

	// BFS with path tracking
	type pathState struct {
		nodeID int64
		path   []int64 // edge IDs
		nodes  []int64 // node IDs
	}

	visited := map[int64]bool{src.ID: true}
	queue := []pathState{{nodeID: src.ID, nodes: []int64{src.ID}}}

	for depth := 0; depth < maxDepth && len(queue) > 0; depth++ {
		var nextQueue []pathState
		for _, state := range queue {
			neighbors, err := g.GetEdgesFrom(ctx, state.nodeID, false)
			if err != nil {
				return nil, nil, err
			}
			for _, e := range neighbors {
				if visited[e.Target] {
					continue
				}
				newPath := make([]int64, len(state.path)+1)
				copy(newPath, state.path)
				newPath[len(state.path)] = e.ID

				newNodes := make([]int64, len(state.nodes)+1)
				copy(newNodes, state.nodes)
				newNodes[len(state.nodes)] = e.Target

				if e.Target == tgt.ID {
					// Build result
					for _, nid := range newNodes {
						n, err := g.GetNodeByID(ctx, nid)
						if err == nil {
							nodes = append(nodes, *n)
						}
					}
					for _, eid := range newPath {
						// Re-query edge by ID (simplified — could batch)
						allEdges, _ := g.queryEdges(ctx,
							`SELECT e.id, e.source, e.target, sn.name, tn.name, e.rel_type, e.weight, e.evidence,
							e.source_hash, e.event_time, e.valid_from, e.valid_until
							FROM kg_edges e JOIN kg_nodes sn ON e.source = sn.id JOIN kg_nodes tn ON e.target = tn.id
							WHERE e.id = ?`, eid)
						if len(allEdges) > 0 {
							edges = append(edges, allEdges[0])
						}
					}
					return nodes, edges, nil
				}

				visited[e.Target] = true
				nextQueue = append(nextQueue, pathState{nodeID: e.Target, path: newPath, nodes: newNodes})
			}
		}
		queue = nextQueue
	}

	return nil, nil, fmt.Errorf("no path found between %q and %q within %d hops", sourceName, targetName, maxDepth)
}

// AddAlias registers an alias for a canonical entity name.
func (g *GraphStore) AddAlias(ctx context.Context, alias, canonical string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	_, err := g.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO kg_aliases (alias, canonical) VALUES (?, ?)`,
		alias, canonical,
	)
	return err
}

// ResolveAlias resolves an alias to its canonical name. Returns the input unchanged if no alias exists.
func (g *GraphStore) ResolveAlias(ctx context.Context, alias string) (string, error) {
	var canonical string
	err := g.db.QueryRowContext(ctx,
		`SELECT canonical FROM kg_aliases WHERE alias = ?`, alias,
	).Scan(&canonical)
	if err == sql.ErrNoRows {
		return alias, nil
	}
	return canonical, err
}

// MergeEntities merges source entity into target: re-points edges, adds alias, deletes source.
func (g *GraphStore) MergeEntities(ctx context.Context, sourceName, targetName string) error {
	src, err := g.GetNode(ctx, sourceName)
	if err != nil {
		return fmt.Errorf("merge: source %q: %w", sourceName, err)
	}
	tgt, err := g.GetNode(ctx, targetName)
	if err != nil {
		return fmt.Errorf("merge: target %q: %w", targetName, err)
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	tx, err := g.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("merge: begin tx: %w", err)
	}
	defer tx.Rollback()

	// Re-point outgoing edges
	_, err = tx.ExecContext(ctx,
		`UPDATE kg_edges SET source = ? WHERE source = ?`, tgt.ID, src.ID)
	if err != nil {
		return fmt.Errorf("merge: repoint outgoing: %w", err)
	}

	// Re-point incoming edges
	_, err = tx.ExecContext(ctx,
		`UPDATE kg_edges SET target = ? WHERE target = ?`, tgt.ID, src.ID)
	if err != nil {
		return fmt.Errorf("merge: repoint incoming: %w", err)
	}

	// Add alias
	_, err = tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO kg_aliases (alias, canonical) VALUES (?, ?)`, sourceName, targetName)
	if err != nil {
		return fmt.Errorf("merge: add alias: %w", err)
	}

	// Delete source node
	_, err = tx.ExecContext(ctx, `DELETE FROM kg_nodes WHERE id = ?`, src.ID)
	if err != nil {
		return fmt.Errorf("merge: delete source: %w", err)
	}

	return tx.Commit()
}

// ApplyConfidenceDecay reduces edge weights using half-life decay.
// weight *= 0.5^(days_since_reinforced / halfLifeDays)
func (g *GraphStore) ApplyConfidenceDecay(ctx context.Context, halfLifeDays float64) (int, error) {
	if halfLifeDays <= 0 {
		halfLifeDays = 30
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	result, err := g.db.ExecContext(ctx,
		`UPDATE kg_edges SET weight = weight * POWER(0.5,
		 (julianday('now') - julianday(COALESCE(NULLIF(last_reinforced, ''), valid_from, '1970-01-01'))) / ?)`,
		halfLifeDays,
	)
	if err != nil {
		return 0, fmt.Errorf("decay: %w", err)
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

// GetConnectedComponents finds entity communities via BFS flood-fill.
func (g *GraphStore) GetConnectedComponents(ctx context.Context) ([][]GraphNode, error) {
	edges, err := g.GetAllEdges(ctx, false)
	if err != nil {
		return nil, err
	}

	// Build adjacency
	adj := make(map[int64][]int64)
	nodeSet := make(map[int64]bool)
	for _, e := range edges {
		adj[e.Source] = append(adj[e.Source], e.Target)
		adj[e.Target] = append(adj[e.Target], e.Source)
		nodeSet[e.Source] = true
		nodeSet[e.Target] = true
	}

	visited := make(map[int64]bool)
	var components [][]GraphNode

	for id := range nodeSet {
		if visited[id] {
			continue
		}
		// BFS for this component
		queue := []int64{id}
		visited[id] = true
		var component []GraphNode

		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]

			n, err := g.GetNodeByID(ctx, current)
			if err == nil {
				component = append(component, *n)
			}

			for _, neighbor := range adj[current] {
				if !visited[neighbor] {
					visited[neighbor] = true
					queue = append(queue, neighbor)
				}
			}
		}
		if len(component) > 0 {
			components = append(components, component)
		}
	}

	return components, nil
}

// ListEntities returns the top entities by mention count.
func (g *GraphStore) ListEntities(ctx context.Context, topN int) ([]GraphNode, error) {
	if topN <= 0 {
		topN = 50
	}
	rows, err := g.db.QueryContext(ctx,
		`SELECT id, name, type, mention_count, first_seen, last_seen, confidence
		 FROM kg_nodes ORDER BY mention_count DESC LIMIT ?`, topN,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []GraphNode
	for rows.Next() {
		var n GraphNode
		if err := rows.Scan(&n.ID, &n.Name, &n.Type, &n.MentionCount, &n.FirstSeen, &n.LastSeen, &n.Confidence); err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

// SearchNodesByName finds entities whose name contains the given fragment.
func (g *GraphStore) SearchNodesByName(ctx context.Context, fragment string, limit int) ([]GraphNode, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := g.db.QueryContext(ctx,
		`SELECT id, name, type, mention_count, first_seen, last_seen, confidence
		 FROM kg_nodes WHERE name LIKE ? ORDER BY mention_count DESC LIMIT ?`,
		"%"+fragment+"%", limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []GraphNode
	for rows.Next() {
		var n GraphNode
		if err := rows.Scan(&n.ID, &n.Name, &n.Type, &n.MentionCount, &n.FirstSeen, &n.LastSeen, &n.Confidence); err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

// resolveName resolves an entity name through aliases.
func (g *GraphStore) resolveName(ctx context.Context, name string) (*GraphNode, error) {
	// Try direct lookup first
	n, err := g.GetNode(ctx, name)
	if err == nil {
		return n, nil
	}

	// Try alias resolution
	canonical, err := g.ResolveAlias(ctx, name)
	if err != nil || canonical == name {
		return nil, fmt.Errorf("entity %q not found", name)
	}

	return g.GetNode(ctx, canonical)
}

// queryEdges is a helper that executes a query and scans GraphEdge results.
func (g *GraphStore) queryEdges(ctx context.Context, query string, args ...interface{}) ([]GraphEdge, error) {
	rows, err := g.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	edges := make([]GraphEdge, 0, 16)
	for rows.Next() {
		var e GraphEdge
		var validFrom, validUntil sql.NullString
		if err := rows.Scan(&e.ID, &e.Source, &e.Target, &e.SourceName, &e.TargetName,
			&e.RelType, &e.Weight, &e.Evidence, &e.SourceHash, &e.EventTime,
			&validFrom, &validUntil); err != nil {
			return nil, err
		}
		if validFrom.Valid {
			e.ValidFrom = validFrom.String
		}
		if validUntil.Valid {
			e.ValidUntil = validUntil.String
		}
		edges = append(edges, e)
	}
	return edges, rows.Err()
}

func nowUTC() string { return time.Now().UTC().Format(time.RFC3339) }

// EbbinghausSalience computes salience with the Ebbinghaus forgetting curve.
// salience(t) = S0 * e^(-t / halfLife)
func EbbinghausSalience(currentSalience float64, daysSinceRecall float64, halfLifeDays float64) float64 {
	if halfLifeDays <= 0 {
		halfLifeDays = 30
	}
	if daysSinceRecall <= 0 {
		return currentSalience
	}
	return currentSalience * math.Exp(-daysSinceRecall/halfLifeDays)
}
