// Package tool provides L1 agent tools for workflow execution.
// workflow_run, workflow_get, workflow_wait, and workflow_list are injected into L1 sessions only.
package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/workflow"
)

// RunTool implements tools.Tool for workflow_run.
type RunTool struct {
	store *workflow.Store
	runs  *workflow.RunManager
	log   *logger.Logger
}

func NewRunToolWithManager(store *workflow.Store, runs *workflow.RunManager, log *logger.Logger) *RunTool {
	return &RunTool{store: store, runs: runs, log: log}
}

// runArgs is the JSON schema for workflow_run.
type runArgs struct {
	Name       string                 `json:"name"`
	Task       *workflow.WorkflowTask `json:"task"`
	Input      string                 `json:"input"`
	WorkDir    string                 `json:"work_dir"`
	Repository string                 `json:"repository"`
	BaseRef    string                 `json:"base_ref"`
	Branch     string                 `json:"branch"`
	Source     string                 `json:"source"`
}

func (t *RunTool) Name() string { return "workflow_run" }
func (t *RunTool) Description() string {
	return "Start a predefined workflow with a structured goal and acceptance criteria in an isolated Git worktree."
}
func (t *RunTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"name": {"type": "string", "description": "The workflow name (from workflow_list)"},
			"input": {"type": "string", "description": "Deprecated legacy task input; use task instead"},
			"task": {
				"type": "object",
				"required": ["goal", "acceptance_criteria"],
				"properties": {
					"goal": {"type": "string"},
					"acceptance_criteria": {"type": "array", "items": {"type": "string"}},
					"constraints": {"type": "array", "items": {"type": "string"}},
					"delivery": {"type": "object", "description": "Optional explicit commit/push/pull-request actions"}
				}
			},
			"work_dir": {"type": "string", "description": "Absolute path to the project working directory"},
			"repository": {"type": "string", "description": "Git repository to isolate"},
			"base_ref": {"type": "string", "description": "Git ref used as worktree base"},
			"branch": {"type": "string", "description": "Optional worktree branch"}
		},
		"required": ["name", "task"]
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
	legacyInput := ra.Task == nil && strings.TrimSpace(ra.Input) != ""
	if legacyInput {
		ra.Task = &workflow.WorkflowTask{
			Goal:               strings.TrimSpace(ra.Input),
			AcceptanceCriteria: []string{"Complete the requested workflow and report verifiable results."},
		}
	}
	if ra.Task == nil {
		return "", fmt.Errorf("workflow_run: task.goal and task.acceptance_criteria are required")
	}
	if ra.Repository == "" {
		ra.Repository = ra.WorkDir
	}
	if t.runs == nil {
		return "", fmt.Errorf("workflow_run: isolated worktree manager is required")
	}
	// Load and validate workflow
	wf, err := t.store.Load(ra.Name)
	if err != nil {
		return "", fmt.Errorf("workflow_run: load %q: %w", ra.Name, err)
	}
	raw, err := t.store.ReadRaw(ra.Name)
	if err != nil {
		return "", fmt.Errorf("workflow_run: load raw %q: %w", ra.Name, err)
	}
	id, err := t.runs.StartTask(context.WithoutCancel(ctx), wf, raw, *ra.Task, ra.Repository, ra.BaseRef, ra.Branch, ra.Source)
	if err != nil {
		return "", fmt.Errorf("workflow_run: start task: %w", err)
	}
	if legacyInput {
		for {
			detail, getErr := t.runs.Get(id)
			if getErr != nil {
				return "", fmt.Errorf("workflow_run: get legacy result: %w", getErr)
			}
			if workflowRunSettled(detail.Status) || hasPendingConfirmation(detail.Confirmations) {
				detail.WorkflowYAML = ""
				return marshalDetail("workflow_run", detail, false)
			}
			select {
			case <-ctx.Done():
				return "", fmt.Errorf("workflow_run: wait legacy result: %w", ctx.Err())
			case <-time.After(250 * time.Millisecond):
			}
		}
	}
	return fmt.Sprintf(`{"run_id":%q,"workflow":%q,"status":"preparing_worktree"}`, id, wf.Name), nil
}

