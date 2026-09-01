package session

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/agent/agenttest"
	"github.com/xiaobaitu/soloqueue/internal/runtime"
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

func TestDestroyL2DisposesSessionBuiltAfterDeletionWins(t *testing.T) {
	dir := t.TempDir()
	registry := agent.NewRegistry(nil)
	rt := &runtime.Stack{AgentRegistry: registry}
	store := NewL2SessionStore(&Builder{WorkDir: dir}, dir, nil)
	built := make(chan struct{})
	buildCanceled := make(chan struct{})
	releaseBuild := make(chan struct{})
	var fresh *agent.Agent
	var resourceCloses atomic.Int32
	store.buildL2 = func(ctx context.Context, id, group, workDir string) (*Session, error) {
		fresh = agent.NewAgent(agent.Definition{ID: "leader"}, &agenttest.FakeLLM{}, nil, agent.WithSchedulingPending())
		if err := fresh.Start(context.Background()); err != nil {
			return nil, err
		}
		if err := registry.Register(fresh); err != nil {
			return nil, err
		}
		sv := agent.NewSupervisor(fresh, nil, nil)
		sess := NewSession("l2-"+id, group, fresh, nil, nil, nil)
		sess.resourceCloser = func() error { resourceCloses.Add(1); return nil }
		sess.SetAgentRegistry(registry)
		sess.SetSupervisor(sv, rt.RemoveSupervisor)
		sess.SetSupervisorPublisher(rt.AddSupervisor)
		sess.PublishInitialGeneration()
		close(built)
		<-ctx.Done()
		close(buildCanceled)
		<-releaseBuild
		return sess, nil
	}
	const id = "destroy-published-build"
	if _, err := store.Create(context.Background(), id, "dev", "", ""); err != nil {
		t.Fatal(err)
	}
	activated := make(chan error, 1)
	go func() {
		_, err := store.Activate(context.Background(), id)
		activated <- err
	}()
	<-built
	destroyed := make(chan error, 1)
	go func() { destroyed <- store.DestroyL2(context.Background(), id, false) }()
	<-buildCanceled
	close(releaseBuild)
	if err := <-activated; err == nil {
		t.Fatal("activation succeeded after deletion won")
	}
	if err := <-destroyed; err != nil {
		t.Fatal(err)
	}
	if registry.Len() != 0 {
		t.Fatalf("rejected activation leaked %d registered Agent(s)", registry.Len())
	}
	if _, ok := registry.Locate("leader"); ok {
		t.Fatal("rejected activation left fresh Agent schedulable")
	}
	if got := len(rt.SupervisorsSnapshot()); got != 0 {
		t.Fatalf("rejected activation leaked %d Runtime Supervisor(s)", got)
	}
	if fresh == nil || fresh.State() != agent.StateStopped {
		t.Fatalf("rejected activation Agent state = %v, want stopped", fresh.State())
	}
	if got := resourceCloses.Load(); got != 1 {
		t.Fatalf("Session resource closes = %d, want 1", got)
	}
}
