// Package workflow provides a user-authored YAML DAG engine with outcome routing,
// parallel fan-out/in, bounded loops, and node-level error retry.
//
// v1 scope: synchronous execution triggered by L1 agent via workflow_run tool.
// No LLM creation, no async execution, no persistence, no visualization.
package workflow

import (
	"context"
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// YAML Schema types (Section 3 of design doc)
// ---------------------------------------------------------------------------

// WorkflowDef represents the raw YAML workflow definition before validation.
type WorkflowDef struct {
	Name        string             `yaml:"name"`
	Description string             `yaml:"description"`
	Version     string             `yaml:"version"`
	Defaults    Defaults           `yaml:"defaults"`
	Agents      map[string]AgentRef `yaml:"agents"`
	Entry       []string           `yaml:"entry"`
	Nodes       []NodeDef          `yaml:"nodes"`
}

// Defaults holds workflow-level default configuration values.
type Defaults struct {
	NodeTimeout     Duration `yaml:"node_timeout"`
	WorkflowTimeout Duration `yaml:"workflow_timeout"`
	MaxNodeRuns     int      `yaml:"max_node_runs"`
	MaxOutputBytes  int      `yaml:"max_output_bytes"`
}

// DefaultDefaults returns the default values used when the YAML omits a field.
func DefaultDefaults() Defaults {
	return Defaults{
		NodeTimeout:     Duration(20 * time.Minute),
		WorkflowTimeout: Duration(45 * time.Minute),
		MaxNodeRuns:     50,
		MaxOutputBytes:  131072, // 128 KiB
	}
}

// AgentRef identifies an agent configuration used by workflow nodes.
type AgentRef struct {
	Template string `yaml:"template"`
	Model    string `yaml:"model"`
}

// NodeDef is a single node in the workflow graph.
type NodeDef struct {
	ID      string              `yaml:"id"`
	Agent   string              `yaml:"agent"`
	Prompt  string              `yaml:"prompt"`
	Timeout Duration            `yaml:"timeout"`
	Join    *JoinDef            `yaml:"join"`
	Outputs map[string]OutputDef `yaml:"outputs"`
	OnError *ErrorPolicy        `yaml:"on_error"`
}

// JoinDef configures fan-in behavior for a node.
type JoinDef struct {
	Mode string   `yaml:"mode"`
	From []string `yaml:"from"`
}

// OutputDef defines a transition from a node after a specific outcome.
type OutputDef struct {
	To            []string `yaml:"to"`
	Loop          bool     `yaml:"loop"`
	MaxTraversals int      `yaml:"max_traversals"`
}

// ErrorPolicy controls what happens when a node encounters a system error.
type ErrorPolicy struct {
	Strategy    string `yaml:"strategy"`
	MaxAttempts int    `yaml:"max_attempts"`
}

// DefaultErrorPolicy returns the default error policy.
func DefaultErrorPolicy() ErrorPolicy {
	return ErrorPolicy{Strategy: "fail"}
}

// Duration is a time.Duration that can be unmarshaled from YAML strings like "20m".
type Duration time.Duration

// UnmarshalYAML implements yaml.Unmarshaler for human-readable duration strings.
func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.ScalarNode {
		return fmt.Errorf("workflow: expected scalar for duration, got node kind %v", value.Kind)
	}
	parsed, err := time.ParseDuration(value.Value)
	if err != nil {
		return fmt.Errorf("workflow: invalid duration %q: %w", value.Value, err)
	}
	if parsed < 0 {
		return fmt.Errorf("workflow: negative duration %q", value.Value)
	}
	*d = Duration(parsed)
	return nil
}

// Duration returns the underlying time.Duration.
func (d Duration) Duration() time.Duration { return time.Duration(d) }

// ---------------------------------------------------------------------------
// Validated / Parsed types
// ---------------------------------------------------------------------------

// ParsedWorkflow is a fully validated workflow ready for execution.
// Nodes are indexed by ID; edges are pre-computed from outputs.
type ParsedWorkflow struct {
	Name        string
	Description string
	Version     string
	Defaults    Defaults
	Agents      map[string]AgentRef
	Entry       []string
	Nodes       map[string]*NodeDef // keyed by node ID
	Edges       []*Edge
}

// Edge represents a directed transition from one node to another.
// Loop edges form the back-edge in a bounded cycle; the rest of the graph
// (with all loop edges removed) must be a DAG.
type Edge struct {
	FromNode      string
	Outcome       string
	ToNode        string
	Loop          bool
	MaxTraversals int
}

// IsLoop reports whether this edge is a declared loop back-edge.
func (e *Edge) IsLoop() bool { return e.Loop }

// IsTerminal reports whether this edge terminates the activation branch.
func (e *Edge) IsTerminal() bool { return e.ToNode == "" }

// ---------------------------------------------------------------------------
// Execution state types (Section 2.2, 4.3 of design doc)
// ---------------------------------------------------------------------------

// NodeRunState is the lifecycle state of a single node execution instance.
type NodeRunState string

const (
	NodeQueued    NodeRunState = "queued"
	NodeRunning   NodeRunState = "running"
	NodeSucceeded NodeRunState = "succeeded"
	NodeFailed    NodeRunState = "failed"
	NodeCancelled NodeRunState = "cancelled"
	NodeTimedOut  NodeRunState = "timed_out"
)

// IsTerminal returns true if the state is a final state.
func (s NodeRunState) IsTerminal() bool {
	return s == NodeSucceeded || s == NodeFailed || s == NodeCancelled || s == NodeTimedOut
}

