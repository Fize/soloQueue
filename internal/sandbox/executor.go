package sandbox

import (
	"context"
	"time"
)

// ─── Executor 接口 ──────────────────────────────────────────────────────────

// Executor 是沙盒执行器，所有工具通过它与外部世界交互。
// 实现可以是 Docker 容器、本地进程、k8s Pod 等。
type Executor interface {
	// RunCommand 在沙盒中执行 shell 命令。
	RunCommand(ctx context.Context, cmd string, opts RunCommandOptions) (RunCommandResult, error)

	// ReadFile 在沙盒中读取文件。
	ReadFile(ctx context.Context, path string, opts ReadFileOptions) (ReadFileResult, error)

	// WriteFile 在沙盒中写入文件（原子写入）。
	WriteFile(ctx context.Context, path string, data []byte, opts WriteFileOptions) (WriteFileResult, error)

	// Stat 在沙盒中获取文件信息。
	Stat(ctx context.Context, path string) (FileInfo, error)

	// Glob 在沙盒中匹配文件路径。
	Glob(ctx context.Context, dir string, pattern string, opts GlobOptions) ([]string, error)

	// Grep 在沙盒中搜索文件内容。
	Grep(ctx context.Context, dir string, pattern string, opts GrepOptions) ([]GrepMatch, error)

	// HTTPGet 在沙盒中发起 HTTP GET 请求。
	HTTPGet(ctx context.Context, url string, opts HTTPOptions) (HTTPResponse, error)

	// HTTPPost 在沙盒中发起 HTTP POST 请求。
	HTTPPost(ctx context.Context, url string, body string, opts HTTPOptions) (HTTPResponse, error)
}

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
