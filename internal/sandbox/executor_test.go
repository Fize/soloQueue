package sandbox

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLocalExecutor_WorkingDirectory verifies the working_directory behavior
// for LocalExecutor:
//   - When empty, the command runs from the default CWD (backward compat).
//   - When set, the command runs from the specified directory.
func TestLocalExecutor_WorkingDirectory(t *testing.T) {
	ctx := context.Background()
	exec := NewLocalExecutor()

	// ── 1. Empty working_directory (backward compatibility) ──────────────
	// The command should run from the process's default CWD.
	res, err := exec.RunCommand(ctx, "pwd", RunCommandOptions{})
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

	// ── 2. With working_directory set ────────────────────────────────────
	tmpDir := os.TempDir()
	res, err = exec.RunCommand(ctx, "pwd", RunCommandOptions{
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

	// ── 3. Verify they are different (the wd actually changed) ───────────
	if defaultCwd == wd {
		t.Error("CWD did not change when working_directory was set")
	}
}

// TestLocalExecutor_DefaultWorkingDirectory verifies that running a command
// without specifying working_directory runs from the process's default CWD.
func TestLocalExecutor_DefaultWorkingDirectory(t *testing.T) {
	ctx := context.Background()
	exec := NewLocalExecutor()

	res, err := exec.RunCommand(ctx, `echo "$PWD"`, RunCommandOptions{})
	if err != nil {
		t.Fatalf("RunCommand: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d", res.ExitCode)
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

// TestRunCommandOptions_ZeroValue verifies that the zero-value of
// RunCommandOptions has an empty WorkingDirectory, ensuring backward
// compatibility when the field was not present in JSON.
func TestRunCommandOptions_ZeroValue(t *testing.T) {
	var opts RunCommandOptions
	if opts.WorkingDirectory != "" {
		t.Errorf("zero value WorkingDirectory = %q, want empty string", opts.WorkingDirectory)
	}
}

// TestRunCommandOptions_JSONUnmarshal_WithoutWorkingDirectory verifies that
// unmarshalling JSON without working_directory yields an empty string, which
// is how shellExecArgs flows when the user doesn't provide the field.
func TestRunCommandOptions_JSONUnmarshal_WithoutWorkingDirectory(t *testing.T) {
	type testArgs struct {
		Command          string `json:"command"`
		WorkingDirectory string `json:"working_directory,omitempty"`
	}

	raw := `{"command":"echo hi"}`
	var args testArgs
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if args.WorkingDirectory != "" {
		t.Errorf("WorkingDirectory = %q, want empty string", args.WorkingDirectory)
	}
	if args.Command != "echo hi" {
		t.Errorf("Command = %q, want %q", args.Command, "echo hi")
	}
}
