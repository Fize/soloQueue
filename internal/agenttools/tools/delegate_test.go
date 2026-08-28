package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/dispatch"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	workdirutil "github.com/xiaobaitu/soloqueue/internal/infra/workdir"
)

type dispatchTestEvent struct{ content string }

func (dispatchTestEvent) IsAgentEvent()                  {}
func (e dispatchTestEvent) ContentDelta() (string, bool) { return e.content, true }
func (dispatchTestEvent) DoneContent() (string, bool)    { return "", false }
func (dispatchTestEvent) Error() (error, bool)           { return nil, false }
func (dispatchTestEvent) ConfirmRequest() (string, bool) { return "", false }

type dispatchTestTarget struct{}

func (dispatchTestTarget) Ask(context.Context, string) (string, error) { return "done", nil }
func (dispatchTestTarget) AskStream(context.Context, string) (<-chan iface.AgentEvent, error) {
	ch := make(chan iface.AgentEvent, 1)
	ch <- dispatchTestEvent{content: "done"}
	close(ch)
	return ch, nil
}
func (dispatchTestTarget) Confirm(string, string) error { return nil }
func (dispatchTestTarget) ErrorCount() int32            { return 0 }
func (dispatchTestTarget) LastError() string            { return "" }

type failingDispatchTarget struct{ err error }

func (f failingDispatchTarget) Ask(context.Context, string) (string, error) { return "", f.err }
func (f failingDispatchTarget) AskStream(context.Context, string) (<-chan iface.AgentEvent, error) {
	return nil, f.err
}
func (f failingDispatchTarget) Confirm(string, string) error { return nil }
func (f failingDispatchTarget) ErrorCount() int32            { return 0 }
func (f failingDispatchTarget) LastError() string            { return "" }

type timeoutDispatchTarget struct{ dispatchTestTarget }

