package tools

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// ─── RunCommand ─────────────────────────────────────────────────────────────

// RunCommandOptions contains command execution options.
type RunCommandOptions struct {
	// Timeout is the execution timeout. 0 means no limit.
	Timeout time.Duration
	// Stdin is optional standard input.
	Stdin string
	// MaxOutput is the maximum output size for stdout/stderr. 0 means no limit.
	MaxOutput int64
	// WorkingDirectory optional working directory for command execution; empty = default
	WorkingDirectory string
}

// RunCommandResult contains the result of a command execution.
type RunCommandResult struct {
	ExitCode  int
	Stdout    []byte
	Stderr    []byte
	Truncated bool
}

// ─── ReadFile ───────────────────────────────────────────────────────────────

// ReadFileOptions contains file read options.
type ReadFileOptions struct {
	// MaxSize is the maximum file size. 0 means no limit.
	MaxSize int64
}

// ReadFileResult contains the file read result.
type ReadFileResult struct {
	Data []byte
}

// ─── WriteFile ──────────────────────────────────────────────────────────────

// WriteFileOptions contains file write options.
type WriteFileOptions struct {
	// Overwrite controls whether an existing file is overwritten. If false and the file exists, an error is returned.
	Overwrite bool
	// MaxSize is the maximum single write size. 0 means no limit.
	MaxSize int64
}

// WriteFileResult contains the result of a file write.
type WriteFileResult struct {
	// Created indicates that the target path did not previously exist (it was created).
	Created bool
}

// ─── Stat ───────────────────────────────────────────────────────────────────

// FileInfo contains file metadata.
type FileInfo struct {
	Size  int64
	IsDir bool
}

// ─── Glob ───────────────────────────────────────────────────────────────────

// GlobOptions contains glob matching options.
type GlobOptions struct {
	// MaxItems is the maximum number of results to return. 0 means no limit.
	MaxItems int
	// Timeout is the timeout for a single glob operation. 0 means use the parent context deadline.
	Timeout time.Duration
}

// ─── Grep ───────────────────────────────────────────────────────────────────

// GrepOptions contains search options.
type GrepOptions struct {
	// MaxMatches is the maximum number of matches to return. 0 means no limit.
	MaxMatches int
	// MaxLineLen is the maximum line length before truncation. 0 means no truncation.
	MaxLineLen int
	// GlobPattern is an optional filename filter pattern such as "*.go".
	GlobPattern string
}

// GrepMatch represents a single-line match result.
type GrepMatch struct {
	File    string
	Line    int
	Content string
}

// ─── HTTP ───────────────────────────────────────────────────────────────────

// HTTPOptions contains HTTP request options.
type HTTPOptions struct {
	// Timeout is the request timeout. 0 means no limit.
	Timeout time.Duration
	// MaxBody is the maximum response body size. 0 means no limit.
	MaxBody int64
	// Headers contains custom request headers.
	Headers map[string]string
	// ContentType is the Content-Type for POST requests.
	ContentType string
	// BlockPrivate controls whether private or loopback addresses are blocked.
	BlockPrivate bool
}

// HTTPResponse contains the HTTP response result.
type HTTPResponse struct {
	StatusCode int
	Body       []byte
}

// ─── Sandbox ────────────────────────────────────────────────────────────────

// Sandbox is the direct execution backend for all tool operations.
// It provides local filesystem and network access for shell commands,
// file I/O, globbing, grep, and HTTP requests.
type Sandbox struct {
	log *logger.Logger
}

// NewSandbox creates the local executor.
func NewSandbox() *Sandbox {
	return &Sandbox{}
}

// SetLogger sets the logger; nil disables logging.
func (s *Sandbox) SetLogger(l *logger.Logger) {
	s.log = l
}

// ─── ReadFile ───────────────────────────────────────────────────────────────

func (s *Sandbox) ReadFile(ctx context.Context, path string, opts ReadFileOptions) (ReadFileResult, error) {
	fi, err := os.Stat(path)
	if err != nil {
		if s.log != nil {
			s.log.LogError(ctx, logger.CatTool, "exec: read file failed", err, "path", path)
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
			if s.log != nil {
				s.log.LogError(ctx, logger.CatTool, "exec: read file failed", res.err, "path", path)
			}
			return ReadFileResult{}, res.err
		}
		return ReadFileResult{Data: res.data}, nil
	case <-ctx.Done():
		return ReadFileResult{}, ctx.Err()
	}
}

// ─── WriteFile ──────────────────────────────────────────────────────────────

