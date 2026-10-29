// Package laya runs the local laya decision model as the task classifier's alternative engine.
//
// The model is never bundled: the manager downloads a pinned graph plus its tokenizer, provisions
// ONNX Runtime, and starts the current executable in its internal Laya mode. Lifecycle mirrors
// engine.VectorManager: Apply starts or stops the worker per configuration, and disabling keeps the
// loaded model warm for 60s so rapid toggling does not pay a 1.3GB reload.
package laya

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/infra/buildidentity"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/infra/onnxruntime"
	"github.com/xiaobaitu/soloqueue/internal/tasktype"
)

// Variant is the only offered artifact: the fp32 multilingual graph. int4 is 1/6 the size but
// measured 2.4x slower and 10 points less accurate on arm64.
const Variant = "laya-multilingual-fp32"

const runtimeVersion = "1.30.0"
const runtimeNamespace = "decide"

const maxRequestFrame = 1 << 20

var errInvalidRequest = errors.New("invalid local classifier request")

// stopGrace keeps a loaded worker alive briefly after the engine is switched off.
var stopGrace = 60 * time.Second

// A 2-core Linux container under load measured p95 894ms and 1.2s worst case, so 2s left no room.
const decideTimeout = 5 * time.Second

const (
	modelRepo     = "onnx-community/laya-multilingual-ONNX"
	modelRevision = "46b77bbf5642fec5f14e540570228a8cbe8ab81f"
	baseRepo      = "convaiinnovations/laya-multilingual"
	baseRevision  = "1720e3e3357cfe1e281542e223f8273b0890ca34"
)

// ErrUnavailable means the local engine could not answer, so the caller falls back.
var ErrUnavailable = errors.New("local task classifier unavailable")

// artifact is one file of the checkpoint, pinned by hash.
type artifact struct {
	path string // destination, relative to the model directory
	url  string
	hash string
}

var artifacts = []artifact{
	{"model.onnx", modelURL("onnx/model.onnx"), "22461d988f675dd6c2b89ff8feb9ed7ffcf7e37854e5ed512f8b275178cf2f9f"},
	{"model.onnx_data", modelURL("onnx/model.onnx_data"), "f01238e183ace77661282e4eb2dd9b872c7044cb9678f904cf409e6cc1146d09"},
	{"tokenizer/tokenizer.json", modelURL("tokenizer.json"), "609d8f4c067cd3950f88594c5a802616cea245823836ef5848ee4fc40aab5b6f"},
	{"tokenizer/tokenizer_config.json", modelURL("tokenizer_config.json"), "424b69444bf7b5809dc2cd2e36d0bd71b8055124dd24274d6db3c655d38205e7"},
	{"rl_agent_config.json", baseURL("rl_agent_config.json"), "25061739243b617ad88d1219ba6f8a9c86c5881ca28df024fa2d9b3b2fcc30c6"},
	{"encoder/config.json", baseURL("encoder/config.json"), "83f6916d13ef0f556ac461f28308dc2bffa7ebeadee8ec9e2db5812020ea5bb4"},
}

func modelURL(path string) string {
	return "https://huggingface.co/" + modelRepo + "/resolve/" + modelRevision + "/" + path
}

func baseURL(path string) string {
	return "https://huggingface.co/" + baseRepo + "/resolve/" + baseRevision + "/" + path
}

// Config is the classifier engine configuration.
type Config struct {
	Enabled  bool
	ModelDir string // optional; empty uses the managed directory under the work dir
	Device   string // "cpu" only
}

// Status reports the worker lifecycle, mirroring the embedding status shape.
type Status struct {
	State           string `json:"state"`
	Model           string `json:"model"`
	Device          string `json:"device"`
	Backend         string `json:"backend"`
	DownloadedBytes int64  `json:"downloadedBytes"`
	TotalBytes      int64  `json:"totalBytes"`
	Error           string `json:"error"`
}

