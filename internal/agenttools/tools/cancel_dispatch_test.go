package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/dispatch"
)

func TestCancelDelegationToolCancelsOwnedRunningDispatch(t *testing.T) {
	m, err := dispatch.NewManager(t.TempDir(), "session-1")
	if err != nil {
		t.Fatal(err)
	}
	result, err := m.Begin(dispatch.BeginInput{TaskName: "stale", Task: "stop this", Requester: "L1", Executor: "team"})
	if err != nil {
		t.Fatal(err)
	}
	cancelled := make(chan struct{}, 1)
	if err := m.RegisterCancel(result.Record.ID, func(error) { cancelled <- struct{}{} }); err != nil {
		t.Fatal(err)
	}
	ctx := dispatch.WithScope(context.Background(), dispatch.Scope{Manager: m})
	got, err := NewCancelDelegationTool().Execute(ctx, `{"task_name":"stale","reason":"obsolete"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "cancellation requested") {
		t.Fatalf("result = %q", got)
	}
	select {
	case <-cancelled:
	default:
		t.Fatal("cancellation callback was not called")
	}
}
