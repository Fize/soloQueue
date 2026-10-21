package lsp

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

func newLifecycleManager(t *testing.T, configure ...func(*Manager)) (*Manager, string) {
	t.Helper()
	root := t.TempDir()
	file := filepath.Join(root, "main.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(root, nil)
	for _, setup := range configure {
		setup(manager)
	}
	t.Cleanup(manager.Shutdown)
	def := ServerDef{ID: "test-lsp", Command: os.Args[0], Args: []string{"-test.run=TestLSPHelperProcess", "--"}, Languages: []string{"go"}, Extensions: []string{".go"}}
	if err := manager.Start(context.Background(), []ServerDef{def}); err != nil {
		t.Fatal(err)
	}
	return manager, file
}

type gatedStopProcess struct {
	entered chan struct{}
	allow   chan struct{}
	done    chan struct{}
}

func (p *gatedStopProcess) Stdin() io.WriteCloser { return nil }
func (p *gatedStopProcess) Stdout() io.Reader     { return nil }
func (p *gatedStopProcess) Stderr() io.Reader     { return nil }
func (p *gatedStopProcess) Wait() error           { <-p.done; return nil }
func (p *gatedStopProcess) Kill() error           { close(p.entered); <-p.allow; close(p.done); return nil }

func TestAcquireWaitsForRetiringGenerationWithoutHoldingGlobalLock(t *testing.T) {
	manager, file, clock := lifecycleManagerWithClock(t)
	process := &gatedStopProcess{entered: make(chan struct{}), allow: make(chan struct{}), done: make(chan struct{})}
	old := &Client{id: "test-lsp", process: process, shutdown: make(chan struct{}), done: process.done}
	key := clientKey(old.id, PathToURI(filepath.Dir(file)))
	manager.mu.Lock()
	manager.servers[key] = old
	manager.usage[old] = &clientUsage{deadline: clock.Now()}
	manager.mu.Unlock()
	reaped := make(chan struct{})
	go func() { manager.reapIdle(); close(reaped) }()
	<-process.entered
	var allowOnce sync.Once
	unblock := func() { allowOnce.Do(func() { close(process.allow) }) }
	defer unblock()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, _, err := manager.acquireForFile(ctx, file); err != context.DeadlineExceeded {
		t.Fatalf("waiting acquire = %v", err)
	}
	// The same key must wait, while a different workspace can start normally.
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer waitCancel()
	requested := make(chan struct{})
	result := make(chan *Client, 1)
	errResult := make(chan error, 1)
	go func() {
		close(requested)
		client, release, err := manager.acquireForFile(waitCtx, file)
		if err != nil {
			errResult <- err
			return
		}
		defer release()
		result <- client
	}()
	<-requested
	other, releaseOther, err := manager.acquireForFile(waitCtx, filepath.Join(t.TempDir(), "other.go"))
	if err != nil {
		t.Fatal(err)
	}
	releaseOther()
	if !other.alive() {
		t.Fatal("global lock blocked another workspace")
	}
	select {
	case <-result:
		t.Fatal("replacement started before retiring process exited")
	default:
	}
	unblock()
	<-reaped
	select {
	case fresh := <-result:
		manager.mu.Lock()
		before := manager.usage[fresh].deadline
		manager.mu.Unlock()
		clock.Advance(time.Minute)
		manager.release(old)
		manager.mu.Lock()
		after := manager.usage[fresh].deadline
		manager.mu.Unlock()
		if !after.Equal(before) {
			t.Fatal("old release changed the new generation")
		}
	case err := <-errResult:
		t.Fatal(err)
	case <-waitCtx.Done():
		t.Fatal("waiting acquire did not resume after process exit")
	}
}

func TestReaperRunsAndShutdownStopsIt(t *testing.T) {
	manager, file := newLifecycleManager(t, func(m *Manager) { m.idleTimeout = 10 * time.Millisecond; m.sweepInterval = 5 * time.Millisecond })
	client, err := manager.clientForFile(context.Background(), file)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-client.processDone:
	case <-time.After(2 * time.Second):
		t.Fatal("periodic reaper did not stop idle process")
	}
	manager.Shutdown()
	manager.Shutdown()
	select {
	case <-manager.reaperDone:
	default:
		t.Fatal("reaper survived manager shutdown")
	}
	if _, err := manager.clientForFile(context.Background(), file); err == nil {
		t.Fatal("stopped manager started a new process")
	}
}