// Manager owns the worker lifecycle. Call New before use.
type Manager struct {
	workDir    string
	client     *http.Client
	log        *logger.Logger
	workerPath func() (string, error)
	// prepareFn and spawnFn replace the download and process steps in tests.
	prepareFn func(context.Context, Config) (string, error)
	spawnFn   func(context.Context, string, string, string) (proc, error)

	applyMu sync.Mutex
	mu      sync.Mutex

	cfg        Config
	workerCfg  Config // identity of the model being prepared or retained, including while disabled
	generation uint64
	status     Status
	worker     proc
	cancel     context.CancelFunc
	stopTimer  *time.Timer
	stopEpoch  uint64
	restarts   int
	failed     bool // terminal inference/startup failure; only retry or model change clears it
	closed     bool
	wg         sync.WaitGroup
}

// New returns a manager that provisions artifacts under workDir.
func New(workDir string, log *logger.Logger) *Manager {
	return &Manager{
		workDir: workDir,
		log:     log,
		status:  Status{State: "disabled", Model: Variant, Device: "cpu", Backend: "cpu"},
	}
}

// Apply starts or stops the worker to match cfg. It never blocks on model loading.
func (m *Manager) Apply(cfg Config) {
	m.applyMu.Lock()
	defer m.applyMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.applyLocked(cfg)
}

// applyLocked requires applyMu and mu. Disabled settings must not overwrite workerCfg:
// a parked process still owns the model it actually loaded.
func (m *Manager) applyLocked(cfg Config) {
	if m.closed {
		return
	}
	m.cfg = cfg
	if cfg.Enabled {
		if m.stopTimer != nil {
			m.stopTimer.Stop()
			m.stopTimer = nil
		}
		if !sameWorkerConfig(m.workerCfg, cfg) {
			m.stopWorkerLocked()
			m.restarts = 0
			m.failed = false
		}
		if m.failed {
			m.status.State = "error"
			return
		}
		if m.worker != nil {
			m.status = Status{State: "ready", Model: Variant, Device: deviceOf(cfg), Backend: "cpu"}
			return
		}
		// Ordinary settings saves do not restart a download/load already in progress.
		if m.cancel == nil {
			m.startLocked(cfg)
		}
		return
	}
	m.status.State = "disabled"
	if m.worker == nil {
		// Invalidate both a pending startup and any queued automatic restart.
		m.stopWorkerLocked()
		return
	}
	if m.stopTimer != nil {
		return
	}
	generation := m.generation
	m.stopEpoch++
	stopEpoch := m.stopEpoch
	m.stopTimer = time.AfterFunc(stopGrace, func() { m.expireWorker(generation, stopEpoch) })
}

func (m *Manager) expireWorker(generation, stopEpoch uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.cfg.Enabled || m.generation != generation || m.stopTimer == nil || m.stopEpoch != stopEpoch {
		return
	}
	m.stopTimer = nil
	m.stopWorkerLocked()
}

// startLocked launches one attempt without changing the consecutive-failure budget.
func (m *Manager) startLocked(cfg Config) {
	m.generation++
	generation := m.generation
	m.workerCfg = cfg
	m.status = Status{State: "preparing", Model: Variant, Device: deviceOf(cfg), Backend: "cpu"}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.start(ctx, generation, cfg)
	}()
}

// Status returns the current lifecycle snapshot plus download progress.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

// Action implements the settings-page controls: retry recreates the worker, cancel stops it now.
func (m *Manager) Action(action string) (Status, error) {
	if action != "retry" && action != "cancel" {
		return m.Status(), fmt.Errorf("unknown action %q", action)
	}
	m.applyMu.Lock()
	defer m.applyMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return m.status, nil
	}
	if m.stopTimer != nil {
		m.stopTimer.Stop()
		m.stopTimer = nil
	}
	m.stopWorkerLocked()
	if action == "retry" {
		m.restarts = 0
		m.failed = false
		cfg := m.cfg
		cfg.Enabled = true
		m.applyLocked(cfg)
	}
	return m.status, nil
}

