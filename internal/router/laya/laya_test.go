package laya

import "github.com/xiaobaitu/soloqueue/internal/testfixtures"
import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/tasktype"
)

type fakeProc struct {
	ctx    context.Context
	label  string
	err    error
	closed atomic.Int32
}

func (f *fakeProc) decide(context.Context, string) (string, error) {
	if f.ctx != nil && f.ctx.Err() != nil {
		return "", f.ctx.Err()
	}
	if f.err != nil {
		return "", f.err
	}
	return f.label, nil
}

func (f *fakeProc) close() error {
	f.closed.Add(1)
	return nil
}

type harness struct {
	manager *Manager
	mu      sync.Mutex
	spawned []*fakeProc
}

func newHarness(t *testing.T, label string) *harness {
	t.Helper()
	h := &harness{}
	m := New(t.TempDir(), nil)
	m.workerPath = func() (string, error) { return "/fake/soloqueue-decide", nil }
	m.prepareFn = func(context.Context, Config) (string, error) { return t.TempDir(), nil }
	m.spawnFn = func(ctx context.Context, _, _, _ string) (proc, error) {
		p := &fakeProc{label: label, ctx: ctx}
		h.mu.Lock()
		h.spawned = append(h.spawned, p)
		h.mu.Unlock()
		return p, nil
	}
	h.manager = m
	t.Cleanup(m.Close)
	return h
}

func (h *harness) spawnCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.spawned)
}

func (h *harness) waitState(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if h.manager.Status().State == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("state never became %q, last %+v", want, h.manager.Status())
}

func TestDecideWhenReady(t *testing.T) {
	h := newHarness(t, "engineering")
	h.manager.Apply(Config{Enabled: true})
	h.waitState(t, "ready")

	taskType, err := h.manager.Decide(context.Background(), testfixtures.Chinese("laya_test_001"))
	if err != nil || taskType != tasktype.Engineering {
		t.Fatalf("got %v %v", taskType, err)
	}
}

func TestRejectedInputKeepsWorkerReady(t *testing.T) {
	h := newHarness(t, "engineering")
	h.manager.Apply(Config{Enabled: true})
	h.waitState(t, "ready")
	h.mu.Lock()
	p := h.spawned[0]
	p.err = errInvalidRequest
	h.mu.Unlock()
	for i := 0; i < 4; i++ {
		if _, err := h.manager.Decide(context.Background(), "invalid"); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("expected fallback: %v", err)
		}
	}
	if h.manager.Status().State != "ready" || h.spawnCount() != 1 || p.closed.Load() != 0 {
		t.Fatalf("input rejection stopped the worker: %+v", h.manager.Status())
	}
	p.err = nil
	if got, err := h.manager.Decide(context.Background(), "valid"); err != nil || got != tasktype.Engineering {
		t.Fatalf("worker did not recover: %v %v", got, err)
	}
}

