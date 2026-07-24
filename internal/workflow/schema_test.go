package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func mustParse(t *testing.T, yaml string) *ParsedWorkflow {
	t.Helper()
	pw, err := ParseWorkflow([]byte(yaml))
	if err != nil {
		t.Fatalf("ParseWorkflow: unexpected error: %v", err)
	}
	return pw
}

func mustFail(t *testing.T, yaml string, wantMsg string) {
	t.Helper()
	_, err := ParseWorkflow([]byte(yaml))
	if err == nil {
		t.Fatalf("ParseWorkflow: expected error containing %q, got nil", wantMsg)
	}
	if !strings.Contains(err.Error(), wantMsg) {
		t.Fatalf("ParseWorkflow: error %q does not contain %q", err.Error(), wantMsg)
	}
}

func writeYAML(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name+".yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writeYAML: %v", err)
	}
	return path
}

// ---------------------------------------------------------------------------
// Schema validation — happy paths
// ---------------------------------------------------------------------------

func TestParseWorkflow_SimpleLinear(t *testing.T) {
	yaml := `
name: simple-pipeline
description: A simple linear workflow
version: "1"
agents:
  worker:
    template: dev
entry: [step1]
nodes:
  - id: step1
    agent: worker
    prompt: Do step 1
    outputs:
      done:
        to: [step2]
  - id: step2
    agent: worker
    prompt: Do step 2
    outputs:
      done:
        to: []
`
	pw := mustParse(t, yaml)
	if pw.Name != "simple-pipeline" {
		t.Errorf("name = %q, want %q", pw.Name, "simple-pipeline")
	}
	if len(pw.Nodes) != 2 {
		t.Errorf("nodes = %d, want 2", len(pw.Nodes))
	}
	if len(pw.Edges) != 2 {
		t.Errorf("edges = %d, want 2", len(pw.Edges))
	}
}

func TestParseWorkflow_FanOut(t *testing.T) {
	yaml := `
name: fanout-test
description: Fan-out test
version: "1"
agents:
  worker:
    template: dev
entry: [dispatch]
nodes:
  - id: dispatch
    agent: worker
    prompt: Dispatch
    outputs:
      ready:
        to: [task_a, task_b]
  - id: task_a
    agent: worker
    prompt: Task A
    outputs:
      done:
        to: []
  - id: task_b
    agent: worker
    prompt: Task B
    outputs:
      done:
        to: []
`
	pw := mustParse(t, yaml)
	if len(pw.Edges) != 4 {
		t.Errorf("edges = %d, want 4", len(pw.Edges))
	}
}

func TestParseWorkflow_FanIn(t *testing.T) {
	yaml := `
name: fanin-test
description: Fan-in test
version: "1"
agents:
  worker:
    template: dev
entry: [dispatch]
nodes:
  - id: dispatch
    agent: worker
    prompt: Dispatch
    outputs:
      ready:
        to: [task_a, task_b]
  - id: task_a
    agent: worker
    prompt: Task A
    outputs:
      done:
        to: [merge]
  - id: task_b
    agent: worker
    prompt: Task B
    outputs:
      done:
        to: [merge]
  - id: merge
    agent: worker
    join:
      mode: all
      from: [task_a, task_b]
    prompt: Merge
    outputs:
      done:
        to: []
`
	mustParse(t, yaml)
}

func TestParseWorkflow_BoundedLoop(t *testing.T) {
	yaml := `
name: loop-test
description: Bounded loop test
version: "1"
agents:
  worker:
    template: dev
  reviewer:
    template: reviewer
entry: [write]
nodes:
  - id: write
    agent: worker
    prompt: Write
    outputs:
      draft:
        to: [review]
  - id: review
    agent: reviewer
    prompt: Review
    outputs:
      approved:
        to: []
      needs_revision:
        to: [write]
        loop: true
        max_traversals: 2
`
	mustParse(t, yaml)
}