// Decide classifies a JSON array of role/content messages locally. Any failure returns
// ErrUnavailable so the caller falls back to the previous task type without a network call.
func (m *Manager) Decide(ctx context.Context, text string) (tasktype.TaskType, error) {
	m.mu.Lock()
	w, generation := m.worker, m.generation
	enabled := m.cfg.Enabled && !m.closed
	m.mu.Unlock()
	if w == nil || !enabled {
		return tasktype.Unknown, ErrUnavailable
	}
	label, err := w.decide(ctx, text)
	if err != nil {
		if !errors.Is(err, errInvalidRequest) {
			m.failWorker(w, generation, err)
		}
		return tasktype.Unknown, ErrUnavailable
	}
	taskType, err := tasktype.Parse(label)
	if err != nil || !taskType.Valid() {
		m.failWorker(w, generation, fmt.Errorf("invalid task type %q", label))
		return tasktype.Unknown, ErrUnavailable
	}
	m.mu.Lock()
	if m.worker == w && m.generation == generation && m.cfg.Enabled && !m.closed {
		m.restarts = 0
	}
	m.mu.Unlock()
	return taskType, nil
}

// Close stops the worker without the stop grace and waits for the start goroutine.
func (m *Manager) Close() {
	m.applyMu.Lock()
	defer m.applyMu.Unlock()
	m.mu.Lock()
	m.closed = true
	if m.stopTimer != nil {
		m.stopTimer.Stop()
		m.stopTimer = nil
	}
	m.stopWorkerLocked()
	m.mu.Unlock()
	m.wg.Wait()
}

func (m *Manager) stopWorkerLocked() {
	m.generation++
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	if m.worker != nil {
		_ = m.worker.close()
		m.worker = nil
	}
	m.status.State = "disabled"
}

func (m *Manager) failWorker(w proc, generation uint64, err error) {
	m.mu.Lock()
	if m.worker != w || m.generation != generation {
		m.mu.Unlock()
		return
	}
	m.stopWorkerLocked()
	m.restarts++
	restarts := m.restarts
	m.failed = restarts > 3
	m.status.State = "error"
	m.status.Error = err.Error()
	if !m.cfg.Enabled {
		m.status.State = "disabled"
	}
	retry := !m.failed && m.cfg.Enabled && !m.closed
	generation = m.generation
	m.mu.Unlock()
	if m.log != nil {
		m.log.Warn(logger.CatApp, "local task classifier worker stopped", "err", err.Error(), "restarts", restarts)
	}
	if retry {
		go m.restart(generation)
	}
}

// restart never re-applies a captured configuration: a late recovery must not undo a
// disable, cancel, model replacement, or another startup.
func (m *Manager) restart(generation uint64) {
	m.applyMu.Lock()
	defer m.applyMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || !m.cfg.Enabled || m.failed || m.generation != generation || m.worker != nil || m.cancel != nil {
		return
	}
	m.startLocked(m.cfg)
}

// start prepares artifacts and spawns the worker for one generation.
func (m *Manager) start(ctx context.Context, generation uint64, cfg Config) {
	dir, err := m.prepare(ctx, generation, cfg)
	if err != nil {
		m.fail(generation, err)
		return
	}
	if ctx.Err() != nil {
		return
	}
	m.setStatus(generation, func(s *Status) { s.State = "loading" })
	path, err := m.resolveWorker()
	if err != nil {
		m.fail(generation, err)
		return
	}
	var runtimePath string
	if m.spawnFn == nil {
		if runtimePath, err = m.runtimePath(ctx, generation); err != nil {
			m.fail(generation, err)
			return
		}
	}
	w, err := m.spawnWorker(ctx, path, dir, runtimePath)
	if err != nil {
		m.fail(generation, err)
		return
	}
	m.mu.Lock()
	if m.closed || m.generation != generation || !m.cfg.Enabled || ctx.Err() != nil {
		m.mu.Unlock()
		_ = w.close()
		return
	}
	m.worker = w
	m.status = Status{State: "ready", Model: Variant, Device: deviceOf(cfg), Backend: "cpu"}
	m.mu.Unlock()
	if m.log != nil {
		m.log.Info(logger.CatApp, "local task classifier ready", "model_dir", dir)
	}
}

