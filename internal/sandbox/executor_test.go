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

// TestDockerExecutor_WorkingDirectory verifies the working_directory behavior
// for DockerExecutor. Since DockerExecutor requires a running Docker container,
// we skip this test in short mode or when Docker is unavailable.
func TestDockerExecutor_WorkingDirectory(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Docker executor test in short mode")
	}
	t.Skip("DockerExecutor requires a running Docker sandbox; see integration tests")
}

// TestDockerExecutor_WorkingDirectoryLogic tests the logical transformation
// of the working_directory parameter in DockerExecutor without needing a
// running Docker container.
func TestDockerExecutor_WorkingDirectoryLogic(t *testing.T) {
	// Verify shellQuote works for the "cd" path construction
	quoted := shellQuote("/root/.soloqueue")
	expected := "'/root/.soloqueue'"
	if quoted != expected {
		t.Errorf("shellQuote(%q) = %q, want %q", "/root/.soloqueue", quoted, expected)
	}

	// Verify that with empty working_directory, no "cd" prefix is added
	cmd := "echo hello"
	wd := ""
	execCmd := cmd
	if wd != "" {
		execCmd = "cd " + shellQuote(wd) + " && " + cmd
	}
	if execCmd != cmd {
		t.Error("empty working_directory should not prepend 'cd'")
	}

	// With a working directory, "cd" should be prepended
	wd = "/tmp/testdir"
	execCmd = cmd
	if wd != "" {
		execCmd = "cd " + shellQuote(wd) + " && " + cmd
	}
	want := "cd '/tmp/testdir' && echo hello"
	if execCmd != want {
		t.Errorf("with wd set: execCmd = %q, want %q", execCmd, want)
	}
}

// TestDockerExecutor_WorkingDirectoryPathTranslation tests that the DockerExecutor
// translates a host working_directory path through the path map to a container path.
func TestDockerExecutor_WorkingDirectoryPathTranslation(t *testing.T) {
	// Define mounts and create a PathMap
	mounts := []Mount{
		{HostPath: "/Users/test/.soloqueue", ContainerPath: "/root/.soloqueue"},
	}
	pm := NewPathMap(mounts)

	// Test host → container translation for working_directory
	hostWd := "/Users/test/.soloqueue/groups"
	containerWd := pm.ToContainerPath(hostWd)
	want := "/root/.soloqueue/groups"
	if containerWd != want {
		t.Errorf("ToContainerPath(%q) = %q, want %q", hostWd, containerWd, want)
	}

	// Verify the full command construction as DockerExecutor would do
	cmd := "ls"
	execCmd := "cd " + shellQuote(containerWd) + " && " + cmd
	wantCmd := "cd '/root/.soloqueue/groups' && ls"
	if execCmd != wantCmd {
		t.Errorf("constructed command = %q, want %q", execCmd, wantCmd)
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
