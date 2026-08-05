package tools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/xiaobaitu/soloqueue/internal/agenttools/tools/sandbox"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

// RuntimeType is the user-visible execution boundary.
type RuntimeType string

const (
	RuntimeHost    RuntimeType = "host"
	RuntimeSandbox RuntimeType = "sandbox"
)

var (
	ErrSandboxUnavailable = errors.New("sandbox runtime unavailable")
	ErrInvalidRuntime     = errors.New("invalid tool runtime")
)

// ProcessSpec describes a long-lived process without shell interpolation.
type ProcessSpec = sandbox.ProcessSpec

// Process is a cancellable long-lived process.
type Process = sandbox.Process

// ToolRuntime is the single execution boundary for model-controlled process,
// filesystem and network operations.
type ToolRuntime interface {
	Type() RuntimeType
	BackendName() string
	RunCommand(context.Context, string, RunCommandOptions) (RunCommandResult, error)
	StartProcess(context.Context, ProcessSpec) (Process, error)
	ReadFile(context.Context, string, ReadFileOptions) (ReadFileResult, error)
	WriteFile(context.Context, string, []byte, WriteFileOptions) (WriteFileResult, error)
	MkdirAll(context.Context, string) error
	Stat(context.Context, string) (FileInfo, error)
	Glob(context.Context, string, string, GlobOptions) ([]string, error)
	Grep(context.Context, string, string, GrepOptions) ([]GrepMatch, error)
	HTTPGet(context.Context, string, HTTPOptions) (HTTPResponse, error)
	HTTPPost(context.Context, string, string, HTTPOptions) (HTTPResponse, error)
	ExportFile(context.Context, string) (string, error)
}

// HostRuntime is the compatibility name for the existing host executor.
// Sandbox remains as a type alias so focused callers and tests can migrate
// without changing behavior.
type HostRuntime = Sandbox

func NewHostRuntime() *HostRuntime { return NewSandbox() }

func (s *Sandbox) Type() RuntimeType   { return RuntimeHost }
func (s *Sandbox) BackendName() string { return "host" }
func (s *Sandbox) ExportFile(ctx context.Context, path string) (string, error) {
	info, err := s.Stat(ctx, path)
	if err != nil {
		return "", err
	}
	if info.IsDir {
		return "", fmt.Errorf("path is a directory, not a file: %s", path)
	}
	return path, nil
}
func (s *Sandbox) StartProcess(ctx context.Context, spec ProcessSpec) (Process, error) {
	if strings.TrimSpace(spec.Command) == "" {
		return nil, fmt.Errorf("%w: empty process command", ErrInvalidArgs)
	}
	cmd := exec.CommandContext(ctx, spec.Command, spec.Args...)
	if spec.WorkingDirectory != "" {
		cmd.Dir = filepath.Clean(spec.WorkingDirectory)
	}
	envMap := minimalHostEnvironment()
	for key, value := range spec.Env {
		envMap[key] = value
	}
	cmd.Env = environmentList(envMap)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	configureRuntimeProcess(cmd)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &hostProcess{cmd: cmd, stdin: stdin, stdout: stdout, stderr: stderr}, nil
}

type hostProcess struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.Reader
	stderr io.Reader
}

func (p *hostProcess) Stdin() io.WriteCloser { return p.stdin }
func (p *hostProcess) Stdout() io.Reader     { return p.stdout }
func (p *hostProcess) Stderr() io.Reader     { return p.stderr }
func (p *hostProcess) Wait() error           { return p.cmd.Wait() }
func (p *hostProcess) Kill() error {
	if p.cmd.Process == nil {
		return nil
	}
	return killRuntimeProcess(p.cmd)
}

// SandboxRuntime delegates all execution to one backend. It never falls back
// to HostRuntime.
type SandboxRuntime struct {
	backend sandbox.Backend
}

func NewSandboxRuntime(backend sandbox.Backend) (*SandboxRuntime, error) {
	if backend == nil {
		return nil, fmt.Errorf("%w: backend is nil", ErrSandboxUnavailable)
	}
	return &SandboxRuntime{backend: backend}, nil
}

