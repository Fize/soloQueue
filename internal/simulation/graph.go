package simulation

import (
	"fmt"
	"strings"
	"sync"
)

// RelationType classifies the nature of an agent-to-agent interaction.
type RelationType string

const (
	RelMention  RelationType = "mention"
	RelRebuttal RelationType = "rebuttal"
	RelAgree    RelationType = "agree"
	RelPropose  RelationType = "propose"
	RelReply    RelationType = "reply"
)

// RelationEdge is a directed edge between two agents.
type RelationEdge struct {
	Source  string       `json:"source"`
	Target  string       `json:"target"`
	Type    RelationType `json:"type"`
	Weight  int          `json:"weight"`
	Rounds  []int        `json:"rounds"`
	Evidence []string     `json:"-"` // raw message excerpts
}

// RelationGraph tracks agent-to-agent interactions during simulation.
// Thread-safe, built incrementally from parsed messages.
type RelationGraph struct {
	nodes map[string]struct{}
	edges map[string]*RelationEdge // key: "source->target:type"
	mu    sync.RWMutex
}

func NewRelationGraph() *RelationGraph {
	return &RelationGraph{
		nodes: make(map[string]struct{}),
		edges: make(map[string]*RelationEdge),
	}
}

// AddNode registers an agent in the graph.
func (g *RelationGraph) AddNode(agentID string) {
	g.mu.Lock()
	g.nodes[agentID] = struct{}{}
	g.mu.Unlock()
}

// RemoveNode removes an agent and all their associated edges from the graph.
func (g *RelationGraph) RemoveNode(agentID string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	delete(g.nodes, agentID)
	for key, edge := range g.edges {
		if edge.Source == agentID || edge.Target == agentID {
			delete(g.edges, key)
		}
	}
}

// NodeCount returns the number of nodes.
func (g *RelationGraph) NodeCount() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.nodes)
}

// AddEdge adds or updates a directed relationship.
func (g *RelationGraph) AddEdge(source, target string, relType RelationType, round int, evidence string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.nodes[source] = struct{}{}
	g.nodes[target] = struct{}{}

	key := fmt.Sprintf("%s->%s:%s", source, target, relType)
	if e, ok := g.edges[key]; ok {
		e.Weight++
		e.Rounds = append(e.Rounds, round)
		if len(e.Evidence) < 5 { // cap evidence
			e.Evidence = append(e.Evidence, truncateStr(evidence, 100))
		}
	} else {
		g.edges[key] = &RelationEdge{
			Source:   source,
			Target:   target,
			Type:     relType,
			Weight:   1,
			Rounds:   []int{round},
			Evidence: []string{truncateStr(evidence, 100)},
		}
	}
}

// Edges returns all edges.
func (g *RelationGraph) Edges() []RelationEdge {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]RelationEdge, 0, len(g.edges))
	for _, e := range g.edges {
		out = append(out, *e)
	}
	return out
}

// EdgesFrom returns all outgoing edges from an agent.
func (g *RelationGraph) EdgesFrom(agentID string) []RelationEdge {
	g.mu.RLock()
	defer g.mu.RUnlock()
	var out []RelationEdge
	for _, e := range g.edges {
		if e.Source == agentID {
			out = append(out, *e)
		}
	}
	return out
}

// TopEdges returns the top N edges by weight.
func (g *RelationGraph) TopEdges(n int) []RelationEdge {
	g.mu.RLock()
	defer g.mu.RUnlock()

	all := make([]RelationEdge, 0, len(g.edges))
	for _, e := range g.edges {
		all = append(all, *e)
	}

	// Sort by weight desc
	for i := 0; i < len(all); i++ {
		for j := i + 1; j < len(all); j++ {
			if all[j].Weight > all[i].Weight {
				all[i], all[j] = all[j], all[i]
			}
		}
	}

	if n > 0 && n < len(all) {
		return all[:n]
	}
	return all
}

// ToEdgeDTOs converts all edges to serializable DTOs for real-time progress updates.
func (g *RelationGraph) ToEdgeDTOs() []EdgeDTO {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]EdgeDTO, 0, len(g.edges))
	for _, e := range g.edges {
		out = append(out, EdgeDTO{
			Source: e.Source,
			Target: e.Target,
			Type:   string(e.Type),
			Weight: e.Weight,
		})
	}
	return out
}

// FormatForReport renders the graph as structured text for the report prompt.
func (g *RelationGraph) FormatForReport() string {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if len(g.edges) == 0 {
		return "## Agent Interaction Graph\n(no interactions recorded)"
	}

	var b strings.Builder
	b.WriteString("## Agent Interaction Graph\n\n")

	// Group edges by source for readability
	sources := make(map[string][]RelationEdge)
	for _, e := range g.edges {
		sources[e.Source] = append(sources[e.Source], *e)
	}

	for source, edges := range sources {
		b.WriteString(fmt.Sprintf("### %s\n", source))
		for _, e := range edges {
			b.WriteString(fmt.Sprintf("- %s → %s (%s) weight=%d rounds=%v\n",
				source, e.Target, e.Type, e.Weight, e.Rounds))
			if len(e.Evidence) > 0 {
				b.WriteString(fmt.Sprintf("  evidence: \"%s\"\n", e.Evidence[0]))
			}
		}
		b.WriteString("\n")
	}

	// Summary stats
	b.WriteString("### Graph Statistics\n")
	b.WriteString(fmt.Sprintf("- Nodes (agents): %d\n", len(g.nodes)))
	b.WriteString(fmt.Sprintf("- Edges (relationships): %d\n", len(g.edges)))

	// Top relationships
	b.WriteString("\n### Strongest Relationships\n")
	top := g.topEdgesLocked(5)
	for _, e := range top {
		b.WriteString(fmt.Sprintf("- %s → %s: %d %s interactions\n", e.Source, e.Target, e.Weight, e.Type))
	}

	return b.String()
}

func (g *RelationGraph) topEdgesLocked(n int) []RelationEdge {
	all := make([]RelationEdge, 0, len(g.edges))
	for _, e := range g.edges {
		all = append(all, *e)
	}
	for i := 0; i < len(all); i++ {
		for j := i + 1; j < len(all); j++ {
			if all[j].Weight > all[i].Weight {
				all[i], all[j] = all[j], all[i]
			}
		}
	}
	if n > 0 && n < len(all) {
		return all[:n]
	}
	return all
}