func (s *Sandbox) WriteFile(ctx context.Context, path string, data []byte, opts WriteFileOptions) (WriteFileResult, error) {
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
		if s.log != nil {
			s.log.LogError(ctx, logger.CatTool, "exec: write file failed", err, "path", path)
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
		if s.log != nil {
			s.log.LogError(ctx, logger.CatTool, "exec: write file failed", err, "path", path)
		}
		return WriteFileResult{}, fmt.Errorf("write tmp: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		if s.log != nil {
			s.log.LogError(ctx, logger.CatTool, "exec: write file failed", err, "path", path)
		}
		return WriteFileResult{}, fmt.Errorf("sync tmp: %w", err)
	}
	if err = tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		if s.log != nil {
			s.log.LogError(ctx, logger.CatTool, "exec: write file failed", err, "path", path)
		}
		return WriteFileResult{}, fmt.Errorf("close tmp: %w", err)
	}

	if err = ctx.Err(); err != nil {
		_ = os.Remove(tmpName)
		return WriteFileResult{}, err
	}

	if err = os.Rename(tmpName, path); err != nil {
		if s.log != nil {
			s.log.LogError(ctx, logger.CatTool, "exec: write file failed", err, "path", path)
		}
		return WriteFileResult{}, fmt.Errorf("rename tmp -> target: %w", err)
	}

	return WriteFileResult{Created: created}, nil
}

// ─── Stat ───────────────────────────────────────────────────────────────────

func (s *Sandbox) Stat(ctx context.Context, path string) (FileInfo, error) {
	fi, err := os.Stat(path)
	if err != nil {
		if s.log != nil {
			s.log.LogError(ctx, logger.CatTool, "exec: stat failed", err, "path", path)
		}
		return FileInfo{}, err
	}
	return FileInfo{Size: fi.Size(), IsDir: fi.IsDir()}, nil
}

// ─── Glob ───────────────────────────────────────────────────────────────────

func (s *Sandbox) Glob(ctx context.Context, dir string, pattern string, opts GlobOptions) ([]string, error) {
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
		if s.log != nil {
			s.log.LogError(ctx, logger.CatTool, "exec: glob failed", res.err, "dir", dir, "pattern", pattern)
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

func (s *Sandbox) Grep(ctx context.Context, dir string, pattern string, opts GrepOptions) ([]GrepMatch, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		if s.log != nil {
			s.log.LogError(ctx, logger.CatTool, "exec: grep failed", err, "dir", dir, "pattern", pattern)
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

		if d.IsDir() || !d.Type().IsRegular() {
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
		if isBinaryExec(head[:n]) {
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
		if s.log != nil {
			s.log.LogError(ctx, logger.CatTool, "exec: grep walk failed", walkErr, "dir", dir, "pattern", pattern)
		}
		return nil, walkErr
	}
	return result, nil
}

// ─── HTTPGet ────────────────────────────────────────────────────────────────

func (s *Sandbox) HTTPGet(ctx context.Context, rawURL string, opts HTTPOptions) (HTTPResponse, error) {
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
		if s.log != nil {
			s.log.LogError(ctx, logger.CatTool, "exec: http get failed", err, "url", rawURL)
		}
		return HTTPResponse{}, err
	}

	for k, v := range opts.Headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if s.log != nil {
			s.log.LogError(ctx, logger.CatTool, "exec: http get failed", err, "url", rawURL)
		}
		return HTTPResponse{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		if s.log != nil {
			s.log.LogError(ctx, logger.CatTool, "exec: http get read body failed", err, "url", rawURL)
		}
		return HTTPResponse{}, err
	}

	return HTTPResponse{
		StatusCode: resp.StatusCode,
		Body:       body,
	}, nil
}

// ─── HTTPPost ───────────────────────────────────────────────────────────────

func (s *Sandbox) HTTPPost(ctx context.Context, rawURL string, body string, opts HTTPOptions) (HTTPResponse, error) {
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
		if s.log != nil {
			s.log.LogError(ctx, logger.CatTool, "exec: http post failed", err, "url", rawURL)
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
		if s.log != nil {
			s.log.LogError(ctx, logger.CatTool, "exec: http post failed", err, "url", rawURL)
		}
		return HTTPResponse{}, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		if s.log != nil {
			s.log.LogError(ctx, logger.CatTool, "exec: http post read body failed", err, "url", rawURL)
		}
		return HTTPResponse{}, err
	}

	return HTTPResponse{
		StatusCode: resp.StatusCode,
		Body:       respBody,
	}, nil
}

// ─── helpers ────────────────────────────────────────────────────────────────

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

// isBinaryExec checks whether the data contains a NUL byte (a binary detection heuristic).
func isBinaryExec(data []byte) bool {
	n := len(data)
	if n > 512 {
		n = 512
	}
	for i := 0; i < n; i++ {
		if data[i] == 0 {
			return true
		}
	}
	return false
}