func TestParseWorkflow_MultiNodeLoop(t *testing.T) {
	// Loop: test -> fix -> test (with verify as terminal)
	yaml := `
name: multiline-loop
description: Multi-node loop
version: "1"
agents:
  worker:
    template: dev
entry: [test]
nodes:
  - id: test
    agent: worker
    prompt: Test
    outputs:
      passed:
        to: [verify]
      failed:
        to: [fix]
        loop: true
        max_traversals: 3
  - id: fix
    agent: worker
    prompt: Fix
    outputs:
      ready:
        to: [test]
  - id: verify
    agent: worker
    prompt: Verify
    outputs:
      done:
        to: []
`
	mustParse(t, yaml)
}

func TestParseWorkflow_OnErrorRetry(t *testing.T) {
	yaml := `
name: retry-test
description: Retry test
version: "1"
agents:
  worker:
    template: dev
entry: [risky]
nodes:
  - id: risky
    agent: worker
    prompt: Risky task
    outputs:
      done:
        to: []
    on_error:
      strategy: retry
      max_attempts: 3
`
	mustParse(t, yaml)
}

func TestParseWorkflow_MultipleTerminalOutputs(t *testing.T) {
	yaml := `
name: multi-terminal
description: Multiple terminal outputs
version: "1"
agents:
  worker:
    template: dev
entry: [check]
nodes:
  - id: check
    agent: worker
    prompt: Check
    outputs:
      path_a:
        to: [handleA]
      path_b:
        to: [handleB]
  - id: handleA
    agent: worker
    prompt: Handle A
    outputs:
      done:
        to: []
  - id: handleB
    agent: worker
    prompt: Handle B
    outputs:
      done:
        to: []
`
	mustParse(t, yaml)
}

func TestParseWorkflow_DefaultsInherited(t *testing.T) {
	yaml := `
name: defaults-test
description: Defaults test
version: "1"
agents:
  worker:
    template: dev
entry: [node1]
nodes:
  - id: node1
    agent: worker
    prompt: Test
    outputs:
      done:
        to: []
`
	pw := mustParse(t, yaml)
	if pw.Defaults.NodeTimeout == 0 {
		t.Error("NodeTimeout should have default value")
	}
	if pw.Defaults.MaxNodeRuns == 0 {
		t.Error("MaxNodeRuns should have default value")
	}
}

func TestParseWorkflow_CustomDefaults(t *testing.T) {
	yaml := `
name: custom-defaults
description: Custom defaults
version: "1"
defaults:
  node_timeout: 10m
  max_node_runs: 20
agents:
  worker:
    template: dev
entry: [node1]
nodes:
  - id: node1
    agent: worker
    prompt: Test
    outputs:
      done:
        to: []
`
	pw := mustParse(t, yaml)
	if pw.Defaults.MaxNodeRuns != 20 {
		t.Errorf("MaxNodeRuns = %d, want 20", pw.Defaults.MaxNodeRuns)
	}
}

func TestParseWorkflow_ModelOverride(t *testing.T) {
	yaml := `
name: model-test
description: Model override test
version: "1"
agents:
  worker:
    template: dev
    model: deepseek-v4-pro-max
entry: [node1]
nodes:
  - id: node1
    agent: worker
    prompt: Test
    outputs:
      done:
        to: []
`
	pw := mustParse(t, yaml)
	if pw.Agents["worker"].Model != "deepseek-v4-pro-max" {
		t.Errorf("model = %q", pw.Agents["worker"].Model)
	}
}

func TestParseWorkflow_SelfLoop(t *testing.T) {
	// Self-loop: reproduce -> reproduce
	yaml := `
name: self-loop
description: Self-loop for retry without cycle
version: "1"
agents:
  worker:
    template: dev
entry: [reproduce]
nodes:
  - id: reproduce
    agent: worker
    prompt: Reproduce
    outputs:
      reproduced:
        to: [diagnose]
      not_reproduced:
        to: [reproduce]
        loop: true
        max_traversals: 2
  - id: diagnose
    agent: worker
    prompt: Diagnose
    outputs:
      done:
        to: []
`
	// Self-loop: reproduce -> reproduce is a cycle of length 1.
	// The non-loop edge graph without the loop edge is a DAG (reproduce -> diagnose).
	mustParse(t, yaml)
}

