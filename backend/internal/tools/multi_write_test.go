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

func mkMultiWriteTool(t *testing.T, files int, totalBytes int64) (*multiWriteTool, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := Config{
		AllowedDirs:        []string{dir},
		MaxWriteSize:       1 << 20,
		MaxMultiWriteFiles: files,
		MaxMultiWriteBytes: totalBytes,
	}
	return newMultiWriteTool(cfg), dir
}

func ptrBool(v bool) *bool { return &v }

func TestMultiWrite_Happy(t *testing.T) {
	tool, dir := mkMultiWriteTool(t, 10, 1024)
	raw, _ := json.Marshal(multiWriteArgs{
		Files: []writeFileArgs{
			{Path: filepath.Join(dir, "a.txt"), Content: "A"},
			{Path: filepath.Join(dir, "b.txt"), Content: "BB"},
			{Path: filepath.Join(dir, "c.txt"), Content: "CCC"},
		},
	})
	out, err := tool.Execute(context.Background(), string(raw))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var r multiWriteResult
	_ = json.Unmarshal([]byte(out), &r)
	if r.Summary.Total != 3 || r.Summary.OK != 3 || r.Summary.Error != 0 {
		t.Errorf("summary = %+v", r.Summary)
	}
	for _, e := range r.Files {
		if e.Status != "ok" {
			t.Errorf("%s: %s (%s)", e.Path, e.Status, e.Err)
		}
	}
}

// TestMultiWrite_PartialFailures: mix of valid + invalid (parent missing) entries.
// Sandbox-valid but runtime-error entries should not abort others.
func TestMultiWrite_PartialFailures(t *testing.T) {
	tool, dir := mkMultiWriteTool(t, 10, 1024)
	raw, _ := json.Marshal(multiWriteArgs{
		Files: []writeFileArgs{
			{Path: filepath.Join(dir, "a.txt"), Content: "ok"},
			{Path: filepath.Join(dir, "missing", "b.txt"), Content: "will fail"},
			{Path: filepath.Join(dir, "c.txt"), Content: "ok2"},
		},
	})
	out, err := tool.Execute(context.Background(), string(raw))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var r multiWriteResult
	_ = json.Unmarshal([]byte(out), &r)
	if r.Summary.OK != 2 || r.Summary.Error != 1 {
		t.Errorf("summary = %+v", r.Summary)
	}
	if r.Files[1].Status != "error" || !strings.Contains(r.Files[1].Err, "parent directory") {
		t.Errorf("files[1] = %+v", r.Files[1])
	}
}

// TestMultiWrite_SandboxVerifiedUpfront: any sandbox-invalid entry → entire
// call rejected (security-first).
func TestMultiWrite_SandboxVerifiedUpfront(t *testing.T) {
	tool, dir := mkMultiWriteTool(t, 10, 1024)
	raw, _ := json.Marshal(multiWriteArgs{
		Files: []writeFileArgs{
			{Path: filepath.Join(dir, "a.txt"), Content: "A"},
			{Path: "/etc/bad.txt", Content: "evil"},
		},
	})
	_, err := tool.Execute(context.Background(), string(raw))
	if err == nil || !strings.Contains(err.Error(), "out of sandbox") {
		t.Errorf("err = %v, want out-of-sandbox", err)
	}
	// first file must NOT have been written (entire call rejected)
	if _, statErr := os.Stat(filepath.Join(dir, "a.txt")); statErr == nil {
		t.Error("first file was written despite sandbox rejection of second")
	}
}

func TestMultiWrite_TooManyFiles(t *testing.T) {
	tool, dir := mkMultiWriteTool(t, 2, 1024)
	raw, _ := json.Marshal(multiWriteArgs{
		Files: []writeFileArgs{
			{Path: filepath.Join(dir, "a.txt"), Content: "1"},
			{Path: filepath.Join(dir, "b.txt"), Content: "2"},
			{Path: filepath.Join(dir, "c.txt"), Content: "3"},
		},
	})
	_, err := tool.Execute(context.Background(), string(raw))
	if !errors.Is(err, ErrTooManyFiles) {
		t.Errorf("err = %v, want ErrTooManyFiles", err)
	}
}

