package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
)

// shellExecTool 执行 shell 命令（白名单校验 + 跨平台 shim）
//
// Schema:
//
//	{"command":"ls -la", "stdin":"optional stdin text"}
//
// 安全：
//   - Command 必须匹配 ShellAllowRegexes 中至少一个；空列表 = 全拒绝
//   - macOS / Linux: /bin/sh -c <command>
//   - Windows:       powershell.exe -NoProfile -Command <command>
//   - ShellTimeout 通过 exec.CommandContext；超时子进程收到 SIGKILL
//   - stdout/stderr 各自限 ShellMaxOutput 字节（超出截断，truncated=true）
//   - 用户可选 Stdin（string）→ 写入 cmd stdin
//
// 返回：{"exit_code":0,"stdout":"...","stderr":"...","truncated":false}
type shellExecTool struct {
	cfg     Config
	regexes []*regexp.Regexp
	regErr  error // 编译失败时的错误（Execute 时返回）
}

func newShellExecTool(cfg Config) *shellExecTool {
	t := &shellExecTool{cfg: cfg}
	t.regexes = make([]*regexp.Regexp, 0, len(cfg.ShellAllowRegexes))
	for _, r := range cfg.ShellAllowRegexes {
		re, err := regexp.Compile(r)
		if err != nil {
			t.regErr = fmt.Errorf("invalid ShellAllowRegex %q: %w", r, err)
			return t
		}
		t.regexes = append(t.regexes, re)
	}
	return t
}

func (shellExecTool) Name() string { return "shell_exec" }

func (shellExecTool) Description() string {
	return "Run a shell command that matches one of the configured allow-regexes. " +
		"On Unix: /bin/sh -c <cmd>; on Windows: powershell -NoProfile -Command <cmd>. " +
		"Returns {exit_code,stdout,stderr,truncated}."
}

func (shellExecTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "command":{"type":"string","description":"Shell command to execute"},
    "stdin":{"type":"string","description":"Optional stdin for the subprocess"}
  },
  "required":["command"]
}`)
}

type shellExecArgs struct {
	Command string `json:"command"`
	Stdin   string `json:"stdin,omitempty"`
}

type shellExecResult struct {
	ExitCode  int    `json:"exit_code"`
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	Truncated bool   `json:"truncated"`
}

func (t *shellExecTool) Execute(ctx context.Context, raw string) (string, error) {
	if err := ctxErrOrNil(ctx); err != nil {
		return "", err
	}
	if t.regErr != nil {
		return "", t.regErr
	}

	var a shellExecArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}
	if err := validateNotZeroLen("command", a.Command); err != nil {
		return "", err
	}

	// 白名单匹配
	matched := false
	for _, re := range t.regexes {
		if re.MatchString(a.Command) {
			matched = true
			break
		}
	}
	if !matched {
		return "", fmt.Errorf("%w: %s", ErrCommandNotAllowed, a.Command)
	}

	// Timeout ctx
	timeout := t.cfg.ShellTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(execCtx, "powershell.exe", "-NoProfile", "-Command", a.Command)
	} else {
		cmd = exec.CommandContext(execCtx, "/bin/sh", "-c", a.Command)
	}

	if a.Stdin != "" {
		cmd.Stdin = strings.NewReader(a.Stdin)
	}

	maxOut := t.cfg.ShellMaxOutput
	if maxOut <= 0 {
		maxOut = 256 << 10
	}

	var stdout, stderr bytes.Buffer
	// limit reader: we cap each stream independently; total cap = 2 * maxOut
	cmd.Stdout = &limitedWriter{w: &stdout, cap: maxOut}
	cmd.Stderr = &limitedWriter{w: &stderr, cap: maxOut}

	err := cmd.Run()

	truncated := false
	if sw, ok := cmd.Stdout.(*limitedWriter); ok && sw.truncated {
		truncated = true
	}
	if sw, ok := cmd.Stderr.(*limitedWriter); ok && sw.truncated {
		truncated = true
	}

	res := shellExecResult{
		Stdout:    stdout.String(),
		Stderr:    stderr.String(),
		Truncated: truncated,
	}

	if err != nil {
		// timeout
		if execCtx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("shell timeout after %s: %w", timeout, context.DeadlineExceeded)
		}
		// caller canceled
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		// non-zero exit → encode to result; do NOT return error (LLM reads exit_code)
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			res.ExitCode = ee.ExitCode()
			b, _ := json.Marshal(res)
			return string(b), nil
		}
		// other errors (couldn't start) → surface
		return "", err
	}

	res.ExitCode = cmd.ProcessState.ExitCode()
	b, _ := json.Marshal(res)
	return string(b), nil
}

// limitedWriter wraps w and drops any bytes written past cap; sets truncated=true
// if dropping happened. Returns nil errors to avoid propagating short-write.
type limitedWriter struct {
	w         io.Writer
	cap       int64
	written   int64
	truncated bool
}

func (lw *limitedWriter) Write(p []byte) (int, error) {
	if lw.written >= lw.cap {
		lw.truncated = true
		return len(p), nil // pretend we wrote; drop silently
	}
	remain := lw.cap - lw.written
	if int64(len(p)) > remain {
		n, err := lw.w.Write(p[:remain])
		lw.written += int64(n)
		lw.truncated = true
		return len(p), err
	}
	n, err := lw.w.Write(p)
	lw.written += int64(n)
	return n, err
}

// Compile-time check
var _ agent.Tool = (*shellExecTool)(nil)
