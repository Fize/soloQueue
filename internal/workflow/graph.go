package workflow

// graph.go — graph algorithms for workflow validation.
// Provides Tarjan's SCC for DAG verification and reachability queries
// used to validate loop edges and join constraints.

// adjacencyList is a directed graph representation.
type adjacencyList map[string][]string

// addEdge adds a directed edge u -> v.
func (g adjacencyList) addEdge(u, v string) {
	g[u] = append(g[u], v)
	// Ensure v exists as a key even if it has no outgoing edges.
	if _, ok := g[v]; !ok {
		g[v] = nil
	}
}

// findSCCs runs Tarjan's algorithm to find all strongly connected components.
// Each SCC of size > 1 indicates a cycle.
func (g adjacencyList) findSCCs() [][]string {
	var (
		index   int
		stack   []string
		onStack = make(map[string]bool)
		indices = make(map[string]int)
		lowLink = make(map[string]int)
		result  [][]string
	)

	var strongConnect func(v string)
	strongConnect = func(v string) {
		indices[v] = index
		lowLink[v] = index
		index++
		stack = append(stack, v)
		onStack[v] = true

		for _, w := range g[v] {
			if _, seen := indices[w]; !seen {
				strongConnect(w)
				if lowLink[w] < lowLink[v] {
					lowLink[v] = lowLink[w]
				}
			} else if onStack[w] {
				if indices[w] < lowLink[v] {
					lowLink[v] = indices[w]
				}
			}
		}

		if lowLink[v] == indices[v] {
			var scc []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				onStack[w] = false
				scc = append(scc, w)
				if w == v {
					break
				}
			}
			result = append(result, scc)
		}
	}

	for node := range g {
		if _, seen := indices[node]; !seen {
			strongConnect(node)
		}
	}
	return result
}

// hasCycle returns true if the graph contains at least one directed cycle.
func (g adjacencyList) hasCycle() bool {
	for _, scc := range g.findSCCs() {
		if len(scc) > 1 {
			return true
		}
	}
	return false
}

// canReach performs BFS to check if target is reachable from start.
func (g adjacencyList) canReach(start, target string) bool {
	visited := make(map[string]bool)
	queue := []string{start}
	visited[start] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current == target {
			return true
		}
		for _, next := range g[current] {
			if !visited[next] {
				visited[next] = true
				queue = append(queue, next)
			}
		}
	}
	return false
}