func (m *Manager) prepare(ctx context.Context, generation uint64, cfg Config) (string, error) {
	if m.prepareFn != nil {
		return m.prepareFn(ctx, cfg)
	}
	return m.prepareModel(ctx, generation, cfg)
}

func (m *Manager) spawnWorker(ctx context.Context, path, modelDir, runtimePath string) (proc, error) {
	if m.spawnFn != nil {
		return m.spawnFn(ctx, path, modelDir, runtimePath)
	}
	return startWorker(ctx, path, modelDir, runtimePath, m.workerPath == nil)
}

func (m *Manager) prepareModel(ctx context.Context, generation uint64, cfg Config) (string, error) {
	dir := cfg.ModelDir
	if dir == "" {
		dir = filepath.Join(m.workDir, "models", "laya", Variant)
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for _, a := range artifacts {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		target := filepath.Join(dir, filepath.FromSlash(a.path))
		err := onnxruntime.Download(ctx, m.client, target, a.path, a.url, a.hash,
			func(state string, done, total int64) {
				m.setStatus(generation, func(s *Status) {
					s.State = state
					s.DownloadedBytes = done
					s.TotalBytes = total
				})
			})
		if err != nil {
			return "", err
		}
	}
	m.setStatus(generation, func(s *Status) { s.DownloadedBytes, s.TotalBytes = 0, 0 })
	return dir, nil
}

func (m *Manager) runtimePath(ctx context.Context, generation uint64) (string, error) {
	return onnxruntime.Ensure(ctx, onnxruntime.Config{
		WorkDir: m.workDir, Namespace: runtimeNamespace, Version: runtimeVersion,
	}, func(state string, done, total int64) {
		m.setStatus(generation, func(s *Status) {
			s.State = state
			s.DownloadedBytes = done
			s.TotalBytes = total
		})
	})
}

func (m *Manager) resolveWorker() (string, error) {
	if m.workerPath != nil {
		return m.workerPath()
	}
	return os.Executable()
}

func (m *Manager) setStatus(generation uint64, fn func(*Status)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.generation != generation || !m.cfg.Enabled || m.status.State == "error" {
		return
	}
	fn(&m.status)
}

func (m *Manager) fail(generation uint64, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.generation != generation {
		return
	}
	// A prepare aborted because the engine was switched off is not a failure.
	if !m.cfg.Enabled {
		m.worker = nil
		m.status.State = "disabled"
		return
	}
	m.failed = true
	m.status.State = "error"
	m.status.Error = err.Error()
	m.worker = nil
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	if m.log != nil {
		m.log.Warn(logger.CatApp, "local task classifier unavailable", "err", err.Error())
	}
}

func sameWorkerConfig(a, b Config) bool {
	return a.ModelDir == b.ModelDir && deviceOf(a) == deviceOf(b)
}

func deviceOf(cfg Config) string {
	if cfg.Device == "" {
		return "cpu"
	}
	return cfg.Device
}

// proc is the worker surface the manager needs, so tests can substitute it.
type proc interface {
	decide(context.Context, string) (string, error)
	close() error
}

// worker is an internal SoloQueue process speaking NDJSON on its stdio.
type worker struct {
	cmd     *exec.Cmd
	workDir string
	stdin   io.WriteCloser
	out     *bufio.Reader
	closed  atomic.Bool
	mu      sync.Mutex
	id      atomic.Uint64
}

type readyFrame struct {
	V     int    `json:"v"`
	ID    uint64 `json:"id"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type responseFrame struct {
	V    int    `json:"v"`
	ID   uint64 `json:"id"`
	Data *struct {
		Choice        string             `json:"choice"`
		Probabilities map[string]float64 `json:"probabilities"`
	} `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func startWorker(ctx context.Context, path, modelDir, runtimePath string, internal bool, testBuildID ...string) (*worker, error) {
	if internal {
		var err error
		modelDir, err = filepath.Abs(modelDir)
		if err != nil {
			return nil, err
		}
		runtimePath, err = filepath.Abs(runtimePath)
		if err != nil {
			return nil, err
		}
	}
	args := []string{"--model-dir", modelDir, "--runtime", runtimePath, "--device", "cpu", "--variant", Variant}
	if internal {
		buildID, err := buildidentity.Current()
		if len(testBuildID) > 0 {
			buildID, err = testBuildID[0], nil
		}
		if err != nil {
			return nil, fmt.Errorf("identify local classifier build: %w", err)
		}
		args = append(args, "--build-id", buildID)
		args = append([]string{"__internal_laya"}, args...)
	}
	cmd := exec.Command(path, args...)
	var workDir string
	if internal {
		var e error
		workDir, e = os.MkdirTemp("", "soloqueue-laya-*")
		if e != nil {
			return nil, e
		}
		cmd.Dir = workDir
	}
	started := false
	defer func() {
		if !started && workDir != "" {
			_ = os.RemoveAll(workDir)
		}
	}()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	w := &worker{cmd: cmd, stdin: stdin, out: bufio.NewReaderSize(stdout, 64<<10), workDir: workDir}
	started = true
	go func() {
		<-ctx.Done()
		_ = w.close()
	}()
	// The ready frame arrives only after the engine has loaded its checkpoint.
	line, err := w.out.ReadString('\n')
	if err != nil {
		_ = w.close()
		return nil, fmt.Errorf("local classifier did not become ready: %w", err)
	}
	var ready readyFrame
	if err := json.Unmarshal([]byte(line), &ready); err != nil {
		_ = w.close()
		return nil, fmt.Errorf("local classifier sent an invalid ready frame: %w", err)
	}
	if ready.V != 1 || ready.ID != 0 {
		_ = w.close()
		return nil, errors.New("local classifier sent an incompatible ready frame")
	}
	if ready.Error != nil {
		_ = w.close()
		return nil, fmt.Errorf("local classifier failed to start: %s", ready.Error.Message)
	}
	return w, nil
}

func (w *worker) decide(ctx context.Context, text string) (string, error) {
	return w.request(ctx, "decide", text)
}

func (w *worker) request(ctx context.Context, op, text string) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed.Load() {
		return "", errors.New("worker closed")
	}
	request, err := json.Marshal(map[string]any{"v": 1, "id": w.id.Add(1), "op": op, "text": text})
	if err != nil {
		return "", err
	}
	if len(request)+1 > maxRequestFrame {
		return "", errInvalidRequest
	}
	type outcome struct {
		label string
		err   error
	}
	results := make(chan outcome, 1)
	go func() {
		if _, err := w.stdin.Write(append(request, '\n')); err != nil {
			results <- outcome{err: err}
			return
		}
		line, err := w.out.ReadString('\n')
		if err != nil {
			results <- outcome{err: err}
			return
		}
		var frame responseFrame
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			results <- outcome{err: err}
			return
		}
		if frame.Error != nil {
			if frame.Error.Code == "bad_request" || frame.Error.Code == "too_large" {
				results <- outcome{err: fmt.Errorf("%w: %s", errInvalidRequest, frame.Error.Code)}
				return
			}
			results <- outcome{err: fmt.Errorf("%s: %s", frame.Error.Code, frame.Error.Message)}
			return
		}
		if frame.Data == nil || frame.Data.Choice == "" {
			results <- outcome{err: errors.New("worker returned no choice")}
			return
		}
		results <- outcome{label: frame.Data.Choice}
	}()
	timeout := decideTimeout
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining < timeout {
			timeout = remaining
		}
	}
	select {
	case result := <-results:
		return result.label, result.err
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(timeout):
		return "", errors.New("local classifier timed out")
	}
}

func (w *worker) close() error {
	if w.closed.Swap(true) {
		return nil
	}
	_ = w.stdin.Close()
	if w.cmd.Process != nil {
		_ = w.cmd.Process.Kill()
	}
	err := w.cmd.Wait()
	if w.workDir != "" {
		err = errors.Join(err, os.RemoveAll(w.workDir))
	}
	return err
}