func (r *SandboxRuntime) Type() RuntimeType   { return RuntimeSandbox }
func (r *SandboxRuntime) BackendName() string { return r.backend.Name() }
func (r *SandboxRuntime) RunCommand(ctx context.Context, cmd string, opts RunCommandOptions) (RunCommandResult, error) {
	return r.backend.RunCommand(ctx, cmd, opts)
}
func (r *SandboxRuntime) StartProcess(ctx context.Context, spec ProcessSpec) (Process, error) {
	return r.backend.StartProcess(ctx, spec)
}
func (r *SandboxRuntime) ReadFile(ctx context.Context, path string, opts ReadFileOptions) (ReadFileResult, error) {
	data, err := r.backend.ReadFile(ctx, path, sandbox.ReadFileOptions{MaxSize: opts.MaxSize})
	if err != nil {
		return ReadFileResult{}, err
	}
	return ReadFileResult{Data: data}, nil
}
func (r *SandboxRuntime) WriteFile(ctx context.Context, path string, data []byte, opts WriteFileOptions) (WriteFileResult, error) {
	result, err := r.backend.WriteFile(ctx, path, data, sandbox.WriteFileOptions{
		Overwrite: opts.Overwrite,
		MaxSize:   opts.MaxSize,
	})
	if err != nil {
		return WriteFileResult{}, err
	}
	return WriteFileResult{Created: result.Created}, nil
}
func (r *SandboxRuntime) MkdirAll(ctx context.Context, path string) error {
	return r.backend.MkdirAll(ctx, path)
}
func (r *SandboxRuntime) Stat(ctx context.Context, path string) (FileInfo, error) {
	info, err := r.backend.Stat(ctx, path)
	if err != nil {
		return FileInfo{}, err
	}
	return FileInfo{Size: info.Size, IsDir: info.IsDir}, nil
}
func (r *SandboxRuntime) Glob(ctx context.Context, dir, pattern string, opts GlobOptions) ([]string, error) {
	return r.backend.Glob(ctx, dir, pattern, sandbox.GlobOptions{
		MaxItems: opts.MaxItems,
		Timeout:  opts.Timeout,
	})
}
func (r *SandboxRuntime) Grep(ctx context.Context, dir, pattern string, opts GrepOptions) ([]GrepMatch, error) {
	matches, err := r.backend.Grep(ctx, dir, pattern, sandbox.GrepOptions{
		MaxMatches:  opts.MaxMatches,
		MaxLineLen:  opts.MaxLineLen,
		GlobPattern: opts.GlobPattern,
	})
	if err != nil {
		return nil, err
	}
	out := make([]GrepMatch, len(matches))
	for i, match := range matches {
		out[i] = GrepMatch{File: match.File, Line: match.Line, Content: match.Content}
	}
	return out, nil
}
func (r *SandboxRuntime) HTTPGet(ctx context.Context, rawURL string, opts HTTPOptions) (HTTPResponse, error) {
	result, err := r.backend.DoHTTP(ctx, sandbox.HTTPRequest{
		Method:  "GET",
		URL:     rawURL,
		Headers: opts.Headers,
		Timeout: opts.Timeout,
		MaxBody: opts.MaxBody,
	})
	if err != nil {
		return HTTPResponse{}, err
	}
	return HTTPResponse{StatusCode: result.StatusCode, Body: result.Body}, nil
}
func (r *SandboxRuntime) HTTPPost(ctx context.Context, rawURL, body string, opts HTTPOptions) (HTTPResponse, error) {
	result, err := r.backend.DoHTTP(ctx, sandbox.HTTPRequest{
		Method:      "POST",
		URL:         rawURL,
		Body:        []byte(body),
		Headers:     opts.Headers,
		ContentType: opts.ContentType,
		Timeout:     opts.Timeout,
		MaxBody:     opts.MaxBody,
	})
	if err != nil {
		return HTTPResponse{}, err
	}
	return HTTPResponse{StatusCode: result.StatusCode, Body: result.Body}, nil
}
func (r *SandboxRuntime) ExportFile(ctx context.Context, path string) (string, error) {
	data, err := r.backend.ReadFile(ctx, path, sandbox.ReadFileOptions{MaxSize: 100 << 20})
	if err != nil {
		return "", err
	}
	dir := defaultArtifactDir()
	if dir == "" {
		return "", fmt.Errorf("sandbox: artifact staging unavailable")
	}
	ext := filepath.Ext(path)
	file, err := os.CreateTemp(dir, "export-*"+ext)
	if err != nil {
		return "", err
	}
	name := file.Name()
	remove := true
	defer func() {
		_ = file.Close()
		if remove {
			_ = os.Remove(name)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return "", err
	}
	if err := file.Sync(); err != nil {
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	remove = false
	return name, nil
}

type runtimeScope struct {
	workspace      string
	planDir        string
	owner          string
	networkEnabled bool
}

type BackendFactory func(runtimeScope) (sandbox.Backend, error)

type runtimePreparation struct {
	done   chan struct{}
	cancel context.CancelFunc
	err    error
}

// RuntimeStatus is safe to expose through the status API.
type RuntimeStatus struct {
	DesiredRuntime    RuntimeType `json:"desired_runtime"`
	State             string      `json:"state"`
	Backend           string      `json:"backend,omitempty"`
	Workspace         string      `json:"workspace,omitempty"`
	IsolationComplete bool        `json:"isolation_complete"`
	HostExceptions    int         `json:"host_exceptions"`
	NetworkEnabled    bool        `json:"network_enabled"`
	LastError         string      `json:"last_error,omitempty"`
}

// RuntimeManager owns the desired Host/Sandbox state and workspace-scoped
// backend instances. Views resolve it on every call so hot reload affects
// already-created agents.
type RuntimeManager struct {
	mu             sync.Mutex
	desired        RuntimeType
	log            *logger.Logger
	factory        BackendFactory
	host           *HostRuntime
	scopes         map[runtimeScope]*SandboxRuntime
	scopeErrors    map[runtimeScope]error
	preparing      map[runtimeScope]*runtimePreparation
	closed         bool
	networkEnabled bool
}

func NewRuntimeManager(desired RuntimeType, log *logger.Logger) *RuntimeManager {
	if desired != RuntimeSandbox {
		desired = RuntimeHost
	}
	manager := &RuntimeManager{
		desired:     desired,
		log:         log,
		host:        NewHostRuntime(),
		scopes:      make(map[runtimeScope]*SandboxRuntime),
		scopeErrors: make(map[runtimeScope]error),
		preparing:   make(map[runtimeScope]*runtimePreparation),
	}
	manager.host.SetLogger(log)
	manager.factory = manager.newDockerBackend
	return manager
}

func (m *RuntimeManager) newDockerBackend(scope runtimeScope) (sandbox.Backend, error) {
	backend, err := sandbox.NewDockerBackend(sandbox.DockerOptions{
		Workspace:      scope.workspace,
		PlanDir:        scope.planDir,
		Owner:          scope.owner,
		NetworkEnabled: scope.networkEnabled,
	}, m.log)
	// Do not return a typed nil through the Backend interface. If Docker is
	// unavailable, the constructor returns (*DockerRunner)(nil, err); returning
	// that value directly would make the interface non-nil and cause cleanup to
	// invoke Stop on a nil receiver during runtime resolution.
	if err != nil {
		return nil, err
	}
	return backend, nil
}

func (m *RuntimeManager) SetBackendFactory(factory BackendFactory) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.factory = factory
}

func (m *RuntimeManager) Desired() RuntimeType {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.desired
}

func (m *RuntimeManager) NetworkEnabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.networkEnabled
}