func TestDecideWithoutWorkerFallsBack(t *testing.T) {
	m := New(t.TempDir(), nil)
	if _, err := m.Decide(context.Background(), "hello"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
	if m.Status().State != "disabled" {
		t.Fatalf("want disabled, got %+v", m.Status())
	}
}

// A disable inside the grace window must not reload the 1.3GB checkpoint.
func TestReenableWithinGraceReusesWorker(t *testing.T) {
	stopGrace = 200 * time.Millisecond
	defer func() { stopGrace = 60 * time.Second }()

	h := newHarness(t, "general")
	h.manager.Apply(Config{Enabled: true})
	h.waitState(t, "ready")

	h.manager.Apply(Config{Enabled: false})
	if got := h.manager.Status().State; got != "disabled" {
		t.Fatalf("config state must flip immediately, got %q", got)
	}
	h.manager.Apply(Config{Enabled: true})
	h.waitState(t, "ready")
	if got := h.spawnCount(); got != 1 {
		t.Fatalf("worker was respawned: %d spawns", got)
	}
	if got := h.spawned[0].closed.Load(); got != 0 {
		t.Fatalf("worker closed inside the grace window: %d", got)
	}
}

func TestDisableAfterGraceStopsWorker(t *testing.T) {
	stopGrace = 30 * time.Millisecond
	defer func() { stopGrace = 60 * time.Second }()

	h := newHarness(t, "general")
	h.manager.Apply(Config{Enabled: true})
	h.waitState(t, "ready")
	h.manager.Apply(Config{Enabled: false})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if h.spawned[0].closed.Load() == 1 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("worker still running after the grace window")
}

func TestRapidToggleSpawnsOnce(t *testing.T) {
	stopGrace = 200 * time.Millisecond
	defer func() { stopGrace = 60 * time.Second }()

	h := newHarness(t, "general")
	h.manager.Apply(Config{Enabled: true})
	h.waitState(t, "ready")
	for i := 0; i < 4; i++ {
		h.manager.Apply(Config{Enabled: i%2 == 0})
		time.Sleep(time.Millisecond)
	}
	h.manager.Apply(Config{Enabled: true})
	h.waitState(t, "ready")
	if got := h.spawnCount(); got != 1 {
		t.Fatalf("spawn count %d, want 1", got)
	}
}

// Switching back while the checkpoint is still downloading must abort the download instead of
// finishing a 1.3GB transfer in the background.
func TestDisableDuringPrepareCancelsDownload(t *testing.T) {
	h := newHarness(t, "general")
	started := make(chan struct{})
	release := make(chan struct{})
	h.manager.prepareFn = func(ctx context.Context, _ Config) (string, error) {
		close(started)
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-release:
			return t.TempDir(), nil
		}
	}
	h.manager.Apply(Config{Enabled: true})
	<-started
	h.manager.Apply(Config{Enabled: false})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if h.manager.Status().State == "disabled" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(release)
	time.Sleep(100 * time.Millisecond)
	if got := h.manager.Status().State; got != "disabled" {
		t.Fatalf("state %q, want disabled", got)
	}
	if got := h.spawnCount(); got != 0 {
		t.Fatalf("worker spawned after the engine was switched off: %d", got)
	}
}

func TestCloseDuringGraceStopsImmediately(t *testing.T) {
	stopGrace = 10 * time.Second
	defer func() { stopGrace = 60 * time.Second }()

	h := newHarness(t, "general")
	h.manager.Apply(Config{Enabled: true})
	h.waitState(t, "ready")
	h.manager.Apply(Config{Enabled: false})
	h.manager.Close()
	if got := h.spawned[0].closed.Load(); got != 1 {
		t.Fatalf("worker not closed on Close: %d", got)
	}
}

func TestFailedDecideDegradesAndRecovers(t *testing.T) {
	h := newHarness(t, "general")
	h.manager.Apply(Config{Enabled: true})
	h.waitState(t, "ready")

	h.spawned[0].err = errors.New("worker died")
	if _, err := h.manager.Decide(context.Background(), "hello"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
	h.waitState(t, "ready")
	if got := h.spawnCount(); got != 2 {
		t.Fatalf("spawn count %d, want 2", got)
	}
	if got := h.manager.Status().State; got != "ready" {
		t.Fatalf("state %q", got)
	}
}

func TestRepeatedDisablePreservesWorkerAndDeadline(t *testing.T) {
	h := newHarness(t, "general")
	cfg := Config{Enabled: true}
	h.manager.Apply(cfg)
	h.waitState(t, "ready")
	cfg.Enabled = false
	h.manager.Apply(cfg)
	h.manager.mu.Lock()
	timer := h.manager.stopTimer
	h.manager.mu.Unlock()
	h.manager.Apply(cfg)
	h.manager.mu.Lock()
	sameTimer := timer == h.manager.stopTimer
	h.manager.mu.Unlock()
	if !sameTimer {
		t.Fatal("repeated disable changed grace deadline")
	}
	if err := h.spawned[0].ctx.Err(); err != nil {
		t.Fatalf("parked worker context canceled: %v", err)
	}
	cfg.Enabled = true
	h.manager.Apply(cfg)
	if _, err := h.manager.Decide(context.Background(), "continue"); err != nil {
		t.Fatalf("reused worker unavailable: %v", err)
	}
	if h.spawnCount() != 1 || h.spawned[0].closed.Load() != 0 {
		t.Fatal("repeated disable replaced or closed parked worker")
	}
}

func TestDisabledWorkerRejectsInference(t *testing.T) {
	h := newHarness(t, "general")
	h.manager.Apply(Config{Enabled: true})
	h.waitState(t, "ready")
	h.manager.Apply(Config{})
	if _, err := h.manager.Decide(context.Background(), "continue"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("disabled Decide: %v", err)
	}
}

func TestDisabledConfigDoesNotRelabelParkedWorker(t *testing.T) {
	h := newHarness(t, "general")
	h.manager.Apply(Config{Enabled: true, ModelDir: "original"})
	h.waitState(t, "ready")
	h.manager.Apply(Config{ModelDir: "replacement"})
	h.manager.Apply(Config{Enabled: true, ModelDir: "replacement"})
	h.waitState(t, "ready")
	if h.spawnCount() != 2 || h.spawned[0].closed.Load() != 1 {
		t.Fatal("new model config reused old model worker")
	}
}

func TestInferenceFailuresExhaustRestartBudget(t *testing.T) {
	h := newHarness(t, "general")
	cfg := Config{Enabled: true}
	h.manager.Apply(cfg)
	for i := 0; i < 4; i++ {
		h.waitState(t, "ready")
		h.spawned[i].err = errors.New("persistent inference failure")
		if _, err := h.manager.Decide(context.Background(), "continue"); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("failure %d: %v", i, err)
		}
	}
	h.waitState(t, "error")
	h.manager.Apply(cfg)
	cfg.Enabled = false
	h.manager.Apply(cfg)
	cfg.Enabled = true
	h.manager.Apply(cfg)
	if h.manager.Status().State != "error" {
		t.Fatalf("exhausted budget bypassed: %+v", h.manager.Status())
	}
	if got := h.spawnCount(); got != 4 {
		t.Fatalf("spawn count=%d, want 4", got)
	}
	if _, err := h.manager.Action("retry"); err != nil {
		t.Fatal(err)
	}
	h.waitState(t, "ready")
	if got := h.spawnCount(); got != 5 {
		t.Fatalf("manual retry spawns=%d", got)
	}
}

