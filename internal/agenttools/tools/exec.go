package tools

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

// RuntimeType represents execution runtime mode.
type RuntimeType = string

const (
	RuntimeHost    RuntimeType = "host"
	RuntimeSandbox RuntimeType = "sandbox"
)

// ToolRuntime is an alias to Executor for compatibility.
type ToolRuntime = Executor

// NewHostRuntime creates a new Executor instance.
func NewHostRuntime() *Executor {
	return NewExecutor()
}

// RunCommandOptions contains command execution options.
type RunCommandOptions struct {
	Timeout          time.Duration
	Stdin            string
	MaxOutput        int64
	WorkingDirectory string
}

// RunCommandResult contains the result of a command execution.
type RunCommandResult struct {
	ExitCode  int
	Stdout    []byte
	Stderr    []byte
	Truncated bool
}

// ProcessSpec describes a long-lived process without shell interpolation.
type ProcessSpec struct {
	Command          string
	Args             []string
	Env              map[string]string
	WorkingDirectory string
}

// Process is a cancellable long-lived process.
type Process interface {
	Stdin() io.WriteCloser
	Stdout() io.Reader
	Stderr() io.Reader
	Wait() error
	Kill() error
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

// ReadFileOptions configures file reading limits.
type ReadFileOptions struct {
	MaxSize int64
}

// ReadFileResult holds raw file bytes.
type ReadFileResult struct {
	Data []byte
}

// WriteFileOptions configures file writing permissions and limits.
type WriteFileOptions struct {
	Overwrite bool
	MaxSize   int64
}

// WriteFileResult holds the file write operation metadata.
type WriteFileResult struct {
	Created bool
}

// FileInfo contains file metadata.
type FileInfo struct {
	Size  int64
	IsDir bool
}

// GlobOptions contains glob matching options.
type GlobOptions struct {
	MaxItems int
	Timeout  time.Duration
}

// GrepOptions contains text search options.
type GrepOptions struct {
	MaxMatches     int
	MaxLineLen     int
	GlobPattern    string
	IncludeIgnored bool
}

func isDefaultIgnoredDir(name string) bool {
	switch strings.ToLower(name) {
	case ".git", "node_modules", "dist", "build", "vendor", ".soloqueue", ".next", ".cache", "coverage", "__pycache__":
		return true
	default:
		return false
	}
}

// GrepMatch represents a single-line match result.
type GrepMatch struct {
	File    string
	Line    int
	Content string
}

// HTTPOptions contains HTTP request options.
type HTTPOptions struct {
	Timeout      time.Duration
	MaxBody      int64
	Headers      map[string]string
	ContentType  string
	BlockPrivate bool
}

// HTTPResponse contains the HTTP response result.
type HTTPResponse struct {
	StatusCode int
	Body       []byte
}

// ─── Executor ───────────────────────────────────────────────────────────────

// Executor is the direct execution engine for all tool operations.
// It provides local filesystem and network access for shell commands,
// process management, file I/O, globbing, grep, and HTTP requests.
type Executor struct {
	log        *logger.Logger
	HTTPPostFn func(ctx context.Context, rawURL string, body string, opts HTTPOptions) (HTTPResponse, error)
}

// NewExecutor creates the local executor.
func NewExecutor() *Executor {
	return &Executor{}
}

// NewSandbox is a compatibility constructor alias for NewExecutor.
func NewSandbox() *Executor {
	return NewExecutor()
}

// Sandbox is a type alias for Executor to maintain backwards compatibility.
type Sandbox = Executor

// SetLogger sets the logger; nil disables logging.
func (e *Executor) SetLogger(l *logger.Logger) {
	e.log = l
}

// StartProcess starts a long-lived process.
func (e *Executor) StartProcess(ctx context.Context, spec ProcessSpec) (Process, error) {
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

// ExportFile returns the path if valid, or an error if it's a directory or missing.
func (e *Executor) ExportFile(ctx context.Context, path string) (string, error) {
	info, err := e.Stat(ctx, path)
	if err != nil {
		return "", err
	}
	if info.IsDir {
		return "", fmt.Errorf("path is a directory, not a file: %s", path)
	}
	return path, nil
}

// ─── ReadFile ───────────────────────────────────────────────────────────────

func (e *Executor) ReadFile(ctx context.Context, path string, opts ReadFileOptions) (ReadFileResult, error) {
	fi, err := os.Stat(path)
	if err != nil {
		if e.log != nil {
			e.log.LogError(ctx, logger.CatTool, "exec: read file failed", err, "path", path)
		}
		return ReadFileResult{}, err
	}
	if opts.MaxSize > 0 && fi.Size() > opts.MaxSize {
		return ReadFileResult{}, fmt.Errorf("file too large: %s (%d bytes > %d). Use Bash with head/tail to read file portions", path, fi.Size(), opts.MaxSize)
	}

	type readResult struct {
		data []byte
		err  error
	}
	resultCh := make(chan readResult, 1)
	go func() {
		f, ferr := os.Open(path)
		if ferr != nil {
			resultCh <- readResult{nil, ferr}
			return
		}
		defer f.Close()
		data, rerr := io.ReadAll(f)
		resultCh <- readResult{data, rerr}
	}()

	select {
	case res := <-resultCh:
		if res.err != nil {
			if e.log != nil {
				e.log.LogError(ctx, logger.CatTool, "exec: read file failed", res.err, "path", path)
			}
			return ReadFileResult{}, res.err
		}
		return ReadFileResult{Data: res.data}, nil
	case <-ctx.Done():
		return ReadFileResult{}, ctx.Err()
	}
}

// ─── WriteFile ──────────────────────────────────────────────────────────────

func (e *Executor) WriteFile(ctx context.Context, path string, data []byte, opts WriteFileOptions) (WriteFileResult, error) {
	if opts.MaxSize > 0 && int64(len(data)) > opts.MaxSize {
		return WriteFileResult{}, fmt.Errorf("write too large: %d bytes > %d", len(data), opts.MaxSize)
	}

	dir := filepath.Dir(path)
	if fi, statErr := os.Stat(dir); statErr != nil || !fi.IsDir() {
		return WriteFileResult{}, fmt.Errorf("parent dir missing: %s", dir)
	}

	if err := ctx.Err(); err != nil {
		return WriteFileResult{}, err
	}

	_, statErr := os.Stat(path)
	existed := statErr == nil
	created := !existed

	if existed && !opts.Overwrite {
		return WriteFileResult{}, fmt.Errorf("file already exists: %s", path)
	}

	tmp, err := os.CreateTemp(dir, ".soloqueue-tmp-*")
	if err != nil {
		if e.log != nil {
			e.log.LogError(ctx, logger.CatTool, "exec: write file failed", err, "path", path)
		}
		return WriteFileResult{}, fmt.Errorf("create tmp: %w", err)
	}
	tmpName := tmp.Name()

	defer func() {
		if err != nil {
			_ = os.Remove(tmpName)
		}
	}()

	if err = ctx.Err(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return WriteFileResult{}, err
	}

	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		if e.log != nil {
			e.log.LogError(ctx, logger.CatTool, "exec: write file failed", err, "path", path)
		}
		return WriteFileResult{}, fmt.Errorf("write tmp: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		if e.log != nil {
			e.log.LogError(ctx, logger.CatTool, "exec: write file failed", err, "path", path)
		}
		return WriteFileResult{}, fmt.Errorf("sync tmp: %w", err)
	}
	if err = tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		if e.log != nil {
			e.log.LogError(ctx, logger.CatTool, "exec: write file failed", err, "path", path)
		}
		return WriteFileResult{}, fmt.Errorf("close tmp: %w", err)
	}

	if err = ctx.Err(); err != nil {
		_ = os.Remove(tmpName)
		return WriteFileResult{}, err
	}

	if err = os.Rename(tmpName, path); err != nil {
		if e.log != nil {
			e.log.LogError(ctx, logger.CatTool, "exec: write file failed", err, "path", path)
		}
		return WriteFileResult{}, fmt.Errorf("rename tmp -> target: %w", err)
	}

	return WriteFileResult{Created: created}, nil
}

