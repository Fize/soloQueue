package process

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// Command preserves raw pipes for batch-backed MCP servers. Cmd.exe uses
// different quoting rules, so reject characters it could interpret as syntax.
func Command(ctx context.Context, name string, args []string, workDir string) (*exec.Cmd, error) {
	resolved := name
	if strings.ContainsAny(name, `/\`) && !filepath.IsAbs(name) {
		if workDir == "" {
			workDir = "."
		}
		var err error
		resolved, err = filepath.Abs(filepath.Join(workDir, name))
		if err != nil {
			return nil, err
		}
	}
	var err error
	resolved, err = exec.LookPath(resolved)
	if err != nil {
		return nil, err
	}

	ext := strings.ToLower(filepath.Ext(resolved))
	if ext != ".cmd" && ext != ".bat" {
		return exec.CommandContext(ctx, resolved, args...), nil
	}
	if !filepath.IsAbs(resolved) {
		var err error
		if strings.ContainsAny(resolved, `/\\`) {
			if workDir == "" {
				workDir = "."
			}
			resolved, err = filepath.Abs(filepath.Join(workDir, resolved))
		} else {
			resolved, err = exec.LookPath(resolved)
		}
		if err != nil {
			return nil, err
		}
	}
	parts := append([]string{resolved}, args...)
	for _, part := range parts {
		if strings.ContainsAny(part, "\"&|<>^%!\r\n\x00") {
			return nil, fmt.Errorf("batch command and arguments cannot contain cmd.exe metacharacters")
		}
	}
	cmd := exec.CommandContext(ctx, "cmd.exe")
	if cmd.Err != nil {
		return nil, cmd.Err
	}
	quoted := make([]string, len(parts))
	for i, part := range parts {
		// Node and most native programs parse backslashes before a closing quote.
		trailing := len(part) - len(strings.TrimRight(part, `\`))
		quoted[i] = "\"" + part + strings.Repeat(`\`, trailing) + "\""
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: syscall.EscapeArg(cmd.Path) + " /d /s /c \"" + strings.Join(quoted, " ") + "\""}
	return cmd, nil
}
