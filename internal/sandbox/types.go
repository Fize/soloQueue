package sandbox

import "time"

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
