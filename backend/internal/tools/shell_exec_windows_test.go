//go:build windows

package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Windows counterpart of shell_exec_unix_test.go — uses PowerShell commands.
// A minimal smoke test; the Unix file contains the broader matrix.

func mkShellTool(t *testing.T, allow []string, timeout time.Duration, maxOut int64) *shellExecTool {
	t.Helper()
	cfg := Config{
		ShellAllowRegexes: allow,
		ShellTimeout:      timeout,
		ShellMaxOutput:    maxOut,
	}
	return newShellExecTool(cfg)
}

func TestShell_HappyWriteOutput(t *testing.T) {
	tool := mkShellTool(t, []string{`^Write-Output\s`}, 5*time.Second, 1<<20)
	raw, _ := json.Marshal(shellExecArgs{Command: `Write-Output ok`})
	out, err := tool.Execute(context.Background(), string(raw))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var r shellExecResult
	_ = json.Unmarshal([]byte(out), &r)
	if r.ExitCode != 0 {
		t.Errorf("exit = %d", r.ExitCode)
	}
	if !strings.Contains(r.Stdout, "ok") {
		t.Errorf("stdout = %q", r.Stdout)
	}
}

func TestShell_Whitelist_Mismatch(t *testing.T) {
	tool := mkShellTool(t, []string{`^Write-Output\s`}, 5*time.Second, 1<<20)
	raw, _ := json.Marshal(shellExecArgs{Command: `Remove-Item C:\`})
	_, err := tool.Execute(context.Background(), string(raw))
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("err = %v, want not allowed", err)
	}
}

func TestShell_MetadataInterface(t *testing.T) {
	tool := mkShellTool(t, []string{`.*`}, 5*time.Second, 1<<20)
	if tool.Name() != "shell_exec" {
		t.Errorf("Name = %q", tool.Name())
	}
	var m map[string]any
	if err := json.Unmarshal(tool.Parameters(), &m); err != nil {
		t.Errorf("Parameters not valid JSON: %v", err)
	}
}