// SetNetworkEnabled updates the global sandbox egress capability. Existing
// tools/LSP sandboxes are destroyed so the new network boundary takes effect.
func (m *RuntimeManager) SetNetworkEnabled(enabled bool) {
	m.mu.Lock()
	if m.networkEnabled == enabled || m.closed {
		m.mu.Unlock()
		return
	}
	m.networkEnabled = enabled
	var toStop []*SandboxRuntime
	var toCancel []context.CancelFunc
	for scope, runtime := range m.scopes {
		if scope.owner == "tools" || scope.owner == "lsp" {
			toStop = append(toStop, runtime)
			delete(m.scopes, scope)
			delete(m.scopeErrors, scope)
		}
	}
	for scope, preparation := range m.preparing {
		if scope.owner == "tools" || scope.owner == "lsp" {
			toCancel = append(toCancel, preparation.cancel)
		}
	}
	m.mu.Unlock()
	for _, cancel := range toCancel {
		cancel()
	}
	for _, runtime := range toStop {
		_ = runtime.backend.Stop(context.Background())
	}
}

func (m *RuntimeManager) View(workspace, planDir string) ToolRuntime {
	return m.ViewOwned("tools", workspace, planDir)
}

// Prepare verifies and starts the global tool sandbox for a workspace. Host
// mode is a no-op. Failures remain scope-local and never trigger host fallback.
func (m *RuntimeManager) Prepare(ctx context.Context, workspace, planDir string) error {
	_, err := m.resolve(ctx, runtimeScope{
		workspace: cleanOptionalPath(workspace),
		planDir:   cleanOptionalPath(planDir),
		owner:     "tools",
	}, nil, nil)
	return err
}

