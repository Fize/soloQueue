package agent

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/dispatch"
	"github.com/xiaobaitu/soloqueue/internal/infra/telemetryctx"
	"github.com/xiaobaitu/soloqueue/internal/observability"
)

func TestToolTraceMainAndChildIdentity(t *testing.T) {
	dir := t.TempDir()
	store := observability.ForWorkDir(dir)
	ctx := observability.WithStore(telemetryctx.WithMetadata(context.Background(), telemetryctx.Metadata{SessionID: "l1", RequestID: "tool-turn"}), store)
	if err := store.Begin("l1", "tool-turn"); err != nil {
		t.Fatal(err)
	}
	out := make(chan AgentEvent, 8)
	for _, id := range []string{"main", "child"} {
		a := &Agent{InstanceID: id}
		scope := ctx
		if id == "child" {
			scope = dispatch.WithScope(ctx, dispatch.Scope{Manager: &dispatch.Manager{}, ParentID: "dlg_child"})
		}
		a.emit(scope, out, ToolExecStartEvent{CallID: "same-id", Name: "Bash", Args: `{"command":"printf hello"}`})
		time.Sleep(30 * time.Millisecond)
		a.emit(scope, out, ToolExecDoneEvent{CallID: "same-id", Name: "Bash", Result: "hello", Duration: 25 * time.Millisecond})
	}
	if err := store.Finish("l1", "tool-turn", "completed"); err != nil {
		t.Fatal(err)
	}
	// Read through a fresh Store so a browser refresh cannot depend on in-memory events.
	r, err := observability.ForWorkDir(dir).Request("l1", "tool-turn")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Tools) != 2 || len(r.Calls) != 0 || r.CallCount != 0 {
		t.Fatalf("tools=%+v, calls=%+v", r.Tools, r.Calls)
	}
	for _, tool := range r.Tools {
		if tool.ToolName != "Bash" || tool.Arguments == "" || tool.Content != "hello" || tool.DurationMS != 25 || tool.Status != "success" {
			t.Fatalf("tool=%+v", tool)
		}
		if tool.AgentID == "child" && tool.DispatchID != "dlg_child" {
			t.Fatalf("child owner=%+v", tool)
		}
	}
}

func TestToolTraceKeepsAsyncFailureWhenResumeReplaysCompletion(t *testing.T) {
	dir := t.TempDir()
	store := observability.ForWorkDir(dir)
	ctx := observability.WithStore(telemetryctx.WithMetadata(context.Background(), telemetryctx.Metadata{SessionID: "l1", RequestID: "failed"}), store)
	a := &Agent{InstanceID: "main"}
	out := make(chan AgentEvent, 4)
	a.emit(ctx, out, ToolExecStartEvent{CallID: "delegate", Name: "delegate", Args: `{"target":"worker"}`})
	a.observeTool(ctx, ToolExecDoneEvent{CallID: "delegate", Name: "delegate", Result: "error: worker failed", Err: fmt.Errorf("worker failed")})
	a.emit(ctx, out, ToolExecDoneEvent{CallID: "delegate", Name: "delegate", Result: "error: worker failed"})
	r, err := store.Request("l1", "failed")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Tools) != 1 || r.Tools[0].Status != "error" || r.Tools[0].Error != "worker failed" {
		t.Fatalf("completion overwritten: %+v", r.Tools)
	}
}