func TestOldGraceCallbackCannotStopNextParkedWorker(t *testing.T) {
	h := newHarness(t, "general")
	cfg := Config{Enabled: true}
	h.manager.Apply(cfg)
	h.waitState(t, "ready")
	cfg.Enabled = false
	h.manager.Apply(cfg)
	h.manager.mu.Lock()
	oldEpoch, generation := h.manager.stopEpoch, h.manager.generation
	h.manager.mu.Unlock()
	cfg.Enabled = true
	h.manager.Apply(cfg)
	cfg.Enabled = false
	h.manager.Apply(cfg)
	// Simulate the previous callback already having fired before Timer.Stop.
	h.manager.expireWorker(generation, oldEpoch)
	if h.spawned[0].closed.Load() != 0 {
		t.Fatal("obsolete timer stopped the newly parked worker")
	}
	cfg.Enabled = true
	h.manager.Apply(cfg)
	if _, err := h.manager.Decide(context.Background(), "continue"); err != nil {
		t.Fatal(err)
	}
}

func TestCancelDuringGraceStopsImmediately(t *testing.T) {
	h := newHarness(t, "general")
	h.manager.Apply(Config{Enabled: true})
	h.waitState(t, "ready")
	h.manager.Apply(Config{})
	status, err := h.manager.Action("cancel")
	if err != nil || status.State != "disabled" || h.spawned[0].closed.Load() != 1 {
		t.Fatalf("cancel: %+v %v", status, err)
	}
}

func TestSuccessResetsInferenceFailureBudget(t *testing.T) {
	h := newHarness(t, "general")
	h.manager.Apply(Config{Enabled: true})
	for i := 0; i < 3; i++ {
		h.waitState(t, "ready")
		h.spawned[i].err = errors.New("worker failed")
		_, _ = h.manager.Decide(context.Background(), "continue")
	}
	h.waitState(t, "ready")
	if _, err := h.manager.Decide(context.Background(), "successful inference"); err != nil {
		t.Fatal(err)
	}
	for i := 3; i < 7; i++ {
		h.waitState(t, "ready")
		h.spawned[i].err = errors.New("worker failed again")
		_, _ = h.manager.Decide(context.Background(), "continue")
	}
	h.waitState(t, "error")
	if got := h.spawnCount(); got != 7 {
		t.Fatalf("spawns=%d, want 7 after successful inference reset", got)
	}
}

type delayedProc struct {
	err              error
	entered, release chan struct{}
	closed           atomic.Bool
}

