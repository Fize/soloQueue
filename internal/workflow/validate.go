package workflow

import (
	"bytes"
	"fmt"
	"regexp"

	"gopkg.in/yaml.v3"
)

// identifierPattern matches names, node IDs, agent keys, and outcomes.
// Must start with a letter, followed by up to 63 alphanumeric/underscore/dash chars.
var identifierPattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]{0,63}$`)

// supportedVersions lists the YAML versions accepted by the parser.
var supportedVersions = map[string]bool{"1": true}

// ---------------------------------------------------------------------------
// Parsing
// ---------------------------------------------------------------------------

// ParseWorkflow reads YAML bytes and returns a fully validated ParsedWorkflow.
// Strict mode rejects unknown fields, duplicate keys, and unsupported versions.
func ParseWorkflow(data []byte) (*ParsedWorkflow, error) {
	// --- Strict YAML decode ---
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)

	var def WorkflowDef
	if err := decoder.Decode(&def); err != nil {
		return nil, fmt.Errorf("workflow: YAML parse error: %w", err)
	}

	// --- Version check ---
	if !supportedVersions[def.Version] {
		return nil, fmt.Errorf("workflow: unsupported version %q (supported: %v)", def.Version, mapKeys(supportedVersions))
	}

	// --- Merge defaults ---
	def.Defaults = mergeDefaults(def.Defaults)

	// --- Validate and build ---
	return validateAndBuild(def)
}

// mergeDefaults fills zero-value fields with DefaultDefaults.
func mergeDefaults(d Defaults) Defaults {
	defaults := DefaultDefaults()
	if d.NodeTimeout == 0 {
		d.NodeTimeout = defaults.NodeTimeout
	}
	if d.WorkflowTimeout == 0 {
		d.WorkflowTimeout = defaults.WorkflowTimeout
	}
	if d.MaxNodeRuns == 0 {
		d.MaxNodeRuns = defaults.MaxNodeRuns
	}
	if d.MaxOutputBytes == 0 {
		d.MaxOutputBytes = defaults.MaxOutputBytes
	}
	return d
}

// mapKeys returns sorted keys for error messages. Order is stable thanks to map iteration.
func mapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

// validateAndBuild performs all structural and semantic validation,
// building a ParsedWorkflow on success.
func validateAndBuild(def WorkflowDef) (*ParsedWorkflow, error) {
	// 1. Name validation
	if !identifierPattern.MatchString(def.Name) {
		return nil, fmt.Errorf("workflow: name %q must match %s", def.Name, identifierPattern)
	}

	// 2. Agents must be non-empty
	if len(def.Agents) == 0 {
		return nil, fmt.Errorf("workflow: agents must be non-empty")
	}
	for key, ref := range def.Agents {
		if !identifierPattern.MatchString(key) {
			return nil, fmt.Errorf("workflow: agent key %q must match %s", key, identifierPattern)
		}
		if !identifierPattern.MatchString(ref.Template) {
			return nil, fmt.Errorf("workflow: agent %q template %q must match %s", key, ref.Template, identifierPattern)
		}
		if ref.Model != "" && !identifierPattern.MatchString(ref.Model) {
			return nil, fmt.Errorf("workflow: agent %q model %q must match %s", key, ref.Model, identifierPattern)
		}
	}

	// 3. Entry must be non-empty and reference existing nodes
	if len(def.Entry) == 0 {
		return nil, fmt.Errorf("workflow: entry nodes must be non-empty")
	}

	// 4. Build node map, validate node IDs
	nodes := make(map[string]*NodeDef, len(def.Nodes))
	for i := range def.Nodes {
		n := &def.Nodes[i]
		if !identifierPattern.MatchString(n.ID) {
			return nil, fmt.Errorf("workflow: node ID %q must match %s", n.ID, identifierPattern)
		}
		if _, exists := nodes[n.ID]; exists {
			return nil, fmt.Errorf("workflow: duplicate node ID %q", n.ID)
		}
		nodes[n.ID] = n
	}

	// 5. Validate entry references
	for _, e := range def.Entry {
		if _, ok := nodes[e]; !ok {
			return nil, fmt.Errorf("workflow: entry node %q not found in nodes", e)
		}
	}

	// 6. Validate each node
	for _, n := range nodes {
		// Agent reference must exist
		if _, ok := def.Agents[n.Agent]; !ok {
			return nil, fmt.Errorf("workflow: node %q references unknown agent %q", n.ID, n.Agent)
		}
		// Node must have at least one output
		if len(n.Outputs) == 0 {
			return nil, fmt.Errorf("workflow: node %q must have at least one output", n.ID)
		}
		// Validate outcome names
		for outcome := range n.Outputs {
			if !identifierPattern.MatchString(outcome) {
				return nil, fmt.Errorf("workflow: node %q outcome %q must match %s", n.ID, outcome, identifierPattern)
			}
		}
		// Validate join
		if n.Join != nil {
			if n.Join.Mode != "all" {
				return nil, fmt.Errorf("workflow: node %q join.mode must be \"all\", got %q", n.ID, n.Join.Mode)
			}
			if len(n.Join.From) < 2 {
				return nil, fmt.Errorf("workflow: node %q join.from must have at least 2 entries", n.ID)
			}
			for _, src := range n.Join.From {
				if _, ok := nodes[src]; !ok {
					return nil, fmt.Errorf("workflow: node %q join.from references unknown node %q", n.ID, src)
				}
			}
		}
		// Validate on_error
		if n.OnError != nil {
			ep := n.OnError
			if ep.Strategy != "fail" && ep.Strategy != "retry" {
				return nil, fmt.Errorf("workflow: node %q on_error.strategy must be \"fail\" or \"retry\", got %q", n.ID, ep.Strategy)
			}
			if ep.Strategy == "retry" && ep.MaxAttempts < 2 {
				return nil, fmt.Errorf("workflow: node %q on_error.max_attempts must be at least 2 for retry strategy", n.ID)
			}
		}
	}

	// 7. Build edge list from outputs
	var edges []*Edge
	for _, n := range nodes {
		for outcome, out := range n.Outputs {
			for _, to := range out.To {
				// Validate target node exists
				if _, ok := nodes[to]; !ok {
					return nil, fmt.Errorf("workflow: node %q output %q references unknown node %q", n.ID, outcome, to)
				}
				edge := &Edge{
					FromNode:      n.ID,
					Outcome:       outcome,
					ToNode:        to,
					Loop:          out.Loop,
					MaxTraversals: out.MaxTraversals,
				}
				edges = append(edges, edge)
			}
			// Terminal output: to is empty
			if len(out.To) == 0 {
				if out.Loop {
					return nil, fmt.Errorf("workflow: node %q output %q: terminal output cannot be a loop", n.ID, outcome)
				}
				if out.MaxTraversals > 0 {
					return nil, fmt.Errorf("workflow: node %q output %q: terminal output cannot have max_traversals", n.ID, outcome)
				}
				edges = append(edges, &Edge{
					FromNode: n.ID,
					Outcome:  outcome,
					ToNode:   "", // terminal
				})
			}
		}
	}

	// 8. Check edge limits
	if len(edges) > 256 { // hard max; detailed checks use EngineLimits
		return nil, fmt.Errorf("workflow: too many edges (%d)", len(edges))
	}
	if len(nodes) > 64 {
		return nil, fmt.Errorf("workflow: too many nodes (%d)", len(nodes))
	}

	// 9. Pre-validate declared loop edges before partitioning
	//    (checks that must happen regardless of whether the edge actually forms a cycle)
	declaredLoopEdges := filterDeclaredLoops(edges)
	for _, e := range declaredLoopEdges {
		// Loop edge cannot target a join:all node
		if targetNode, ok := nodes[e.ToNode]; ok && targetNode.Join != nil {
			return nil, fmt.Errorf("workflow: loop edge %s --(%s)--> %s cannot target join:all node %q", e.FromNode, e.Outcome, e.ToNode, e.ToNode)
		}
		// MaxTraversals must be positive
		if e.MaxTraversals <= 0 {
			return nil, fmt.Errorf("workflow: loop edge %s --(%s)--> %s must have positive max_traversals", e.FromNode, e.Outcome, e.ToNode)
		}
	}

	// Partition edges into actual loop edges and non-loop edges
	loopEdges, nonLoopEdges := partitionEdges(edges)

	// 10. Non-loop edges must form a DAG
	nonLoopGraph := make(adjacencyList)
	for _, n := range nodes {
		nonLoopGraph[n.ID] = nil // ensure all nodes are in the graph
	}
	for _, e := range nonLoopEdges {
		nonLoopGraph.addEdge(e.FromNode, e.ToNode)
	}
	if nonLoopGraph.hasCycle() {
		// Find and report the cycle
		sccs := nonLoopGraph.findSCCs()
		for _, scc := range sccs {
			if len(scc) > 1 {
				return nil, fmt.Errorf("workflow: non-loop edges form a cycle involving nodes %v", scc)
			}
		}
		return nil, fmt.Errorf("workflow: non-loop edges form a cycle")
	}

	// 11. For edges that declare loop:true but are not actual loop edges
	//    (i.e., they don't form a cycle), report them as dead-ends.
	actualLoopIDs := make(map[string]bool)
	for _, e := range loopEdges {
		key := fmt.Sprintf("%s|%s|%s", e.FromNode, e.Outcome, e.ToNode)
		actualLoopIDs[key] = true
	}
	for _, e := range declaredLoopEdges {
		key := fmt.Sprintf("%s|%s|%s", e.FromNode, e.Outcome, e.ToNode)
		if !actualLoopIDs[key] {
			return nil, fmt.Errorf("workflow: loop edge %s --(%s)--> %s is not part of any reachable cycle", e.FromNode, e.Outcome, e.ToNode)
		}
	}

	// 12. Validate join.from matches actual incoming non-loop edges
	for _, n := range nodes {
		if n.Join == nil {
			continue
		}
		incomingFrom := make(map[string]bool)
		for _, e := range nonLoopEdges {
			if e.ToNode == n.ID {
				incomingFrom[e.FromNode] = true
			}
		}
		// Also check loop edges that point to non-join nodes (should not happen, already checked)
		for _, src := range n.Join.From {
			if !incomingFrom[src] {
				return nil, fmt.Errorf("workflow: node %q join.from %q does not match any incoming non-loop edge", n.ID, src)
			}
		}
		// Every incoming edge source must be in join.from
		for src := range incomingFrom {
			found := false
			for _, jf := range n.Join.From {
				if jf == src {
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("workflow: node %q has incoming edge from %q that is not listed in join.from", n.ID, src)
			}
		}
	}

	// 13. Verify at least one terminal path exists
	if !hasTerminalPath(nodes, nonLoopEdges, def.Entry) {
		return nil, fmt.Errorf("workflow: no terminal path exists — at least one output must lead to a terminal (to: [])")
	}

	// 14. Build ParsedWorkflow
	pw := &ParsedWorkflow{
		Name:        def.Name,
		Description: def.Description,
		Version:     def.Version,
		Defaults:    def.Defaults,
		Agents:      def.Agents,
		Entry:       def.Entry,
		Nodes:       nodes,
		Edges:       edges,
	}
	return pw, nil
}

// partitionEdges separates edges into loop and non-loop based on whether
// they participate in a cycle in the full graph. An edge is considered a
// loop edge if it is declared with loop:true AND the full graph (including
// all edges) has a path from the edge's target back to its source.
func partitionEdges(edges []*Edge) (loopEdges, nonLoopEdges []*Edge) {
	// First pass: separate by loop flag
	var declaredLoop, declaredNonLoop []*Edge
	for _, e := range edges {
		if e.IsTerminal() {
			declaredNonLoop = append(declaredNonLoop, e)
			continue
		}
		if e.Loop {
			declaredLoop = append(declaredLoop, e)
		} else {
			declaredNonLoop = append(declaredNonLoop, e)
		}
	}

	// Build non-loop graph for reachability checks
	g := make(adjacencyList)
	for _, e := range declaredNonLoop {
		if !e.IsTerminal() {
			g.addEdge(e.FromNode, e.ToNode)
		}
	}

	// For each declared loop edge, check if it actually forms a cycle
	// (target can reach source through non-loop edges)
	actualLoop := make([]*Edge, 0, len(declaredLoop))
	actualNonLoop := make([]*Edge, 0, len(declaredNonLoop)+len(declaredLoop))
	actualNonLoop = append(actualNonLoop, declaredNonLoop...)

	for _, e := range declaredLoop {
		if g.canReach(e.ToNode, e.FromNode) {
			actualLoop = append(actualLoop, e)
		} else {
			actualNonLoop = append(actualNonLoop, e)
		}
	}

	return actualLoop, actualNonLoop
}

// filterDeclaredLoops returns all edges that declare loop:true.
func filterDeclaredLoops(edges []*Edge) []*Edge {
	var result []*Edge
	for _, e := range edges {
		if e.Loop {
			result = append(result, e)
		}
	}
	return result
}

// hasTerminalPath checks if every entry node can reach at least one terminal
// output (to: []) through the non-loop edge graph.
func hasTerminalPath(nodes map[string]*NodeDef, nonLoopEdges []*Edge, entry []string) bool {
	// Build forward graph from non-loop edges
	g := make(adjacencyList)
	for id := range nodes {
		g[id] = nil
	}
	for _, e := range nonLoopEdges {
		if e.ToNode != "" {
			g.addEdge(e.FromNode, e.ToNode)
		}
	}

	// Find nodes with terminal outputs
	terminalNodes := make(map[string]bool)
	for _, n := range nodes {
		for _, out := range n.Outputs {
			if len(out.To) == 0 {
				terminalNodes[n.ID] = true
				break
			}
		}
	}

	if len(terminalNodes) == 0 {
		return false
	}

	// BFS from each entry node; at least one must reach a terminal node
	for _, start := range entry {
		for terminal := range terminalNodes {
			if g.canReach(start, terminal) {
				return true
			}
		}
	}
	return false
}
