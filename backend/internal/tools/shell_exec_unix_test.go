//go:build !windows

package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
)

func mkShellTool(t *testing.T, confirm []string, timeout time.Duration, maxOut int64) *shellExecTool {
	t.Helper()
	cfg := Config{
		ShellConfirmRegexes: confirm,
		ShellTimeout:        timeout,
		ShellMaxOutput:      maxOut,
	}
	return newShellExecTool(cfg)
}

func TestShell_HappyEcho(t *testing.T) {
	tool := mkShellTool(t, nil, 5*time.Second, 1<<20)
	raw, _ := json.Marshal(shellExecArgs{Command: "echo ok"})
	out, err := tool.Execute(context.Background(), string(raw))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var r shellExecResult
	_ = json.Unmarshal([]byte(out), &r)
	if r.ExitCode != 0 {
		t.Errorf("exit = %d", r.ExitCode)
	}
	if strings.TrimSpace(r.Stdout) != "ok" {
		t.Errorf("stdout = %q", r.Stdout)
	}
}

func TestShell_NonZeroExit(t *testing.T) {
	tool := mkShellTool(t, nil, 5*time.Second, 1<<20)
	raw, _ := json.Marshal(shellExecArgs{Command: "exit 7"})
	out, err := tool.Execute(context.Background(), string(raw))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var r shellExecResult
	_ = json.Unmarshal([]byte(out), &r)
	if r.ExitCode != 7 {
		t.Errorf("exit = %d, want 7", r.ExitCode)
	}
}

func TestShell_Stderr(t *testing.T) {
	tool := mkShellTool(t, nil, 5*time.Second, 1<<20)
	raw, _ := json.Marshal(shellExecArgs{Command: `echo err>&2`})
	out, err := tool.Execute(context.Background(), string(raw))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var r shellExecResult
	_ = json.Unmarshal([]byte(out), &r)
	if !strings.Contains(r.Stderr, "err") {
		t.Errorf("stderr = %q", r.Stderr)
	}
}

func TestShell_BlockList(t *testing.T) {
	tool := newShellExecTool(Config{
		ShellBlockRegexes: []string{`^rm\b`},
	})
	raw, _ := json.Marshal(shellExecArgs{Command: "rm -rf /"})
	_, err := tool.Execute(context.Background(), string(raw))
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Errorf("err = %v, want blocked", err)
	}
}

func TestShell_Timeout(t *testing.T) {
	tool := mkShellTool(t, nil, 50*time.Millisecond, 1<<20)
	raw, _ := json.Marshal(shellExecArgs{Command: "sleep 5"})
	start := time.Now()
	_, err := tool.Execute(context.Background(), string(raw))
	elapsed := time.Since(start)
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Errorf("err = %v, want timeout", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("timeout too slow: %v", elapsed)
	}
}

func TestShell_CtxCancel(t *testing.T) {
	tool := mkShellTool(t, nil, 5*time.Second, 1<<20)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	raw, _ := json.Marshal(shellExecArgs{Command: "sleep 5"})
	_, err := tool.Execute(ctx, string(raw))
	if err == nil {
		t.Error("expected ctx cancel error")
	}
}

func TestShell_Stdin(t *testing.T) {
	tool := mkShellTool(t, nil, 5*time.Second, 1<<20)
	raw, _ := json.Marshal(shellExecArgs{Command: "cat", Stdin: "piped input"})
	out, err := tool.Execute(context.Background(), string(raw))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var r shellExecResult
	_ = json.Unmarshal([]byte(out), &r)
	if !strings.Contains(r.Stdout, "piped input") {
		t.Errorf("stdout = %q", r.Stdout)
	}
}

func TestShell_OutputTruncation(t *testing.T) {
	tool := mkShellTool(t, nil, 500*time.Millisecond, 100)
	// "yes" prints "y\n" forever; timeout kicks it, but truncation should fire first
	raw, _ := json.Marshal(shellExecArgs{Command: "yes"})
	out, err := tool.Execute(context.Background(), string(raw))
	// either timeout or execution returns a result; we care about truncation when not a hard error
	if err == nil {
		var r shellExecResult
		_ = json.Unmarshal([]byte(out), &r)
		if len(r.Stdout) > 100 {
			t.Errorf("stdout = %d bytes, want ≤ 100", len(r.Stdout))
		}
		if !r.Truncated {
			t.Error("truncated should be true")
		}
	}
}

func TestShell_EmptyCommand(t *testing.T) {
	tool := mkShellTool(t, nil, 5*time.Second, 1<<20)
	raw, _ := json.Marshal(shellExecArgs{Command: ""})
	_, err := tool.Execute(context.Background(), string(raw))
	if err == nil {
		t.Error("empty command should error")
	}
}

func TestShell_InvalidRegex(t *testing.T) {
	tool := newShellExecTool(Config{
		ShellBlockRegexes: []string{"[unclosed"},
	})
	raw, _ := json.Marshal(shellExecArgs{Command: "echo hi"})
	_, err := tool.Execute(context.Background(), string(raw))
	if err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Errorf("err = %v, want invalid regex", err)
	}
}

func TestShell_InvalidJSON(t *testing.T) {
	tool := mkShellTool(t, nil, 5*time.Second, 1<<20)
	_, err := tool.Execute(context.Background(), `{not json`)
	if err == nil {
		t.Error("invalid JSON should error")
	}
}

func TestShell_MetadataInterface(t *testing.T) {
	tool := mkShellTool(t, nil, 5*time.Second, 1<<20)
	if tool.Name() != "shell_exec" {
		t.Errorf("Name = %q", tool.Name())
	}
	var m map[string]any
	if err := json.Unmarshal(tool.Parameters(), &m); err != nil {
		t.Errorf("Parameters not valid JSON: %v", err)
	}
}

// ─── Confirmable 接口测试 ────────────────────────────────────────────────────

func TestShell_CheckConfirmation_NeedsConfirm(t *testing.T) {
	tool := newShellExecTool(Config{
		ShellConfirmRegexes: []string{`^rm\b`},
	})
	needs, prompt := tool.CheckConfirmation(`{"command":"rm -rf /"}`)
	if !needs {
		t.Error("expected needsConfirm=true for rm")
	}
	if prompt == "" {
		t.Error("expected non-empty prompt")
	}
}

func TestShell_CheckConfirmation_AlreadyConfirmed(t *testing.T) {
	tool := newShellExecTool(Config{
		ShellConfirmRegexes: []string{`^rm\b`},
	})
	needs, _ := tool.CheckConfirmation(`{"command":"rm -rf /","confirmed":true}`)
	if needs {
		t.Error("confirmed=true should not need confirm")
	}
}

func TestShell_CheckConfirmation_NotInList(t *testing.T) {
	tool := newShellExecTool(Config{
		ShellConfirmRegexes: []string{`^rm\b`},
	})
	needs, _ := tool.CheckConfirmation(`{"command":"ls -la"}`)
	if needs {
		t.Error("ls should not need confirm")
	}
}

func TestShell_ConfirmArgs(t *testing.T) {
	tool := newShellExecTool(Config{})
	out := tool.ConfirmArgs(`{"command":"rm -rf /"}`)
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m["confirmed"] != true {
		t.Errorf("confirmed = %v, want true", m["confirmed"])
	}
}

func TestShell_Confirmable_CompileTimeCheck(t *testing.T) {
	// 编译时检查：shellExecTool 实现了 agent.Confirmable
	var _ agent.Confirmable = (*shellExecTool)(nil)
}
