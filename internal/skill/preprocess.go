package skill

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ─── Preprocessing pipeline ─────────────────────────────────────────────────────

// PreprocessConfig configuration for preprocessing pipeline
type PreprocessConfig struct {
	// ShellTimeout shell command execution timeout
	ShellTimeout time.Duration
	// WorkingDir working directory for shell commands (defaults to skill.Dir)
	WorkingDir string
}

// DefaultPreprocessConfig default preprocessing configuration
func DefaultPreprocessConfig() PreprocessConfig {
	return PreprocessConfig{
		ShellTimeout: 10 * time.Minute,
	}
}

var (
	// shellExpandRegex matches !`command` pattern
	shellExpandRegex = regexp.MustCompile("!`([^`]+)`")
	// fileRefRegex matches @filepath pattern
	// Does not match @@ (escaped) or @ in code blocks
	fileRefRegex = regexp.MustCompile("(?:^|[^@])@([\\w./\\-]+)")
)

// PreprocessContent executes preprocessing pipeline on skill content
//
// Pipeline order (consistent with Claude Code):
//  1. $ARGUMENTS → replace with user-provided args
//  2. !`command` → execute shell command and replace with stdout
//  3. @filepath  → replace with file content
//
// baseDir is used to resolve relative paths for @filepath and working directory for shell commands.
func PreprocessContent(content, args, baseDir string) string {
	// 1. $ARGUMENTS replacement
	content = strings.ReplaceAll(content, "$ARGUMENTS", args)

	// 2. !`command` shell execution
	cfg := DefaultPreprocessConfig()
	if baseDir != "" {
		cfg.WorkingDir = baseDir
	}
	content = expandShellCommands(content, cfg)

	// 3. @file references
	content = expandFileRefs(content, baseDir)

	return content
}

// expandShellCommands executes shell commands in !`command` pattern
//
// stdout output replaces the command line. On failure, replaces with empty string (consistent with CC behavior).
func expandShellCommands(content string, cfg PreprocessConfig) string {
	return shellExpandRegex.ReplaceAllStringFunc(content, func(match string) string {
		// Extract command content: !`cmd` → cmd
		cmdStr := shellExpandRegex.FindStringSubmatch(match)[1]
		if cmdStr == "" {
			return ""
		}

		ctx, cancel := context.WithTimeout(context.Background(), cfg.ShellTimeout)
		defer cancel()

		cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
		if cfg.WorkingDir != "" {
			cmd.Dir = cfg.WorkingDir
		}

		var stdout bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = nil // Discard stderr

		if err := cmd.Run(); err != nil {
			// Failure becomes empty string (consistent with CC behavior)
			return ""
		}

		return strings.TrimSpace(stdout.String())
	})
}

// expandFileRefs resolves @filepath references
//
// File paths can be absolute or relative to baseDir.
// On read failure, replaces with error message.
func expandFileRefs(content, baseDir string) string {
	return fileRefRegex.ReplaceAllStringFunc(content, func(match string) string {
		// Extract file path: match may include leading non-@ character
		submatch := fileRefRegex.FindStringSubmatch(match)
		if len(submatch) < 2 || submatch[1] == "" {
			return match
		}

		relPath := submatch[1]

		// Resolve path
		var absPath string
		if filepath.IsAbs(relPath) {
			absPath = relPath
		} else {
			absPath = filepath.Join(baseDir, relPath)
		}

		data, err := os.ReadFile(absPath)
		if err != nil {
			return fmt.Sprintf("<file error: %s>", err)
		}

		// Preserve leading character (non-@ part) from match
		prefix := ""
		if len(match) > 0 && match[0] != '@' {
			prefix = string(match[0])
		}
		return prefix + string(data)
	})
}