// ─── Stat ───────────────────────────────────────────────────────────────────

func (e *Executor) Stat(ctx context.Context, path string) (FileInfo, error) {
	fi, err := os.Stat(path)
	if err != nil {
		if e.log != nil {
			e.log.LogError(ctx, logger.CatTool, "exec: stat failed", err, "path", path)
		}
		return FileInfo{}, err
	}
	return FileInfo{Size: fi.Size(), IsDir: fi.IsDir()}, nil
}

// MkdirAll creates a directory tree on the host.
func (e *Executor) MkdirAll(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.MkdirAll(path, 0o755)
}

// ─── Glob ───────────────────────────────────────────────────────────────────

func (e *Executor) Glob(ctx context.Context, dir string, pattern string, opts GlobOptions) ([]string, error) {
	maxItems := opts.MaxItems
	if maxItems <= 0 {
		maxItems = 10000
	}

	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	fsys := os.DirFS(dir)

	type globResult struct {
		matches []string
		err     error
	}
	resultCh := make(chan globResult, 1)
	go func() {
		matches, err := doublestar.Glob(fsys, pattern)
		resultCh <- globResult{matches, err}
	}()

	var res globResult
	select {
	case res = <-resultCh:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	if res.err != nil {
		if e.log != nil {
			e.log.LogError(ctx, logger.CatTool, "exec: glob failed", res.err, "dir", dir, "pattern", pattern)
		}
		return nil, res.err
	}

	matches := res.matches
	if len(matches) > maxItems {
		matches = matches[:maxItems]
	}

	result := make([]string, len(matches))
	for i, m := range matches {
		result[i] = filepath.Join(dir, m)
	}

	return result, nil
}

// ─── Grep ───────────────────────────────────────────────────────────────────

func (e *Executor) Grep(ctx context.Context, dir string, pattern string, opts GrepOptions) ([]GrepMatch, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		if e.log != nil {
			e.log.LogError(ctx, logger.CatTool, "exec: grep failed", err, "dir", dir, "pattern", pattern)
		}
		return nil, fmt.Errorf("invalid pattern: %w", err)
	}

	maxMatches := opts.MaxMatches
	if maxMatches <= 0 {
		maxMatches = 1000
	}
	maxLineLen := opts.MaxLineLen
	var (
		result    []GrepMatch
		walkCount int
	)

	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		walkCount++
		if walkCount%256 == 0 {
			if cerr := ctx.Err(); cerr != nil {
				return cerr
			}
		}

		if d.IsDir() {
			if path != dir && !opts.IncludeIgnored && isDefaultIgnoredDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if len(result) >= maxMatches {
			return fs.SkipAll
		}
		if opts.GlobPattern != "" {
			rel, rerr := filepath.Rel(dir, path)
			if rerr != nil {
				return nil
			}
			ok, _ := doublestar.PathMatch(opts.GlobPattern, filepath.ToSlash(rel))
			if !ok {
				return nil
			}
		}

		f, ferr := os.Open(path)
		if ferr != nil {
			return nil
		}
		defer f.Close()

		head := make([]byte, 512)
		n, _ := f.Read(head)
		if looksBinary(head[:n]) {
			return nil
		}
		if _, serr := f.Seek(0, 0); serr != nil {
			return nil
		}

		scanner := bufio.NewScanner(f)
		buf := make([]byte, 0, 64*1024)
		cap_ := maxLineLen * 4
		if cap_ < 1<<20 {
			cap_ = 1 << 20
		}
		scanner.Buffer(buf, cap_)

		lineNo := 0
		for scanner.Scan() {
			lineNo++
			text := scanner.Text()
			if !re.MatchString(text) {
				continue
			}
			if maxLineLen > 0 && len(text) > maxLineLen {
				text = text[:maxLineLen] + "…"
			}
			result = append(result, GrepMatch{
				File:    path,
				Line:    lineNo,
				Content: text,
			})
			if len(result) >= maxMatches {
				return fs.SkipAll
			}
		}
		return nil
	})

	if walkErr != nil && walkErr != fs.SkipAll {
		if e.log != nil {
			e.log.LogError(ctx, logger.CatTool, "exec: grep walk failed", walkErr, "dir", dir, "pattern", pattern)
		}
		return nil, walkErr
	}
	return result, nil
}

