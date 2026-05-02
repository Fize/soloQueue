package sandbox

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// LocalExecutor 直接在宿主机上执行操作的执行器实现。
// 适用于无 Docker 环境或调试场景。
type LocalExecutor struct {
	log *logger.Logger
}

// NewLocalExecutor 创建本地执行器。
func NewLocalExecutor() *LocalExecutor {
	return &LocalExecutor{}
}

// SetLogger 设置 logger，nil 表示不记录日志。
func (e *LocalExecutor) SetLogger(l *logger.Logger) {
	e.log = l
}

// ─── RunCommand ─────────────────────────────────────────────────────────────

func (e *LocalExecutor) RunCommand(ctx context.Context, cmd string, opts RunCommandOptions) (RunCommandResult, error) {
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	maxOut := opts.MaxOutput
	if maxOut <= 0 {
		maxOut = 256 << 10
	}

	c := exec.CommandContext(ctx, "/bin/sh", "-c", cmd)
	if opts.Stdin != "" {
		c.Stdin = strings.NewReader(opts.Stdin)
	}

	var stdout, stderr bytes.Buffer
	c.Stdout = &limitedWriterLocal{w: &stdout, cap: maxOut}
	c.Stderr = &limitedWriterLocal{w: &stderr, cap: maxOut}

	err := c.Run()

	res := RunCommandResult{
		Stdout: stdout.Bytes(),
		Stderr: stderr.Bytes(),
	}

	if sw, ok := c.Stdout.(*limitedWriterLocal); ok && sw.truncated {
		res.Truncated = true
	}
	if sw, ok := c.Stderr.(*limitedWriterLocal); ok && sw.truncated {
		res.Truncated = true
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return res, fmt.Errorf("command timeout")
		}
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			res.ExitCode = ee.ExitCode()
			return res, nil
		}
		if e.log != nil {
			e.log.LogError(ctx, logger.CatTool, "sandbox: run command failed", err, "command", cmd)
		}
		return res, err
	}

	res.ExitCode = c.ProcessState.ExitCode()
	return res, nil
}

// ─── ReadFile ───────────────────────────────────────────────────────────────

func (e *LocalExecutor) ReadFile(ctx context.Context, path string, opts ReadFileOptions) (ReadFileResult, error) {
	fi, err := os.Stat(path)
	if err != nil {
		if e.log != nil {
			e.log.LogError(ctx, logger.CatTool, "sandbox: read file failed", err, "path", path)
		}
		return ReadFileResult{}, err
	}
	if opts.MaxSize > 0 && fi.Size() > opts.MaxSize {
		return ReadFileResult{}, fmt.Errorf("file too large: %s (%d bytes > %d)", path, fi.Size(), opts.MaxSize)
	}

	f, err := os.Open(path)
	if err != nil {
		if e.log != nil {
			e.log.LogError(ctx, logger.CatTool, "sandbox: read file failed", err, "path", path)
		}
		return ReadFileResult{}, err
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		if e.log != nil {
			e.log.LogError(ctx, logger.CatTool, "sandbox: read file failed", err, "path", path)
		}
		return ReadFileResult{}, err
	}

	return ReadFileResult{Data: data}, nil
}

// ─── WriteFile ──────────────────────────────────────────────────────────────

func (e *LocalExecutor) WriteFile(ctx context.Context, path string, data []byte, opts WriteFileOptions) (WriteFileResult, error) {
	if opts.MaxSize > 0 && int64(len(data)) > opts.MaxSize {
		return WriteFileResult{}, fmt.Errorf("write too large: %d bytes > %d", len(data), opts.MaxSize)
	}

	dir := filepath.Dir(path)
	if fi, statErr := os.Stat(dir); statErr != nil || !fi.IsDir() {
		return WriteFileResult{}, fmt.Errorf("parent dir missing: %s", dir)
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
			e.log.LogError(ctx, logger.CatTool, "sandbox: write file failed", err, "path", path)
		}
		return WriteFileResult{}, fmt.Errorf("create tmp: %w", err)
	}
	tmpName := tmp.Name()

	defer func() {
		if err != nil {
			_ = os.Remove(tmpName)
		}
	}()

	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		if e.log != nil {
			e.log.LogError(ctx, logger.CatTool, "sandbox: write file failed", err, "path", path)
		}
		return WriteFileResult{}, fmt.Errorf("write tmp: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		if e.log != nil {
			e.log.LogError(ctx, logger.CatTool, "sandbox: write file failed", err, "path", path)
		}
		return WriteFileResult{}, fmt.Errorf("sync tmp: %w", err)
	}
	if err = tmp.Close(); err != nil {
		if e.log != nil {
			e.log.LogError(ctx, logger.CatTool, "sandbox: write file failed", err, "path", path)
		}
		return WriteFileResult{}, fmt.Errorf("close tmp: %w", err)
	}
	if err = os.Rename(tmpName, path); err != nil {
		if e.log != nil {
			e.log.LogError(ctx, logger.CatTool, "sandbox: write file failed", err, "path", path)
		}
		return WriteFileResult{}, fmt.Errorf("rename tmp → target: %w", err)
	}

	return WriteFileResult{Created: created}, nil
}

