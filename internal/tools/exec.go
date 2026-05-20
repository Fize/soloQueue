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

// RunCommandOptions 命令执行选项。
type RunCommandOptions struct {
	// Timeout 执行超时。0 表示不限制。
	Timeout time.Duration
	// Stdin 可选的标准输入。
	Stdin string
	// MaxOutput stdout/stderr 各自的最大字节数。0 表示不限制。
	MaxOutput int64
	// WorkingDirectory optional working directory for command execution; empty = default
	WorkingDirectory string
}

// RunCommandResult 命令执行结果。
type RunCommandResult struct {
	ExitCode  int
	Stdout    []byte
	Stderr    []byte
	Truncated bool
}

// ─── ReadFile ───────────────────────────────────────────────────────────────

// ReadFileOptions 文件读取选项。
type ReadFileOptions struct {
	// MaxSize 文件大小上限。0 表示不限制。
	MaxSize int64
}

// ReadFileResult 文件读取结果。
type ReadFileResult struct {
	Data []byte
}

// ─── WriteFile ──────────────────────────────────────────────────────────────

// WriteFileOptions 文件写入选项。
type WriteFileOptions struct {
	// Overwrite 是否覆盖已存在的文件。false 时若文件已存在则返回错误。
	Overwrite bool
	// MaxSize 单次写入大小上限。0 表示不限制。
	MaxSize int64
}

// WriteFileResult 文件写入结果。
type WriteFileResult struct {
	// Created 表示目标路径之前不存在（新创建）。
	Created bool
}

// ─── Stat ───────────────────────────────────────────────────────────────────

// FileInfo 文件元信息。
type FileInfo struct {
	Size  int64
	IsDir bool
}

// ─── Glob ───────────────────────────────────────────────────────────────────

// GlobOptions glob 匹配选项。
type GlobOptions struct {
	// MaxItems 返回结果数量上限。0 表示不限制。
	MaxItems int
	// Timeout 单次 glob 执行超时。0 表示使用父 context 的 deadline。
	Timeout time.Duration
}

// ─── Grep ───────────────────────────────────────────────────────────────────

// GrepOptions 搜索选项。
type GrepOptions struct {
	// MaxMatches 匹配数量上限。0 表示不限制。
	MaxMatches int
	// MaxLineLen 单行截断长度。0 表示不截断。
	MaxLineLen int
	// GlobPattern 可选的文件名过滤模式（如 "*.go"）。
	GlobPattern string
}

// GrepMatch 单行匹配结果。
type GrepMatch struct {
	File    string
	Line    int
	Content string
}

// ─── HTTP ───────────────────────────────────────────────────────────────────

// HTTPOptions HTTP 请求选项。
type HTTPOptions struct {
	// Timeout 请求超时。0 表示不限制。
	Timeout time.Duration
	// MaxBody 响应体大小上限。0 表示不限制。
	MaxBody int64
	// Headers 自定义请求头。
	Headers map[string]string
	// ContentType POST 请求的 Content-Type。
	ContentType string
	// BlockPrivate 是否拦截私有/环回地址。
	BlockPrivate bool
}

// HTTPResponse HTTP 响应结果。
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

// NewSandbox 创建本地执行器。
func NewSandbox() *Sandbox {
	return &Sandbox{}
}

// SetLogger 设置 logger，nil 表示不记录日志。
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

// limitedWriterExec 截断写入器
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

// isBinaryExec 检查数据是否包含 NUL 字节（二进制检测）。
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