func (timeoutDispatchTarget) AskStream(ctx context.Context, _ string) (<-chan iface.AgentEvent, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestDelegateToolPanicAfterBeginPersistsFailedTerminal(t *testing.T) {
	m, err := dispatch.NewManager(t.TempDir(), "session-1")
	if err != nil {
		t.Fatal(err)
	}
	resolver := func(context.Context, string, string, string, string, string, string) (iface.Locatable, bool, error) {
		panic("resolver exploded")
	}
	dt := NewDelegateTool("L2", time.Minute, resolver, nil, nil, WorkDirExplicitOrInherited)
	ctx := dispatch.WithScope(iface.ContextWithWorkDir(context.Background(), t.TempDir()), dispatch.Scope{Manager: m})
	if _, err := dt.Execute(ctx, `{"target":"worker","task_name":"panic cleanup","task":"Trigger panic."}`); err == nil || !strings.Contains(err.Error(), "resolver exploded") {
		t.Fatalf("Execute panic error = %v", err)
	}
	records := m.List()
	if len(records) != 1 || records[0].Status != dispatch.StatusFailed {
		t.Fatalf("records after panic = %#v", records)
	}
	retry, err := m.Begin(dispatch.BeginInput{TaskName: "panic cleanup", Task: "Trigger panic.", Requester: "L2", Executor: "worker"})
	if err != nil || retry.Reused || retry.Record.ID == records[0].ID {
		t.Fatalf("retry after panic = %#v, %v", retry, err)
	}
}

func TestDelegateToolAsyncSetupPanicAfterBeginPersistsFailedTerminalOnce(t *testing.T) {
	m, err := dispatch.NewManager(t.TempDir(), "session-1")
	if err != nil {
		t.Fatal(err)
	}
	resolver := func(context.Context, string, string, string, string, string, string) (iface.Locatable, bool, error) {
		panic("async resolver exploded")
	}
	dt := NewDelegateTool("L1", time.Minute, resolver, nil, nil, WorkDirExplicitOrInherited, WithAlwaysAsyncDelegation())
	ctx := dispatch.WithScope(iface.ContextWithWorkDir(context.Background(), t.TempDir()), dispatch.Scope{Manager: m})
	if _, err := dt.ExecuteAsync(ctx, `{"target":"worker","task_name":"async panic cleanup","task":"Trigger async setup panic."}`); err == nil || !strings.Contains(err.Error(), "async resolver exploded") {
		t.Fatalf("ExecuteAsync panic error = %v", err)
	}
	records := m.List()
	if len(records) != 1 || records[0].Status != dispatch.StatusFailed {
		t.Fatalf("records after async setup panic = %#v", records)
	}
	events, err := m.Tail(records[0].ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	terminalCount := 0
	for _, event := range events {
		if event.Type == string(dispatch.StatusFailed) {
			terminalCount++
		}
	}
	if terminalCount != 1 {
		t.Fatalf("failed terminal events = %d, want 1; events=%#v", terminalCount, events)
	}
	retry, err := m.Begin(dispatch.BeginInput{TaskName: "async panic cleanup", Task: "Trigger async setup panic.", Requester: "L1", Executor: "worker"})
	if err != nil || retry.Reused || retry.Record.ID == records[0].ID {
		t.Fatalf("retry after async setup panic = %#v, %v", retry, err)
	}
}

type persistenceFailTarget struct {
	root    string
	manager *dispatch.Manager
}

func (p persistenceFailTarget) Ask(context.Context, string) (string, error) { return "", nil }
func (p persistenceFailTarget) AskStream(context.Context, string) (<-chan iface.AgentEvent, error) {
	record := p.manager.List()[0]
	stream := filepath.Join(p.root, "delegations", record.ID, "stream-"+record.CreatedAt.Format("2006-01-02")+".jsonl")
	if err := os.Chmod(stream, 0o400); err != nil {
		return nil, err
	}
	ch := make(chan iface.AgentEvent, 1)
	ch <- dispatchTestEvent{content: "must not succeed"}
	close(ch)
	return ch, nil
}
func (p persistenceFailTarget) Confirm(string, string) error { return nil }
func (p persistenceFailTarget) ErrorCount() int32            { return 0 }
func (p persistenceFailTarget) LastError() string            { return "" }

func TestDelegateToolSchemaRequiresTaskNameAndDoesNotExposeAsync(t *testing.T) {
	dt := NewDelegateTool("leader", time.Minute, nil, nil, nil, WorkDirInheritOnly)
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(dt.Parameters(), &schema); err != nil {
		t.Fatal(err)
	}
	if _, ok := schema.Properties["async"]; ok {
		t.Fatal("delegate schema must not expose async scheduling")
	}
	if _, ok := schema.Properties["task_name"]; !ok {
		t.Fatal("delegate schema must expose task_name")
	}
	if !slices.Contains(schema.Required, "task_name") {
		t.Fatalf("required = %v, want task_name", schema.Required)
	}
}

func TestDelegateToolPersistsPeerHelpLifecycle(t *testing.T) {
	m, err := dispatch.NewManager(t.TempDir(), "session-1")
	if err != nil {
		t.Fatal(err)
	}
	resolver := func(context.Context, string, string, string, string, string, string) (iface.Locatable, bool, error) {
		return dispatchTestTarget{}, false, nil
	}
	dt := NewDelegateTool("engineering", time.Minute, resolver, nil, nil, WorkDirExplicitOrInherited, WithPeerTarget(func(target string) bool { return target == "security" }))
	ctx := dispatch.WithScope(iface.ContextWithWorkDir(context.Background(), t.TempDir()), dispatch.Scope{Manager: m})
	result, err := dt.Execute(ctx, `{"target":"security","task_name":"review auth","task":"Review authentication."}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "dispatch_id: dlg_") {
		t.Fatalf("result = %q", result)
	}
	records := m.List()
	if len(records) != 1 || records[0].Kind != dispatch.KindPeerHelp || records[0].Status != dispatch.StatusCompleted {
		t.Fatalf("records = %#v", records)
	}
	events, err := m.Tail(records[0].ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || !strings.HasPrefix(events[1].Type, "agent_event:") || events[2].Type != "completed" {
		t.Fatalf("events = %#v", events)
	}
	again, err := m.Begin(dispatch.BeginInput{Kind: dispatch.KindPeerHelp, TaskName: "review auth", Task: "Review authentication.", Requester: "engineering", Executor: "security"})
	if err != nil {
		t.Fatal(err)
	}
	if again.Record.ID == records[0].ID || again.Reused {
		t.Fatalf("terminal retry must receive a new ID: %#v", again)
	}
	if _, err := dt.Execute(ctx, `{"target":"security","task":"missing name"}`); err == nil {
		t.Fatal("managed delegate without task_name must fail")
	}
	if !errors.Is(m.Finish("missing", dispatch.StatusCompleted, nil), os.ErrNotExist) {
		t.Fatal("missing dispatch should report not found")
	}
}

func TestDelegateToolAskStreamSetupFailureReleasesActiveClaim(t *testing.T) {
	m, err := dispatch.NewManager(t.TempDir(), "session-1")
	if err != nil {
		t.Fatal(err)
	}
	resolver := func(context.Context, string, string, string, string, string, string) (iface.Locatable, bool, error) {
		return timeoutDispatchTarget{}, false, nil
	}
	dt := NewDelegateTool("L2", time.Millisecond, resolver, nil, nil, WorkDirExplicitOrInherited)
	ctx := dispatch.WithScope(iface.ContextWithWorkDir(context.Background(), t.TempDir()), dispatch.Scope{Manager: m})
	if _, err := dt.Execute(ctx, `{"target":"worker","task_name":"fail setup","task":"Fail immediately."}`); err == nil {
		t.Fatal("expected setup failure")
	}
	records := m.List()
	if len(records) != 1 || records[0].Status != dispatch.StatusFailed {
		t.Fatalf("records=%#v", records)
	}
	retry, err := m.Begin(dispatch.BeginInput{TaskName: "fail setup", Task: "Fail immediately.", Requester: "L2", Executor: "worker"})
	if err != nil || retry.Reused {
		t.Fatalf("retry=%#v err=%v", retry, err)
	}
}

func TestDelegateToolPropagatesSyncAndAsyncPersistenceFailures(t *testing.T) {
	root := t.TempDir()
	m, err := dispatch.NewManager(root, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	target := persistenceFailTarget{root: root, manager: m}
	resolver := func(context.Context, string, string, string, string, string, string) (iface.Locatable, bool, error) {
		return target, false, nil
	}
	ctx := dispatch.WithScope(iface.ContextWithWorkDir(context.Background(), t.TempDir()), dispatch.Scope{Manager: m})
	syncTool := NewDelegateTool("L2", time.Minute, resolver, nil, nil, WorkDirExplicitOrInherited)
	if _, err := syncTool.Execute(ctx, `{"target":"worker","task_name":"sync persist","task":"Run sync."}`); err == nil {
		t.Fatal("sync persistence failure must propagate")
	}
	for _, record := range m.List() {
		stream := filepath.Join(root, "delegations", record.ID, "stream-"+record.CreatedAt.Format("2006-01-02")+".jsonl")
		_ = os.Chmod(stream, 0o600)
		_ = m.Finish(record.ID, dispatch.StatusFailed, errors.New("cleanup"))
	}

	asyncTool := NewDelegateTool("L1", time.Minute, resolver, nil, nil, WorkDirExplicitOrInherited, WithAlwaysAsyncDelegation())
	action, err := asyncTool.ExecuteAsync(ctx, `{"target":"worker","task_name":"async persist","task":"Run async."}`)
	if err != nil {
		t.Fatal(err)
	}
	record := m.List()[1]
	stream := filepath.Join(root, "delegations", record.ID, "stream-"+record.CreatedAt.Format("2006-01-02")+".jsonl")
	if err := os.Chmod(stream, 0o400); err != nil {
		t.Fatal(err)
	}
	if err := action.OnEvent(dispatchTestEvent{content: "event"}); err == nil {
		t.Fatal("async event persistence failure must propagate")
	}
	if err := action.OnFinish(errors.New("event persistence failed")); err == nil {
		t.Fatal("async terminal persistence failure must propagate")
	}
	_ = os.Chmod(stream, 0o600)
}

func TestDelegateTool_PreferredTimeout_Explicit(t *testing.T) {
	dt := NewDelegateTool("leader", 20*time.Minute, nil, nil, nil, WorkDirInheritOnly)
	if got := dt.PreferredTimeout(); got != 20*time.Minute {
		t.Errorf("PreferredTimeout() = %v, want 20m", got)
	}
}

func TestDelegateTool_PreferredTimeout_Default(t *testing.T) {
	dt := NewDelegateTool("leader", 0, nil, nil, nil, WorkDirInheritOnly)
	if got := dt.PreferredTimeout(); got != DelegateDefaultTimeout {
		t.Errorf("PreferredTimeout() = %v, want DelegateDefaultTimeout (%v)", got, DelegateDefaultTimeout)
	}
}

func TestDelegateTool_PreferredTimeout_Capped(t *testing.T) {
	// PreferredTimeout returns the raw dt.Timeout / DelegateDefaultTimeout;
	// the actual capping to DelegateMaxTimeout happens inside Execute/ExecuteAsync.
	dt := NewDelegateTool("leader", 99*time.Minute, nil, nil, nil, WorkDirInheritOnly)
	if got := dt.PreferredTimeout(); got != 99*time.Minute {
		t.Errorf("PreferredTimeout() = %v, want 99m (uncapped)", got)
	}
}

func TestDelegateTool_InheritOnlySchemaAndResolution(t *testing.T) {
	dt := NewDelegateTool("worker", time.Minute, nil, nil, nil, WorkDirInheritOnly)
	if strings.Contains(string(dt.Parameters()), "work_dir") {
		t.Fatal("inherit-only schema must not expose work_dir")
	}

	ctx := iface.ContextWithWorkDir(context.Background(), "/parent/project")
	got, err := dt.resolveWorkDir(ctx, "/wrong/project")
	if err != nil {
		t.Fatalf("resolveWorkDir: %v", err)
	}
	if got != "/parent/project" {
		t.Fatalf("resolveWorkDir = %q, want parent directory", got)
	}
}

func TestDelegateTool_ExplicitOrInheritedResolution(t *testing.T) {
	dt := NewDelegateTool("leader", time.Minute, nil, nil, nil, WorkDirExplicitOrInherited)
	if !strings.Contains(string(dt.Parameters()), "work_dir") {
		t.Fatal("explicit schema should expose optional work_dir")
	}

	parentDir := t.TempDir()
	selectedDir := t.TempDir()
	ctx := iface.ContextWithWorkDir(context.Background(), parentDir)
	wantSelected, err := workdirutil.NormalizeExistingDir(selectedDir)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := dt.resolveWorkDir(ctx, selectedDir); err != nil || got != wantSelected {
		t.Fatalf("explicit resolveWorkDir = %q, %v", got, err)
	}
	if got, err := dt.resolveWorkDir(ctx, ""); err != nil || got != parentDir {
		t.Fatalf("inherited resolveWorkDir = %q, %v", got, err)
	}
}

func TestDelegateTool_ExecuteAsyncAlwaysAsyncRejectsCycle(t *testing.T) {
	var resolverCalls atomic.Int32
	resolver := func(context.Context, string, string, string, string, string, string) (iface.Locatable, bool, error) {
		resolverCalls.Add(1)
		return nil, false, nil
	}
	dt := NewDelegateTool(
		"research",
		time.Minute,
		resolver,
		nil,
		nil,
		WorkDirInheritOnly,
		WithAlwaysAsyncDelegation(),
	)
	ctx := ContextWithDelegationChain(
		iface.ContextWithWorkDir(context.Background(), "/parent/project"),
		[]string{"engineering", "research"},
	)

	action, err := dt.ExecuteAsync(ctx, `{"target":"Engineering","task_name":"continue loop","task":"continue the loop"}`)
	if err == nil || !strings.Contains(err.Error(), "delegation cycle detected") {
		t.Fatalf("ExecuteAsync error = %v, want delegation cycle error", err)
	}
	if action != nil {
		t.Fatalf("ExecuteAsync action = %#v, want nil", action)
	}
	if got := resolverCalls.Load(); got != 0 {
		t.Fatalf("resolver calls = %d, want 0 when cycle validation fails", got)
	}
}

func TestDelegateTool_ExecuteAsyncAlwaysAsyncRejectsDepth(t *testing.T) {
	var resolverCalls atomic.Int32
	resolver := func(context.Context, string, string, string, string, string, string) (iface.Locatable, bool, error) {
		resolverCalls.Add(1)
		return nil, false, nil
	}
	dt := NewDelegateTool(
		"research",
		time.Minute,
		resolver,
		nil,
		nil,
		WorkDirInheritOnly,
		WithAlwaysAsyncDelegation(),
	)
	ctx := ContextWithDelegationChain(
		iface.ContextWithWorkDir(context.Background(), "/parent/project"),
		[]string{"planning", "engineering"},
	)

	action, err := dt.ExecuteAsync(ctx, `{"target":"research","task_name":"one more hop","task":"one more hop"}`)
	if err == nil || !strings.Contains(err.Error(), "delegation depth limit reached") {
		t.Fatalf("ExecuteAsync error = %v, want delegation depth error", err)
	}
	if action != nil {
		t.Fatalf("ExecuteAsync action = %#v, want nil", action)
	}
	if got := resolverCalls.Load(); got != 0 {
		t.Fatalf("resolver calls = %d, want 0 when depth validation fails", got)
	}
}
