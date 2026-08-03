package tools

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/tools/sandbox"
)

type fakeSandboxBackend struct {
	runCount  atomic.Int32
	stopCount atomic.Int32
}

type blockingSandboxBackend struct {
	fakeSandboxBackend
	started chan struct{}
	release chan struct{}
}

func (b *blockingSandboxBackend) Prepare(ctx context.Context) error {
	close(b.started)
	select {
	case <-b.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (*fakeSandboxBackend) Name() string                  { return "fake" }
func (*fakeSandboxBackend) Prepare(context.Context) error { return nil }
func (f *fakeSandboxBackend) RunCommand(_ context.Context, _ string, _ sandbox.RunCommandOptions) (sandbox.RunCommandResult, error) {
	f.runCount.Add(1)
	return sandbox.RunCommandResult{ExitCode: 0, Stdout: []byte("sandbox")}, nil
}
func (*fakeSandboxBackend) StartProcess(context.Context, sandbox.ProcessSpec) (sandbox.Process, error) {
	return nil, errors.New("not implemented")
}
func (*fakeSandboxBackend) ReadFile(context.Context, string, sandbox.ReadFileOptions) ([]byte, error) {
	return []byte("sandbox"), nil
}
func (*fakeSandboxBackend) WriteFile(context.Context, string, []byte, sandbox.WriteFileOptions) (sandbox.WriteFileResult, error) {
	return sandbox.WriteFileResult{Created: true}, nil
}
func (*fakeSandboxBackend) MkdirAll(context.Context, string) error { return nil }
func (*fakeSandboxBackend) Stat(context.Context, string) (sandbox.FileInfo, error) {
	return sandbox.FileInfo{Size: 7}, nil
}
func (*fakeSandboxBackend) Glob(context.Context, string, string, sandbox.GlobOptions) ([]string, error) {
	return nil, nil
}
func (*fakeSandboxBackend) Grep(context.Context, string, string, sandbox.GrepOptions) ([]sandbox.GrepMatch, error) {
	return nil, nil
}
func (*fakeSandboxBackend) DoHTTP(context.Context, sandbox.HTTPRequest) (sandbox.HTTPResponse, error) {
	return sandbox.HTTPResponse{StatusCode: 200}, nil
}
func (f *fakeSandboxBackend) Stop(context.Context) error {
	f.stopCount.Add(1)
	return nil
}

func TestRuntimeManagerSandboxFailureDoesNotFallbackToHost(t *testing.T) {
	manager := NewRuntimeManager(RuntimeSandbox, nil)
	manager.SetBackendFactory(func(runtimeScope) (sandbox.Backend, error) {
		return nil, errors.New("backend unavailable")
	})
	view := manager.View(t.TempDir(), "")

	result, err := view.RunCommand(context.Background(), "printf host-side-effect", RunCommandOptions{})
	if err == nil {
		t.Fatal("expected sandbox startup failure")
	}
	if !errors.Is(err, ErrSandboxUnavailable) {
		t.Fatalf("error = %v, want ErrSandboxUnavailable", err)
	}
	if len(result.Stdout) != 0 {
		t.Fatalf("unexpected host fallback output: %q", result.Stdout)
	}
}

func TestRuntimeManagerExistingViewTracksRuntimeChanges(t *testing.T) {
	manager := NewRuntimeManager(RuntimeHost, nil)
	backend := &fakeSandboxBackend{}
	manager.SetBackendFactory(func(runtimeScope) (sandbox.Backend, error) {
		return backend, nil
	})
	view := manager.View(t.TempDir(), "")

	hostResult, err := view.RunCommand(context.Background(), "printf host", RunCommandOptions{})
	if err != nil {
		t.Fatalf("host run: %v", err)
	}
	if string(hostResult.Stdout) != "host" {
		t.Fatalf("host stdout = %q", hostResult.Stdout)
	}

	if err := manager.SetDesired(RuntimeSandbox); err != nil {
		t.Fatalf("set sandbox: %v", err)
	}
	sandboxResult, err := view.RunCommand(context.Background(), "ignored", RunCommandOptions{})
	if err != nil {
		t.Fatalf("sandbox run: %v", err)
	}
	if string(sandboxResult.Stdout) != "sandbox" {
		t.Fatalf("sandbox stdout = %q", sandboxResult.Stdout)
	}
	if backend.runCount.Load() != 1 {
		t.Fatalf("sandbox run count = %d", backend.runCount.Load())
	}

	if err := manager.SetDesired(RuntimeHost); err != nil {
		t.Fatalf("set host: %v", err)
	}
	if backend.stopCount.Load() != 1 {
		t.Fatalf("backend stop count = %d", backend.stopCount.Load())
	}
}

func TestSandboxRuntimeExportStagesBackendFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	backend := &fakeSandboxBackend{}
	runtime, err := NewSandboxRuntime(backend)
	if err != nil {
		t.Fatal(err)
	}
	path, err := runtime.ExportFile(context.Background(), "/sandbox/result.txt")
	if err != nil {
		t.Fatal(err)
	}
	data, err := NewHostRuntime().ReadFile(context.Background(), path, ReadFileOptions{MaxSize: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data.Data)) != "sandbox" {
		t.Fatalf("staged content = %q", data.Data)
	}
}

func TestRuntimeManagerForcedMCPRuntimeIsIndependentFromGlobalRuntime(t *testing.T) {
	manager := NewRuntimeManager(RuntimeSandbox, nil)
	backends := make(map[string]*fakeSandboxBackend)
	manager.SetBackendFactory(func(scope runtimeScope) (sandbox.Backend, error) {
		backend := &fakeSandboxBackend{}
		backends[scope.owner] = backend
		return backend, nil
	})

	workspace := t.TempDir()
	globalView := manager.View(workspace, "")
	mcpView := manager.ViewForType(RuntimeSandbox, "mcp:global:files", workspace, "")
	if _, err := globalView.RunCommand(context.Background(), "global", RunCommandOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := mcpView.RunCommand(context.Background(), "mcp", RunCommandOptions{}); err != nil {
		t.Fatal(err)
	}

	if err := manager.SetDesired(RuntimeHost); err != nil {
		t.Fatal(err)
	}
	if backends["tools"].stopCount.Load() != 1 {
		t.Fatal("global tools backend was not stopped")
	}
	if backends["mcp:global:files"].stopCount.Load() != 0 {
		t.Fatal("explicit sandbox MCP was stopped by unrelated global runtime switch")
	}
	result, err := mcpView.RunCommand(context.Background(), "still-sandboxed", RunCommandOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if string(result.Stdout) != "sandbox" {
		t.Fatalf("forced MCP runtime fell back to host: %q", result.Stdout)
	}
}

func TestHostRuntimeProcessDoesNotInheritArbitraryEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("env command is Unix-specific")
	}
	envCommand, err := exec.LookPath("env")
	if err != nil {
		t.Skip("env command unavailable")
	}
	t.Setenv("SOLOQUEUE_SHOULD_NOT_LEAK", "secret")

	process, err := NewHostRuntime().StartProcess(context.Background(), ProcessSpec{
		Command: envCommand,
		Env:     map[string]string{"SOLOQUEUE_ALLOWED": "yes"},
	})
	if err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(process.Stdout())
	if err != nil {
		t.Fatal(err)
	}
	if err := process.Wait(); err != nil {
		t.Fatal(err)
	}
	text := string(output)
	if strings.Contains(text, "SOLOQUEUE_SHOULD_NOT_LEAK=") {
		t.Fatal("arbitrary host environment leaked into child process")
	}
	if !strings.Contains(text, "SOLOQUEUE_ALLOWED=yes") {
		t.Fatal("explicit process environment was not injected")
	}
}

func TestRuntimeManagerNetworkCapabilityIsExplicitAndRecreatesSandbox(t *testing.T) {
	manager := NewRuntimeManager(RuntimeSandbox, nil)
	var scopes []runtimeScope
	var backends []*fakeSandboxBackend
	manager.SetBackendFactory(func(scope runtimeScope) (sandbox.Backend, error) {
		backend := &fakeSandboxBackend{}
		scopes = append(scopes, scope)
		backends = append(backends, backend)
		return backend, nil
	})
	view := manager.View(t.TempDir(), "")
	if _, err := view.RunCommand(context.Background(), "offline", RunCommandOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(scopes) != 1 || scopes[0].networkEnabled {
		t.Fatalf("default network scope = %#v", scopes)
	}

	manager.SetNetworkEnabled(true)
	if backends[0].stopCount.Load() != 1 {
		t.Fatal("network policy change did not stop the old sandbox")
	}
	if _, err := view.RunCommand(context.Background(), "online", RunCommandOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(scopes) != 2 || !scopes[1].networkEnabled {
		t.Fatalf("updated network scope = %#v", scopes)
	}
}

func TestRuntimeManagerPreparationIsSingleflightAndStatusDoesNotBlock(t *testing.T) {
	manager := NewRuntimeManager(RuntimeSandbox, nil)
	backend := &blockingSandboxBackend{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	var factoryCalls atomic.Int32
	manager.SetBackendFactory(func(runtimeScope) (sandbox.Backend, error) {
		factoryCalls.Add(1)
		return backend, nil
	})
	workspace := t.TempDir()
	view := manager.View(workspace, "")
	results := make(chan error, 2)
	go func() {
		_, err := view.RunCommand(context.Background(), "one", RunCommandOptions{})
		results <- err
	}()
	<-backend.started
	go func() {
		_, err := view.RunCommand(context.Background(), "two", RunCommandOptions{})
		results <- err
	}()

	statusResult := make(chan RuntimeStatus, 1)
	go func() { statusResult <- manager.Status(workspace, "") }()
	select {
	case status := <-statusResult:
		if status.State != "starting" {
			t.Fatalf("status while preparing = %q", status.State)
		}
	case <-time.After(time.Second):
		t.Fatal("status blocked behind backend preparation")
	}

	close(backend.release)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if factoryCalls.Load() != 1 {
		t.Fatalf("backend factory calls = %d, want 1", factoryCalls.Load())
	}
}

func TestRuntimeManagerCloseCancelsPreparation(t *testing.T) {
	manager := NewRuntimeManager(RuntimeSandbox, nil)
	backend := &blockingSandboxBackend{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	manager.SetBackendFactory(func(runtimeScope) (sandbox.Backend, error) {
		return backend, nil
	})
	workspace := t.TempDir()
	result := make(chan error, 1)
	go func() {
		_, err := manager.View(workspace, "").RunCommand(context.Background(), "blocked", RunCommandOptions{})
		result <- err
	}()
	<-backend.started
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, ErrSandboxUnavailable) {
			t.Fatalf("prepare error = %v, want ErrSandboxUnavailable", err)
		}
	case <-time.After(time.Second):
		t.Fatal("backend preparation was not cancelled on close")
	}
}

var _ sandbox.Backend = (*fakeSandboxBackend)(nil)
var _ sandbox.Backend = (*blockingSandboxBackend)(nil)