func TestMultiWrite_TotalBytesTooLarge(t *testing.T) {
	tool, dir := mkMultiWriteTool(t, 10, 10)
	raw, _ := json.Marshal(multiWriteArgs{
		Files: []writeFileArgs{
			{Path: filepath.Join(dir, "a.txt"), Content: "hello"},
			{Path: filepath.Join(dir, "b.txt"), Content: "world!"}, // total 11 bytes > 10
		},
	})
	_, err := tool.Execute(context.Background(), string(raw))
	if !errors.Is(err, ErrTotalBytesTooLarge) {
		t.Errorf("err = %v, want ErrTotalBytesTooLarge", err)
	}
}

func TestMultiWrite_EmptyFiles(t *testing.T) {
	tool, _ := mkMultiWriteTool(t, 10, 1024)
	raw, _ := json.Marshal(multiWriteArgs{Files: []writeFileArgs{}})
	_, err := tool.Execute(context.Background(), string(raw))
	if !errors.Is(err, ErrEmptyInput) {
		t.Errorf("err = %v, want ErrEmptyInput", err)
	}
}

// TestMultiWrite_OverwriteFalseMixed: some files already exist (with overwrite=false)
// → those error, others succeed.
func TestMultiWrite_OverwriteFalseMixed(t *testing.T) {
	tool, dir := mkMultiWriteTool(t, 10, 1024)
	existing := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(existing, []byte("old"), 0o644)

	raw, _ := json.Marshal(multiWriteArgs{
		Files: []writeFileArgs{
			{Path: existing, Content: "new", Overwrite: ptrBool(false)}, // fails
			{Path: filepath.Join(dir, "b.txt"), Content: "B"},            // ok
		},
	})
	out, err := tool.Execute(context.Background(), string(raw))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var r multiWriteResult
	_ = json.Unmarshal([]byte(out), &r)
	if r.Summary.OK != 1 || r.Summary.Error != 1 {
		t.Errorf("summary = %+v", r.Summary)
	}
	// existing content unchanged
	got, _ := os.ReadFile(existing)
	if string(got) != "old" {
		t.Errorf("existing changed: %q", got)
	}
	// b.txt written
	got2, _ := os.ReadFile(filepath.Join(dir, "b.txt"))
	if string(got2) != "B" {
		t.Errorf("b.txt = %q", got2)
	}
}

func TestMultiWrite_EmptyPath(t *testing.T) {
	tool, dir := mkMultiWriteTool(t, 10, 1024)
	raw, _ := json.Marshal(multiWriteArgs{
		Files: []writeFileArgs{
			{Path: "", Content: "x"},
			{Path: filepath.Join(dir, "b.txt"), Content: "B"},
		},
	})
	_, err := tool.Execute(context.Background(), string(raw))
	if err == nil || !strings.Contains(err.Error(), "is empty") {
		t.Errorf("err = %v, want 'is empty'", err)
	}
}

func TestMultiWrite_InvalidJSON(t *testing.T) {
	tool, _ := mkMultiWriteTool(t, 10, 1024)
	_, err := tool.Execute(context.Background(), `{not json`)
	if err == nil {
		t.Error("invalid JSON should error")
	}
}

func TestMultiWrite_CtxPreCanceled(t *testing.T) {
	tool, dir := mkMultiWriteTool(t, 10, 1024)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	raw, _ := json.Marshal(multiWriteArgs{
		Files: []writeFileArgs{
			{Path: filepath.Join(dir, "a.txt"), Content: "x"},
		},
	})
	_, err := tool.Execute(ctx, string(raw))
	if err != context.Canceled {
		t.Errorf("err = %v, want Canceled", err)
	}
}

func TestMultiWrite_MetadataInterface(t *testing.T) {
	tool, _ := mkMultiWriteTool(t, 10, 1024)
	if tool.Name() != "multi_write" {
		t.Errorf("Name = %q", tool.Name())
	}
	var m map[string]any
	if err := json.Unmarshal(tool.Parameters(), &m); err != nil {
		t.Errorf("Parameters not valid JSON: %v", err)
	}
}