// ViewOwned follows the global Host/Sandbox setting while isolating backend
// lifecycle from other subsystem owners.
func (m *RuntimeManager) ViewOwned(owner, workspace, planDir string) ToolRuntime {
	if strings.TrimSpace(owner) == "" {
		owner = "tools"
	}
	return &managedRuntime{manager: m, scope: runtimeScope{
		workspace: cleanOptionalPath(workspace),
		planDir:   cleanOptionalPath(planDir),
		owner:     owner,
	}}
}

// ViewForType returns an explicitly typed runtime for an independently
// governed subsystem such as MCP. The owner becomes part of the backend
// identity so secrets, process state and writable caches are not shared.
func (m *RuntimeManager) ViewForType(runtimeType RuntimeType, owner, workspace, planDir string) ToolRuntime {
	return m.ViewForPolicy(runtimeType, owner, workspace, planDir, false)
}

// ViewForPolicy returns an explicitly governed runtime for MCP or another
// independently approved subsystem.
func (m *RuntimeManager) ViewForPolicy(runtimeType RuntimeType, owner, workspace, planDir string, networkEnabled bool) ToolRuntime {
	if runtimeType != RuntimeSandbox {
		runtimeType = RuntimeHost
	}
	if strings.TrimSpace(owner) == "" {
		owner = "external"
	}
	forced := runtimeType
	forcedNetwork := networkEnabled
	return &managedRuntime{manager: m, scope: runtimeScope{
		workspace: cleanOptionalPath(workspace),
		planDir:   cleanOptionalPath(planDir),
		owner:     owner,
	}, forced: &forced, forcedNetwork: &forcedNetwork}
}

func (m *RuntimeManager) SetDesired(desired RuntimeType) error {
	if desired != RuntimeHost && desired != RuntimeSandbox {
		return fmt.Errorf("%w: %q", ErrInvalidRuntime, desired)
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return fmt.Errorf("%w: manager is closed", ErrSandboxUnavailable)
	}
	if m.desired == desired {
		m.mu.Unlock()
		return nil
	}
	m.desired = desired
	var toStop []*SandboxRuntime
	var toCancel []context.CancelFunc
	if desired == RuntimeHost {
		for scope, runtime := range m.scopes {
			if scope.owner == "tools" || scope.owner == "lsp" {
				toStop = append(toStop, runtime)
				delete(m.scopes, scope)
				delete(m.scopeErrors, scope)
			}
		}
		for scope, preparation := range m.preparing {
			if scope.owner == "tools" || scope.owner == "lsp" {
				toCancel = append(toCancel, preparation.cancel)
			}
		}
	}
	m.mu.Unlock()
	for _, cancel := range toCancel {
		cancel()
	}
	for _, runtime := range toStop {
		_ = runtime.backend.Stop(context.Background())
	}
	return nil
}