type statusArgs struct {
	RunID          string `json:"run_id"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

// GetTool returns the latest durable state for an asynchronous workflow run.
type GetTool struct{ runs *workflow.RunManager }

func NewGetTool(runs *workflow.RunManager) *GetTool { return &GetTool{runs: runs} }
func (t *GetTool) Name() string                     { return "workflow_get" }
func (t *GetTool) Description() string {
	return "Get the latest status, node results, outputs, and pending confirmations for a workflow run."
}
func (t *GetTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"run_id":{"type":"string"}},"required":["run_id"]}`)
}
func (t *GetTool) Execute(_ context.Context, args string) (string, error) {
	parsed, err := parseStatusArgs("workflow_get", args)
	if err != nil {
		return "", err
	}
	if t.runs == nil {
		return "", fmt.Errorf("workflow_get: run manager is required")
	}
	detail, err := t.runs.Get(parsed.RunID)
	if err != nil {
		return "", fmt.Errorf("workflow_get: %w", err)
	}
	detail.WorkflowYAML = ""
	return marshalDetail("workflow_get", detail, false)
}

// WaitTool waits for a run to reach a user-action or terminal boundary.
type WaitTool struct{ runs *workflow.RunManager }

func NewWaitTool(runs *workflow.RunManager) *WaitTool { return &WaitTool{runs: runs} }
func (t *WaitTool) Name() string                      { return "workflow_wait" }
func (t *WaitTool) Description() string {
	return "Wait up to five minutes for a workflow run to complete, pause, fail, block, cancel, or require user action."
}
func (t *WaitTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"run_id":{"type":"string"},"timeout_seconds":{"type":"integer","minimum":1,"maximum":300,"default":300}},"required":["run_id"]}`)
}
func (t *WaitTool) PreferredTimeout() time.Duration { return 5*time.Minute + 5*time.Second }
func (t *WaitTool) Execute(ctx context.Context, args string) (string, error) {
	parsed, err := parseStatusArgs("workflow_wait", args)
	if err != nil {
		return "", err
	}
	if t.runs == nil {
		return "", fmt.Errorf("workflow_wait: run manager is required")
	}
	seconds := parsed.TimeoutSeconds
	if seconds == 0 {
		seconds = 300
	}
	if seconds < 1 || seconds > 300 {
		return "", fmt.Errorf("workflow_wait: timeout_seconds must be between 1 and 300")
	}
	timer := time.NewTimer(time.Duration(seconds) * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		detail, getErr := t.runs.Get(parsed.RunID)
		if getErr != nil {
			return "", fmt.Errorf("workflow_wait: %w", getErr)
		}
		if workflowRunSettled(detail.Status) || hasPendingConfirmation(detail.Confirmations) {
			detail.WorkflowYAML = ""
			return marshalDetail("workflow_wait", detail, false)
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("workflow_wait: %w", ctx.Err())
		case <-timer.C:
			detail.WorkflowYAML = ""
			return marshalDetail("workflow_wait", detail, true)
		case <-ticker.C:
		}
	}
}

func parseStatusArgs(toolName, args string) (statusArgs, error) {
	var parsed statusArgs
	if err := json.Unmarshal([]byte(args), &parsed); err != nil {
		return parsed, fmt.Errorf("%s: invalid arguments: %w", toolName, err)
	}
	if parsed.RunID == "" {
		return parsed, fmt.Errorf("%s: run_id is required", toolName)
	}
	return parsed, nil
}

func marshalDetail(toolName string, detail any, waitTimedOut bool) (string, error) {
	raw, err := json.Marshal(map[string]any{"run": detail, "wait_timed_out": waitTimedOut})
	if err != nil {
		return "", fmt.Errorf("%s: marshal: %w", toolName, err)
	}
	return string(raw), nil
}

func workflowRunSettled(status workflow.RunStatus) bool {
	switch status {
	case workflow.RunPaused, workflow.RunInterrupted, workflow.RunCompleted, workflow.RunBlocked, workflow.RunFailed, workflow.RunCancelled, workflow.RunAbandoned:
		return true
	default:
		return false
	}
}

func hasPendingConfirmation(confirmations []workflow.ConfirmationView) bool {
	for _, confirmation := range confirmations {
		if confirmation.Status == "pending" {
			return true
		}
	}
	return false
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

func (t *ListTool) Name() string { return "workflow_list" }
func (t *ListTool) Description() string {
	return "List all available workflows with their descriptions and validity status."
}
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
