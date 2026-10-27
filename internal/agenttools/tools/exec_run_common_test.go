package tools

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunCommand_WorkingDirectory(t *testing.T) {
	ctx := context.Background()
	s := NewExecutor()

	cmd := "pwd"
	if runtime.GOOS == "windows" {
		cmd = "(Get-Location).Path"
	}
	res, err := s.RunCommand(ctx, cmd, RunCommandOptions{})
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
	res, err = s.RunCommand(ctx, cmd, RunCommandOptions{
		WorkingDirectory: tmpDir,
	})
	if err != nil {
		t.Fatalf("RunCommand (with wd=%q): %v", tmpDir, err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", res.ExitCode)
	}
	wd := strings.TrimSpace(string(res.Stdout))
	actualInfo, actualErr := os.Stat(wd)
	wantInfo, wantErr := os.Stat(filepath.Clean(tmpDir))
	if actualErr != nil || wantErr != nil || !os.SameFile(actualInfo, wantInfo) {
		t.Errorf("pwd = %q, want %q", wd, filepath.Clean(tmpDir))
	}

	if defaultCwd == wd {
		t.Error("CWD did not change when working_directory was set")
	}
}

func TestRunCommand_DefaultWorkingDirectory(t *testing.T) {
	ctx := context.Background()
	s := NewExecutor()

	cmd := `echo "$PWD"`
	if runtime.GOOS == "windows" {
		cmd = "(Get-Location).Path"
	}
	res, err := s.RunCommand(ctx, cmd, RunCommandOptions{})
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
