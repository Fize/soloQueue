package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// helper: construct a Read tool backed by a temp dir
func mkFileReadTool(t *testing.T, maxSize int64) (*fileReadTool, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := Config{
		MaxFileSize: maxSize,
	}
	return newFileReadTool(cfg), dir
}

func TestFileRead_Happy(t *testing.T) {
	tool, dir := mkFileReadTool(t, 1024)
	path := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(path, []byte("hello world"), 0o644)

	args, _ := json.Marshal(map[string]string{"path": path})
	out, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var res fileReadResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if res.Content != "hello world" {
		t.Errorf("content = %q", res.Content)
	}
	if res.Size != 11 {
		t.Errorf("size = %d, want 11", res.Size)
	}
	if res.Path != path {
		t.Errorf("path = %q, want %q", res.Path, path)
	}
}

func TestFileRead_TooLarge(t *testing.T) {
	// Read tool no longer rejects by MaxFileSize — instead it truncates by token limit.
	// A 25KB file (~25000 chars) should fit within ReadDefaultMaxTokens (~8000 tokens).
	// A 200KB file (~200000 chars ≈ 60K+ tokens) should exceed the limit and be truncated.
	big := strings.Repeat("hello world this is test content for truncation check. ", 5000)
	tool, dir := mkFileReadTool(t, 100<<20)
	path := filepath.Join(dir, "big.txt")
	_ = os.WriteFile(path, []byte(big), 0o644)
	args, _ := json.Marshal(map[string]string{"path": path})
	out, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var res fileReadResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !res.Truncated {
		t.Error("expected truncated=true for file exceeding token limit")
	}
	if res.Error == "" {
		t.Error("expected error message about token limit")
	}
	if len(res.Content) < 1000 {
		t.Error("truncated content should still be substantial")
	}
}

func TestFileRead_Offset(t *testing.T) {
	tool, dir := mkFileReadTool(t, 100<<20)
	content := "abcdefghijklmnopqrstuvwxyz"
	path := filepath.Join(dir, "offset.txt")
	_ = os.WriteFile(path, []byte(content), 0o644)

	args, _ := json.Marshal(map[string]any{"path": path, "offset": 5})
	out, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var res fileReadResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if res.Content != "fghijklmnopqrstuvwxyz" {
		t.Errorf("content = %q, want 'fghijklmnopqrstuvwxyz'", res.Content)
	}
	if res.Size != 26 {
		t.Errorf("size = %d, want 26", res.Size)
	}
}

func TestFileRead_LimitParam(t *testing.T) {
	tool, dir := mkFileReadTool(t, 100<<20)
	path := filepath.Join(dir, "limit.txt")
	_ = os.WriteFile(path, []byte("hello world this is a test file"), 0o644)

	args, _ := json.Marshal(map[string]any{"path": path, "limit": 1})
	out, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var res fileReadResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !res.Truncated {
		t.Error("expected truncated=true for limit=1")
	}
}

func TestFileRead_NotFound(t *testing.T) {
	tool, dir := mkFileReadTool(t, 1024)
	args, _ := json.Marshal(map[string]string{"path": filepath.Join(dir, "missing.txt")})
	_, err := tool.Execute(context.Background(), string(args))
	if err == nil {
		t.Error("missing file should error")
	}
}

func TestFileRead_BinaryRejected(t *testing.T) {
	tool, dir := mkFileReadTool(t, 1024)
	path := filepath.Join(dir, "bin.dat")
	_ = os.WriteFile(path, []byte("pre\x00post"), 0o644)
	args, _ := json.Marshal(map[string]string{"path": path})
	_, err := tool.Execute(context.Background(), string(args))
	if err == nil || !strings.Contains(err.Error(), "binary") {
		t.Errorf("err = %v, want binary content", err)
	}
}

func TestFileRead_InvalidJSON(t *testing.T) {
	tool, _ := mkFileReadTool(t, 1024)
	_, err := tool.Execute(context.Background(), `{not json`)
	if err == nil || !strings.Contains(err.Error(), "invalid arguments") {
		t.Errorf("err = %v, want invalid arguments", err)
	}
}

func TestFileRead_EmptyPath(t *testing.T) {
	tool, _ := mkFileReadTool(t, 1024)
	_, err := tool.Execute(context.Background(), `{"path":""}`)
	if err == nil || !strings.Contains(err.Error(), "path is empty") {
		t.Errorf("err = %v, want path is empty", err)
	}
}

func TestFileRead_CtxAlreadyCanceled(t *testing.T) {
	tool, dir := mkFileReadTool(t, 1024)
	path := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(path, []byte("x"), 0o644)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // preemptively
	args, _ := json.Marshal(map[string]string{"path": path})
	_, err := tool.Execute(ctx, string(args))
	if err != context.Canceled {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestFileRead_MetadataInterface(t *testing.T) {
	tool, _ := mkFileReadTool(t, 1024)
	if tool.Name() != "Read" {
		t.Errorf("Name = %q", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("Description should not be empty")
	}
	if len(tool.Parameters()) == 0 {
		t.Error("Parameters should not be empty")
	}
	// parameters must be valid JSON
	var m map[string]any
	if err := json.Unmarshal(tool.Parameters(), &m); err != nil {
		t.Errorf("Parameters not valid JSON: %v", err)
	}
}
