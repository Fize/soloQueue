package runwatch

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestManager_HealthyChildPreventsRootOrphanCancellation(t *testing.T) {
	clock := NewFakeClock(time.Unix(1_700_000_000, 0))
	manager := NewManager(Policy{RootIdle: 15 * time.Minute}, WithClock(clock))
	defer manager.Close()

	ctx, root, err := manager.Start(context.Background(), Metadata{RunID: "run-1"})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	child, err := root.BeginOperation(KindModel, "model-1", Policy{SemanticIdle: 10 * time.Minute})
	if err != nil {
		t.Fatalf("BeginOperation() error = %v", err)
	}

	clock.Advance(16 * time.Minute)
	child.Pulse(ProgressSemantic, "content")
	manager.Scan()

	if err := context.Cause(ctx); err != nil {
		t.Fatalf("healthy child cancelled root: %v", err)
	}
}

func TestManager_RootSnapshotUsesHealthyChildEffectiveDue(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	clock := NewFakeClock(start)
	manager := NewManager(Policy{RootIdle: 10 * time.Minute}, WithClock(clock))
	defer manager.Close()

	_, root, err := manager.Start(context.Background(), Metadata{RunID: "run-effective-due"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := root.BeginOperation(KindDelegation, "delegation-effective-due", Policy{OrphanIdle: 15 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(9 * time.Minute)
	child.Pulse(ProgressStructural, "delegating")
	clock.Advance(2 * time.Minute)

	snapshot, ok := manager.Snapshot("run-effective-due")
	if !ok {
		t.Fatal("Snapshot() did not find root")
	}
	want := start.Add(24 * time.Minute)
	if !snapshot.WatchdogDueAt.Equal(want) {
		t.Fatalf("WatchdogDueAt = %v, want healthy child due %v", snapshot.WatchdogDueAt, want)
	}
}

func TestManager_ModelSemanticSilenceHasDistinctCause(t *testing.T) {
	clock := NewFakeClock(time.Unix(1_700_000_000, 0))
	manager := NewManager(Policy{}, WithClock(clock))
	defer manager.Close()

	ctx, root, err := manager.Start(context.Background(), Metadata{RunID: "run-semantic"})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	model, err := root.BeginOperation(KindModel, "model-semantic", Policy{
		TransportIdle: 2 * time.Minute,
		SemanticIdle:  10 * time.Minute,
	})
	if err != nil {
		t.Fatalf("BeginOperation() error = %v", err)
	}
	model.Pulse(ProgressSemantic, "streaming")
	clock.Advance(9 * time.Minute)
	model.Pulse(ProgressTransport, "keepalive")
	clock.Advance(time.Minute)
	manager.Scan()

	if got := CodeOf(context.Cause(ctx)); got != CodeModelSemanticStalled {
		t.Fatalf("cancel code = %q, want %q", got, CodeModelSemanticStalled)
	}
}

func TestManager_SnapshotExposesWatchdogState(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	clock := NewFakeClock(start)
	manager := NewManager(Policy{RootIdle: 15 * time.Minute}, WithClock(clock))
	defer manager.Close()

	_, _, err := manager.Start(context.Background(), Metadata{RunID: "run-status", Phase: "routing"})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	snapshot, ok := manager.Snapshot("run-status")
	if !ok {
		t.Fatal("Snapshot() did not find active run")
	}
	if snapshot.Phase != "routing" {
		t.Fatalf("Snapshot() = %+v", snapshot)
	}
}

func TestManager_ScannerCancelsExpiredRun(t *testing.T) {
	clock := NewFakeClock(time.Unix(1_700_000_000, 0))
	manager := NewManager(Policy{ScanInterval: time.Millisecond, RootIdle: 15 * time.Minute}, WithClock(clock))
	defer manager.Close()

	ctx, _, err := manager.Start(context.Background(), Metadata{RunID: "run-scanner"})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	clock.Advance(15 * time.Minute)

	select {
	case <-ctx.Done():
		if got := CodeOf(context.Cause(ctx)); got != CodeRootOrphaned {
			t.Fatalf("cancel code = %q, want %q", got, CodeRootOrphaned)
		}
	case <-time.After(time.Second):
		t.Fatal("scanner did not cancel expired run")
	}
}

func TestManager_ScanMarksExpiredRunTerminalOnce(t *testing.T) {
	clock := NewFakeClock(time.Unix(1_700_000_000, 0))
	manager := NewManager(Policy{RootIdle: time.Minute}, WithClock(clock))
	defer manager.Close()
	ctx, _, err := manager.Start(context.Background(), Metadata{RunID: "run-scan-terminal"})
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Minute)
	manager.Scan()
	snapshot, ok := manager.Snapshot("run-scan-terminal")
	if !ok || !snapshot.Terminated || snapshot.TerminalCode != CodeRootOrphaned || !snapshot.WatchdogDueAt.IsZero() {
		t.Fatalf("expired snapshot = %+v, want terminal root with no due time", snapshot)
	}
	manager.Scan()
	if got := context.Cause(ctx); got == nil || CodeOf(got) != CodeRootOrphaned {
		t.Fatalf("scan cause changed after second scan: %v", got)
	}
}

func TestManager_CancelTargetsOnlyRequestedRun(t *testing.T) {
	manager := NewManager(Policy{})
	defer manager.Close()
	ctxA, _, err := manager.Start(context.Background(), Metadata{RunID: "run-a"})
	if err != nil {
		t.Fatalf("Start(run-a) error = %v", err)
	}
	ctxB, _, err := manager.Start(context.Background(), Metadata{RunID: "run-b"})
	if err != nil {
		t.Fatalf("Start(run-b) error = %v", err)
	}

	if err := manager.Cancel("run-a", &Cause{Code: CodeCancelledByUser, OperationID: "run-a"}); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if got := CodeOf(context.Cause(ctxA)); got != CodeCancelledByUser {
		t.Fatalf("run-a cancel code = %q", got)
	}
	if err := context.Cause(ctxB); err != nil {
		t.Fatalf("run-b was cancelled: %v", err)
	}
}

func TestHandleFailWithUncodedErrorIsTerminal(t *testing.T) {
	manager := NewManager(Policy{})
	defer manager.Close()
	ctx, root, err := manager.Start(context.Background(), Metadata{RunID: "run-uncoded-failure"})
	if err != nil {
		t.Fatal(err)
	}

	root.Fail(errors.New("provider failed"))
	if context.Cause(ctx) == nil {
		t.Fatal("Fail did not cancel the root")
	}
	snapshot, ok := manager.Snapshot("run-uncoded-failure")
	if !ok || !snapshot.Terminated {
		t.Fatalf("snapshot = %+v, want terminated", snapshot)
	}
	if err := manager.Cancel("run-uncoded-failure", &Cause{Code: CodeRootOrphaned}); err == nil {
		t.Fatal("terminal run accepted a second cancellation")
	}
}

func TestManager_RootAndChildIDsCannotCrossResolve(t *testing.T) {
	manager := NewManager(Policy{})
	defer manager.Close()
	ctxA, rootA, err := manager.Start(context.Background(), Metadata{RunID: "request-a"})
	if err != nil {
		t.Fatal(err)
	}
	childA, err := rootA.BeginOperation(KindTool, "request-b", Policy{})
	if err != nil {
		t.Fatal(err)
	}
	ctxB, rootB, err := manager.Start(context.Background(), Metadata{RunID: "request-b"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rootB.BeginOperation(KindTool, "request-b", Policy{}); err != nil {
		t.Fatalf("same local child ID in another root must be allowed: %v", err)
	}
	childA.Fail(&Cause{Code: CodeToolStalled, OperationID: "request-b"})
	if got := CodeOf(context.Cause(ctxA)); got != CodeToolStalled {
		t.Fatalf("request-a cause = %q", got)
	}
	if cause := context.Cause(ctxB); cause != nil {
		t.Fatalf("path-scoped child retargeted request-b root: %v", cause)
	}
}

func TestHandle_FailIsIdempotentAndPreservesTypedCause(t *testing.T) {
	manager := NewManager(Policy{})
	defer manager.Close()
	ctx, root, err := manager.Start(context.Background(), Metadata{RunID: "run-fail"})
	if err != nil {
		t.Fatal(err)
	}
	cause := &Cause{Code: CodeToolStalled, OperationID: "tool-1"}
	root.Fail(cause)
	root.Fail(&Cause{Code: CodeRootOrphaned, OperationID: "run-fail"})
	if got := CodeOf(context.Cause(ctx)); got != CodeToolStalled {
		t.Fatalf("terminal cause = %q, want first cause %q", got, CodeToolStalled)
	}
}

func TestManager_ModelLeaseDoesNotApplyToRoot(t *testing.T) {
	clock := NewFakeClock(time.Unix(1_700_000_000, 0))
	manager := NewManager(Policy{TransportIdle: 2 * time.Minute, RootIdle: 15 * time.Minute}, WithClock(clock))
	defer manager.Close()
	ctx, _, err := manager.Start(context.Background(), Metadata{RunID: "run-root-policy"})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	clock.Advance(3 * time.Minute)
	manager.Scan()
	if err := context.Cause(ctx); err != nil {
		t.Fatalf("model transport lease cancelled root: %v", err)
	}
}

func TestManager_StallLayersProduceDistinctCauses(t *testing.T) {
	tests := []struct {
		name   string
		kind   Kind
		policy Policy
		pulse  ProgressKind
		want   Code
	}{
		{name: "transport", kind: KindModel, policy: Policy{TransportIdle: time.Minute}, want: CodeModelTransportStalled},
		{name: "first semantic", kind: KindModel, policy: Policy{FirstSemantic: time.Minute}, want: CodeModelFirstProgressStalled},
		{name: "semantic", kind: KindModel, policy: Policy{SemanticIdle: time.Minute}, pulse: ProgressSemantic, want: CodeModelSemanticStalled},
		{name: "tool", kind: KindTool, policy: Policy{OrphanIdle: time.Minute}, want: CodeToolStalled},
		{name: "delegation", kind: KindDelegation, policy: Policy{OrphanIdle: time.Minute}, want: CodeDelegationOrphaned},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clock := NewFakeClock(time.Unix(1_700_000_000, 0))
			manager := NewManager(Policy{}, WithClock(clock))
			defer manager.Close()
			ctx, root, err := manager.Start(context.Background(), Metadata{RunID: "run-" + tt.name})
			if err != nil {
				t.Fatal(err)
			}
			leaf, err := root.BeginOperation(tt.kind, "leaf-"+tt.name, tt.policy)
			if err != nil {
				t.Fatal(err)
			}
			if tt.pulse != "" {
				leaf.Pulse(tt.pulse, "active")
			}
			clock.Advance(2 * time.Minute)
			manager.Scan()
			if got := CodeOf(context.Cause(ctx)); got != tt.want {
				t.Fatalf("cancel code = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestManager_ProgressingDelegationOutlivesLegacyThirtyMinuteCap(t *testing.T) {
	clock := NewFakeClock(time.Unix(1_700_000_000, 0))
	manager := NewManager(Policy{RootIdle: 15 * time.Minute}, WithClock(clock))
	defer manager.Close()
	ctx, root, err := manager.Start(context.Background(), Metadata{RunID: "run-long-delegation"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := root.BeginOperation(KindDelegation, "dispatch-long", Policy{OrphanIdle: 15 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		clock.Advance(10 * time.Minute)
		child.Pulse(ProgressSemantic, "streaming")
		manager.Scan()
	}
	if err := context.Cause(ctx); err != nil {
		t.Fatalf("progressing delegation was cancelled after 40m: %v", err)
	}
}

func TestHandle_ChildCompletionRefreshesParentLease(t *testing.T) {
	clock := NewFakeClock(time.Unix(1_700_000_000, 0))
	manager := NewManager(Policy{RootIdle: 10 * time.Minute}, WithClock(clock))
	defer manager.Close()
	ctx, root, err := manager.Start(context.Background(), Metadata{RunID: "run-child-complete"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := root.BeginOperation(KindTool, "tool-child", Policy{OrphanIdle: 10 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(9 * time.Minute)
	child.Complete()
	clock.Advance(2 * time.Minute)
	manager.Scan()
	if cause := context.Cause(ctx); cause != nil {
		t.Fatalf("parent expired immediately after child completion: %v", cause)
	}
}

func TestDefaultPolicyIsFixedDeepSeekPolicy(t *testing.T) {
	got := DefaultPolicy()
	want := Policy{
		ScanInterval:  5 * time.Second,
		FirstSemantic: 5 * time.Minute,
		TransportIdle: 2 * time.Minute,
		SemanticIdle:  10 * time.Minute,
		RootIdle:      15 * time.Minute,
		OrphanIdle:    15 * time.Minute,
	}
	if got != want {
		t.Fatalf("DefaultPolicy() = %+v, want %+v", got, want)
	}
}

func TestManager_CloseCancelsAndClearsActiveRunsIdempotently(t *testing.T) {
	manager := NewManager(Policy{RootIdle: time.Hour})
	ctx, handle, err := manager.Start(context.Background(), Metadata{RunID: "shutdown-run"})
	if err != nil {
		t.Fatal(err)
	}
	manager.Close()
	manager.Close()
	if !errors.Is(context.Cause(ctx), context.Canceled) {
		t.Fatalf("close cause = %v, want context.Canceled", context.Cause(ctx))
	}
	if _, ok := manager.Snapshot("shutdown-run"); ok {
		t.Fatal("closed manager retained active run")
	}
	handle.Complete()
}