func (m *RuntimeManager) resolve(
	ctx context.Context,
	scope runtimeScope,
	forced *RuntimeType,
	forcedNetwork *bool,
) (ToolRuntime, error) {
	for {
		m.mu.Lock()
		if m.closed {
			m.mu.Unlock()
			return nil, fmt.Errorf("%w: manager is closed", ErrSandboxUnavailable)
		}
		desired := m.desired
		if forced != nil {
			desired = *forced
		}
		if desired == RuntimeHost {
			host := m.host
			m.mu.Unlock()
			return host, nil
		}
		scope.networkEnabled = m.networkEnabled
		if forcedNetwork != nil {
			scope.networkEnabled = *forcedNetwork
		}
		if runtime := m.scopes[scope]; runtime != nil {
			m.mu.Unlock()
			return runtime, nil
		}
		if preparation := m.preparing[scope]; preparation != nil {
			done := preparation.done
			m.mu.Unlock()
			select {
			case <-done:
				if preparation.err != nil {
					return nil, preparation.err
				}
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		if m.factory == nil {
			err := fmt.Errorf("%w: backend factory is not configured", ErrSandboxUnavailable)
			m.scopeErrors[scope] = err
			m.mu.Unlock()
			return nil, err
		}
		factory := m.factory
		prepareCtx, cancel := context.WithCancel(ctx)
		preparation := &runtimePreparation{done: make(chan struct{}), cancel: cancel}
		m.preparing[scope] = preparation
		m.mu.Unlock()

		backend, backendErr := factory(scope)
		if backendErr == nil {
			backendErr = backend.Prepare(prepareCtx)
		}
		cancel()

		var runtime *SandboxRuntime
		if backendErr == nil {
			runtime, backendErr = NewSandboxRuntime(backend)
		}
		if backendErr != nil && backend != nil {
			_ = backend.Stop(context.Background())
		}

		m.mu.Lock()
		delete(m.preparing, scope)
		resultErr := backendErr
		currentDesired := m.desired
		if forced != nil {
			currentDesired = *forced
		}
		currentNetwork := m.networkEnabled
		if forcedNetwork != nil {
			currentNetwork = *forcedNetwork
		}
		stillRelevant := !m.closed && currentDesired == RuntimeSandbox &&
			currentNetwork == scope.networkEnabled
		if resultErr != nil {
			resultErr = fmt.Errorf("%w: prepare backend: %v", ErrSandboxUnavailable, resultErr)
			if stillRelevant {
				m.scopeErrors[scope] = resultErr
			}
		} else {
			if !stillRelevant {
				resultErr = fmt.Errorf("%w: runtime policy changed while backend was starting", ErrSandboxUnavailable)
			} else {
				m.scopes[scope] = runtime
				delete(m.scopeErrors, scope)
			}
		}
		preparation.err = resultErr
		close(preparation.done)
		m.mu.Unlock()

		if resultErr != nil {
			if backendErr == nil && backend != nil {
				_ = backend.Stop(context.Background())
			}
			return nil, resultErr
		}
		return runtime, nil
	}
}

func (m *RuntimeManager) Status(workspace, planDir string) RuntimeStatus {
	cleanWorkspace := cleanOptionalPath(workspace)
	cleanPlanDir := cleanOptionalPath(planDir)
	m.mu.Lock()
	defer m.mu.Unlock()
	scope := runtimeScope{
		workspace:      cleanWorkspace,
		planDir:        cleanPlanDir,
		owner:          "tools",
		networkEnabled: m.networkEnabled,
	}
	status := RuntimeStatus{
		DesiredRuntime:    m.desired,
		IsolationComplete: m.desired == RuntimeSandbox,
		Workspace:         workspace,
		NetworkEnabled:    m.networkEnabled,
	}
	if m.desired == RuntimeHost {
		status.State = "ready"
		status.Backend = "host"
		return status
	}
	if scopeErr := m.scopeErrors[scope]; scopeErr != nil {
		status.State = "failed"
		status.LastError = scopeErr.Error()
		return status
	}
	if m.preparing[scope] != nil {
		status.State = "starting"
		status.Backend = "docker"
		return status
	}
	if runtime := m.scopes[scope]; runtime != nil {
		status.State = "ready"
		status.Backend = runtime.BackendName()
	} else {
		// Sandbox runtimes are prepared lazily on the first tool operation. No
		// scope here means Docker has not been requested yet; reporting
		// "starting" makes a healthy, idle installation look stuck forever.
		status.State = "idle"
		status.Backend = "docker"
	}
	return status
}

func cleanOptionalPath(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	return filepath.Clean(path)
}

func minimalHostEnvironment() map[string]string {
	keys := []string{
		"PATH", "HOME", "TMPDIR", "LANG", "LC_ALL",
		"SystemRoot", "ComSpec", "PATHEXT",
	}
	env := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := os.LookupEnv(key); ok {
			env[key] = value
		}
	}
	return env
}

func environmentList(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+env[key])
	}
	return out
}