// ─── HTTPGet ────────────────────────────────────────────────────────────────

func (e *Executor) HTTPGet(ctx context.Context, rawURL string, opts HTTPOptions) (HTTPResponse, error) {
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	maxBody := opts.MaxBody
	if maxBody <= 0 {
		maxBody = 5 << 20
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		if e.log != nil {
			e.log.LogError(ctx, logger.CatTool, "exec: http get failed", err, "url", rawURL)
		}
		return HTTPResponse{}, err
	}

	for k, v := range opts.Headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if e.log != nil {
			e.log.LogError(ctx, logger.CatTool, "exec: http get failed", err, "url", rawURL)
		}
		return HTTPResponse{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		if e.log != nil {
			e.log.LogError(ctx, logger.CatTool, "exec: http get read body failed", err, "url", rawURL)
		}
		return HTTPResponse{}, err
	}

	return HTTPResponse{
		StatusCode: resp.StatusCode,
		Body:       body,
	}, nil
}

// ─── HTTPPost ───────────────────────────────────────────────────────────────

func (e *Executor) HTTPPost(ctx context.Context, rawURL string, body string, opts HTTPOptions) (HTTPResponse, error) {
	if e != nil && e.HTTPPostFn != nil {
		return e.HTTPPostFn(ctx, rawURL, body, opts)
	}

	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	maxBody := opts.MaxBody
	if maxBody <= 0 {
		maxBody = 5 << 20
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, strings.NewReader(body))
	if err != nil {
		if e.log != nil {
			e.log.LogError(ctx, logger.CatTool, "exec: http post failed", err, "url", rawURL)
		}
		return HTTPResponse{}, err
	}

	if opts.ContentType != "" {
		req.Header.Set("Content-Type", opts.ContentType)
	}
	for k, v := range opts.Headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if e.log != nil {
			e.log.LogError(ctx, logger.CatTool, "exec: http post failed", err, "url", rawURL)
		}
		return HTTPResponse{}, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		if e.log != nil {
			e.log.LogError(ctx, logger.CatTool, "exec: http post read body failed", err, "url", rawURL)
		}
		return HTTPResponse{}, err
	}

	return HTTPResponse{
		StatusCode: resp.StatusCode,
		Body:       respBody,
	}, nil
}

// ─── helpers ────────────────────────────────────────────────────────────────

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

// limitedWriterExec is a truncating writer.
type limitedWriterExec struct {
	w         io.Writer
	cap       int64
	written   int64
	truncated bool
}

func (lw *limitedWriterExec) Write(p []byte) (int, error) {
	if lw.written >= lw.cap {
		lw.truncated = true
		return len(p), nil
	}
	remain := lw.cap - lw.written
	if int64(len(p)) > remain {
		n, err := lw.w.Write(p[:remain])
		lw.written += int64(n)
		lw.truncated = true
		return len(p), err
	}
	n, err := lw.w.Write(p)
	lw.written += int64(n)
	return n, err
}
