package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/xiaobaitu/soloqueue/internal/dispatch"
)

func TestInspectDelegationIsBoundToContextManager(t *testing.T) {
	m, err := dispatch.NewManager(t.TempDir(), "session-1")
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.Begin(dispatch.BeginInput{Kind: dispatch.KindDelegate, TaskName: "audit", Task: "Audit it", Requester: "L1", Executor: "qa"})
	if err != nil {
		t.Fatal(err)
	}
	tool := NewInspectDelegationTool()
	if _, err := tool.Execute(context.Background(), `{"action":"list"}`); err == nil {
		t.Fatal("inspect without session dispatch manager must fail")
	}
	ctx := dispatch.WithScope(context.Background(), dispatch.Scope{Manager: m})
	result, err := tool.Execute(ctx, `{"action":"detail","task_name":"audit"}`)
	if err != nil {
		t.Fatal(err)
	}
	var got dispatchSummary
	if err := json.Unmarshal([]byte(result), &got); err != nil {
		t.Fatal(err)
	}
	if got.TaskName != "audit" || got.Status != dispatch.StatusRunning {
		t.Fatalf("detail = %#v", got)
	}
	foreignCtx := dispatch.WithScope(context.Background(), dispatch.Scope{Manager: m, RootID: "dlg_other_root"})
	if _, err := tool.Execute(foreignCtx, `{"action":"detail","task_name":"audit"}`); err == nil {
		t.Fatal("dispatch context must not inspect a foreign dispatch tree")
	}
}

func TestTruncateSummaryTextPreservesUTF8Boundaries(t *testing.T) {
	value := strings.Repeat("烟", maxSummaryText+1)
	got := truncateSummaryText(value)
	if !utf8.ValidString(got) {
		t.Fatalf("truncated text is not valid UTF-8: %q", got)
	}
	if got != strings.Repeat("烟", maxSummaryText)+"…" {
		t.Fatalf("truncated text = %q", got)
	}
}

func TestInspectDelegationListAndTailAreBoundedSummaries(t *testing.T) {
	m, err := dispatch.NewManager(t.TempDir(), "session-1")
	if err != nil {
		t.Fatal(err)
	}
	large := strings.Repeat("sensitive-detail-", 8_000)
	created, err := m.Begin(dispatch.BeginInput{
		Kind: dispatch.KindDelegate, TaskName: "audit", Task: large, Context: large,
		Requester: "L1", Executor: "qa",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Append(created.Record.ID, "progress", map[string]string{"detail": large}); err != nil {
		t.Fatal(err)
	}
	ctx := dispatch.WithScope(context.Background(), dispatch.Scope{Manager: m})
	tool := NewInspectDelegationTool()

	list, err := tool.Execute(ctx, `{"action":"list"}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) > 16_000 || strings.Contains(list, "sensitive-detail-") || !strings.Contains(list, `"dispatches"`) {
		t.Fatalf("list must be a concise dispatch summary, len=%d", len(list))
	}

	detail, err := tool.Execute(ctx, `{"action":"detail","task_name":"audit"}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail) > 16_000 || strings.Contains(detail, "sensitive-detail-") || strings.Contains(detail, "dispatch_id") || strings.Contains(detail, "root_dispatch_id") {
		t.Fatalf("detail must be a concise dispatch summary, len=%d", len(detail))
	}

	tail, err := tool.Execute(ctx, `{"action":"tail","task_name":"audit","limit":100}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(tail) > 16_000 || strings.Contains(tail, "sensitive-detail-") || !strings.Contains(tail, `"events"`) {
		t.Fatalf("tail must be a concise event summary, len=%d", len(tail))
	}
}
