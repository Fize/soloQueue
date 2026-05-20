package tools

import (
	"encoding/json"
	"testing"
)

func TestRunCommandOptions_ZeroValue(t *testing.T) {
	var opts RunCommandOptions
	if opts.WorkingDirectory != "" {
		t.Errorf("zero value WorkingDirectory = %q, want empty string", opts.WorkingDirectory)
	}
}

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
