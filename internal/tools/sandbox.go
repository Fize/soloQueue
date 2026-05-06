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
// Returns:
//   - abs: the cleaned absolute path (os-native separators)
//   - err: ErrInvalidArgs if the path is empty or cannot be resolved
//
// Strategy:
//  1. Expand ~ to the user's home directory
//  2. filepath.Abs converts to absolute (relative paths resolved against CWD)
//  3. filepath.Clean removes .. / . / redundant separators
func absPath(input string) (string, error) {
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
	abs, err := filepath.Abs(input)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgs, err)
	}
	return filepath.Clean(abs), nil
}
