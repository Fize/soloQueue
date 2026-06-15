package memoryengine

import (
	"context"
	"fmt"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// Consolidator performs memory maintenance operations.
type Consolidator struct {
	mem   *MemoryStore
	graph *GraphStore
	log   *logger.Logger
}

// NewConsolidator creates a consolidator.
func NewConsolidator(mem *MemoryStore, graph *GraphStore, log *logger.Logger) *Consolidator {
	return &Consolidator{mem: mem, graph: graph, log: log}
}

// Run performs all consolidation steps and returns a report.
func (c *Consolidator) Run(ctx context.Context, halfLifeDays float64, staleMemoryDays int) (*ConsolidationReport, error) {
	report := &ConsolidationReport{}

	// 1. Apply confidence decay on edges
	decayed, err := c.graph.ApplyConfidenceDecay(ctx, halfLifeDays)
	if err != nil {
		return report, fmt.Errorf("consolidate decay: %w", err)
	}
	report.EdgesDecayed = decayed

	// 2. Deduplicate entities (case-insensitive name merging)
	merged, err := c.deduplicateEntities(ctx)
	if err != nil {
		return report, fmt.Errorf("consolidate dedup: %w", err)
	}
	report.EntitiesMerged = merged

	// 3. Detect stale memories (low salience, old)
	staleRemoved, err := c.removeStaleMemories(ctx, staleMemoryDays)
	if err != nil {
		return report, fmt.Errorf("consolidate stale: %w", err)
	}
	report.StaleMemoriesRemoved = staleRemoved

	// 4. Find connected components
	components, err := c.graph.GetConnectedComponents(ctx)
	if err != nil {
		return report, fmt.Errorf("consolidate communities: %w", err)
	}
	report.CommunitiesFound = len(components)
	for _, comp := range components {
		names := make([]string, len(comp))
		for i, n := range comp {
			names[i] = n.Name
		}
		report.Communities = append(report.Communities, names)
	}

	return report, nil
}

// deduplicateEntities finds case-insensitive duplicate entity names and merges them.
// The entity with the highest mention_count is kept as the canonical name.
func (c *Consolidator) deduplicateEntities(ctx context.Context) (int, error) {
	// Find case-insensitive duplicates
	rows, err := c.graph.db.QueryContext(ctx,
		`SELECT LOWER(name) as normalized, COUNT(*) as cnt
		 FROM kg_nodes
		 GROUP BY LOWER(name)
		 HAVING cnt > 1`)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return 0, nil
		}
		return 0, err
	}
	defer rows.Close()

	var normalizeds []string
	for rows.Next() {
		var norm string
		var cnt int
		if err := rows.Scan(&norm, &cnt); err != nil {
			return 0, err
		}
		normalizeds = append(normalizeds, norm)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(normalizeds) == 0 {
		return 0, nil
	}

	total := 0
	for _, norm := range normalizeds {
		n, err := c.mergeDuplicates(ctx, norm)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

// mergeDuplicates merges one group of case-insensitive duplicates for the given normalized name.
func (c *Consolidator) mergeDuplicates(ctx context.Context, normalized string) (int, error) {
	rows, err := c.graph.db.QueryContext(ctx,
		`SELECT id, name, mention_count
		 FROM kg_nodes
		 WHERE LOWER(name) = ?
		 ORDER BY mention_count DESC`, normalized)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type node struct {
		id           int64
		name         string
		mentionCount int
	}
	var nodes []node
	for rows.Next() {
		var n node
		if err := rows.Scan(&n.id, &n.name, &n.mentionCount); err != nil {
			return 0, err
		}
		nodes = append(nodes, n)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(nodes) <= 1 {
		return 0, nil
	}

	// First entry is the winner (highest mention_count from ORDER BY DESC)
	winner := nodes[0].name
	merged := 0
	for _, loser := range nodes[1:] {
		if err := c.graph.MergeEntities(ctx, loser.name, winner); err != nil {
			return merged, fmt.Errorf("merge %q into %q: %w", loser.name, winner, err)
		}
		merged++
		if c.log != nil {
			c.log.InfoContext(ctx, logger.CatApp, "merged duplicate entity",
				"loser", loser.name, "winner", winner)
		}
	}
	return merged, nil
}

// removeStaleMemories removes memories with very low salience older than the threshold.
func (c *Consolidator) removeStaleMemories(ctx context.Context, olderThanDays int) (int, error) {
	if olderThanDays <= 0 {
		olderThanDays = 90
	}

	// Find and delete entries with salience below threshold and older than cutoff
	c.graph.mu.Lock()
	defer c.graph.mu.Unlock()

	result, err := c.graph.db.ExecContext(ctx,
		`DELETE FROM mem_entries WHERE salience < 0.1
		 AND julianday('now') - julianday(event_time) > ?`, olderThanDays,
	)
	if err != nil {
		if strings.Contains(err.Error(), "no such column") {
			return 0, nil // Table might not exist yet
		}
		return 0, err
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}
