package tool

import (
	"context"
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
	runTool := NewRunTool(nil, nil, log)
	if runTool.Name() != "workflow_run" {
		t.Errorf("Name() = %q, want %q", runTool.Name(), "workflow_run")
	}

	want := workflow.DefaultEngineLimits().MaxWorkflowTimeout + 5*time.Minute
	if got := runTool.PreferredTimeout(); got != want {
		t.Errorf("PreferredTimeout() = %v, want %v", got, want)
	}
}