func (p *delayedProc) decide(_ context.Context, text string) (string, error) {
	if text == "late success" {
		close(p.entered)
		<-p.release
		return "general", p.err
	}
	return "", errors.New("worker died")
}
func (p *delayedProc) close() error { p.closed.Store(true); return nil }

func TestStaleSuccessDoesNotResetNewWorkerBudget(t *testing.T) {
	h := newHarness(t, "general")
	originalSpawn := h.manager.spawnFn
	old := &delayedProc{entered: make(chan struct{}), release: make(chan struct{})}
	first := true
	h.manager.spawnFn = func(ctx context.Context, a, b, c string) (proc, error) {
		if first {
			first = false
			return old, nil
		}
		return originalSpawn(ctx, a, b, c)
	}
	h.manager.Apply(Config{Enabled: true})
	h.waitState(t, "ready")
	result := make(chan struct{})
	go func() { defer close(result); _, _ = h.manager.Decide(context.Background(), "late success") }()
	<-old.entered
	_, _ = h.manager.Decide(context.Background(), "failure")
	h.waitState(t, "ready")
	close(old.release)
	<-result
	// Three further failures must exhaust the original four-worker budget.
	for i := 0; i < 3; i++ {
		h.waitState(t, "ready")
		h.spawned[i].err = errors.New("persistent failure")
		_, _ = h.manager.Decide(context.Background(), "continue")
	}
	h.waitState(t, "error")
	if h.spawnCount() != 3 {
		t.Fatalf("late success granted extra restarts: %d replacement workers", h.spawnCount())
	}
}

func TestModelChangeResetsExhaustedBudget(t *testing.T) {
	h := newHarness(t, "general")
	cfg := Config{Enabled: true, ModelDir: "original"}
	h.manager.Apply(cfg)
	for i := 0; i < 4; i++ {
		h.waitState(t, "ready")
		h.spawned[i].err = errors.New("persistent failure")
		_, _ = h.manager.Decide(context.Background(), "continue")
	}
	h.waitState(t, "error")
	cfg.ModelDir = "replacement"
	h.manager.Apply(cfg)
	h.waitState(t, "ready")
	if h.spawnCount() != 5 {
		t.Fatalf("spawns=%d", h.spawnCount())
	}
	h.manager.mu.Lock()
	restarts := h.manager.restarts
	h.manager.mu.Unlock()
	if restarts != 0 {
		t.Fatalf("new model retained old failure budget: %d", restarts)
	}
}

func TestStartupFailureRequiresManualRetry(t *testing.T) {
	h := newHarness(t, "general")
	var prepares atomic.Int32
	h.manager.prepareFn = func(context.Context, Config) (string, error) {
		prepares.Add(1)
		return "", errors.New("missing checkpoint")
	}
	cfg := Config{Enabled: true}
	h.manager.Apply(cfg)
	h.waitState(t, "error")
	h.manager.Apply(cfg)
	cfg.Enabled = false
	h.manager.Apply(cfg)
	cfg.Enabled = true
	h.manager.Apply(cfg)
	if prepares.Load() != 1 || h.manager.Status().State != "error" {
		t.Fatal("settings save or toggle retried startup failure")
	}
	if _, err := h.manager.Action("retry"); err != nil {
		t.Fatal(err)
	}
	h.waitState(t, "error")
	if prepares.Load() != 2 {
		t.Fatal("manual retry did not retry startup")
	}
}

func TestRepeatedApplyDoesNotRestartPreparingWorker(t *testing.T) {
	h := newHarness(t, "general")
	entered, release := make(chan struct{}), make(chan struct{})
	var prepares atomic.Int32
	h.manager.prepareFn = func(ctx context.Context, _ Config) (string, error) {
		if prepares.Add(1) == 1 {
			close(entered)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-release:
			return "model", nil
		}
	}
	cfg := Config{Enabled: true}
	h.manager.Apply(cfg)
	<-entered
	h.manager.Apply(cfg)
	h.manager.Apply(cfg)
	close(release)
	h.waitState(t, "ready")
	if prepares.Load() != 1 || h.spawnCount() != 1 {
		t.Fatal("repeated settings apply restarted preparing worker")
	}
}

