package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// shellExecTool 执行 shell 命令（黑名单/确认名单校验）
//
// Schema:
//
//	{"command":"ls -la", "stdin":"optional stdin text", "confirmed":false}
//
// 安全：
//   - Command 命中 ShellBlockRegexes → 直接拒绝
//   - Command 命中 ShellConfirmRegexes + confirmed=false → 需要用户确认
//   - /bin/sh -c <command>
//   - ShellTimeout 通过 exec.CommandContext；超时子进程收到 SIGKILL
//   - stdout/stderr 各自限 ShellMaxOutput 字节（超出截断，truncated=true）
//   - 用户可选 Stdin（string）→ 写入 cmd stdin
//
// 返回：{"exit_code":0,"stdout":"...","stderr":"...","truncated":false}
type shellExecTool struct {
	cfg           Config
	blockRegexes  []*regexp.Regexp
	confirmRegexes []*regexp.Regexp
	regErr        error // 编译失败时的错误（Execute 时返回）
}

func newShellExecTool(cfg Config) *shellExecTool {
	t := &shellExecTool{cfg: cfg}
	for _, r := range cfg.ShellBlockRegexes {
		re, err := regexp.Compile(r)
		if err != nil {
			t.regErr = fmt.Errorf("invalid ShellBlockRegex %q: %w", r, err)
			return t
		}
		t.blockRegexes = append(t.blockRegexes, re)
	}
	for _, r := range cfg.ShellConfirmRegexes {
		re, err := regexp.Compile(r)
		if err != nil {
			t.regErr = fmt.Errorf("invalid ShellConfirmRegex %q: %w", r, err)
			return t
		}
		t.confirmRegexes = append(t.confirmRegexes, re)
	}
	return t
}

func (shellExecTool) Name() string { return "Bash" }

func (shellExecTool) Description() string {
	return "Run a shell command. Dangerous commands (rm, dd, mkfs, etc.) require user confirmation. " +
		"Uses /bin/sh -c <cmd>. " +
		"Returns {exit_code,stdout,stderr,truncated}."
}

func (shellExecTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "command":{"type":"string","description":"Shell command to execute"},
    "stdin":{"type":"string","description":"Optional stdin for the subprocess"},
    "confirmed":{"type":"boolean","description":"Set to true after user confirms a dangerous command"}
  },
  "required":["command"]
}`)
}

type shellExecArgs struct {
	Command   string `json:"command"`
	Stdin     string `json:"stdin,omitempty"`
	Confirmed bool   `json:"confirmed,omitempty"`
}

type shellExecResult struct {
	ExitCode  int    `json:"exit_code"`
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	Truncated bool   `json:"truncated"`
}

// CheckConfirmation 实现 Confirmable。
// 黑名单命中 → 不确认（让 Execute 直接拒绝）；
// 已 confirmed → 不确认；
// 确认名单命中 → 需要确认；
// 其他 → 不确认。
func (t *shellExecTool) CheckConfirmation(raw string) (bool, string) {
	var a shellExecArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return false, ""
	}
	if a.Confirmed {
		return false, ""
	}
	// 黑名单由 Execute 处理；这里只需判断是否命中确认名单
	if matchesAny(a.Command, t.confirmRegexes) {
		return true, fmt.Sprintf("The command %q may be dangerous. Do you want to execute it?", a.Command)
	}
	return false, ""
}

// ConfirmationOptions 实现 Confirmable。
// Bash 使用二元确认，返回 nil。
func (shellExecTool) ConfirmationOptions(_ string) []string { return nil }

// ConfirmArgs 实现 Confirmable：choice == ChoiceApprove 时注入 confirmed=true。
func (shellExecTool) ConfirmArgs(original string, choice ConfirmChoice) string {
	if choice != ChoiceApprove {
		return original
	}
	var a map[string]any
	if err := json.Unmarshal([]byte(original), &a); err != nil {
		return original
	}
	a["confirmed"] = true
	b, _ := json.Marshal(a)
	return string(b)
}

// SupportsSessionWhitelist 实现 Confirmable。
// Bash 支持 allow-in-session。
func (shellExecTool) SupportsSessionWhitelist() bool { return true }

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

	// 黑名单检查
	if matchesAny(a.Command, t.blockRegexes) {
		return "", fmt.Errorf("%w: %s", ErrCommandBlocked, a.Command)
	}

	// Timeout ctx
	timeout := t.cfg.ShellTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "/bin/sh", "-c", a.Command)

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

// Compile-time checks
var _ Tool = (*shellExecTool)(nil)
var _ Confirmable = (*shellExecTool)(nil)
