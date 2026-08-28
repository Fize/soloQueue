package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/dispatch"
)

func TestInspectDelegationIsBoundToContextManager(t *testing.T) {
	m, err := dispatch.NewManager(t.TempDir(), "session-1")
	if err != nil {
		t.Fatal(err)
	}
	created, err := m.Begin(dispatch.BeginInput{Kind: dispatch.KindDelegate, TaskName: "audit", Task: "Audit it", Requester: "L1", Executor: "qa"})
	if err != nil {
		t.Fatal(err)
	}
	tool := NewInspectDelegationTool()
	if _, err := tool.Execute(context.Background(), `{"action":"list"}`); err == nil {
		t.Fatal("inspect without session dispatch manager must fail")
	}
	ctx := dispatch.WithScope(context.Background(), dispatch.Scope{Manager: m})
	result, err := tool.Execute(ctx, `{"action":"detail","dispatch_id":"`+created.Record.ID+`"}`)
	if err != nil {
		t.Fatal(err)
	}
	var got dispatch.Record
	if err := json.Unmarshal([]byte(result), &got); err != nil {
		t.Fatal(err)
	}
	if got.Task != "Audit it" || got.OwnerSessionID != "session-1" {
		t.Fatalf("detail = %#v", got)
	}
	foreignCtx := dispatch.WithScope(context.Background(), dispatch.Scope{Manager: m, RootID: "dlg_other_root"})
	if _, err := tool.Execute(foreignCtx, `{"action":"detail","dispatch_id":"`+created.Record.ID+`"}`); err == nil {
		t.Fatal("dispatch context must not inspect a foreign dispatch tree")
	}
}
