package workdir

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// NormalizeExistingDir expands a leading ~, resolves the path to an absolute
// canonical directory, and verifies that it exists.
func NormalizeExistingDir(path string) (string, error) {
	if path == "" {
		return "", nil
	}

	expanded, err := expandHome(path)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", fmt.Errorf("resolve work directory %q: %w", path, err)
	}
	abs = filepath.Clean(abs)

	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("work directory %q: %w", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("work directory %q is not a directory", abs)
	}

	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve work directory symlinks %q: %w", abs, err)
	}
	return filepath.Clean(resolved), nil
}

func expandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
}
