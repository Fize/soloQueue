// Package tool provides L1 agent tools for workflow execution.
// workflow_run and workflow_list are injected into L1 sessions only.
package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/workflow"
)

// RunTool implements tools.Tool for workflow_run.
type RunTool struct {
	store  *workflow.Store
	engine *workflow.Engine
	log    *logger.Logger
}

// NewRunTool creates a workflow_run tool.
func NewRunTool(store *workflow.Store, engine *workflow.Engine, log *logger.Logger) *RunTool {
	return &RunTool{store: store, engine: engine, log: log}
}

// runArgs is the JSON schema for workflow_run.
type runArgs struct {
	Name    string `json:"name"`
	Input   string `json:"input"`
	WorkDir string `json:"work_dir"`
}

func (t *RunTool) Name() string        { return "workflow_run" }
func (t *RunTool) Description() string { return "Execute a predefined workflow by name. Returns structured results including terminal outputs and per-node execution status." }
func (t *RunTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string", "description": "The workflow name (from workflow_list)"},
			"input": {"type": "string", "description": "The task description or input for the workflow"},
			"work_dir": {"type": "string", "description": "Absolute path to the project working directory"}
		},
		"required": ["name", "input", "work_dir"]
	}`)
}

// PreferredTimeout returns a timeout slightly longer than the max workflow timeout.
func (t *RunTool) PreferredTimeout() time.Duration {
	return workflow.DefaultEngineLimits().MaxWorkflowTimeout + 5*time.Minute
}

func (t *RunTool) Execute(ctx context.Context, args string) (string, error) {
	var ra runArgs
	if err := json.Unmarshal([]byte(args), &ra); err != nil {
		return "", fmt.Errorf("workflow_run: invalid arguments: %w", err)
	}

	// Load and validate workflow
	wf, err := t.store.Load(ra.Name)
	if err != nil {
		return "", fmt.Errorf("workflow_run: load %q: %w", ra.Name, err)
	}

	t.log.Info(logger.CatTool, "workflow_run start",
		"name", ra.Name,
		"work_dir", ra.WorkDir,
	)

	start := time.Now()

	// Run workflow synchronously
	rs, err := t.engine.Run(ctx, wf, ra.Input, ra.WorkDir)
	if err != nil {
		return "", fmt.Errorf("workflow_run: engine error: %w", err)
	}

	dur := time.Since(start)

	// Build structured result
	type nodeResult struct {
		Node    string `json:"node"`
		Attempt int    `json:"attempt"`
		Status  string `json:"status"`
		Outcome string `json:"outcome,omitempty"`
	}
	type terminalOut struct {
		Node    string `json:"node"`
		Outcome string `json:"outcome"`
		Content string `json:"content"`
	}
	type resultJSON struct {
		RunID           string         `json:"run_id"`
		Workflow        string         `json:"workflow"`
		Status          string         `json:"status"`
		DurationMs      int64          `json:"duration_ms"`
		TerminalOutputs []terminalOut  `json:"terminal_outputs"`
		NodeRuns        []nodeResult   `json:"node_runs"`
	}

	res := resultJSON{
		RunID:      rs.ID,
		Workflow:   wf.Name,
		Status:     string(rs.Status),
		DurationMs: dur.Milliseconds(),
	}

	for _, to := range rs.TerminalOutput {
		res.TerminalOutputs = append(res.TerminalOutputs, terminalOut{
			Node: to.Node, Outcome: to.Outcome, Content: to.Content,
		})
	}

	for _, nr := range rs.NodeRuns {
		nrRes := nodeResult{
			Node:    nr.NodeID,
			Attempt: nr.Attempt,
			Status:  string(nr.State),
		}
		if nr.Result != nil {
			nrRes.Outcome = nr.Result.Outcome
		}
		res.NodeRuns = append(res.NodeRuns, nrRes)
	}

	out, err := json.Marshal(res)
	if err != nil {
		return "", fmt.Errorf("workflow_run: marshal result: %w", err)
	}

	t.log.Info(logger.CatTool, "workflow_run done",
		"name", ra.Name,
		"status", string(rs.Status),
		"duration_ms", dur.Milliseconds(),
	)

	return string(out), nil
}

// ─── workflow_list ────────────────────────────────────────────────────────

// ListTool implements tools.Tool for workflow_list.
type ListTool struct {
	store *workflow.Store
	log   *logger.Logger
}

// NewListTool creates a workflow_list tool.
func NewListTool(store *workflow.Store, log *logger.Logger) *ListTool {
	return &ListTool{store: store, log: log}
}

func (t *ListTool) Name() string        { return "workflow_list" }
func (t *ListTool) Description() string { return "List all available workflows with their descriptions and validity status." }
func (t *ListTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type": "object", "properties": {}}`)
}

func (t *ListTool) Execute(ctx context.Context, args string) (string, error) {
	metas, err := t.store.List()
	if err != nil {
		return "", fmt.Errorf("workflow_list: %w", err)
	}

	out, err := json.Marshal(metas)
	if err != nil {
		return "", fmt.Errorf("workflow_list: marshal: %w", err)
	}
	return string(out), nil
}