// ─── Stat ───────────────────────────────────────────────────────────────────

func (e *LocalExecutor) Stat(ctx context.Context, path string) (FileInfo, error) {
	fi, err := os.Stat(path)
	if err != nil {
		if e.log != nil {
			e.log.LogError(ctx, logger.CatTool, "sandbox: stat failed", err, "path", path)
		}
		return FileInfo{}, err
	}
	return FileInfo{Size: fi.Size(), IsDir: fi.IsDir()}, nil
}

// ─── Glob ───────────────────────────────────────────────────────────────────

func (e *LocalExecutor) Glob(ctx context.Context, dir string, pattern string, opts GlobOptions) ([]string, error) {
	maxItems := opts.MaxItems
	if maxItems <= 0 {
		maxItems = 10000
	}

	fsys := os.DirFS(dir)
	matches, err := doublestar.Glob(fsys, pattern)
	if err != nil {
		if e.log != nil {
			e.log.LogError(ctx, logger.CatTool, "sandbox: glob failed", err, "dir", dir, "pattern", pattern)
		}
		return nil, err
	}

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

func (e *LocalExecutor) Grep(ctx context.Context, dir string, pattern string, opts GrepOptions) ([]GrepMatch, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		if e.log != nil {
			e.log.LogError(ctx, logger.CatTool, "sandbox: grep failed", err, "dir", dir, "pattern", pattern)
		}
		return nil, fmt.Errorf("invalid pattern: %w", err)
	}

	maxMatches := opts.MaxMatches
	if maxMatches <= 0 {
		maxMatches = 1000
	}
	maxLineLen := opts.MaxLineLen

	var result []GrepMatch

	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		if len(result) >= maxMatches {
			return fs.SkipAll
		}
		// glob filter
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

		// binary detection
		head := make([]byte, 512)
		n, _ := f.Read(head)
		if isBinaryLocal(head[:n]) {
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
			e.log.LogError(ctx, logger.CatTool, "sandbox: grep walk failed", walkErr, "dir", dir, "pattern", pattern)
		}
		return nil, walkErr
	}
	return result, nil
}

// ─── HTTPGet ────────────────────────────────────────────────────────────────

func (e *LocalExecutor) HTTPGet(ctx context.Context, rawURL string, opts HTTPOptions) (HTTPResponse, error) {
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
			e.log.LogError(ctx, logger.CatTool, "sandbox: http get failed", err, "url", rawURL)
		}
		return HTTPResponse{}, err
	}

	for k, v := range opts.Headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if e.log != nil {
			e.log.LogError(ctx, logger.CatTool, "sandbox: http get failed", err, "url", rawURL)
		}
		return HTTPResponse{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		if e.log != nil {
			e.log.LogError(ctx, logger.CatTool, "sandbox: http get read body failed", err, "url", rawURL)
		}
		return HTTPResponse{}, err
	}

	return HTTPResponse{
		StatusCode: resp.StatusCode,
		Body:       body,
	}, nil
}

// ─── HTTPPost ───────────────────────────────────────────────────────────────

func (e *LocalExecutor) HTTPPost(ctx context.Context, rawURL string, body string, opts HTTPOptions) (HTTPResponse, error) {
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
			e.log.LogError(ctx, logger.CatTool, "sandbox: http post failed", err, "url", rawURL)
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
			e.log.LogError(ctx, logger.CatTool, "sandbox: http post failed", err, "url", rawURL)
		}
		return HTTPResponse{}, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		if e.log != nil {
			e.log.LogError(ctx, logger.CatTool, "sandbox: http post read body failed", err, "url", rawURL)
		}
		return HTTPResponse{}, err
	}

	return HTTPResponse{
		StatusCode: resp.StatusCode,
		Body:       respBody,
	}, nil
}

// ─── helpers ────────────────────────────────────────────────────────────────

// limitedWriterLocal 截断写入器
type limitedWriterLocal struct {
	w         io.Writer
	cap       int64
	written   int64
	truncated bool
}

func (lw *limitedWriterLocal) Write(p []byte) (int, error) {
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

// isBinaryLocal checks if data contains NUL bytes (binary detection).
func isBinaryLocal(data []byte) bool {
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

// Compile-time check
var _ Executor = (*LocalExecutor)(nil)