// ---------------------------------------------------------------------------
// Error cases — YAML parsing
// ---------------------------------------------------------------------------

func TestParseWorkflow_UnknownField(t *testing.T) {
	yaml := `
name: test
description: Test
version: "1"
agents:
  worker:
    template: dev
entry: [node1]
nodes:
  - id: node1
    agent: worker
    prompt: Test
    outputs:
      done:
        to: []
unkown_field: boom
`
	mustFail(t, yaml, "not found")
}

func TestParseWorkflow_IllegalDuration(t *testing.T) {
	yaml := `
name: dur-test
description: Test
version: "1"
defaults:
  node_timeout: not-a-duration
agents:
  worker:
    template: dev
entry: [node1]
nodes:
  - id: node1
    agent: worker
    prompt: Test
    outputs:
      done:
        to: []
`
	mustFail(t, yaml, "invalid duration")
}

func TestParseWorkflow_UnsupportedVersion(t *testing.T) {
	yaml := `
name: test
description: Test
version: "2"
agents:
  worker:
    template: dev
entry: [node1]
nodes:
  - id: node1
    agent: worker
    prompt: Test
    outputs:
      done:
        to: []
`
	mustFail(t, yaml, "unsupported version")
}

// ---------------------------------------------------------------------------
// Error cases — structural validation
// ---------------------------------------------------------------------------

func TestParseWorkflow_MissingAgents(t *testing.T) {
	yaml := `
name: no-agents
description: Test
version: "1"
entry: [node1]
nodes:
  - id: node1
    prompt: Test
    outputs:
      done:
        to: []
`
	mustFail(t, yaml, "agents")
}

func TestParseWorkflow_MissingEntry(t *testing.T) {
	yaml := `
name: no-entry
description: Test
version: "1"
agents:
  worker:
    template: dev
nodes:
  - id: node1
    agent: worker
    prompt: Test
    outputs:
      done:
        to: []
`
	mustFail(t, yaml, "entry")
}

func TestParseWorkflow_EntryReferencesUnknownNode(t *testing.T) {
	yaml := `
name: bad-entry
description: Test
version: "1"
agents:
  worker:
    template: dev
entry: [nonexistent]
nodes:
  - id: node1
    agent: worker
    prompt: Test
    outputs:
      done:
        to: []
`
	mustFail(t, yaml, "entry node")
}

func TestParseWorkflow_AgentReferencesUnknownAgent(t *testing.T) {
	yaml := `
name: bad-agent-ref
description: Test
version: "1"
agents:
  worker:
    template: dev
entry: [node1]
nodes:
  - id: node1
    agent: nonexistent
    prompt: Test
    outputs:
      done:
        to: []
`
	mustFail(t, yaml, "unknown agent")
}

func TestParseWorkflow_NodeMissingOutputs(t *testing.T) {
	yaml := `
name: no-outputs
description: Test
version: "1"
agents:
  worker:
    template: dev
entry: [node1]
nodes:
  - id: node1
    agent: worker
    prompt: Test
    outputs: {}
`
	mustFail(t, yaml, "must have at least one output")
}

func TestParseWorkflow_OutputReferencesUnknownNode(t *testing.T) {
	yaml := `
name: bad-out-ref
description: Test
version: "1"
agents:
  worker:
    template: dev
entry: [node1]
nodes:
  - id: node1
    agent: worker
    prompt: Test
    outputs:
      done:
        to: [nonexistent]
`
	mustFail(t, yaml, "unknown node")
}