func TestStaleStartupCannotPublishAfterCancel(t *testing.T) {
	h := newHarness(t, "general")
	entered, release := make(chan struct{}), make(chan struct{})
	old := &fakeProc{label: "research"}
	h.manager.spawnFn = func(context.Context, string, string, string) (proc, error) {
		close(entered)
		<-release
		return old, nil
	}
	h.manager.Apply(Config{Enabled: true})
	<-entered
	_, _ = h.manager.Action("cancel")
	close(release)
	h.manager.wg.Wait()
	if h.manager.Status().State != "disabled" || old.closed.Load() != 1 {
		t.Fatal("canceled startup published a stale worker")
	}
}

func TestLateRestartCannotUndoDisableOrCancel(t *testing.T) {
	for _, action := range []string{"disable", "cancel"} {
		t.Run(action, func(t *testing.T) {
			h := newHarness(t, "general")
			h.manager.Apply(Config{Enabled: true})
			h.waitState(t, "ready")
			h.manager.mu.Lock()
			generation := h.manager.generation
			h.manager.mu.Unlock()
			if action == "disable" {
				h.manager.Apply(Config{})
			} else {
				_, _ = h.manager.Action("cancel")
			}
			h.manager.restart(generation)
			if h.manager.Status().State != "disabled" || h.spawnCount() != 1 {
				t.Fatalf("stale restart undid %s", action)
			}
		})
	}
}

func TestLateInferenceFailureKeepsDisabledState(t *testing.T) {
	h := newHarness(t, "general")
	old := &delayedProc{entered: make(chan struct{}), release: make(chan struct{}), err: errors.New("worker died while parked")}
	h.manager.spawnFn = func(context.Context, string, string, string) (proc, error) { return old, nil }
	h.manager.Apply(Config{Enabled: true})
	h.waitState(t, "ready")
	result := make(chan error, 1)
	go func() { _, err := h.manager.Decide(context.Background(), "late success"); result <- err }()
	<-old.entered
	h.manager.Apply(Config{})
	close(old.release)
	if err := <-result; !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if h.manager.Status().State != "disabled" {
		t.Fatalf("late failure replaced disabled state: %+v", h.manager.Status())
	}
	h.manager.mu.Lock()
	worker, restarts := h.manager.worker, h.manager.restarts
	h.manager.mu.Unlock()
	if worker != nil || restarts != 1 || !old.closed.Load() {
		t.Fatal("late failure did not clean up worker and preserve budget")
	}
}

func TestInvalidLabelDoesNotResetBudget(t *testing.T) {
	h := newHarness(t, "general")
	h.manager.Apply(Config{Enabled: true})
	for i := 0; i < 3; i++ {
		h.waitState(t, "ready")
		h.spawned[i].err = errors.New("worker failure")
		_, _ = h.manager.Decide(context.Background(), "continue")
	}
	h.waitState(t, "ready")
	h.spawned[3].label = "invalid label"
	if _, err := h.manager.Decide(context.Background(), "continue"); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	h.waitState(t, "error")
	if h.spawnCount() != 4 {
		t.Fatal("invalid result reset restart budget")
	}
}

func TestStaleStartupCannotReplaceNewWorker(t *testing.T) {
	h := newHarness(t, "general")
	originalSpawn := h.manager.spawnFn
	entered, release := make(chan struct{}), make(chan struct{})
	old := &fakeProc{label: "research"}
	var spawns atomic.Int32
	h.manager.spawnFn = func(ctx context.Context, a, b, c string) (proc, error) {
		if spawns.Add(1) == 1 {
			close(entered)
			<-release
			return old, nil
		}
		return originalSpawn(ctx, a, b, c)
	}
	h.manager.Apply(Config{Enabled: true})
	<-entered
	_, _ = h.manager.Action("cancel")
	h.manager.Apply(Config{Enabled: true})
	h.waitState(t, "ready")
	close(release)
	h.manager.wg.Wait()
	task, err := h.manager.Decide(context.Background(), "continue")
	if task != tasktype.General || err != nil || old.closed.Load() != 1 {
		t.Fatalf("stale startup displaced new worker: %s %v", task, err)
	}
}

func TestResolveWorkerUsesMainExecutable(t *testing.T) {
	t.Setenv("SOLOQUEUE_DECIDE_WORKER", "/missing/obsolete-worker")
	manager := New(t.TempDir(), nil)
	t.Cleanup(manager.Close)
	got, err := manager.resolveWorker()
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("worker path %q, want main executable %q", got, want)
	}
}
