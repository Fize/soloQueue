package sandbox

import (
	"context"
	"io"
	"time"
)

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

// FileInfo is the backend-neutral subset of file metadata exposed to tools.
type FileInfo struct {
	Size  int64
	IsDir bool
}

// ReadFileOptions controls a backend file read.
type ReadFileOptions struct {
	MaxSize int64
}

// WriteFileOptions controls a backend file write.
type WriteFileOptions struct {
	Overwrite bool
	MaxSize   int64
}

// WriteFileResult describes whether a write created a new file.
type WriteFileResult struct {
	Created bool
}

// GlobOptions controls backend glob enumeration.
type GlobOptions struct {
	MaxItems int
	Timeout  time.Duration
}

// GrepOptions controls backend text search.
type GrepOptions struct {
	MaxMatches  int
	MaxLineLen  int
	GlobPattern string
}

// GrepMatch is a single backend text-search result.
type GrepMatch struct {
	File    string
	Line    int
	Content string
}

// HTTPRequest describes an outbound request executed by a backend.
type HTTPRequest struct {
	Method      string
	URL         string
	Body        []byte
	Headers     map[string]string
	ContentType string
	Timeout     time.Duration
	MaxBody     int64
}

// HTTPResponse is the bounded response returned by a backend.
type HTTPResponse struct {
	StatusCode int
	Body       []byte
}

// ProcessSpec describes a long-lived process without shell interpolation.
type ProcessSpec struct {
	Command          string
	Args             []string
	Env              map[string]string
	WorkingDirectory string
}

// Process is a cancellable long-lived backend process.
type Process interface {
	Stdin() io.WriteCloser
	Stdout() io.Reader
	Stderr() io.Reader
	Wait() error
	Kill() error
}

// ProcessExitError reports a non-zero process exit while preserving its code.
type ProcessExitError struct {
	Code int
}

func (e *ProcessExitError) Error() string { return "sandbox: process exited with non-zero status" }
func (e *ProcessExitError) ExitCode() int { return e.Code }

// Backend is the execution contract required by SandboxRuntime.
//
// Implementations must execute every method within the same sandbox boundary.
// Returning an error is required when a capability is unavailable; callers must
// never fall back to the host.
type Backend interface {
	Name() string
	Prepare(context.Context) error
	RunCommand(context.Context, string, RunCommandOptions) (RunCommandResult, error)
	StartProcess(context.Context, ProcessSpec) (Process, error)
	ReadFile(context.Context, string, ReadFileOptions) ([]byte, error)
	WriteFile(context.Context, string, []byte, WriteFileOptions) (WriteFileResult, error)
	MkdirAll(context.Context, string) error
	Stat(context.Context, string) (FileInfo, error)
	Glob(context.Context, string, string, GlobOptions) ([]string, error)
	Grep(context.Context, string, string, GrepOptions) ([]GrepMatch, error)
	DoHTTP(context.Context, HTTPRequest) (HTTPResponse, error)
	Stop(context.Context) error
}
