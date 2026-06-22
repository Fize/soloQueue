package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

var useRTK = false

func init() {
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		if _, err := exec.LookPath("rtk"); err == nil {
			useRTK = true
		}
	}
}

// IsRTKEnabled returns whether RTK is enabled (supported platform and rtk binary found).
func IsRTKEnabled() bool {
	return useRTK
}

// shellExecTool executes shell commands with blocklist and confirmation checks.
//
// Schema:
//
//	{"command":"ls -la", "stdin":"optional stdin text", "confirmed":false}
//
// Security:
//   - Commands matching ShellBlockRegexes are rejected immediately.
//   - Commands matching ShellConfirmRegexes with confirmed=false require user confirmation.
//   - Uses /bin/sh -c <command>.
//   - Timeout is controlled by the parent context (DefaultToolTimeout), and the subprocess receives SIGKILL.
//   - stdout/stderr each have a ShellMaxOutput byte cap; overflow truncates and sets truncated=true.
//   - An optional Stdin string may be written to the subprocess stdin.
//
// Returns: {"exit_code":0,"stdout":"...","stderr":"...","truncated":false}
type shellExecTool struct {
	cfg            Config
	logger         *logger.Logger
	blockRegexes   []*regexp.Regexp
	confirmRegexes []*regexp.Regexp
	regErr         error // compilation error returned during Execute
}

func newShellExecTool(cfg Config) *shellExecTool {
	ensureSandbox(&cfg)
	t := &shellExecTool{cfg: cfg, logger: cfg.Logger}
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
	shell := "/bin/sh -c"
	if runtime.GOOS == "windows" {
		shell = "powershell.exe -Command (falls back to cmd.exe /c)"
	}
	return "Run a shell command. Dangerous commands (rm, dd, mkfs, etc.) require user confirmation. " +
		"Uses " + shell + " <cmd>. " +
		"Returns {exit_code,stdout,stderr,truncated}. " +
		"Supports optional working_directory parameter."
}

func (shellExecTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
  "type":"object",
  "properties":{
    "command":{"type":"string","description":"Shell command to execute"},
    "stdin":{"type":"string","description":"Optional stdin for the subprocess"},
    "confirmed":{"type":"boolean","description":"Set to true after user confirms a dangerous command"},
    "working_directory":{"type":"string","description":"Optional working directory for the command. If set, the command runs from this directory."}
  },
  "required":["command"]
}`)
}

type shellExecArgs struct {
	Command          string `json:"command"`
	Stdin            string `json:"stdin,omitempty"`
	Confirmed        bool   `json:"confirmed,omitempty"`
	WorkingDirectory string `json:"working_directory,omitempty"`
}

type shellExecResult struct {
	ExitCode  int    `json:"exit_code"`
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	Truncated bool   `json:"truncated"`
}

// CheckConfirmation implements Confirmable.
// Blocklist hit → no confirmation needed (Execute rejects directly);
// already confirmed → no confirmation needed;
// confirmation regex hit → confirmation required;
// otherwise → no confirmation needed.
func (t *shellExecTool) CheckConfirmation(raw string) (bool, string) {
	var a shellExecArgs
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		return false, ""
	}
	if a.Confirmed {
		return false, ""
	}
	// The blocklist is handled by Execute; here we only need to check whether the confirmation regexes match
	if matchesAny(a.Command, t.confirmRegexes) {
		return true, fmt.Sprintf("The command %q may be dangerous. Do you want to execute it?", a.Command)
	}
	return false, ""
}

// ConfirmationOptions implements Confirmable.
// Bash uses binary confirmation and returns nil.
func (shellExecTool) ConfirmationOptions(_ string) []string { return nil }

// ConfirmArgs implements Confirmable: when choice == ChoiceApprove, inject confirmed=true.
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

// SupportsSessionWhitelist implements Confirmable.
// Bash supports allow-in-session.
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

	// Blocklist check
	if matchesAny(a.Command, t.blockRegexes) {
		if t.logger != nil {
			t.logger.WarnContext(ctx, logger.CatTool, "shell: command blocked",
				"command", a.Command)
		}
		return "", fmt.Errorf("%w: %s", ErrCommandBlocked, a.Command)
	}

	cmdToRun := a.Command
	if useRTK {
		rewritten := rewriteCommand(ctx, cmdToRun)
		if rewritten != cmdToRun {
			if t.logger != nil {
				t.logger.InfoContext(ctx, logger.CatTool, "shell: rewritten by rtk",
					"original", cmdToRun, "rewritten", rewritten)
			}
			cmdToRun = rewritten
		}
	}

	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "shell: executing",
			"command", cmdToRun)
	}
	start := time.Now()

	maxOut := t.cfg.ShellMaxOutput
	if maxOut <= 0 {
		maxOut = 256 << 10
	}

	wd := a.WorkingDirectory
	if wd == "" && t.cfg.WorkDir != "" {
		wd = t.cfg.WorkDir
	}
	res, err := t.cfg.Sandbox.RunCommand(ctx, cmdToRun, RunCommandOptions{
		Stdin:            a.Stdin,
		MaxOutput:        maxOut,
		WorkingDirectory: wd,
	})

	shellRes := shellExecResult{
		Stdout:    string(res.Stdout),
		Stderr:    string(res.Stderr),
		Truncated: res.Truncated,
	}

	if err != nil {
		return "", err
	}

	shellRes.ExitCode = res.ExitCode
	if t.logger != nil {
		if res.ExitCode != 0 {
			t.logger.DebugContext(ctx, logger.CatTool, "shell: non-zero exit",
				"command", cmdToRun,
				"exit_code", res.ExitCode,
				"duration_ms", time.Since(start).Milliseconds())
		} else {
			t.logger.InfoContext(ctx, logger.CatTool, "shell: completed",
				"command", cmdToRun,
				"exit_code", res.ExitCode,
				"duration_ms", time.Since(start).Milliseconds())
		}
	}
	b, _ := json.Marshal(shellRes)
	return string(b), nil
}

func rewriteCommand(ctx context.Context, cmd string) string {
	if !useRTK {
		return cmd
	}

	// Set a strict timeout of 500ms for rtk rewrite to avoid hanging.
	rewriteCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()

	c := exec.CommandContext(rewriteCtx, "rtk", "rewrite", cmd)
	var stdout bytes.Buffer
	c.Stdout = &stdout

	err := c.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code := exitErr.ExitCode()
			if code != 3 && code != 0 {
				return cmd
			}
		} else {
			return cmd
		}
	}

	rewritten := strings.TrimSpace(stdout.String())
	if rewritten != "" {
		return rewritten
	}
	return cmd
}

// Compile-time checks
var _ Tool = (*shellExecTool)(nil)
var _ Confirmable = (*shellExecTool)(nil)
