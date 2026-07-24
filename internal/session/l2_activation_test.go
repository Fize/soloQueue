package session

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

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
