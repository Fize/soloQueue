package dispatch

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/runwatch"
)

func TestManagerBeginPersistsAndDeduplicatesActiveWork(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(root, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	in := BeginInput{
		Kind: KindDelegate, TaskName: "Implement cache", Task: "Add the cache.",
		Requester: "L1", Executor: "engineering",
	}

	const callers = 8
	results := make(chan BeginResult, callers)
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := m.Begin(in)
			results <- result
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)

	var id string
	for err := range errs {
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
	}
	for result := range results {
		if id == "" {
			id = result.Record.ID
		}
		if result.Record.ID != id {
			t.Fatalf("duplicate active IDs: %q and %q", id, result.Record.ID)
		}
	}
	if id == "" {
		t.Fatal("missing dispatch ID")
	}
	if _, err := os.Stat(filepath.Join(root, "delegations", id, "meta.json")); err != nil {
		t.Fatalf("meta.json: %v", err)
	}
	streams, err := filepath.Glob(filepath.Join(root, "delegations", id, "stream-*.jsonl"))
	if err != nil || len(streams) != 1 {
		t.Fatalf("streams = %v, err = %v", streams, err)
	}

	conflict := in
	conflict.Task = "Replace the cache implementation."
	if _, err := m.Begin(conflict); !errors.Is(err, ErrActiveConflict) {
		t.Fatalf("changed-content Begin error = %v, want ErrActiveConflict", err)
	}
}

func TestFinishPersistsTypedWatchdogTerminalCode(t *testing.T) {
	for _, code := range []runwatch.Code{
		runwatch.CodeDelegationOrphaned,
		runwatch.CodeModelSemanticStalled,
		runwatch.CodeCancelledByUser,
	} {
		t.Run(string(code), func(t *testing.T) {
			m, err := NewManager(t.TempDir(), "session-1")
			if err != nil {
				t.Fatal(err)
			}
			created, err := m.Begin(BeginInput{TaskName: string(code), Task: "typed terminal", Requester: "L1", Executor: "worker"})
			if err != nil {
				t.Fatal(err)
			}
			cause := &runwatch.Cause{Code: code, OperationID: created.Record.ID}
			if err := m.Finish(created.Record.ID, StatusInterrupted, cause); err != nil {
				t.Fatal(err)
			}
			record, ok := m.Get(created.Record.ID)
			if !ok || record.TerminalCode != string(code) {
				t.Fatalf("record = %#v, want terminal_code %q", record, code)
			}
		})
	}
}

func TestBeginAndStructuralProgressPersistAuthoritativeRunCorrelation(t *testing.T) {
	m, err := NewManager(t.TempDir(), "session-correlation")
	if err != nil {
		t.Fatal(err)
	}
	created, err := m.Begin(BeginInput{TaskName: "correlate", Task: "work", Requester: "L1", Executor: "worker", RunID: "run-root", Phase: "delegating"})
	if err != nil {
		t.Fatal(err)
	}
	if created.Record.RunID != "run-root" || created.Record.Phase != "delegating" || created.Record.LastProgressAt.IsZero() {
		t.Fatalf("created record lacks correlation: %+v", created.Record)
	}
	if err := m.AppendProgress(created.Record.ID, "agent_event:done", nil, "run-root", "worker_done"); err != nil {
		t.Fatal(err)
	}
	rec, _ := m.Get(created.Record.ID)
	if rec.RunID != "run-root" || rec.Phase != "worker_done" || !rec.LastProgressAt.After(created.Record.LastProgressAt) {
		t.Fatalf("structural progress not durable: %+v", rec)
	}
}

func TestManager_CheckpointCoalescesHighFrequencyProgress(t *testing.T) {
	m, err := NewManager(t.TempDir(), "session-checkpoint")
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	created, err := m.Begin(BeginInput{TaskName: "stream", Task: "Stream tokens", Requester: "L1", Executor: "worker"})
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	writeCount := 0
	originalWrite := m.writeEvent
	m.writeEvent = func(file *os.File, data []byte) (int, error) {
		writeCount++
		return originalWrite(file, data)
	}

	for range 1_000 {
		if err := m.Checkpoint(created.Record.ID, "run-1", "streaming", 30*time.Second); err != nil {
			t.Fatalf("Checkpoint() error = %v", err)
		}
	}
	if writeCount > 1 {
		t.Fatalf("1,000 progress pulses produced %d durable writes", writeCount)
	}
	record, ok := m.Get(created.Record.ID)
	if !ok || record.RunID != "run-1" || record.Phase != "streaming" || record.LastProgressAt.IsZero() {
		t.Fatalf("checkpoint record = %+v, found=%v", record, ok)
	}
}