// NodeRun is a single execution instance of a node.
// A node may have multiple NodeRuns due to loops or retries.
type NodeRun struct {
	ID           string
	NodeID       string
	Attempt      int
	ActivationID string
	State        NodeRunState
	Inputs       []NodeInput
	Result       *HandoffData
	Error        error
	StartedAt    time.Time
	FinishedAt   time.Time
}

// NodeInput is structured input data from an upstream node execution.
type NodeInput struct {
	FromNode     string
	Outcome      string
	Content      string
	ActivationID string
}

// HandoffData captures the outcome and content from workflow_handoff.
type HandoffData struct {
	Outcome string
	Content string
}

// RunStatus is the overall workflow execution status.
type RunStatus string

const (
	RunPending   RunStatus = "pending"
	RunRunning   RunStatus = "running"
	RunCompleted RunStatus = "completed"
	RunFailed    RunStatus = "failed"
	RunCancelled RunStatus = "cancelled"
)

// TerminalOutput is the final output from a node that reached to:[].
type TerminalOutput struct {
	Node    string
	Outcome string
	Content string
}

// RunState holds all mutable state for an active workflow execution.
// It uses a single-writer model: only the engine's scheduler goroutine
// modifies ready queue, join buckets, loop counters, and NodeRun states.
type RunState struct {
	ID             string
	Workflow       *ParsedWorkflow
	Status         RunStatus
	NodeRuns       map[string]*NodeRun  // keyed by NodeRun.ID
	ReadyQueue     []string             // NodeRun IDs ready to execute
	Running        map[string]contextCanceller
	JoinBuckets    map[JoinKey]*JoinBucket
	LoopCounters   map[LoopKey]int
	TerminalOutput []TerminalOutput
	StartedAt      time.Time
	FinishedAt     time.Time
	Input          string // original workflow_run input
	WorkDir        string // shared project directory
}

// contextCanceller is a func that cancels a running node's context.
// Avoids direct import of context package for dependency clarity.
type contextCanceller func()

// JoinKey identifies a join bucket: a specific node + activation combination.
type JoinKey struct {
	NodeID       string
	ActivationID string
}

// JoinBucket accumulates upstream inputs for a join:all node.
type JoinBucket struct {
	Received map[string]NodeInput // source node ID -> received input
	Expected map[string]bool      // source node IDs still expected
}

// IsSatisfied returns true when all expected upstream inputs have arrived.
func (b *JoinBucket) IsSatisfied() bool {
	return len(b.Received) == len(b.Expected)
}

// LoopKey identifies a specific loop edge traversal count bucket.
type LoopKey struct {
	EdgeID       string // "fromNode:outcome:toNode"
	ActivationID string
}

// ---------------------------------------------------------------------------
// Engine limits (Section 4.3 of design doc)
// ---------------------------------------------------------------------------

// EngineLimits defines hard resource caps enforced by the server.
// YAML can only tighten these, never expand.
type EngineLimits struct {
	MaxYAMLBytes       int64
	MaxNodes           int
	MaxEdges           int
	MaxParallelNodes   int
	MaxNodeRuns        int
	MaxWorkflowTimeout time.Duration
	MaxNodeTimeout     time.Duration
	MaxOutputBytes     int
}

// DefaultEngineLimits returns the recommended production limits.
func DefaultEngineLimits() EngineLimits {
	return EngineLimits{
		MaxYAMLBytes:       1 << 20,    // 1 MiB
		MaxNodes:           64,
		MaxEdges:           256,
		MaxParallelNodes:   4,
		MaxNodeRuns:        100,
		MaxWorkflowTimeout: 60 * time.Minute,
		MaxNodeTimeout:     30 * time.Minute,
		MaxOutputBytes:     256 << 10, // 256 KiB
	}
}

// Clamp applies engine limits to a Defaults config, returning the tighter of the two.
func (l EngineLimits) Clamp(d Defaults) Defaults {
	clamped := d
	if d.NodeTimeout <= 0 || d.NodeTimeout.Duration() > l.MaxNodeTimeout {
		clamped.NodeTimeout = Duration(l.MaxNodeTimeout)
	}
	if d.WorkflowTimeout <= 0 || d.WorkflowTimeout.Duration() > l.MaxWorkflowTimeout {
		clamped.WorkflowTimeout = Duration(l.MaxWorkflowTimeout)
	}
	if d.MaxNodeRuns <= 0 || d.MaxNodeRuns > l.MaxNodeRuns {
		clamped.MaxNodeRuns = l.MaxNodeRuns
	}
	if d.MaxOutputBytes <= 0 || d.MaxOutputBytes > l.MaxOutputBytes {
		clamped.MaxOutputBytes = l.MaxOutputBytes
	}
	return clamped
}

// ---------------------------------------------------------------------------
// NodeExecutor interface (Section 6 of design doc)
// ---------------------------------------------------------------------------

// NodeRunRequest is the input to a NodeExecutor.Execute call.
type NodeRunRequest struct {
	RunID        string
	Workflow     *ParsedWorkflow
	Node         *NodeDef
	AgentRef     AgentRef
	NodeRun      *NodeRun
	WorkflowInput string
	WorkDir      string
}

// NodeRunResult is the output from a NodeExecutor.Execute call.
type NodeRunResult struct {
	Handoff *HandoffData
	Error   error
}

// NodeExecutor abstracts agent creation and execution.
// The agentexec sub-package provides the real implementation;
// tests use a fake implementation.
type NodeExecutor interface {
	Execute(ctx context.Context, req NodeRunRequest) (NodeRunResult, error)
}

// ---------------------------------------------------------------------------
// Workflow metadata (for workflow_list)
// ---------------------------------------------------------------------------

// WorkflowMeta is lightweight metadata returned by Store.List.
type WorkflowMeta struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Valid       bool   `json:"valid"`
	Error       string `json:"error,omitempty"`
}
