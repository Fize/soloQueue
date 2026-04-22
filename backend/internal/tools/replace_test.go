package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func mkReplaceTool(t *testing.T, maxFile, maxWrite int64) (*replaceTool, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := Config{
		AllowedDirs:  []string{dir},
		MaxFileSize:  maxFile,
		MaxWriteSize: maxWrite,
	}
	return newReplaceTool(cfg), dir
}

func TestReplace_Happy(t *testing.T) {
	tool, dir := mkReplaceTool(t, 1024, 1024)
	path := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(path, []byte("hello world"), 0o644)

	raw, _ := json.Marshal(replaceArgs{
		Path: path, OldString: "world", NewString: "go",
	})
	out, err := tool.Execute(context.Background(), string(raw))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var r replaceResult
	_ = json.Unmarshal([]byte(out), &r)
	if r.Replacements != 1 {
		t.Errorf("replacements = %d", r.Replacements)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "hello go" {
		t.Errorf("content = %q", got)
	}
}

func TestReplace_All(t *testing.T) {
	tool, dir := mkReplaceTool(t, 1024, 1024)
	path := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(path, []byte("foo bar foo baz foo"), 0o644)

	raw, _ := json.Marshal(replaceArgs{
		Path: path, OldString: "foo", NewString: "X", ReplaceAll: true,
	})
	out, err := tool.Execute(context.Background(), string(raw))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var r replaceResult
	_ = json.Unmarshal([]byte(out), &r)
	if r.Replacements != 3 {
		t.Errorf("replacements = %d, want 3", r.Replacements)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "X bar X baz X" {
		t.Errorf("content = %q", got)
	}
}

func TestReplace_NotFound(t *testing.T) {
	tool, dir := mkReplaceTool(t, 1024, 1024)
	path := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(path, []byte("hello"), 0o644)
	raw, _ := json.Marshal(replaceArgs{Path: path, OldString: "zzz", NewString: "q"})
	_, err := tool.Execute(context.Background(), string(raw))
	if !errors.Is(err, ErrOldStringNotFound) {
		t.Errorf("err = %v, want ErrOldStringNotFound", err)
	}
}

func TestReplace_Ambiguous(t *testing.T) {
	tool, dir := mkReplaceTool(t, 1024, 1024)
	path := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(path, []byte("foo foo"), 0o644)
	raw, _ := json.Marshal(replaceArgs{Path: path, OldString: "foo", NewString: "X"})
	_, err := tool.Execute(context.Background(), string(raw))
	if !errors.Is(err, ErrOldStringAmbiguous) {
		t.Errorf("err = %v, want ErrOldStringAmbiguous", err)
	}
	// file unchanged
	got, _ := os.ReadFile(path)
	if string(got) != "foo foo" {
		t.Errorf("content changed: %q", got)
	}
}

func TestReplace_NoopEqualStrings(t *testing.T) {
	tool, dir := mkReplaceTool(t, 1024, 1024)
	path := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(path, []byte("foo"), 0o644)
	raw, _ := json.Marshal(replaceArgs{Path: path, OldString: "foo", NewString: "foo"})
	_, err := tool.Execute(context.Background(), string(raw))
	if !errors.Is(err, ErrNoopReplace) {
		t.Errorf("err = %v, want ErrNoopReplace", err)
	}
}

func TestReplace_EmptyOldString(t *testing.T) {
	tool, dir := mkReplaceTool(t, 1024, 1024)
	path := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(path, []byte("foo"), 0o644)
	raw, _ := json.Marshal(replaceArgs{Path: path, OldString: "", NewString: "x"})
	_, err := tool.Execute(context.Background(), string(raw))
	if err == nil || !strings.Contains(err.Error(), "old_string is empty") {
		t.Errorf("err = %v, want old_string is empty", err)
	}
}

func TestReplace_OutOfSandbox(t *testing.T) {
	tool, _ := mkReplaceTool(t, 1024, 1024)
	raw, _ := json.Marshal(replaceArgs{Path: "/etc/passwd", OldString: "a", NewString: "b"})
	_, err := tool.Execute(context.Background(), string(raw))
	if err == nil || !strings.Contains(err.Error(), "out of sandbox") {
		t.Errorf("err = %v, want out-of-sandbox", err)
	}
}

func TestReplace_FileMissing(t *testing.T) {
	tool, dir := mkReplaceTool(t, 1024, 1024)
	raw, _ := json.Marshal(replaceArgs{
		Path: filepath.Join(dir, "missing.txt"), OldString: "x", NewString: "y",
	})
	_, err := tool.Execute(context.Background(), string(raw))
	if err == nil {
		t.Error("missing file should error")
	}
}

func TestReplace_ResultTooLarge(t *testing.T) {
	tool, dir := mkReplaceTool(t, 1<<20, 10)
	path := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(path, []byte("aaa"), 0o644)
	raw, _ := json.Marshal(replaceArgs{
		Path: path, OldString: "a", NewString: strings.Repeat("b", 50), ReplaceAll: true,
	})
	_, err := tool.Execute(context.Background(), string(raw))
	if err == nil || !strings.Contains(err.Error(), "content too large") {
		t.Errorf("err = %v, want content too large", err)
	}
	// file unchanged
	got, _ := os.ReadFile(path)
	if string(got) != "aaa" {
		t.Errorf("file changed on rejected write: %q", got)
	}
}

// TestReplace_ConcurrentSameFile: two goroutines call replace simultaneously;
// at least one succeeds (atomic).
func TestReplace_ConcurrentSameFile(t *testing.T) {
	tool, dir := mkReplaceTool(t, 1<<20, 1<<20)
	path := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(path, []byte("aaaa"), 0o644)

	var wg sync.WaitGroup
	var oks atomic.Int32
	var mu sync.Mutex
	errs := []error{}

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			raw, _ := json.Marshal(replaceArgs{
				Path: path, OldString: "a", NewString: "b", ReplaceAll: true,
			})
			_, err := tool.Execute(context.Background(), string(raw))
			if err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
				return
			}
			oks.Add(1)
		}()
	}
	wg.Wait()

	// file must be either "aaaa" or "bbbb" (atomicity: not a mix)
	got, _ := os.ReadFile(path)
	s := string(got)
	if s != "aaaa" && s != "bbbb" {
		t.Errorf("partial write detected: %q", s)
	}
	// at least one succeeds
	if oks.Load() < 1 && len(errs) == 4 {
		t.Error("no replace succeeded (errs):")
		for _, e := range errs {
			t.Logf(" - %v", e)
		}
	}
}

func TestReplace_InvalidJSON(t *testing.T) {
	tool, _ := mkReplaceTool(t, 1024, 1024)
	_, err := tool.Execute(context.Background(), `{not json`)
	if err == nil {
		t.Error("invalid JSON should error")
	}
}

func TestReplace_MetadataInterface(t *testing.T) {
	tool, _ := mkReplaceTool(t, 1024, 1024)
	if tool.Name() != "replace" {
		t.Errorf("Name = %q", tool.Name())
	}
	var m map[string]any
	if err := json.Unmarshal(tool.Parameters(), &m); err != nil {
		t.Errorf("Parameters not valid JSON: %v", err)
	}
}
