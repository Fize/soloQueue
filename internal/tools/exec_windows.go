//go:build windows

package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

func (s *Sandbox) RunCommand(ctx context.Context, cmd string, opts RunCommandOptions) (RunCommandResult, error) {
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	maxOut := opts.MaxOutput
	if maxOut <= 0 {
		maxOut = 256 << 10
	}

	// Auto-detect shell: prefer PowerShell, fall back to cmd.exe.
	shell, arg := detectShell()
	c := exec.CommandContext(ctx, shell, arg, cmd)

	// Go 1.25's CommandContext already terminates the process on cancellation.
	// No Setpgid / process-group kill needed on Windows.
	c.Cancel = func() error {
		if c.Process != nil {
			return c.Process.Kill()
		}
		return nil
	}

	if opts.WorkingDirectory != "" {
		wd := opts.WorkingDirectory
		if strings.HasPrefix(wd, "~/") {
			usr, err := user.Current()
			if err == nil {
				wd = filepath.Join(usr.HomeDir, wd[2:])
			}
		} else if wd == "~" {
			usr, err := user.Current()
			if err == nil {
				wd = usr.HomeDir
			}
		}
		c.Dir = filepath.Clean(wd)
	}
	if opts.Stdin != "" {
		c.Stdin = strings.NewReader(opts.Stdin)
	}

	var stdout, stderr bytes.Buffer
	c.Stdout = &limitedWriterExec{w: &stdout, cap: maxOut}
	c.Stderr = &limitedWriterExec{w: &stderr, cap: maxOut}

	err := c.Run()

	res := RunCommandResult{
		Stdout: stdout.Bytes(),
		Stderr: stderr.Bytes(),
	}

	if sw, ok := c.Stdout.(*limitedWriterExec); ok && sw.truncated {
		res.Truncated = true
	}
	if sw, ok := c.Stderr.(*limitedWriterExec); ok && sw.truncated {
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
		if s.log != nil {
			s.log.LogError(ctx, logger.CatTool, "exec: run command failed", err, "command", cmd)
		}
		return res, err
	}

	res.ExitCode = c.ProcessState.ExitCode()
	return res, nil
}

// detectShell returns the preferred shell executable and its "run command" flag.
// Attempts PowerShell first (richer, closer to bash), falls back to cmd.exe.
func detectShell() (string, string) {
	if _, err := exec.LookPath("powershell.exe"); err == nil {
		return "powershell.exe", "-Command"
	}
	return "cmd.exe", "/c"
}
