//go:build !windows

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

func TestStartProcessWaitPreservesExitCode(t *testing.T) {
	p, err := NewExecutor().StartProcess(context.Background(), ProcessSpec{Command: "/bin/sh", Args: []string{"-c", "exit 3"}})
	if err != nil {
		t.Fatal(err)
	}
	err = p.Wait()
	exitErr, ok := err.(interface{ ExitCode() int })
	if !ok || exitErr.ExitCode() != 3 {
		t.Fatalf("wait error = %v; want exit code 3", err)
	}
}
