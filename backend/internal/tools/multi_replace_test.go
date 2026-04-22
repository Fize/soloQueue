package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	if tool.Name() != "multi_replace" {
		t.Errorf("Name = %q", tool.Name())
	}
	var m map[string]any
	if err := json.Unmarshal(tool.Parameters(), &m); err != nil {
		t.Errorf("Parameters not valid JSON: %v", err)
	}
}