func TestParseWorkflow_InvalidName(t *testing.T) {
	yaml := `
name: "123bad-name"
description: Test
version: "1"
agents:
  worker:
    template: dev
entry: [node1]
nodes:
  - id: node1
    agent: worker
    prompt: Test
    outputs:
      done:
        to: []
`
	mustFail(t, yaml, "name")
}

func TestParseWorkflow_InvalidNodeID(t *testing.T) {
	yaml := `
name: test
description: Test
version: "1"
agents:
  worker:
    template: dev
entry: [valid-node]
nodes:
  - id: "123bad"
    agent: worker
    prompt: Test
    outputs:
      done:
        to: []
  - id: valid-node
    agent: worker
    prompt: Test
    outputs:
      done:
        to: []
`
	mustFail(t, yaml, "node ID")
}

func TestParseWorkflow_DuplicateNodeID(t *testing.T) {
	yaml := `
name: dup-node
description: Test
version: "1"
agents:
  worker:
    template: dev
entry: [step1]
nodes:
  - id: step1
    agent: worker
    prompt: Step 1
    outputs:
      done:
        to: []
  - id: step1
    agent: worker
    prompt: Step 1 duplicate
    outputs:
      done:
        to: []
`
	mustFail(t, yaml, "duplicate node ID")
}

// ---------------------------------------------------------------------------
// Error cases — cycle detection
// ---------------------------------------------------------------------------

func TestParseWorkflow_NonLoopCycle(t *testing.T) {
	yaml := `
name: cycle-test
description: Test
version: "1"
agents:
  worker:
    template: dev
entry: [node_a]
nodes:
  - id: node_a
    agent: worker
    prompt: A
    outputs:
      done:
        to: [node_b]
  - id: node_b
    agent: worker
    prompt: B
    outputs:
      done:
        to: [node_a]
`
	// This forms a cycle because node_a -> node_b -> node_a with no loop declaration.
	// After removing loop edges, the non-loop graph should be a DAG.
	// But neither edge is declared as loop, so both are non-loop and form a cycle.
	// The test should fail with "non-loop edges form a cycle".
	mustFail(t, yaml, "cycle")
}

func TestParseWorkflow_ThreeNodeCycle(t *testing.T) {
	yaml := `
name: three-cycle
description: Three node cycle
version: "1"
agents:
  worker:
    template: dev
entry: [a]
nodes:
  - id: a
    agent: worker
    prompt: A
    outputs:
      done:
        to: [b]
  - id: b
    agent: worker
    prompt: B
    outputs:
      done:
        to: [c]
  - id: c
    agent: worker
    prompt: C
    outputs:
      done:
        to: [a]
`
	mustFail(t, yaml, "cycle")
}

func TestParseWorkflow_LoopWithoutDeclaration(t *testing.T) {
	// test -> fix -> test forms a cycle through non-loop edges (neither declares loop:true)
	yaml := `
name: hidden-loop
description: Hidden loop test
version: "1"
agents:
  worker:
    template: dev
entry: [test]
nodes:
  - id: test
    agent: worker
    prompt: Test
    outputs:
      failed:
        to: [fix]
      passed:
        to: []
  - id: fix
    agent: worker
    prompt: Fix
    outputs:
      done:
        to: [test]
`
	// Here, test -> fix -> test. Neither edge is loop. The non-loop graph has a cycle.
	mustFail(t, yaml, "cycle")
}

func TestParseWorkflow_LoopWithoutMaxTraversals(t *testing.T) {
	yaml := `
name: no-max-loop
description: Loop without limit
version: "1"
agents:
  worker:
    template: dev
entry: [write]
nodes:
  - id: write
    agent: worker
    prompt: Write
    outputs:
      draft:
        to: [review]
  - id: review
    agent: worker
    prompt: Review
    outputs:
      approved:
        to: []
      needs_revision:
        to: [write]
        loop: true
`
	mustFail(t, yaml, "max_traversals")
}

