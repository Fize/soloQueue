package tools

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"golang.org/x/sys/windows"
	"strings"
	"testing"
)

func TestRunCommand_WindowsNativeExitCode(t *testing.T) {
	result, err := NewExecutor().RunCommand(context.Background(), "cmd.exe /c exit 7", RunCommandOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 7 {
		t.Fatalf("exit code = %d, want 7; stderr = %q", result.ExitCode, result.Stderr)
	}
}

func TestRunCommand_WindowsUnicodeAndStdin(t *testing.T) {
	result, err := NewExecutor().RunCommand(context.Background(), "[Console]::Out.Write([Console]::In.ReadToEnd())", RunCommandOptions{Stdin: "测试 input"})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExitCode != 0 || strings.TrimSpace(string(result.Stdout)) != "测试 input" {
		t.Fatalf("result = %+v, output = %q", result, result.Stdout)
	}
}

func TestRunCommand_WindowsCancellationTerminatesChild(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child pid.txt")
	escaped := strings.ReplaceAll(pidFile, "'", "''")
	command := "$child = Start-Process powershell.exe -ArgumentList '-NoProfile','-Command','Start-Sleep -Seconds 60' -PassThru; [IO.File]::WriteAllText('" + escaped + "', [string]$child.Id); $child.WaitForExit()"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := NewExecutor().RunCommand(ctx, command, RunCommandOptions{}); done <- err }()
	var pid uint64
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(pidFile)
		if err == nil {
			pid, err = strconv.ParseUint(strings.TrimSpace(string(data)), 10, 32)
			if err == nil {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if pid == 0 {
		t.Fatal("child process did not report its PID")
	}
	child, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(child)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel result: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("command did not exit after cancellation")
	}
	state, err := windows.WaitForSingleObject(child, 10000)
	if err != nil || state != windows.WAIT_OBJECT_0 {
		t.Fatalf("child survived cancellation: wait=%d err=%v", state, err)
	}
}

func TestStartProcess_WindowsLeaderExitTerminatesChild(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child pid.txt")
	escaped := strings.ReplaceAll(pidFile, "'", "''")
	script := "$child = Start-Process powershell.exe -ArgumentList '-NoProfile','-Command','Start-Sleep -Seconds 60' -PassThru; [IO.File]::WriteAllText('" + escaped + "', [string]$child.Id)"
	process, err := NewExecutor().StartProcess(context.Background(), ProcessSpec{Command: "powershell.exe", Args: []string{"-NoProfile", "-NonInteractive", "-Command", script}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = process.Kill(); _ = process.Wait() }()
	stderr := make(chan string, 1)
	go func() { b, _ := io.ReadAll(process.Stderr()); stderr <- string(b) }()
	var pid uint64
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(pidFile)
		if err == nil {
			pid, err = strconv.ParseUint(strings.TrimSpace(string(data)), 10, 32)
			if err == nil {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if pid == 0 {
		killErr := process.Kill()
		select {
		case output := <-stderr:
			t.Fatalf("child process did not report its PID; kill: %v; stderr: %s", killErr, output)
		case <-time.After(5 * time.Second):
			t.Fatalf("child process did not report its PID; kill: %v; stderr still open", killErr)
		}
	}
	child, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(child)
	if err := process.Wait(); err != nil {
		t.Fatal(err)
	}
	state, err := windows.WaitForSingleObject(child, 10000)
	if err != nil || state != windows.WAIT_OBJECT_0 {
		t.Fatalf("child survived protocol process exit: wait=%d err=%v", state, err)
	}
}