func TestShutdownCancelsConcurrentStartup(t *testing.T) {
	resolving := make(chan struct{})
	allow := make(chan struct{})
	manager, file := newLifecycleManager(t)
	manager.mu.Lock()
	def := manager.defs["test-lsp"]
	def.Resolve = func(string) string { close(resolving); <-allow; return os.Args[0] }
	manager.defs[def.ID] = def
	manager.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { _, _, err := manager.acquireForFile(ctx, file); result <- err }()
	<-resolving
	finished := make(chan struct{})
	go func() { manager.Shutdown(); close(finished) }()
	<-manager.reaperStop
	close(allow)
	select {
	case <-finished:
	case <-ctx.Done():
		t.Fatal("shutdown did not finish concurrent initialization")
	}
	if err := <-result; err == nil {
		t.Fatal("startup succeeded after manager shutdown")
	}
	manager.mu.RLock()
	remaining := len(manager.servers) + len(manager.starting) + len(manager.retiring)
	manager.mu.RUnlock()
	if remaining != 0 {
		t.Fatal("shutdown retained a live or initializing generation")
	}
}

func TestToolCancellationReleasesLeaseAndResetsIdleTime(t *testing.T) {
	manager, file, clock := lifecycleManagerWithClock(t)
	holdFile := filepath.Join(filepath.Dir(file), "hold.go")
	if err := os.WriteFile(holdFile, []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	client, err := manager.clientForFile(context.Background(), holdFile)
	if err != nil {
		t.Fatal(err)
	}
	client.resetDiagnostics("file:///pending", 1)
	ready := client.diagnostics["file:///pending"].ready
	log, err := logger.New(t.TempDir(), logger.WithFile(false))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	tool := &LSPTool{manager: manager, action: "hover", log: log}
	args, _ := json.Marshal(map[string]any{"file": holdFile, "line": 1, "character": 1})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan struct{})
	go func() { _, _ = tool.Execute(ctx, string(args)); close(finished) }()
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("tool request did not reach the helper")
	}
	clock.Advance(20 * time.Minute)
	manager.reapIdle()
	if !client.alive() {
		t.Fatal("reaper killed an active tool operation")
	}
	cancel()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("tool did not release after cancellation")
	}
	manager.mu.RLock()
	state := *manager.usage[client]
	manager.mu.RUnlock()
	if state.active != 0 || !state.deadline.Equal(clock.Now().Add(10*time.Minute)) {
		t.Fatalf("release state = %+v", state)
	}
}