func TestParseWorkflow_LoopWithZeroMaxTraversals(t *testing.T) {
	yaml := `
name: zero-max-loop
description: Loop with max_traversals 0
version: "1"
agents:
  worker:
    template: dev
entry: [write]
nodes:
  - id: write
    agent: worker
    prompt: Write
    outputs:
      draft:
        to: [review]
  - id: review
    agent: worker
    prompt: Review
    outputs:
      approved:
        to: []
      needs_revision:
        to: [write]
        loop: true
        max_traversals: 0
`
	mustFail(t, yaml, "max_traversals")
}

func TestParseWorkflow_LoopIntoTargetJoinAll(t *testing.T) {
	yaml := `
name: loop-to-join
description: Loop targeting join:all
version: "1"
agents:
  worker:
    template: dev
entry: [fix]
nodes:
  - id: fix
    agent: worker
    prompt: Fix
    outputs:
      retry:
        to: [merge]
        loop: true
        max_traversals: 2
      done:
        to: []
  - id: merge
    agent: worker
    join:
      mode: all
      from: [task_a, fix]
    prompt: Merge
    outputs:
      done:
        to: []
  - id: task_a
    agent: worker
    prompt: Task A
    outputs:
      done:
        to: [merge]
`
	mustFail(t, yaml, "cannot target join:all")
}

func TestParseWorkflow_LoopEdgeDeadEnd(t *testing.T) {
	// Loop edge declared but the target can't reach the source (no cycle)
	yaml := `
name: dead-loop
description: Dead-end loop
version: "1"
agents:
  worker:
    template: dev
entry: [a]
nodes:
  - id: a
    agent: worker
    prompt: A
    outputs:
      next:
        to: [b]
        loop: true
        max_traversals: 2
  - id: b
    agent: worker
    prompt: B
    outputs:
      done:
        to: []
`
	// a -> b declares loop, but b can't reach a (it goes to terminal).
	// This should fail because loop edge needs a cycle.
	mustFail(t, yaml, "reachable cycle")
}

// ---------------------------------------------------------------------------
// Error cases — join validation
// ---------------------------------------------------------------------------

func TestParseWorkflow_JoinFromMismatch(t *testing.T) {
	// join.from has nodes not in the incoming edges
	yaml := `
name: join-mismatch
description: Join from mismatch
version: "1"
agents:
  worker:
    template: dev
entry: [a]
nodes:
  - id: a
    agent: worker
    prompt: A
    outputs:
      done:
        to: [merge]
  - id: merge
    agent: worker
    join:
      mode: all
      from: [a, b]
    prompt: Merge
    outputs:
      done:
        to: []
`
	mustFail(t, yaml, "join.from")
}

func TestParseWorkflow_JoinIncomplete(t *testing.T) {
	// Incoming edge from 'a' but join.from doesn't include it
	yaml := `
name: join-incomplete
description: Join incomplete
version: "1"
agents:
  worker:
    template: dev
entry: [a, b]
nodes:
  - id: a
    agent: worker
    prompt: A
    outputs:
      done:
        to: [merge]
  - id: b
    agent: worker
    prompt: B
    outputs:
      done:
        to: [merge]
  - id: merge
    agent: worker
    join:
      mode: all
      from: [a]
    prompt: Merge
    outputs:
      done:
        to: []
`
	mustFail(t, yaml, "join.from")
}

func TestParseWorkflow_JoinFromSingle(t *testing.T) {
	// join.from must have at least 2 entries
	yaml := `
name: join-single
description: Join with single source
version: "1"
agents:
  worker:
    template: dev
entry: [a]
nodes:
  - id: a
    agent: worker
    prompt: A
    outputs:
      done:
        to: [merge]
  - id: merge
    agent: worker
    join:
      mode: all
      from: [a]
    prompt: Merge
    outputs:
      done:
        to: []
`
	// This fails because join.from needs at least 2, but also because incoming edge
	// from 'a' is listed. The first error should be "at least 2 entries".
	mustFail(t, yaml, "at least 2")
}

