package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ─── Path helper ──────────────────────────────────────────────────────

// absPath normalizes an input path to an absolute, cleaned path.
//
// Relative paths (not starting with / or ~) are resolved against workDir.
// When workDir is empty, falls back to process CWD (for callers that don't
// have a configured working directory, e.g. isPlanDirFile or tests).
//
// Returns:
//   - abs: the cleaned absolute path (os-native separators)
//   - err: ErrInvalidArgs if the path is empty or cannot be resolved
func absPath(input, workDir string) (string, error) {
	if input == "" {
		return "", fmt.Errorf("%w: empty path", ErrInvalidArgs)
	}
	// Expand ~ to the user's home directory so the LLM can use ~/ paths in prompts.
	if strings.HasPrefix(input, "~/") || input == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			input = filepath.Join(home, input[1:]) // strip leading ~
		}
	}
	// Resolve relative paths against workDir (not process CWD).
	// This ensures tool paths are consistent with the configured working
	// directory, matching the behaviour of shell commands (RunCommand).
	if workDir != "" && !filepath.IsAbs(input) {
		input = filepath.Join(workDir, input)
	}
	abs, err := filepath.Abs(input)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}
	return filepath.Clean(abs), nil
}
