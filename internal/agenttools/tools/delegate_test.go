package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/dispatch"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/infra/telemetryctx"
	workdirutil "github.com/xiaobaitu/soloqueue/internal/infra/workdir"
	"github.com/xiaobaitu/soloqueue/internal/runwatch"
)

type dispatchTestEvent struct{ content string }

func (dispatchTestEvent) IsAgentEvent()                  {}
func (e dispatchTestEvent) ContentDelta() (string, bool) { return e.content, true }
func (dispatchTestEvent) DoneContent() (string, bool)    { return "", false }
func (dispatchTestEvent) Error() (error, bool)           { return nil, false }

type dispatchTestTarget struct{}

func (dispatchTestTarget) Ask(context.Context, string) (string, error) { return "done", nil }
func (dispatchTestTarget) AskStream(context.Context, string) (<-chan iface.AgentEvent, error) {
	ch := make(chan iface.AgentEvent, 1)
	ch <- dispatchTestEvent{content: "done"}
	close(ch)
	return ch, nil
}
func (dispatchTestTarget) ErrorCount() int32 { return 0 }
func (dispatchTestTarget) LastError() string { return "" }

type timeoutDispatchTarget struct{ dispatchTestTarget }

func (timeoutDispatchTarget) AskStream(ctx context.Context, _ string) (<-chan iface.AgentEvent, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

type routedDispatchTarget struct {
	routes   []*iface.ModelOverrideParams
	setCalls atomic.Int32
}

func TestDelegateTargetValidatorRejectsUnknownTeams(t *testing.T) {
	resolverCalled := false
	resolver := func(context.Context, string, string, string, string, string, string) (iface.Locatable, bool, error) {
		resolverCalled = true
		return dispatchTestTarget{}, false, nil
	}
	allowed := func(target string) bool { return strings.EqualFold(target, "engineering") }
	dt := NewDelegateTool("assistant", time.Minute, resolver, nil, nil, WorkDirExplicitOrInherited,
		WithTargetValidator(allowed), WithAlwaysAsyncDelegation())
	ctx := iface.ContextWithWorkDir(context.Background(), t.TempDir())
	if _, err := dt.Execute(ctx, `{"target":"personal-memory","task":"lookup"}`); err == nil || !strings.Contains(err.Error(), "not an available Team") {
		t.Fatalf("sync unknown target error = %v", err)
	}
	if _, err := dt.ExecuteAsync(ctx, `{"target":"personal-memory","task":"lookup"}`); err == nil || !strings.Contains(err.Error(), "not an available Team") {
		t.Fatalf("async unknown target error = %v", err)
	}
	if resolverCalled {
		t.Fatal("target resolver was called for a rejected Team")
	}
}

func (t *routedDispatchTarget) Ask(context.Context, string) (string, error) { return "", nil }
func (t *routedDispatchTarget) AskStream(ctx context.Context, _ string) (<-chan iface.AgentEvent, error) {
	t.routes = append(t.routes, iface.ModelOverrideFromContext(ctx))
	ch := make(chan iface.AgentEvent)
	close(ch)
	return ch, nil
}
func (t *routedDispatchTarget) SetModelOverride(*iface.ModelOverrideParams) { t.setCalls.Add(1) }
func (*routedDispatchTarget) ErrorCount() int32                             { return 0 }
func (*routedDispatchTarget) LastError() string                             { return "" }

func TestDelegateRoutingIsRequestScopedForSyncAndAsync(t *testing.T) {
	target := &routedDispatchTarget{}
	resolver := func(context.Context, string, string, string, string, string, string) (iface.Locatable, bool, error) {
		return target, false, nil
	}
	params := &iface.ModelOverrideParams{ProviderID: "routed-provider", ModelID: "routed-model", TaskType: "engineering"}
	ctx := iface.ContextWithModelOverride(iface.ContextWithWorkDir(context.Background(), t.TempDir()), params)

	syncTool := NewDelegateTool("L2", time.Minute, resolver, nil, nil, WorkDirExplicitOrInherited)
	if _, err := syncTool.Execute(ctx, `{"target":"worker","task":"sync"}`); err != nil {
		t.Fatal(err)
	}
	asyncTool := NewDelegateTool("L1", time.Minute, resolver, nil, nil, WorkDirExplicitOrInherited, WithAlwaysAsyncDelegation())
	action, err := asyncTool.ExecuteAsync(ctx, `{"target":"worker","task":"async"}`)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := action.Target.AskStream(action.Context, action.Prompt)
	if err != nil {
		t.Fatal(err)
	}
	for range stream {
	}
	// An independent call must not inherit the delegated request route.
	stream, err = target.AskStream(context.Background(), "independent")
	if err != nil {
		t.Fatal(err)
	}
	for range stream {
	}
	if target.setCalls.Load() != 0 {
		t.Fatalf("delegate mutated reusable target route %d times", target.setCalls.Load())
	}
	if len(target.routes) != 3 || target.routes[0] == nil || target.routes[1] == nil || target.routes[2] != nil {
		t.Fatalf("captured routes = %#v, want routed sync/async then nil independent", target.routes)
	}
	for i := 0; i < 2; i++ {
		if target.routes[i].ModelID != params.ModelID || target.routes[i].ProviderID != params.ProviderID {
			t.Fatalf("delegated route[%d] = %#v", i, target.routes[i])
		}
	}
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
	watch := runwatch.NewManager(runwatch.Policy{RootIdle: time.Minute, OrphanIdle: 10 * time.Minute})
	t.Cleanup(watch.Close)
	watchCtx, rootWatch, err := watch.Start(context.Background(), runwatch.Metadata{RunID: "sync-panic"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := dispatch.WithScope(iface.ContextWithWorkDir(watchCtx, t.TempDir()), dispatch.Scope{Manager: m})
	if _, err := dt.Execute(ctx, `{"target":"worker","task_name":"panic cleanup","task":"Trigger panic."}`); err == nil || !strings.Contains(err.Error(), "resolver exploded") {
		t.Fatalf("Execute panic error = %v", err)
	}
	records := m.List()
	if len(records) != 1 || records[0].Status != dispatch.StatusFailed {
		t.Fatalf("records after panic = %#v", records)
	}
	if snapshot, ok := rootWatch.Snapshot(); !ok || snapshot.WatchdogDueAt.Sub(snapshot.LastProgressAt) != time.Minute {
		t.Fatalf("sync setup leaked watchdog child: %+v, ok=%v", snapshot, ok)
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
	watch := runwatch.NewManager(runwatch.Policy{RootIdle: time.Minute, OrphanIdle: 10 * time.Minute})
	t.Cleanup(watch.Close)
	watchCtx, rootWatch, err := watch.Start(context.Background(), runwatch.Metadata{RunID: "async-panic"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := dispatch.WithScope(iface.ContextWithWorkDir(watchCtx, t.TempDir()), dispatch.Scope{Manager: m})
	if _, err := dt.ExecuteAsync(ctx, `{"target":"worker","task_name":"async panic cleanup","task":"Trigger async setup panic."}`); err == nil || !strings.Contains(err.Error(), "async resolver exploded") {
		t.Fatalf("ExecuteAsync panic error = %v", err)
	}
	records := m.List()
	if len(records) != 1 || records[0].Status != dispatch.StatusFailed {
		t.Fatalf("records after async setup panic = %#v", records)
	}
	if snapshot, ok := rootWatch.Snapshot(); !ok || snapshot.WatchdogDueAt.Sub(snapshot.LastProgressAt) != time.Minute {
		t.Fatalf("async setup leaked watchdog child: %+v, ok=%v", snapshot, ok)
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

// asyncSetupFailure captures what a caller can observe after ExecuteAsync fails
// while setting up a delegation.
type asyncSetupFailure struct {
	err            error
	records        []dispatch.Record
	delegationCtx  context.Context
	liveAtTerminal bool
}

// runAsyncSetupFailure delegates from a managed, watched parent run. With
// rejectLocalOperations the parent run has already finished, so the watchdog
// refuses the delegation's local operation.
func runAsyncSetupFailure(t *testing.T, resolver LocateOrSpawnResolver, rejectLocalOperations bool) asyncSetupFailure {
	t.Helper()
	m, err := dispatch.NewManager(t.TempDir(), "session-1")
	if err != nil {
		t.Fatal(err)
	}
	var out asyncSetupFailure
	dt := NewDelegateTool("L1", time.Minute, resolver, nil, nil, WorkDirExplicitOrInherited, WithAlwaysAsyncDelegation())
	dt.delegationContextHook = func(ctx context.Context) { out.delegationCtx = ctx }
	watch := runwatch.NewManager(runwatch.Policy{RootIdle: time.Minute, OrphanIdle: 10 * time.Minute})
	t.Cleanup(watch.Close)
	watchCtx, rootWatch, err := watch.Start(context.Background(), runwatch.Metadata{RunID: "async-setup-failure"})
	if err != nil {
		t.Fatal(err)
	}
	if rejectLocalOperations {
		rootWatch.Complete()
	}
	scope := dispatch.Scope{Manager: m, OnTerminal: func(dispatch.Record) {
		out.liveAtTerminal = out.delegationCtx != nil && out.delegationCtx.Err() == nil
	}}
	ctx := dispatch.WithScope(iface.ContextWithWorkDir(watchCtx, t.TempDir()), scope)
	action, err := dt.ExecuteAsync(ctx, `{"target":"worker","task_name":"setup failure","task":"Fail during async setup."}`)
	if action != nil {
		t.Fatalf("ExecuteAsync returned an action after a setup failure: %#v", action)
	}
	out.err = err
	out.records = m.List()
	return out
}

// requireReleased checks the contract shared by every failed async setup: the
// failure is returned and recorded as the dispatch's failed terminal state while
// the delegation context is still live, and only then is that context cancelled
// with the failure as its cause.
func (f asyncSetupFailure) requireReleased(t *testing.T, wantErr string) {
	t.Helper()
	if f.err == nil || !strings.Contains(f.err.Error(), wantErr) {
		t.Fatalf("ExecuteAsync error = %v, want it to contain %q", f.err, wantErr)
	}
	if len(f.records) != 1 || f.records[0].Status != dispatch.StatusFailed {
		t.Fatalf("records after setup failure = %#v, want one failed dispatch", f.records)
	}
	if f.delegationCtx == nil {
		t.Fatal("async delegation context was never created")
	}
	if !f.liveAtTerminal {
		t.Fatal("delegation context was not live when the failed terminal state was recorded")
	}
	if f.delegationCtx.Err() == nil {
		t.Fatal("delegation context stayed live after the setup failure")
	}
	if cause := context.Cause(f.delegationCtx); !errors.Is(cause, f.err) {
		t.Fatalf("delegation context cause = %v, want the setup failure %v", cause, f.err)
	}
}

func TestDelegateToolAsyncLocalOperationFailureCancelsDelegationContext(t *testing.T) {
	resolver := func(context.Context, string, string, string, string, string, string) (iface.Locatable, bool, error) {
		t.Error("target resolver ran although the delegation's local operation was rejected")
		return nil, false, errors.New("unexpected resolver call")
	}
	runAsyncSetupFailure(t, resolver, true).requireReleased(t, "parent is not active")
}

func TestDelegateToolAsyncSetupFailureAfterLocalOperationCancelsDelegationContext(t *testing.T) {
	tests := []struct {
		name     string
		resolver LocateOrSpawnResolver
		wantErr  string
	}{
		{
			name: "target unavailable",
			resolver: func(context.Context, string, string, string, string, string, string) (iface.Locatable, bool, error) {
				return nil, false, errors.New("no capacity")
			},
			wantErr: "no capacity",
		},
		{
			name: "setup panic",
			resolver: func(context.Context, string, string, string, string, string, string) (iface.Locatable, bool, error) {
				panic("resolver exploded")
			},
			wantErr: "resolver exploded",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runAsyncSetupFailure(t, tt.resolver, false).requireReleased(t, tt.wantErr)
		})
	}
}

func TestDelegateToolAsyncSuccessLeavesDelegationContextToDispatchCancellation(t *testing.T) {
	m, err := dispatch.NewManager(t.TempDir(), "session-1")
	if err != nil {
		t.Fatal(err)
	}
	resolver := func(context.Context, string, string, string, string, string, string) (iface.Locatable, bool, error) {
		return dispatchTestTarget{}, false, nil
	}
	dt := NewDelegateTool("L1", time.Minute, resolver, nil, nil, WorkDirExplicitOrInherited, WithAlwaysAsyncDelegation())
	ctx := dispatch.WithScope(iface.ContextWithWorkDir(context.Background(), t.TempDir()), dispatch.Scope{Manager: m})
	action, err := dt.ExecuteAsync(ctx, `{"target":"worker","task_name":"hand off","task":"Setup succeeds."}`)
	if err != nil || action == nil {
		t.Fatalf("ExecuteAsync = %#v, %v", action, err)
	}
	if err := action.Context.Err(); err != nil {
		t.Fatalf("successful setup cancelled the delegation context: %v", err)
	}
	if err := m.Cancel(action.DispatchID, "operator stop"); err != nil {
		t.Fatal(err)
	}
	if cause := context.Cause(action.Context); !errors.Is(cause, dispatch.ErrDelegationCancelled) {
		t.Fatalf("delegation context cause after dispatch cancellation = %v", cause)
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
func (p persistenceFailTarget) ErrorCount() int32 { return 0 }
func (p persistenceFailTarget) LastError() string { return "" }

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
	for _, field := range []string{"dynamic_worker", "parallel_tasks", "efficiency_reason"} {
		if _, ok := schema.Properties[field]; !ok {
			t.Fatalf("delegate schema must expose %s", field)
		}
	}
}

func TestDelegateToolDynamicWorkerRequiresExplicitEfficiencyPlan(t *testing.T) {
	resolver := func(context.Context, string, string, string, string, string, string) (iface.Locatable, bool, error) {
		return dispatchTestTarget{}, false, nil
	}
	ctx := iface.ContextWithWorkDir(context.Background(), t.TempDir())
	plain := NewDelegateTool("leader", time.Minute, resolver, nil, nil, WorkDirExplicitOrInherited)
	if _, err := plain.Execute(ctx, `{"target":"dynamic-1","task_name":"parallel-check","task":"check","dynamic_worker":true,"parallel_tasks":["a","b"],"efficiency_reason":"parallel"}`); err == nil || !strings.Contains(err.Error(), "not enabled") {
		t.Fatalf("plain delegate should reject dynamic worker: %v", err)
	}

	dynamic := NewDelegateTool("leader", time.Minute, resolver, nil, nil, WorkDirExplicitOrInherited, WithDynamicWorkers())
	if _, err := dynamic.Execute(ctx, `{"target":"dynamic-1","task_name":"parallel-check","task":"check","dynamic_worker":true,"parallel_tasks":["a"],"efficiency_reason":"parallel"}`); err == nil || !strings.Contains(err.Error(), "at least two") {
		t.Fatalf("dynamic delegate should require two independent tasks: %v", err)
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
	if strings.Contains(result, "dispatch_id") || strings.Contains(result, "dlg_") {
		t.Fatalf("delegation result leaked internal identifier: %q", result)
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
	parent, cancel := context.WithCancel(context.Background())
	cancel()
	ctx := dispatch.WithScope(iface.ContextWithWorkDir(parent, t.TempDir()), dispatch.Scope{Manager: m})
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

func TestDelegateToolWatchdogSetupFailureReleasesDurableClaim(t *testing.T) {
	for _, async := range []bool{false, true} {
		t.Run(map[bool]string{false: "sync", true: "async"}[async], func(t *testing.T) {
			m, err := dispatch.NewManager(t.TempDir(), "session-watch-setup")
			if err != nil {
				t.Fatal(err)
			}
			watch := runwatch.NewManager(runwatch.Policy{RootIdle: time.Minute})
			t.Cleanup(watch.Close)
			watchCtx, root, err := watch.Start(context.Background(), runwatch.Metadata{RunID: "terminal-parent"})
			if err != nil {
				t.Fatal(err)
			}
			root.Fail(&runwatch.Cause{Code: runwatch.CodeCancelledByUser, OperationID: "terminal-parent"})
			resolver := func(context.Context, string, string, string, string, string, string) (iface.Locatable, bool, error) {
				t.Fatal("resolver called after watchdog setup failed")
				return nil, false, nil
			}
			opts := []DelegateToolOption{}
			if async {
				opts = append(opts, WithAlwaysAsyncDelegation())
			}
			dt := NewDelegateTool("L1", time.Minute, resolver, nil, nil, WorkDirExplicitOrInherited, opts...)
			ctx := dispatch.WithScope(iface.ContextWithWorkDir(watchCtx, t.TempDir()), dispatch.Scope{Manager: m})
			args := `{"target":"worker","task_name":"watch setup","task":"fail"}`
			if async {
				_, err = dt.ExecuteAsync(ctx, args)
			} else {
				_, err = dt.Execute(ctx, args)
			}
			if err == nil {
				t.Fatal("expected watchdog setup failure")
			}
			records := m.List()
			if len(records) != 1 || records[0].Status != dispatch.StatusFailed {
				t.Fatalf("durable claim not terminalized: %+v", records)
			}
			retry, err := m.Begin(dispatch.BeginInput{TaskName: "watch setup", Task: "fail", Requester: "L1", Executor: "worker"})
			if err != nil || retry.Reused {
				t.Fatalf("claim not released for retry: %+v, %v", retry, err)
			}
		})
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

func TestDelegateTool_DoesNotDeclareTotalTimeout(t *testing.T) {
	dt := NewDelegateTool("leader", 0, nil, nil, nil, WorkDirInheritOnly)
	if _, ok := any(dt).(interface{ PreferredTimeout() time.Duration }); ok {
		t.Fatal("DelegateTool still declares a total runtime timeout")
	}
}

func TestDelegateTool_DeclaresProgressSupervision(t *testing.T) {
	dt := NewDelegateTool("leader", 0, nil, nil, nil, WorkDirInheritOnly)
	progressAware, ok := any(dt).(ProgressSupervised)
	if !ok || !progressAware.ProgressSupervised() {
		t.Fatal("DelegateTool does not opt into progress supervision")
	}
}

func TestDelegateTool_ProgressCheckpointIntervalIsConfigurable(t *testing.T) {
	dt := NewDelegateTool("leader", 0, nil, nil, nil, WorkDirInheritOnly,
		WithProgressCheckpointInterval(7*time.Second))
	if dt.progressCheckpointInterval != 7*time.Second {
		t.Fatalf("progress checkpoint interval = %s, want 7s", dt.progressCheckpointInterval)
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

func TestL1DynamicWorkerDoesNotRequireParallelPlan(t *testing.T) {
	resolver := func(context.Context, string, string, string, string, string, string) (iface.Locatable, bool, error) {
		return dispatchTestTarget{}, false, nil
	}
	dt := NewDelegateTool("assistant", time.Minute, resolver, nil, nil, WorkDirExplicitOrInherited, WithL1DynamicWorkers())
	ctx := iface.ContextWithWorkDir(context.Background(), t.TempDir())
	if _, err := dt.Execute(ctx, `{"target":"temporary","task_name":"investigate","task":"investigate","dynamic_worker":true}`); err != nil {
		t.Fatal(err)
	}
}

func TestDynamicWorkerRejectsVisibleTarget(t *testing.T) {
	dt := NewDelegateTool("assistant", time.Minute, nil, nil, nil, WorkDirExplicitOrInherited, WithL1DynamicWorkers(), WithTargetValidator(func(target string) bool { return target == "engineering" }))
	ctx := iface.ContextWithWorkDir(context.Background(), t.TempDir())
	if _, err := dt.Execute(ctx, `{"target":"engineering","task_name":"x","task":"x","dynamic_worker":true}`); err == nil || !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("error = %v", err)
	}
}

type panicOnInstanceTarget struct {
	dispatchTestTarget
	done atomic.Int32
}

func (*panicOnInstanceTarget) InstanceID() string  { panic("setup failed") }
func (p *panicOnInstanceTarget) OnDelegationDone() { p.done.Add(1) }
func TestAsyncSetupPanicCleansSpawnedExecutor(t *testing.T) {
	manager, err := dispatch.NewManager(t.TempDir(), "session")
	if err != nil {
		t.Fatal(err)
	}
	target := &panicOnInstanceTarget{}
	resolver := func(context.Context, string, string, string, string, string, string) (iface.Locatable, bool, error) {
		return target, true, nil
	}
	dt := NewDelegateTool("assistant", time.Minute, resolver, nil, nil, WorkDirExplicitOrInherited, WithL1DynamicWorkers(), WithAlwaysAsyncDelegation())
	ctx := dispatch.WithScope(iface.ContextWithWorkDir(context.Background(), t.TempDir()), dispatch.Scope{Manager: manager})
	_, err = dt.ExecuteAsync(ctx, `{"target":"temp","task_name":"test","task":"test","dynamic_worker":true}`)
	if err == nil || target.done.Load() != 1 {
		t.Fatalf("error=%v, cleanup=%d", err, target.done.Load())
	}
}

func TestL1DynamicWorkerRejectsUnsupportedSkillID(t *testing.T) {
	dt := NewDelegateTool("assistant", time.Minute, nil, nil, nil, WorkDirExplicitOrInherited, WithL1DynamicWorkers(), WithAlwaysAsyncDelegation())
	ctx := iface.ContextWithWorkDir(context.Background(), t.TempDir())
	args := `{"target":"temp","task_name":"test","task":"test","dynamic_worker":true,"skill_id":"example"}`
	if _, err := dt.ExecuteAsync(ctx, args); err == nil || !strings.Contains(err.Error(), "skill_id") {
		t.Fatalf("error = %v", err)
	}
}

type delegationWatchClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *delegationWatchClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *delegationWatchClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type silentDelegationTarget struct {
	dispatchTestTarget
	started chan struct{}
}

func (t silentDelegationTarget) AskStream(ctx context.Context, _ string) (<-chan iface.AgentEvent, error) {
	watch := runwatch.HandleFromContext(ctx)
	if _, err := watch.BeginOperation(runwatch.KindModel, "silent-model", runwatch.Policy{}); err != nil {
		return nil, err
	}
	close(t.started)
	return make(chan iface.AgentEvent), nil
}

func TestSyncDelegateWatchdogFailureStopsSilentStreamAndPreservesParent(t *testing.T) {
	clock := &delegationWatchClock{now: time.Unix(1700000000, 0)}
	watch := runwatch.NewManager(runwatch.DefaultPolicy(), runwatch.WithClock(clock))
	defer watch.Close()
	rootCtx, root, err := watch.Start(context.Background(), runwatch.Metadata{RunID: "sync-silent"})
	if err != nil {
		t.Fatal(err)
	}
	defer root.Complete()
	manager, err := dispatch.NewManager(t.TempDir(), "sync-silent")
	if err != nil {
		t.Fatal(err)
	}
	terminal := make(chan dispatch.Record, 1)
	ctx := dispatch.WithScope(iface.ContextWithWorkDir(rootCtx, t.TempDir()), dispatch.Scope{Manager: manager, OnTerminal: func(r dispatch.Record) { terminal <- r }})
	target := silentDelegationTarget{started: make(chan struct{})}
	resolver := func(context.Context, string, string, string, string, string, string) (iface.Locatable, bool, error) {
		return target, false, nil
	}
	delegate := NewDelegateTool("L1", 0, resolver, nil, nil, WorkDirExplicitOrInherited)
	result := make(chan error, 1)
	go func() {
		_, err := delegate.Execute(ctx, `{"target":"worker","task_name":"silent","task":"work"}`)
		result <- err
	}()
	select {
	case <-target.started:
	case <-time.After(2 * time.Second):
		t.Fatal("child never started")
	}
	clock.advance(3 * time.Minute)
	watch.Scan()
	select {
	case err := <-result:
		if runwatch.CodeOf(err) != runwatch.CodeModelTransportStalled {
			t.Fatalf("error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("sync delegation did not stop after watchdog expiry")
	}
	if rootCtx.Err() != nil {
		t.Fatalf("parent cancelled: %v", context.Cause(rootCtx))
	}
	select {
	case record := <-terminal:
		if record.Status != dispatch.StatusFailed || !strings.Contains(record.Error, string(runwatch.CodeModelTransportStalled)) {
			t.Fatalf("terminal = %+v", record)
		}
	default:
		t.Fatal("missing failed terminal notification")
	}
	records := manager.List()
	if len(records) != 1 || records[0].Status != dispatch.StatusFailed {
		t.Fatalf("records = %+v", records)
	}
	next, err := root.BeginOperation(runwatch.KindModel, "recovery", runwatch.Policy{})
	if err != nil {
		t.Fatalf("parent unusable after failure: %v", err)
	}
	next.Complete()
}

type identityCapturingTarget struct {
	dispatchTestTarget
	instanceID string
	childCtx   chan context.Context
}

func (t *identityCapturingTarget) InstanceID() string { return t.instanceID }

func (t *identityCapturingTarget) AskStream(ctx context.Context, prompt string) (<-chan iface.AgentEvent, error) {
	t.childCtx <- ctx
	return t.dispatchTestTarget.AskStream(ctx, prompt)
}

// The dispatch identity must exist before the executor is known and be published
// again once it is assigned, so the UI and the transcript can attribute the
// delegation without guessing from the agent name.
func TestDelegateToolPublishesDelegationIdentity(t *testing.T) {
	manager, err := dispatch.NewManager(t.TempDir(), "session")
	if err != nil {
		t.Fatal(err)
	}
	target := &identityCapturingTarget{instanceID: "inst-a", childCtx: make(chan context.Context, 1)}
	resolver := func(context.Context, string, string, string, string, string, string) (iface.Locatable, bool, error) {
		return target, false, nil
	}
	dt := NewDelegateTool("L2", time.Minute, resolver, nil, nil, WorkDirExplicitOrInherited)
	var seen []dispatch.Record
	ctx := dispatch.WithScope(iface.ContextWithWorkDir(context.Background(), t.TempDir()), dispatch.Scope{
		Manager: manager,
		OnBegin: func(record dispatch.Record) { seen = append(seen, record) },
	})

	if _, err := dt.Execute(ctx, `{"target":"worker","task_name":"identity","task":"do it"}`); err != nil {
		t.Fatalf("Execute error = %v", err)
	}

	records := manager.List()
	if len(records) != 1 {
		t.Fatalf("records = %#v", records)
	}
	dispatchID := records[0].ID
	if len(seen) < 2 {
		t.Fatalf("OnBegin calls = %d, want the identity before and after executor assignment: %#v", len(seen), seen)
	}
	if seen[0].ID != dispatchID || seen[0].ExecutorInstanceID != "" {
		t.Fatalf("first OnBegin = %#v", seen[0])
	}
	if last := seen[len(seen)-1]; last.ID != dispatchID || last.ExecutorInstanceID != "inst-a" {
		t.Fatalf("last OnBegin = %#v", last)
	}
	if got := telemetryctx.FromContext(<-target.childCtx).DispatchID; got != dispatchID {
		t.Fatalf("child turn dispatch = %q, want %q", got, dispatchID)
	}
}