func TestParseWorkflow_JoinBadMode(t *testing.T) {
	yaml := `
name: bad-join-mode
description: Bad join mode
version: "1"
agents:
  worker:
    template: dev
entry: [a, b]
nodes:
  - id: a
    agent: worker
    prompt: A
    outputs:
      done:
        to: [merge]
  - id: b
    agent: worker
    prompt: B
    outputs:
      done:
        to: [merge]
  - id: merge
    agent: worker
    join:
      mode: any
      from: [a, b]
    prompt: Merge
    outputs:
      done:
        to: []
`
	mustFail(t, yaml, "join.mode")
}

// ---------------------------------------------------------------------------
// Error cases — on_error validation
// ---------------------------------------------------------------------------

func TestParseWorkflow_OnErrorBadStrategy(t *testing.T) {
	yaml := `
name: bad-strategy
description: Bad error strategy
version: "1"
agents:
  worker:
    template: dev
entry: [node1]
nodes:
  - id: node1
    agent: worker
    prompt: Test
    outputs:
      done:
        to: []
    on_error:
      strategy: ignore
`
	mustFail(t, yaml, "strategy")
}

func TestParseWorkflow_OnErrorRetryNoMaxAttempts(t *testing.T) {
	yaml := `
name: retry-no-max
description: Retry without max_attempts
version: "1"
agents:
  worker:
    template: dev
entry: [node1]
nodes:
  - id: node1
    agent: worker
    prompt: Test
    outputs:
      done:
        to: []
    on_error:
      strategy: retry
      max_attempts: 1
`
	mustFail(t, yaml, "max_attempts")
}

// ---------------------------------------------------------------------------
// Error cases — no terminal path
// ---------------------------------------------------------------------------

func TestParseWorkflow_NoTerminalPath(t *testing.T) {
	yaml := `
name: no-terminal
description: No terminal path
version: "1"
agents:
  worker:
    template: dev
entry: [a, b]
nodes:
  - id: a
    agent: worker
    prompt: A
    outputs:
      done:
        to: [c]
  - id: b
    agent: worker
    prompt: B
    outputs:
      done:
        to: [c]
  - id: c
    agent: worker
    prompt: C
    outputs:
      done:
        to: [a]
`
	// c -> a -> c is a cycle with no loop declaration AND no terminal outputs.
	// This should fail first on the cycle detection.
	mustFail(t, yaml, "cycle")
}

func TestParseWorkflow_TerminalEdgeWithLoop(t *testing.T) {
	yaml := `
name: bad-terminal-loop
description: Terminal output with loop flag
version: "1"
agents:
  worker:
    template: dev
entry: [node1]
nodes:
  - id: node1
    agent: worker
    prompt: Test
    outputs:
      done:
        to: []
        loop: true
`
	mustFail(t, yaml, "terminal output cannot be a loop")
}

func TestParseWorkflow_TerminalEdgeWithMaxTraversals(t *testing.T) {
	yaml := `
name: bad-terminal-max
description: Terminal output with max_traversals
version: "1"
agents:
  worker:
    template: dev
entry: [node1]
nodes:
  - id: node1
    agent: worker
    prompt: Test
    outputs:
      done:
        to: []
        max_traversals: 3
`
	mustFail(t, yaml, "terminal output")
}

// ---------------------------------------------------------------------------
// Error cases — invalid identifiers
// ---------------------------------------------------------------------------

func TestParseWorkflow_InvalidOutcomeName(t *testing.T) {
	yaml := `
name: bad-outcome
description: Test
version: "1"
agents:
  worker:
    template: dev
entry: [node1]
nodes:
  - id: node1
    agent: worker
    prompt: Test
    outputs:
      "123bad":
        to: []
`
	mustFail(t, yaml, "outcome")
}

