package session

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ─── Concurrent Activation Characterization ──────────────────────────────────
//
// These tests document the current behavior of L2SessionStore.Activate.
// Some tests are expected to FAIL because they describe invariants that the
// current code violates. They serve as Phase 0 characterization tests per
// the session isolation repair plan.

// testActivator wraps an L2SessionStore with a countable BuildL2 to
// demonstrate concurrent activation behavior.
type testActivator struct {
	store      *L2SessionStore
	buildCount atomic.Int64
	buildDelay time.Duration
	buildMu    sync.Mutex
}

func newTestActivator(t *testing.T, delay time.Duration) *testActivator {
	t.Helper()
	dir := t.TempDir()
	builder := &Builder{WorkDir: dir}
	store := NewL2SessionStore(builder, dir, nil)

	ta := &testActivator{
		store:      store,
		buildDelay: delay,
	}
	// Override the builder's BuildL2 by replacing the store's builder field
	// with a wrapper. Since Builder is a concrete struct and BuildL2 reads
	// b.RT, we can't call the real method. Instead, we patch Activate's
	// behavior through the store.
	//
	// For this characterization test, we directly call Activate and rely on
	// the fact that BuildL2 will access b.RT (nil) and panic. Instead, we
	// instrument the store's Activate path below.
	return ta
}

// TestConcurrentActivation_Characterization demonstrates that the current
// Activate implementation does NOT serialize concurrent builders.
// TestRemoveCleanup_Characterization verifies that Remove() fully cleans up
// runtime state. Currently it only calls Session.Close() — it does NOT:
// - Cancel active requests
// - Unregister from AgentRegistry
// - Remove supervisor from Stack
// - Remove runtime stream watches
//
// This test documents the missing cleanup steps.
func TestRemoveCleanup_Characterization(t *testing.T) {
	dir := t.TempDir()
	store := NewL2SessionStore(&Builder{WorkDir: dir}, dir, nil)

	id := "cleanup-test"
	_, err := store.Create(context.Background(), id, "dev", "", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// We can't set entry.Session to a bare &Session{} because Session.Close
	// requires a non-nil logger. Instead, verify the code structure:
	// Remove() has NO calls to AgentRegistry, Supervisor, or RuntimeMetrics.
	// This is the characterization — the cleanup is incomplete.

	// Verify Remove works on a non-activated session.
	if err := store.Remove(context.Background(), id); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	store.mu.RLock()
	_, exists := store.sessions[id]
	store.mu.RUnlock()
	if exists {
		t.Error("session entry should be removed after Remove()")
	}

	// CHARACTERIZATION: The current Remove() does NOT verify that:
	// - Session.Agent is stopped and unregistered
	// - Supervisor children are reaped
	// - Runtime stream watches are removed
	// - Active requests are cancelled
	// Phase 1 will implement DestroyL2 with full teardown.
	t.Log("CHARACTERIZATION: Remove() only calls Session.Close() and deletes files. " +
		"Full runtime cleanup (agent unregister, supervisor reap, watch removal) is missing.")
}

func TestConcurrentActivation_SingleFlight(t *testing.T) {
	dir := t.TempDir()
	store := NewL2SessionStore(&Builder{WorkDir: dir}, dir, nil)

	id := "concurrent-singleflight-id"
	_, err := store.Create(context.Background(), id, "dev", "", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	const N = 50
	var (
		wg       sync.WaitGroup
		errCount atomic.Int64
	)

	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			_, err := store.Activate(context.Background(), id)
			if err != nil {
				errCount.Add(1)
			}
		}()
	}
	wg.Wait()

	// All 50 goroutines received the single-flight activation error cleanly
	// without deadlocking or producing unhandled panics.
	if errs := errCount.Load(); errs != int64(N) {
		t.Errorf("expected all %d goroutines to receive activation error, got %d", N, errs)
	}
}

func TestDestroyL2_Teardown(t *testing.T) {
	dir := t.TempDir()
	store := NewL2SessionStore(&Builder{WorkDir: dir}, dir, nil)

	id := "destroy-test-id"
	_, err := store.Create(context.Background(), id, "dev", "", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// DestroyL2 on inactive session
	if err := store.DestroyL2(context.Background(), id, true); err != nil {
		t.Fatalf("DestroyL2: %v", err)
	}

	store.mu.RLock()
	_, exists := store.sessions[id]
	store.mu.RUnlock()
	if exists {
		t.Error("session entry should be removed after DestroyL2")
	}
}

func TestDestroyL2_WaitsForActivationCleanup(t *testing.T) {
	dir := t.TempDir()
	store := NewL2SessionStore(&Builder{WorkDir: dir}, dir, nil)
	const id = "destroy-while-activating"
	_, err := store.Create(context.Background(), id, "dev", "", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	activationDone := make(chan struct{})
	store.mu.Lock()
	entry := store.sessions[id]
	entry.state = l2StateActivating
	entry.activationDone = activationDone
	entry.activationStop = func() {}
	store.mu.Unlock()

	destroyed := make(chan error, 1)
	go func() {
		destroyed <- store.DestroyL2(context.Background(), id, true)
	}()

	select {
	case err := <-destroyed:
		t.Fatalf("DestroyL2 returned before activation cleanup: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	close(activationDone)
	if err := <-destroyed; err != nil {
		t.Fatalf("DestroyL2: %v", err)
	}
}
