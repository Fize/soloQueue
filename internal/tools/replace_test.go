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

// ─── Helpers ─────────────────────────────────────────────────────────────────

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

func mkMultiReplaceTool(t *testing.T, maxEdits int, maxFile, maxWrite int64) (*multiReplaceTool, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := Config{
		AllowedDirs:     []string{dir},
		MaxFileSize:     maxFile,
		MaxWriteSize:    maxWrite,
		MaxReplaceEdits: maxEdits,
	}
	return newMultiReplaceTool(cfg), dir
}

// ─── Single replace tests ────────────────────────────────────────────────────

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
	if tool.Name() != "Edit" {
		t.Errorf("Name = %q", tool.Name())
	}
	var m map[string]any
	if err := json.Unmarshal(tool.Parameters(), &m); err != nil {
		t.Errorf("Parameters not valid JSON: %v", err)
	}
}

// ─── Multi-replace tests ─────────────────────────────────────────────────────

func TestMultiReplace_Happy(t *testing.T) {
	tool, dir := mkMultiReplaceTool(t, 10, 1024, 1024)
	path := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(path, []byte("alpha beta gamma"), 0o644)

	raw, _ := json.Marshal(multiReplaceArgs{
		Path: path,
		Edits: []replaceEdit{
			{OldString: "alpha", NewString: "ALPHA"},
			{OldString: "beta", NewString: "BETA"},
			{OldString: "gamma", NewString: "GAMMA"},
		},
	})
	out, err := tool.Execute(context.Background(), string(raw))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var r multiReplaceResult
	_ = json.Unmarshal([]byte(out), &r)
	if r.Applied != 3 {
		t.Errorf("applied = %d", r.Applied)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "ALPHA BETA GAMMA" {
		t.Errorf("content = %q", got)
	}
}

// TestMultiReplace_SequentialDependency: second edit only works because
// first edit created its target text.
func TestMultiReplace_SequentialDependency(t *testing.T) {
	tool, dir := mkMultiReplaceTool(t, 10, 1024, 1024)
	path := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(path, []byte("foo"), 0o644)

	raw, _ := json.Marshal(multiReplaceArgs{
		Path: path,
		Edits: []replaceEdit{
			{OldString: "foo", NewString: "bar-baz"},
			{OldString: "bar-baz", NewString: "done"},
		},
	})
	_, err := tool.Execute(context.Background(), string(raw))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "done" {
		t.Errorf("content = %q, want 'done'", got)
	}
}

// TestMultiReplace_MiddleFails_FileUnchanged: second edit fails with
// ErrOldStringNotFound; original file remains intact.
func TestMultiReplace_MiddleFails_FileUnchanged(t *testing.T) {
	tool, dir := mkMultiReplaceTool(t, 10, 1024, 1024)
	path := filepath.Join(dir, "a.txt")
	orig := "alpha beta"
	_ = os.WriteFile(path, []byte(orig), 0o644)

	raw, _ := json.Marshal(multiReplaceArgs{
		Path: path,
		Edits: []replaceEdit{
			{OldString: "alpha", NewString: "A"},
			{OldString: "nonexistent", NewString: "X"}, // fails
			{OldString: "beta", NewString: "B"},
		},
	})
	_, err := tool.Execute(context.Background(), string(raw))
	if !errors.Is(err, ErrOldStringNotFound) {
		t.Errorf("err = %v, want ErrOldStringNotFound", err)
	}
	// file must be unchanged
	got, _ := os.ReadFile(path)
	if string(got) != orig {
		t.Errorf("file changed: %q", got)
	}
}

func TestMultiReplace_EmptyEdits(t *testing.T) {
	tool, dir := mkMultiReplaceTool(t, 10, 1024, 1024)
	path := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(path, []byte("x"), 0o644)
	raw, _ := json.Marshal(multiReplaceArgs{Path: path, Edits: []replaceEdit{}})
	_, err := tool.Execute(context.Background(), string(raw))
	if !errors.Is(err, ErrEmptyInput) {
		t.Errorf("err = %v, want ErrEmptyInput", err)
	}
}

func TestMultiReplace_TooManyEdits(t *testing.T) {
	tool, dir := mkMultiReplaceTool(t, 2, 1024, 1024)
	path := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(path, []byte("abc"), 0o644)
	raw, _ := json.Marshal(multiReplaceArgs{
		Path: path,
		Edits: []replaceEdit{
			{OldString: "a", NewString: "x"},
			{OldString: "b", NewString: "y"},
			{OldString: "c", NewString: "z"},
		},
	})
	_, err := tool.Execute(context.Background(), string(raw))
	if !errors.Is(err, ErrTooManyEdits) {
		t.Errorf("err = %v, want ErrTooManyEdits", err)
	}
}

func TestMultiReplace_ContentTooLargeAfter(t *testing.T) {
	tool, dir := mkMultiReplaceTool(t, 10, 1<<20, 10)
	path := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(path, []byte("a"), 0o644)
	raw, _ := json.Marshal(multiReplaceArgs{
		Path: path,
		Edits: []replaceEdit{
			{OldString: "a", NewString: strings.Repeat("b", 50)},
		},
	})
	_, err := tool.Execute(context.Background(), string(raw))
	if !errors.Is(err, ErrContentTooLarge) {
		t.Errorf("err = %v, want ErrContentTooLarge", err)
	}
	// unchanged
	got, _ := os.ReadFile(path)
	if string(got) != "a" {
		t.Errorf("file changed: %q", got)
	}
}

func TestMultiReplace_OutOfSandbox(t *testing.T) {
	tool, _ := mkMultiReplaceTool(t, 10, 1024, 1024)
	raw, _ := json.Marshal(multiReplaceArgs{
		Path: "/etc/passwd",
		Edits: []replaceEdit{
			{OldString: "a", NewString: "b"},
		},
	})
	_, err := tool.Execute(context.Background(), string(raw))
	if err == nil || !strings.Contains(err.Error(), "out of sandbox") {
		t.Errorf("err = %v, want out-of-sandbox", err)
	}
}

func TestMultiReplace_NoopInEdit(t *testing.T) {
	tool, dir := mkMultiReplaceTool(t, 10, 1024, 1024)
	path := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(path, []byte("a"), 0o644)
	raw, _ := json.Marshal(multiReplaceArgs{
		Path: path,
		Edits: []replaceEdit{
			{OldString: "a", NewString: "a"},
		},
	})
	_, err := tool.Execute(context.Background(), string(raw))
	if !errors.Is(err, ErrNoopReplace) {
		t.Errorf("err = %v, want ErrNoopReplace", err)
	}
}

func TestMultiReplace_EmptyOldInEdit(t *testing.T) {
	tool, dir := mkMultiReplaceTool(t, 10, 1024, 1024)
	path := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(path, []byte("a"), 0o644)
	raw, _ := json.Marshal(multiReplaceArgs{
		Path: path,
		Edits: []replaceEdit{
			{OldString: "", NewString: "x"},
		},
	})
	_, err := tool.Execute(context.Background(), string(raw))
	if err == nil || !strings.Contains(err.Error(), "old_string is empty") {
		t.Errorf("err = %v", err)
	}
}

func TestMultiReplace_AmbiguousInEdit(t *testing.T) {
	tool, dir := mkMultiReplaceTool(t, 10, 1024, 1024)
	path := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(path, []byte("foo foo"), 0o644)
	raw, _ := json.Marshal(multiReplaceArgs{
		Path: path,
		Edits: []replaceEdit{
			{OldString: "foo", NewString: "X"}, // ambiguous
		},
	})
	_, err := tool.Execute(context.Background(), string(raw))
	if !errors.Is(err, ErrOldStringAmbiguous) {
		t.Errorf("err = %v, want ErrOldStringAmbiguous", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "foo foo" {
		t.Errorf("file changed: %q", got)
	}
}

func TestMultiReplace_MetadataInterface(t *testing.T) {
	tool, _ := mkMultiReplaceTool(t, 10, 1024, 1024)
	if tool.Name() != "MultiEdit" {
		t.Errorf("Name = %q", tool.Name())
	}
	var m map[string]any
	if err := json.Unmarshal(tool.Parameters(), &m); err != nil {
		t.Errorf("Parameters not valid JSON: %v", err)
	}
}

// ─── Confirmable 接口测试 ────────────────────────────────────────────────────

func TestReplace_CheckConfirmation_AlwaysNeedsConfirm(t *testing.T) {
	tool, _ := mkReplaceTool(t, 1024, 1024)
	raw, _ := json.Marshal(replaceArgs{Path: "/tmp/test.go", OldString: "foo", NewString: "bar"})
	needs, prompt := tool.CheckConfirmation(string(raw))
	if !needs {
		t.Error("replace should always need confirmation")
	}
	if prompt == "" {
		t.Error("expected non-empty prompt")
	}
	if !strings.Contains(prompt, "test.go") || !strings.Contains(prompt, "foo") {
		t.Errorf("prompt should contain path and old_string, got: %s", prompt)
	}
}

func TestReplace_CheckConfirmation_InvalidJSON(t *testing.T) {
	tool, _ := mkReplaceTool(t, 1024, 1024)
	needs, prompt := tool.CheckConfirmation(`{not json`)
	if !needs {
		t.Error("should still need confirm even with invalid JSON")
	}
	if prompt == "" {
		t.Error("expected non-empty fallback prompt")
	}
}

func TestReplace_ConfirmationOptions_Binary(t *testing.T) {
	tool, _ := mkReplaceTool(t, 1024, 1024)
	raw, _ := json.Marshal(replaceArgs{Path: "a.go", OldString: "x", NewString: "y"})
	if opts := tool.ConfirmationOptions(string(raw)); opts != nil {
		t.Errorf("expected nil for binary confirm, got %v", opts)
	}
}

func TestReplace_ConfirmArgs_PreservesOriginal(t *testing.T) {
	tool, _ := mkReplaceTool(t, 1024, 1024)
	original := `{"path":"a.go","old_string":"foo","new_string":"bar"}`
	for _, choice := range []ConfirmChoice{ChoiceApprove, ChoiceDeny, ChoiceAllowInSession} {
		got := tool.ConfirmArgs(original, choice)
		if got != original {
			t.Errorf("choice=%v: expected original preserved, got %s", choice, got)
		}
	}
}

func TestReplace_SupportsSessionWhitelist(t *testing.T) {
	tool, _ := mkReplaceTool(t, 1024, 1024)
	if !tool.SupportsSessionWhitelist() {
		t.Error("should support session whitelist")
	}
}

// ─── MultiReplace Confirmable 测试 ──────────────────────────────────────────

func TestMultiReplace_CheckConfirmation_AlwaysNeedsConfirm(t *testing.T) {
	tool, _ := mkMultiReplaceTool(t, 10, 1024, 1024)
	raw, _ := json.Marshal(multiReplaceArgs{
		Path: "/tmp/test.go",
		Edits: []replaceEdit{
			{OldString: "a", NewString: "b"},
			{OldString: "c", NewString: "d"},
			{OldString: "e", NewString: "f"},
		},
	})
	needs, prompt := tool.CheckConfirmation(string(raw))
	if !needs {
		t.Error("multi_replace should always need confirmation")
	}
	if prompt == "" {
		t.Error("expected non-empty prompt")
	}
	if !strings.Contains(prompt, "test.go") || !strings.Contains(prompt, "3") {
		t.Errorf("prompt should contain path and edit count, got: %s", prompt)
	}
}

func TestMultiReplace_CheckConfirmation_InvalidJSON(t *testing.T) {
	tool, _ := mkMultiReplaceTool(t, 10, 1024, 1024)
	needs, prompt := tool.CheckConfirmation(`{not json`)
	if !needs {
		t.Error("should still need confirm even with invalid JSON")
	}
	if prompt == "" {
		t.Error("expected non-empty fallback prompt")
	}
}

func TestMultiReplace_ConfirmationOptions_Binary(t *testing.T) {
	tool, _ := mkMultiReplaceTool(t, 10, 1024, 1024)
	raw, _ := json.Marshal(multiReplaceArgs{
		Path: "a.go",
		Edits: []replaceEdit{{OldString: "x", NewString: "y"}},
	})
	if opts := tool.ConfirmationOptions(string(raw)); opts != nil {
		t.Errorf("expected nil for binary confirm, got %v", opts)
	}
}

func TestMultiReplace_ConfirmArgs_PreservesOriginal(t *testing.T) {
	tool, _ := mkMultiReplaceTool(t, 10, 1024, 1024)
	original := `{"path":"a.go","edits":[{"old_string":"foo","new_string":"bar"}]}`
	for _, choice := range []ConfirmChoice{ChoiceApprove, ChoiceDeny, ChoiceAllowInSession} {
		got := tool.ConfirmArgs(original, choice)
		if got != original {
			t.Errorf("choice=%v: expected original preserved, got %s", choice, got)
		}
	}
}

func TestMultiReplace_SupportsSessionWhitelist(t *testing.T) {
	tool, _ := mkMultiReplaceTool(t, 10, 1024, 1024)
	if !tool.SupportsSessionWhitelist() {
		t.Error("should support session whitelist")
	}
}