func TestParseWorkflow_InvalidAgentKey(t *testing.T) {
	yaml := `
name: bad-agent-key
description: Test
version: "1"
agents:
  123bad:
    template: dev
entry: [node1]
nodes:
  - id: node1
    agent: 123bad
    prompt: Test
    outputs:
      done:
        to: []
`
	mustFail(t, yaml, "agent key")
}

// ---------------------------------------------------------------------------
// Store tests
// ---------------------------------------------------------------------------

func TestStore_LoadHappy(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir, 1<<20)

	yaml := `
name: test-wf
description: Test workflow
version: "1"
agents:
  worker:
    template: dev
entry: [node1]
nodes:
  - id: node1
    agent: worker
    prompt: Test
    outputs:
      done:
        to: []
`
	writeYAML(t, dir, "test-wf", yaml)

	pw, err := store.Load("test-wf")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if pw.Name != "test-wf" {
		t.Errorf("name = %q", pw.Name)
	}
}

func TestStore_LoadNotFound(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir, 1<<20)
	_, err := store.Load("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent workflow")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want 'not found'", err.Error())
	}
}

func TestStore_LoadNameMismatch(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir, 1<<20)

	yaml := `
name: wrong-name
description: Test
version: "1"
agents:
  worker:
    template: dev
entry: [node1]
nodes:
  - id: node1
    agent: worker
    prompt: Test
    outputs:
      done:
        to: []
`
	writeYAML(t, dir, "correct-name", yaml)

	_, err := store.Load("correct-name")
	if err == nil {
		t.Fatal("expected error for name mismatch")
	}
	if !strings.Contains(err.Error(), "name mismatch") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestStore_LoadPathTraversal(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir, 1<<20)

	_, err := store.Load("../escape")
	if err == nil {
		t.Fatal("expected error for path traversal in name")
	}
}

func TestStore_LoadFileTooLarge(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir, 100) // 100 byte limit

	largeYaml := strings.Repeat("# comment\n", 200)
	writeYAML(t, dir, "large", largeYaml)

	_, err := store.Load("large")
	if err == nil {
		t.Fatal("expected error for too-large file")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("error = %q", err.Error())
	}
}

func TestStore_ListHappy(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir, 1<<20)

	validYaml := `
name: valid-wf
description: Valid
version: "1"
agents:
  worker:
    template: dev
entry: [node1]
nodes:
  - id: node1
    agent: worker
    prompt: Test
    outputs:
      done:
        to: []
`
	invalidYaml := `not: valid: yaml:`

	writeYAML(t, dir, "valid-wf", validYaml)
	writeYAML(t, dir, "broken-wf", invalidYaml)

	metas, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 2 {
		t.Fatalf("List returned %d entries, want 2", len(metas))
	}

	found := map[string]WorkflowMeta{}
	for _, m := range metas {
		found[m.Name] = m
	}

	if v, ok := found["valid-wf"]; !ok || !v.Valid {
		t.Error("valid-wf should be valid")
	}
	if v, ok := found["broken-wf"]; !ok || v.Valid {
		t.Error("broken-wf should be invalid")
	}
}

func TestStore_ListEmptyDir(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir, 1<<20)

	metas, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 0 {
		t.Errorf("List returned %d entries for empty dir", len(metas))
	}
}

func TestStore_ListSkipsNonYaml(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir, 1<<20)

	os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hello"), 0644)
	os.WriteFile(filepath.Join(dir, "notes.md"), []byte("# Notes"), 0644)

	metas, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 0 {
		t.Errorf("List returned %d entries for non-yaml files", len(metas))
	}
}

func TestStore_ListInvalidFilename(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir, 1<<20)

	os.WriteFile(filepath.Join(dir, "123bad.yaml"), []byte("name: x"), 0644)

	metas, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(metas) != 1 {
		t.Fatalf("List returned %d entries, want 1", len(metas))
	}
	if metas[0].Valid {
		t.Error("invalid filename should produce invalid meta")
	}
}