func TestBeginProjectionAndCompensationFailureStaysPersistencePending(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(root, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	var writes atomic.Int32
	m.writeMeta = func(string, any) error { return errors.New("injected meta failure") }
	m.writeEvent = func(f *os.File, data []byte) (int, error) {
		if writes.Add(1) == 1 {
			return f.Write(data)
		}
		return 0, errors.New("injected terminal failure")
	}
	in := BeginInput{TaskName: "setup persistence", Task: "Do not start a worker.", Requester: "L1", Executor: "dev"}
	result, err := m.Begin(in)
	if err == nil {
		t.Fatal("Begin must expose projection and compensation failures")
	}
	if result.Record.Status != StatusPersistencePending {
		t.Fatalf("Begin record status = %q, want persistence_pending", result.Record.Status)
	}
	record, ok := m.Get(result.Record.ID)
	if !ok || record.Status != StatusPersistencePending {
		t.Fatalf("dead dispatch must remain retryable, got %#v ok=%v", record, ok)
	}
	if retry, err := m.Begin(in); !errors.Is(err, ErrPersistencePending) || retry.Reused {
		t.Fatalf("Begin while failed terminal is pending = %#v, %v", retry, err)
	}

	m.writeEvent = (*os.File).Write
	m.writeMeta = writeAtomicJSON
	retry, err := m.Begin(in)
	if err != nil || retry.Reused || retry.Record.ID == result.Record.ID {
		t.Fatalf("Begin after persistence recovery = %#v, %v", retry, err)
	}
	if old, ok := m.Get(result.Record.ID); !ok || old.Status != StatusFailed {
		t.Fatalf("old setup failure terminal state = %#v ok=%v", old, ok)
	}
}

func TestAppendRollsBackPartialTrailingWrite(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(root, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	created, err := m.Begin(BeginInput{TaskName: "partial write", Task: "Persist output.", Requester: "L1", Executor: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	m.writeEvent = func(f *os.File, data []byte) (int, error) {
		n, writeErr := f.Write(data[:len(data)/2])
		return n, errors.Join(writeErr, errors.New("injected partial write failure"))
	}
	if err := m.Append(created.Record.ID, "partial", map[string]string{"output": "broken"}); err == nil {
		t.Fatal("partial append must fail")
	}
	m.writeEvent = (*os.File).Write
	events, err := m.Tail(created.Record.ID, 0)
	if err != nil || len(events) != 1 || events[0].Type != "created" {
		t.Fatalf("tail after partial failure = %#v, %v", events, err)
	}
	if err := m.Append(created.Record.ID, "recovered", map[string]string{"output": "ok"}); err != nil {
		t.Fatalf("append after storage recovery: %v", err)
	}
	events, err = m.Tail(created.Record.ID, 0)
	if err != nil || len(events) != 2 || events[1].Type != "recovered" {
		t.Fatalf("tail after retry = %#v, %v", events, err)
	}
}

func TestManagerRestartRepairsProjectionAndInterruptsActiveWork(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(root, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	created, err := m.Begin(BeginInput{Kind: KindPeerHelp, TaskName: "security review", Task: "Review auth.", Requester: "engineering", Executor: "security", RootID: "dlg_root", ParentID: "dlg_parent"})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Append(created.Record.ID, "agent_event:test", map[string]string{"delta": "working"}); err != nil {
		t.Fatal(err)
	}
	claimPath := filepath.Join(root, "delegations", ".active", created.Record.TaskKey+".json")
	if err := os.WriteFile(claimPath, []byte(`{"dispatch_id":"`+created.Record.ID+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	metaPath := filepath.Join(root, "delegations", created.Record.ID, "meta.json")
	if err := os.WriteFile(metaPath, []byte(`{"revision":0}`), 0o600); err != nil {
		t.Fatal(err)
	}

	restarted, err := NewManager(root, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	record, ok := restarted.Get(created.Record.ID)
	if !ok {
		t.Fatal("restarted manager did not load dispatch")
	}
	if record.Status != StatusInterrupted || record.Kind != KindPeerHelp || record.TerminalCode != "interrupted_by_restart" {
		t.Fatalf("reconciled record = %#v", record)
	}
	if record.RootID != "dlg_root" || record.ParentID != "dlg_parent" {
		t.Fatalf("hierarchy lost: %#v", record)
	}
	var projected Record
	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &projected); err != nil {
		t.Fatal(err)
	}
	if projected.Status != StatusInterrupted || projected.Revision != record.Revision {
		t.Fatalf("projection = %#v, record = %#v", projected, record)
	}
	events, err := restarted.Tail(record.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[1].Type != string(StatusInterrupted) {
		t.Fatalf("tail = %#v", events)
	}
}

func TestLargeEventsTailAndReconcile(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(root, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	created, err := m.Begin(BeginInput{TaskName: "large", Task: "Persist large output.", Requester: "L1", Executor: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	large := strings.Repeat("x", 96*1024)
	if err := m.Append(created.Record.ID, "agent_event:large", map[string]string{"delta": large}); err != nil {
		t.Fatal(err)
	}
	events, err := m.Tail(created.Record.ID, 1)
	if err != nil || len(events) != 1 || !bytes.Contains(events[0].Payload, []byte(large[:1024])) {
		t.Fatalf("tail len=%d err=%v", len(events), err)
	}
	reloaded, err := NewManager(root, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if record, ok := reloaded.Get(created.Record.ID); !ok || record.Status != StatusInterrupted {
		t.Fatalf("reloaded=%#v ok=%v", record, ok)
	}
}

func TestReconcileRejectsCorruptStream(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "delegations", "dlg_corrupt")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stream-2026-01-01.jsonl"), []byte("{not-json}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewManager(root, "session-1"); err == nil {
		t.Fatal("corrupt stream must fail reconciliation")
	}
}

func TestManagersForDistinctSessionRootsAreIsolated(t *testing.T) {
	base := t.TempDir()
	m1, err := NewManager(filepath.Join(base, "session-1"), "session-1")
	if err != nil {
		t.Fatal(err)
	}
	created, err := m1.Begin(BeginInput{TaskName: "owned", Task: "Session one work.", Requester: "L1", Executor: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	m2, err := NewManager(filepath.Join(base, "session-2"), "session-2")
	if err != nil {
		t.Fatal(err)
	}
	if len(m2.List()) != 0 {
		t.Fatalf("foreign records exposed: %#v", m2.List())
	}
	if _, ok := m2.Get(created.Record.ID); ok {
		t.Fatal("foreign record exposed by Get")
	}
	if record, ok := m1.Get(created.Record.ID); !ok || record.Status != StatusRunning {
		t.Fatalf("owner record interrupted: %#v", record)
	}
}

func TestTerminalPersistencePendingRecoversBeforeSameTaskResubmission(t *testing.T) {
	root := t.TempDir()
	m, err := NewManager(root, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	created, err := m.Begin(BeginInput{TaskName: "persistence", Task: "Persist safely.", Requester: "L1", Executor: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	stream := filepath.Join(root, "delegations", created.Record.ID, "stream-"+created.Record.CreatedAt.Format("2006-01-02")+".jsonl")
	if err := os.Chmod(stream, 0o400); err != nil {
		t.Fatal(err)
	}
	before := m.records[created.Record.ID]
	if err := m.Append(created.Record.ID, "must-fail", map[string]string{"x": "y"}); err == nil {
		t.Fatal("Append must expose JSONL failure")
	}
	if after := m.records[created.Record.ID]; after.Revision != before.Revision {
		t.Fatalf("Append pre-mutated state: before=%d after=%d", before.Revision, after.Revision)
	}
	if err := m.Finish(created.Record.ID, StatusCompleted, nil); err == nil {
		t.Fatal("Finish must expose JSONL failure")
	}
	if after, ok := m.Get(created.Record.ID); !ok || after.Status != StatusPersistencePending {
		t.Fatalf("ended worker must be persistence-pending, got %#v ok=%v", after, ok)
	}
	listed := m.List()
	if len(listed) != 1 || listed[0].Status != StatusPersistencePending {
		t.Fatalf("List must expose persistence-pending worker, got %#v", listed)
	}
	claimPath := filepath.Join(root, "delegations", ".active", created.Record.TaskKey+".json")
	if _, err := os.Stat(claimPath); err != nil {
		t.Fatalf("active claim released before terminal commit: %v", err)
	}
	in := BeginInput{TaskName: "persistence", Task: "Persist safely.", Requester: "L1", Executor: "dev"}
	if result, err := m.Begin(in); !errors.Is(err, ErrPersistencePending) || result.Reused {
		t.Fatalf("Begin while terminal is pending = %#v, %v", result, err)
	}
	if err := os.Chmod(stream, 0o600); err != nil {
		t.Fatal(err)
	}
	retry, err := m.Begin(in)
	if err != nil || retry.Reused || retry.Record.ID == created.Record.ID {
		t.Fatalf("Begin after storage recovery = %#v, %v", retry, err)
	}
	if old, ok := m.Get(created.Record.ID); !ok || old.Status != StatusCompleted {
		t.Fatalf("old terminal recovery = %#v ok=%v", old, ok)
	}
	active, err := readClaim(claimPath)
	if err != nil || active.ID != retry.Record.ID {
		t.Fatalf("active claim after terminal recovery = %#v, %v", active, err)
	}
}