func TestOldDocumentOpenCannotOverwriteReplacement(t *testing.T) {
	manager, file := newLifecycleManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	old, err := manager.clientForFile(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	_ = old.process.Kill()
	<-old.processDone
	fresh, err := manager.clientForFile(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.ensureOpen(fresh, file, PathToURI(file)); err != nil {
		t.Fatal(err)
	}
	if err := manager.ensureOpen(old, file, PathToURI(file)); err == nil {
		t.Fatal("accepted stale generation")
	}
	manager.mu.RLock()
	owner := manager.docs[file].client
	manager.mu.RUnlock()
	if owner != fresh {
		t.Fatal("stale operation overwrote the new document owner")
	}
}

func TestConcurrentCloseAndOpenPreservesServerDocumentState(t *testing.T) {
	manager, file := newLifecycleManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, release, err := manager.acquireForFile(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	uri := PathToURI(file)
	for range 20 {
		if err := manager.ensureOpen(client, file, uri); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		wg.Add(2)
		errors := make(chan error, 1)
		go func() { defer wg.Done(); manager.NotifyFileClosed(file) }()
		go func() {
			defer wg.Done()
			if err := manager.ensureOpen(client, file, uri); err != nil {
				errors <- err
			}
		}()
		wg.Wait()
		close(errors)
		for err := range errors {
			t.Fatal(err)
		}
		if err := manager.ensureOpen(client, file, uri); err != nil {
			t.Fatal(err)
		}
		diagnostics, err := client.Diagnostics(ctx, uri)
		if err != nil || len(diagnostics) != 0 {
			t.Fatalf("manager and server disagree on open state: %v, %v", diagnostics, err)
		}
	}
}

func TestDiagnosticWaitersWakeWhenDocumentChanges(t *testing.T) {
	client := &Client{}
	uri := "file:///main.go"
	client.resetDiagnostics(uri, 1)
	previous := client.diagnostics[uri]
	client.resetDiagnostics(uri, 2)
	select {
	case <-previous.ready:
	default:
		t.Fatal("document replacement did not wake existing diagnostic waiters")
	}
	client.mu.Lock()
	client.diagnostics[uri].items = []Diagnostic{{Message: "current version"}}
	close(client.diagnostics[uri].ready)
	client.mu.Unlock()
	diagnostics, err := client.Diagnostics(context.Background(), uri)
	if err != nil || len(diagnostics) != 1 || diagnostics[0].Message != "current version" {
		t.Fatalf("diagnostics = %v, %v", diagnostics, err)
	}
}

func TestBlockedDocumentWriteDoesNotBlockShutdown(t *testing.T) {
	manager, file := newLifecycleManager(t)
	manager.mu.Lock()
	def := manager.defs["test-lsp"]
	def.Args = append(def.Args, "stop-reading")
	manager.defs[def.ID] = def
	manager.mu.Unlock()
	client, release, err := manager.acquireForFile(context.Background(), file)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := os.WriteFile(file, []byte(strings.Repeat("x", 2<<20)), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := manager.ensureOpenContext(ctx, client, file, PathToURI(file)); err == nil {
		t.Fatal("blocked write did not honor cancellation")
	}
	finished := make(chan struct{})
	go func() { manager.Shutdown(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("blocked document write prevented shutdown")
	}
	select {
	case <-client.processDone:
	default:
		t.Fatal("shutdown did not reap blocked process")
	}
}

type lifecycleClock struct{ nanos atomic.Int64 }

func (c *lifecycleClock) Now() time.Time          { return time.Unix(0, c.nanos.Load()) }
func (c *lifecycleClock) Advance(d time.Duration) { c.nanos.Add(int64(d)) }

func lifecycleManagerWithClock(t *testing.T) (*Manager, string, *lifecycleClock) {
	t.Helper()
	manager, file := newLifecycleManager(t)
	clock := &lifecycleClock{}
	clock.nanos.Store(time.Now().UnixNano())
	manager.mu.Lock()
	manager.now = clock.Now
	manager.mu.Unlock()
	if manager.idleTimeout != 10*time.Minute {
		t.Fatalf("idle timeout = %v, want 10m", manager.idleTimeout)
	}
	return manager, file, clock
}

func TestIdleDeadlineStartsAfterLastOperation(t *testing.T) {
	manager, file, clock := lifecycleManagerWithClock(t)
	client, releaseFirst, err := manager.acquireForFile(context.Background(), file)
	if err != nil {
		t.Fatal(err)
	}
	_, releaseLast, err := manager.acquireForFile(context.Background(), file)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseFirst()
	defer releaseLast()
	clock.Advance(11 * time.Minute)
	manager.reapIdle()
	if !client.alive() {
		t.Fatal("reaped active client")
	}
	releaseFirst()
	clock.Advance(11 * time.Minute)
	manager.reapIdle()
	if !client.alive() {
		t.Fatal("reaped client while another operation was active")
	}
	releaseLast()
	clock.Advance(10*time.Minute - time.Nanosecond)
	manager.reapIdle()
	if !client.alive() {
		t.Fatal("reaped before the renewed deadline")
	}
	clock.Advance(time.Nanosecond)
	manager.reapIdle()
	if client.alive() || len(manager.RunningServerIDs()) != 0 {
		t.Fatal("idle client was not reaped at the deadline")
	}
	select {
	case <-client.processDone:
	default:
		t.Fatal("reaper returned before process exit")
	}
}

func TestActivityAndFileChangesResetIdleDeadline(t *testing.T) {
	manager, file, clock := lifecycleManagerWithClock(t)
	client, err := manager.clientForFile(context.Background(), file)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.ensureOpen(client, file, PathToURI(file)); err != nil {
		t.Fatal(err)
	}
	clock.Advance(9 * time.Minute)
	_, release, err := manager.acquireForFile(context.Background(), file)
	if err != nil {
		t.Fatal(err)
	}
	release()
	clock.Advance(9 * time.Minute)
	manager.reapIdle()
	if !client.alive() {
		t.Fatal("activity did not refresh the deadline")
	}
	if err := manager.NotifyFileChanged(file); err != nil {
		t.Fatal(err)
	}
	clock.Advance(9 * time.Minute)
	manager.reapIdle()
	if !client.alive() {
		t.Fatal("file change did not refresh the deadline")
	}
	// Diagnostic cache activity belongs to the server, not a user operation.
	client.resetDiagnostics(PathToURI(file), 3)
	clock.Advance(time.Minute)
	manager.reapIdle()
	if client.alive() {
		t.Fatal("background diagnostic state kept an idle process alive")
	}
}

func TestIdleReopenRestoresLatestDocumentDiagnostics(t *testing.T) {
	manager, file, clock := lifecycleManagerWithClock(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	old, err := manager.clientForFile(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	uri := PathToURI(file)
	if err := manager.ensureOpen(old, file, uri); err != nil {
		t.Fatal(err)
	}
	if diagnostics, err := old.Diagnostics(ctx, uri); err != nil || len(diagnostics) != 0 {
		t.Fatalf("initial diagnostics = %v, %v", diagnostics, err)
	}
	clock.Advance(10 * time.Minute)
	manager.reapIdle()
	if err := os.WriteFile(file, []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	fresh, err := manager.clientForFile(ctx, file)
	if err != nil || fresh == old {
		t.Fatalf("lazy reload = %p, %v", fresh, err)
	}
	if err := manager.ensureOpen(fresh, file, uri); err != nil {
		t.Fatal(err)
	}
	diagnostics, err := fresh.Diagnostics(ctx, uri)
	if err != nil || len(diagnostics) != 1 || diagnostics[0].Message != "broken document" {
		t.Fatalf("reopened diagnostics = %v, %v", diagnostics, err)
	}
}

func TestConcurrentCallsReloadExitedClientOnlyOnce(t *testing.T) {
	manager, file := newLifecycleManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	old, err := manager.clientForFile(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	if err := old.process.Kill(); err != nil {
		t.Fatal(err)
	}
	<-old.processDone
	const callers = 12
	clients := make(chan *Client, callers)
	errors := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client, release, err := manager.acquireForFile(ctx, file)
			if err != nil {
				errors <- err
				return
			}
			defer release()
			clients <- client
		}()
	}
	wg.Wait()
	close(clients)
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	var fresh *Client
	for client := range clients {
		if client == old {
			t.Fatal("reused the dead generation")
		}
		if fresh != nil && fresh != client {
			t.Fatal("concurrent callers started multiple replacement processes")
		}
		fresh = client
	}
	if fresh == nil {
		t.Fatal("no replacement client")
	}
}

func TestExitedProcessFailsPendingRequestPromptly(t *testing.T) {
	manager, file := newLifecycleManager(t)
	client, err := manager.clientForFile(context.Background(), file)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result := make(chan error, 1)
	client.resetDiagnostics("file:///pending", 1)
	ready := client.diagnostics["file:///pending"].ready
	go func() { result <- client.call(ctx, "test/hang", nil, nil) }()
	select {
	case <-ready:
	case <-ctx.Done():
		t.Fatal("request did not reach helper before the crash")
	}
	if err := client.process.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err == nil || err == context.DeadlineExceeded {
			t.Fatalf("exit error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("request kept waiting after server exit")
	}
}

func TestManagerReloadsExitedClient(t *testing.T) {
	manager, file := newLifecycleManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	old, err := manager.clientForFile(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.ensureOpen(old, file, PathToURI(file)); err != nil {
		t.Fatal(err)
	}
	if err := old.process.Kill(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-old.done:
	case <-ctx.Done():
		t.Fatal("killed language server did not disconnect")
	}
	fresh, err := manager.clientForFile(ctx, file)
	if err != nil {
		t.Fatal(err)
	}
	if fresh == old {
		t.Fatal("reused exited language server instead of lazy-loading a new process")
	}
	if err := manager.ensureOpen(fresh, file, PathToURI(file)); err != nil {
		t.Fatal(err)
	}
	manager.mu.RLock()
	doc := manager.docs[file]
	manager.mu.RUnlock()
	if doc == nil || doc.client != fresh {
		t.Fatal("new process did not reopen the document")
	}
}
