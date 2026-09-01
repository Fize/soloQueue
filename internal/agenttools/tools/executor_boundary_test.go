package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModelToolsDoNotBypassExecutor(t *testing.T) {
	t.Parallel()

	allowDirectHostAccess := map[string]bool{
		"exec.go":            true,
		"run_command.go":     true,
		"runtime_process.go": true,
	}
	forbidden := []string{
		"os.Open(",
		"os.OpenFile(",
		"os.Create(",
		"os.ReadFile(",
		"os.WriteFile(",
		"os.Stat(",
		"os.Lstat(",
		"os.Mkdir(",
		"os.MkdirAll(",
		"os.Remove(",
		"os.Rename(",
		"filepath.Walk(",
		"filepath.WalkDir(",
		"exec.Command(",
		"exec.CommandContext(",
		"http.Get(",
		"http.Post(",
		"client.Do(",
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") ||
			strings.HasSuffix(name, "_test.go") || allowDirectHostAccess[name] {
			continue
		}
		data, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatal(err)
		}
		source := string(data)
		for _, token := range forbidden {
			if strings.Contains(source, token) {
				t.Errorf("%s bypasses Executor with %q", name, token)
			}
		}
	}
}
