package tool

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/workflow"
)

func TestListTool(t *testing.T) {
	dir := t.TempDir()
	store := workflow.NewStore(dir, 0)
	log, _ := logger.New(dir)

	listTool := NewListTool(store, log)
	if listTool.Name() != "workflow_list" {
		t.Errorf("Name() = %q, want %q", listTool.Name(), "workflow_list")
	}

	res, err := listTool.Execute(context.Background(), "{}")
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if res != "null" {
		t.Errorf("Unexpected result for empty store: %q", res)
	}
}

func TestRunTool_PreferredTimeout(t *testing.T) {
	log, _ := logger.New(t.TempDir())
	runTool := NewRunToolWithManager(nil, nil, log)
	if runTool.Name() != "workflow_run" {
		t.Errorf("Name() = %q, want %q", runTool.Name(), "workflow_run")
	}

	want := workflow.DefaultEngineLimits().MaxWorkflowTimeout + 5*time.Minute
	if got := runTool.PreferredTimeout(); got != want {
		t.Errorf("PreferredTimeout() = %v, want %v", got, want)
	}
}

func TestRunToolAcceptsLegacyInputContract(t *testing.T) {
	dir := t.TempDir()
	store := workflow.NewStore(dir, 0)
	raw := []byte(`name: demo
description: test workflow
version: "1"
agents:
  worker:
    template: worker
entry: [start]
nodes:
  - id: start
    agent: worker
    prompt: Do work.
    outputs:
      done:
        to: []
`)
	if _, err := store.Save("demo", raw); err != nil {
		t.Fatal(err)
	}
	log, _ := logger.New(t.TempDir())
	runTool := NewRunToolWithManager(store, nil, log)

	_, err := runTool.Execute(context.Background(), `{"name":"demo","input":"legacy","work_dir":"/tmp/project"}`)
	if err == nil || !strings.Contains(err.Error(), "isolated worktree manager is required") {
		t.Fatalf("Execute error = %v, want legacy input to pass task parsing", err)
	}
}

func TestRunToolRejectsExecutionWithoutWorktreeManager(t *testing.T) {
	dir := t.TempDir()
	store := workflow.NewStore(dir, 0)
	raw := []byte(`name: demo
description: test workflow
version: "1"
agents:
  worker:
    template: worker
entry: [start]
nodes:
  - id: start
    agent: worker
    prompt: Do work.
    outputs:
      done:
        to: []
`)
	if _, err := store.Save("demo", raw); err != nil {
		t.Fatal(err)
	}
	log, _ := logger.New(t.TempDir())
	runTool := NewRunToolWithManager(store, nil, log)

	_, err := runTool.Execute(context.Background(), `{"name":"demo","task":{"goal":"implement","acceptance_criteria":["tests pass"]}}`)
	if err == nil || !strings.Contains(err.Error(), "isolated worktree manager is required") {
		t.Fatalf("Execute error = %v, want isolated worktree manager error", err)
	}
}

func TestStatusToolsExposeAsyncFollowUpContract(t *testing.T) {
	getTool := NewGetTool(nil)
	waitTool := NewWaitTool(nil)
	if getTool.Name() != "workflow_get" || waitTool.Name() != "workflow_wait" {
		t.Fatalf("unexpected names: %s %s", getTool.Name(), waitTool.Name())
	}
	if _, err := getTool.Execute(context.Background(), `{"run_id":"wf_missing"}`); err == nil || !strings.Contains(err.Error(), "manager is required") {
		t.Fatalf("workflow_get error = %v", err)
	}
	if _, err := waitTool.Execute(context.Background(), `{"run_id":"wf_missing","timeout_seconds":1}`); err == nil || !strings.Contains(err.Error(), "manager is required") {
		t.Fatalf("workflow_wait error = %v", err)
	}
}
