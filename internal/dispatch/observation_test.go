package dispatch

import (
	"context"
	"encoding/json"
	"github.com/xiaobaitu/soloqueue/internal/observability"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCompletedDetailCompactionPreservesRecovery(t *testing.T) {
	root := t.TempDir()
	m, e := NewManager(root, "l1")
	if e != nil {
		t.Fatal(e)
	}
	result, e := m.Begin(BeginInput{TaskName: "task", Task: "work", Requester: "main", Executor: "child", RequestID: "req", ToolCallID: "tool"})
	if e != nil {
		t.Fatal(e)
	}
	id := result.Record.ID
	dir := filepath.Join(root, "delegations", id)
	if e = CompactCompletedDetails(dir); e == nil {
		t.Fatal("compacted running task")
	}
	if e = m.Append(id, "agent_event:result", map[string]string{"text": "complete body"}); e != nil {
		t.Fatal(e)
	}
	if e = m.Finish(id, StatusCompleted, nil); e != nil {
		t.Fatal(e)
	}
	if e = CompactCompletedDetails(dir); e != nil {
		t.Fatal(e)
	}
	loaded, e := NewManager(root, "l1")
	if e != nil {
		t.Fatal(e)
	}
	rec, ok := loaded.Get(id)
	if !ok || rec.Status != StatusCompleted || !rec.DetailsExpired || rec.RequestID != "req" || rec.ToolCallID != "tool" {
		t.Fatalf("%#v", rec)
	}
	events, e := ReadEvents(dir)
	if e != nil || len(events) != 1 || events[0].Sequence != rec.Revision {
		t.Fatalf("%#v %v", events, e)
	}
	_ = context.Background()
}
func TestCompactionRejectsCorruptStream(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "dlg_corrupt")
	_ = os.MkdirAll(dir, 0700)
	p := filepath.Join(dir, "stream-2026-01-01.jsonl")
	_ = os.WriteFile(p, []byte("{broken"), 0600)
	if e := CompactCompletedDetails(dir); e == nil {
		t.Fatal("accepted corrupt journal")
	}
	b, _ := os.ReadFile(p)
	if string(b) != "{broken" {
		t.Fatal("changed corrupt source")
	}
}

func TestSharedBudgetBoundsRepeatedChildPayloadsAndRecovery(t *testing.T) {
	root := t.TempDir()
	release := observability.RegisterPolicy(root, func() observability.Policy {
		return observability.Policy{MaxBytes: 64 << 10, MaxTurnBytes: 16 << 10, MaxFileBytes: 4 << 10}
	})
	defer release()
	store := observability.ForWorkDir(root)
	_ = store.SaveCall(observability.Call{CallID: "main", SessionID: "l1", RequestID: "round", StartedAt: time.Now(), Content: strings.Repeat("m", 4<<10)}, nil)
	manager, e := NewManager(filepath.Join(root, "logs", "timelines", "default"), "l1-session")
	if e != nil {
		t.Fatal(e)
	}
	result, e := manager.Begin(BeginInput{TaskName: "child", Requester: "main", Task: strings.Repeat("task", 10000), Context: strings.Repeat("context", 10000), RequestID: "round", Executor: "child"})
	if e != nil {
		t.Fatal(e)
	}
	id := result.Record.ID
	payload := map[string]string{"result": strings.Repeat("r", 2000)}
	for i := 0; i < 30; i++ {
		if e = manager.Append(id, "agent_event:tool_result", payload); e != nil {
			t.Fatal(e)
		}
	}
	if len(payload["result"]) != 2000 {
		t.Fatal("business payload mutated")
	}
	rec, ok := manager.Get(id)
	if !ok || !rec.DetailsTruncated {
		t.Fatalf("missing live partial marker %#v", rec)
	}
	dir := filepath.Join(root, "logs", "timelines", "default", "delegations", id)
	paths, _ := filepath.Glob(filepath.Join(dir, "stream-*.jsonl"))
	for _, p := range paths {
		st, _ := os.Stat(p)
		if st.Size() > 4<<10 {
			t.Fatalf("journal file exceeds cap %d", st.Size())
		}
	}
	if e = manager.Finish(id, StatusCompleted, nil); e != nil {
		t.Fatal(e)
	}
	restored, e := NewManager(filepath.Join(root, "logs", "timelines", "default"), "l1-session")
	if e != nil {
		t.Fatal(e)
	}
	rec, ok = restored.Get(id)
	if !ok || rec.Status != StatusCompleted || !rec.DetailsTruncated {
		t.Fatalf("recovery lost %#v", rec)
	}
	events, e := ReadEvents(dir)
	if e != nil || len(events) > 8 {
		t.Fatalf("unbounded repeated events %d %v", len(events), e)
	}
	var reduced struct {
		Truncated bool `json:"truncated"`
	}
	_ = json.Unmarshal(events[len(events)-1].Payload, &reduced)
	if !reduced.Truncated {
		t.Fatal("missing persisted truncation marker")
	}
}

func TestCompactionCountsRemovalWhenMetadataCreated(t *testing.T) {
	dir, rec := journalFixture(t, 1, 4096)
	rec.Revision = 2
	data, err := json.Marshal(Event{Sequence: 2, Record: rec, Type: "terminal"})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "stream-2026-01-02.jsonl"), append(data, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(dir, "meta.json")); !os.IsNotExist(err) {
		t.Fatalf("fixture already has metadata: %v", err)
	}
	beforeFiles, beforeBytes, err := dispatchDirectorySize(dir)
	if err != nil {
		t.Fatal(err)
	}
	stats, err := CompactCompletedDetailsWithStats(dir)
	if err != nil {
		t.Fatal(err)
	}
	afterFiles, afterBytes, err := dispatchDirectorySize(dir)
	if err != nil {
		t.Fatal(err)
	}
	if stats.DeletedFiles != 1 {
		t.Errorf("deleted files = %d, want 1 (file count %d -> %d)", stats.DeletedFiles, beforeFiles, afterFiles)
	}
	if stats.FreedBytes != beforeBytes-afterBytes {
		t.Errorf("freed bytes = %d, want %d", stats.FreedBytes, beforeBytes-afterBytes)
	}
	if _, err = os.Stat(filepath.Join(dir, "meta.json")); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(dir, "stream-2026-01-01.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("old journal retained: %v", err)
	}
}
