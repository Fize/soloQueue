package memoryengine

import (
	"context"
	"fmt"
	"strings"
)

// Consolidator performs memory maintenance operations.
type Consolidator struct {
	mem   *MemoryStore
	graph *GraphStore
}

// NewConsolidator creates a consolidator.
func NewConsolidator(mem *MemoryStore, graph *GraphStore) *Consolidator {
	return &Consolidator{mem: mem, graph: graph}
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

	// 2. Detect stale memories (low salience, old)
	staleRemoved, err := c.removeStaleMemories(ctx, staleMemoryDays)
	if err != nil {
		return report, fmt.Errorf("consolidate stale: %w", err)
	}
	report.StaleMemoriesRemoved = staleRemoved

	// 3. Find connected components
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
