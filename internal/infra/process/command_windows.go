package process

import (
	"context"
	"debug/pe"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func windowsExtensions() []string {
	raw := os.Getenv("PATHEXT")
	if raw == "" {
		raw = ".COM;.EXE;.BAT;.CMD"
	}
	var result []string
	for _, suffix := range strings.Split(raw, ";") {
		if suffix == "" {
			continue
		}
		if !strings.HasPrefix(suffix, ".") {
			suffix = "." + suffix
		}
		result = append(result, suffix)
	}
	return result
}

func isPE(path string) bool {
	file, err := pe.Open(path)
	if err != nil {
		return false
	}
	_ = file.Close()
	return true
}

// preferWindowsExecutable finds Windows shims before POSIX scripts, while
// preserving a genuine PE executable with a nonstandard extension.
func preferWindowsExecutable(candidate string, extensions []string) string {
	ext := filepath.Ext(candidate)
	switch strings.ToLower(ext) {
	case ".exe", ".com", ".cmd", ".bat":
		return candidate
	}
	for _, suffix := range extensions {
		if strings.EqualFold(ext, suffix) {
			return candidate
		}
	}
	if isPE(candidate) {
		return candidate
	}
	for _, suffix := range extensions {
		path := candidate + suffix
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return path
		}
	}
	return candidate
}

func isBatchOrNativeExtension(path string, extensions []string) bool {
	ext := filepath.Ext(path)
	for _, suffix := range extensions {
		if strings.EqualFold(ext, suffix) {
			return true
		}
	}
	return false
}

// Command preserves raw pipes for batch-backed MCP servers. Cmd.exe uses
// different quoting rules, so reject characters it could interpret as syntax.
func Command(ctx context.Context, name string, args []string, workDir string) (*exec.Cmd, error) {
	extensions := windowsExtensions()
	resolved := name
	if strings.ContainsAny(name, `/\\`) {
		candidate := name
		if !filepath.IsAbs(candidate) {
			if workDir == "" {
				workDir = "."
			}
			candidate = filepath.Join(workDir, candidate)
		}
		if absolute, err := filepath.Abs(candidate); err == nil {
			preferred := preferWindowsExecutable(absolute, extensions)
			if preferred != absolute || isPE(absolute) {
				resolved = preferred
			}
		}
	} else {
		path, err := exec.LookPath(name)
		if err != nil {
			return nil, err
		}
		resolved = preferWindowsExecutable(path, extensions)
		if resolved == path && !isPE(path) && !isBatchOrNativeExtension(path, extensions) {
			for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
				if dir == "" {
					continue
				}
				candidate := preferWindowsExecutable(filepath.Join(dir, name), extensions)
				info, statErr := os.Stat(candidate)
				if statErr != nil || !info.Mode().IsRegular() || (!isPE(candidate) && !isBatchOrNativeExtension(candidate, extensions)) {
					continue
				}
				if !filepath.IsAbs(dir) {
					return nil, &exec.Error{Name: name, Err: exec.ErrDot}
				}
				resolved = candidate
				break
			}
		}
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
