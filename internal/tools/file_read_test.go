package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// helper: construct a file_read tool backed by a temp sandbox
func mkFileReadTool(t *testing.T, maxSize int64) (*fileReadTool, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := Config{
		AllowedDirs: []string{dir},
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

func TestFileRead_OutOfSandbox(t *testing.T) {
	tool, _ := mkFileReadTool(t, 1024)
	args := `{"path":"/etc/passwd"}`
	_, err := tool.Execute(context.Background(), args)
	if err == nil || !strings.Contains(err.Error(), "out of sandbox") {
		t.Errorf("err = %v, want out-of-sandbox", err)
	}
}

func TestFileRead_TooLarge(t *testing.T) {
	tool, dir := mkFileReadTool(t, 10)
	path := filepath.Join(dir, "big.txt")
	_ = os.WriteFile(path, []byte("this is way more than ten bytes"), 0o644)
	args, _ := json.Marshal(map[string]string{"path": path})
	_, err := tool.Execute(context.Background(), string(args))
	if err == nil || !strings.Contains(err.Error(), "file too large") {
		t.Errorf("err = %v, want file too large", err)
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
	if tool.Name() != "file_read" {
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