func (m *RuntimeManager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	var runtimes []*SandboxRuntime
	var cancellations []context.CancelFunc
	for _, runtime := range m.scopes {
		runtimes = append(runtimes, runtime)
	}
	m.scopes = nil
	m.scopeErrors = nil
	for _, preparation := range m.preparing {
		cancellations = append(cancellations, preparation.cancel)
	}
	m.preparing = nil
	m.mu.Unlock()

	for _, cancel := range cancellations {
		cancel()
	}
	var errs []error
	for _, runtime := range runtimes {
		if err := runtime.backend.Stop(context.Background()); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

type managedRuntime struct {
	manager       *RuntimeManager
	scope         runtimeScope
	forced        *RuntimeType
	forcedNetwork *bool
}

func (r *managedRuntime) current(ctx context.Context) (ToolRuntime, error) {
	if r.manager == nil {
		return nil, fmt.Errorf("%w: manager is nil", ErrSandboxUnavailable)
	}
	return r.manager.resolve(ctx, r.scope, r.forced, r.forcedNetwork)
}

func (r *managedRuntime) Type() RuntimeType {
	if r.manager == nil {
		return RuntimeHost
	}
	if r.forced != nil {
		return *r.forced
	}
	r.manager.mu.Lock()
	defer r.manager.mu.Unlock()
	return r.manager.desired
}
func (r *managedRuntime) BackendName() string {
	current, err := r.current(context.Background())
	if err != nil {
		return ""
	}
	return current.BackendName()
}
func (r *managedRuntime) RunCommand(ctx context.Context, cmd string, opts RunCommandOptions) (RunCommandResult, error) {
	current, err := r.current(ctx)
	if err != nil {
		return RunCommandResult{}, err
	}
	return current.RunCommand(ctx, cmd, opts)
}
func (r *managedRuntime) StartProcess(ctx context.Context, spec ProcessSpec) (Process, error) {
	current, err := r.current(ctx)
	if err != nil {
		return nil, err
	}
	return current.StartProcess(ctx, spec)
}
func (r *managedRuntime) ReadFile(ctx context.Context, path string, opts ReadFileOptions) (ReadFileResult, error) {
	current, err := r.current(ctx)
	if err != nil {
		return ReadFileResult{}, err
	}
	return current.ReadFile(ctx, path, opts)
}
func (r *managedRuntime) WriteFile(ctx context.Context, path string, data []byte, opts WriteFileOptions) (WriteFileResult, error) {
	current, err := r.current(ctx)
	if err != nil {
		return WriteFileResult{}, err
	}
	return current.WriteFile(ctx, path, data, opts)
}
func (r *managedRuntime) MkdirAll(ctx context.Context, path string) error {
	current, err := r.current(ctx)
	if err != nil {
		return err
	}
	return current.MkdirAll(ctx, path)
}
func (r *managedRuntime) Stat(ctx context.Context, path string) (FileInfo, error) {
	current, err := r.current(ctx)
	if err != nil {
		return FileInfo{}, err
	}
	return current.Stat(ctx, path)
}
func (r *managedRuntime) Glob(ctx context.Context, dir, pattern string, opts GlobOptions) ([]string, error) {
	current, err := r.current(ctx)
	if err != nil {
		return nil, err
	}
	return current.Glob(ctx, dir, pattern, opts)
}
func (r *managedRuntime) Grep(ctx context.Context, dir, pattern string, opts GrepOptions) ([]GrepMatch, error) {
	current, err := r.current(ctx)
	if err != nil {
		return nil, err
	}
	return current.Grep(ctx, dir, pattern, opts)
}
func (r *managedRuntime) HTTPGet(ctx context.Context, rawURL string, opts HTTPOptions) (HTTPResponse, error) {
	current, err := r.current(ctx)
	if err != nil {
		return HTTPResponse{}, err
	}
	return current.HTTPGet(ctx, rawURL, opts)
}
func (r *managedRuntime) HTTPPost(ctx context.Context, rawURL, body string, opts HTTPOptions) (HTTPResponse, error) {
	current, err := r.current(ctx)
	if err != nil {
		return HTTPResponse{}, err
	}
	return current.HTTPPost(ctx, rawURL, body, opts)
}
func (r *managedRuntime) ExportFile(ctx context.Context, path string) (string, error) {
	current, err := r.current(ctx)
	if err != nil {
		return "", err
	}
	return current.ExportFile(ctx, path)
}

func defaultArtifactDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	path := filepath.Join(home, ".soloqueue", "artifacts")
	if err := os.MkdirAll(path, 0o755); err != nil {
		return ""
	}
	return path
}

var _ ToolRuntime = (*Sandbox)(nil)
var _ ToolRuntime = (*SandboxRuntime)(nil)
var _ ToolRuntime = (*managedRuntime)(nil)
