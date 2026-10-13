package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunCommand_WorkingDirectory(t *testing.T) {
	ctx := context.Background()
	s := NewExecutor()

	res, err := s.RunCommand(ctx, "pwd", RunCommandOptions{})
	if err != nil {
		t.Fatalf("RunCommand (empty wd): %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", res.ExitCode)
	}
	defaultCwd := strings.TrimSpace(string(res.Stdout))
	if defaultCwd == "" {
		t.Fatal("empty pwd output")
	}

	tmpDir := os.TempDir()
	res, err = s.RunCommand(ctx, "pwd", RunCommandOptions{
		WorkingDirectory: tmpDir,
	})
	if err != nil {
		t.Fatalf("RunCommand (with wd=%q): %v", tmpDir, err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", res.ExitCode)
	}
	wd := strings.TrimSpace(string(res.Stdout))
	if wd != filepath.Clean(tmpDir) {
		t.Errorf("pwd = %q, want %q", wd, filepath.Clean(tmpDir))
	}

	if defaultCwd == wd {
		t.Error("CWD did not change when working_directory was set")
	}
}

func TestRunCommand_ReturnsAfterShellExitsWhenBackgroundChildKeepsPipeOpen(t *testing.T) {
	s := NewExecutor()
	started := time.Now()

	res, err := s.RunCommand(context.Background(), "sleep 3 & echo foreground-done", RunCommandOptions{})
	if err != nil {
		t.Fatalf("RunCommand: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", res.ExitCode)
	}
	if got := strings.TrimSpace(string(res.Stdout)); got != "foreground-done" {
		t.Fatalf("stdout = %q, want foreground-done", got)
	}
	if elapsed := time.Since(started); elapsed >= 2*time.Second {
		t.Fatalf("RunCommand waited %s for a background child after the shell exited", elapsed)
	}
}

func TestRunCommand_CancellationKillsBackgroundProcessGroup(t *testing.T) {
	s := NewExecutor()
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := s.RunCommand(ctx, "sleep 30 & echo $! > "+strconv.Quote(pidFile)+"; wait", RunCommandOptions{})
		done <- err
	}()

	var childPID int
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(pidFile)
		if err == nil {
			childPID, err = strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				t.Fatalf("parse child pid: %v", err)
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if childPID <= 0 {
		t.Fatal("background child pid was not recorded")
	}

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("RunCommand cancellation error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunCommand did not return promptly after cancellation")
	}

	for time.Now().Before(deadline.Add(time.Second)) {
		err := syscall.Kill(childPID, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("background child process %d survived cancellation", childPID)
}

func TestRunCommand_DefaultWorkingDirectory(t *testing.T) {
	ctx := context.Background()
	s := NewExecutor()

	res, err := s.RunCommand(ctx, `echo "$PWD"`, RunCommandOptions{})
	if err != nil {
		t.Fatalf("RunCommand: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", res.ExitCode)
	}

	pwd := strings.TrimSpace(string(res.Stdout))
	if pwd == "" {
		t.Fatal("empty output from echo $PWD")
	}

	cwd, _ := os.Getwd()
	if pwd != cwd {
		t.Errorf("PWD = %q, want process CWD = %q", pwd, cwd)
	}
}
