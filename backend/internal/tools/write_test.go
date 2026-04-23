package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// ─── Helpers ─────────────────────────────────────────────────────────────────

func mkWriteFileTool(t *testing.T, maxSize int64) (*writeFileTool, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := Config{
		AllowedDirs:  []string{dir},
		MaxWriteSize: maxSize,
	}
	return newWriteFileTool(cfg), dir
}

func callWriteFile(t *testing.T, tool *writeFileTool, a writeFileArgs) (writeFileResult, error) {
	t.Helper()
	raw, _ := json.Marshal(a)
	out, err := tool.Execute(context.Background(), string(raw))
	if err != nil {
		return writeFileResult{}, err
	}
	var r writeFileResult
	if uerr := json.Unmarshal([]byte(out), &r); uerr != nil {
		t.Fatalf("unmarshal: %v", uerr)
	}
	return r, nil
}

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

// ─── Single write tests ──────────────────────────────────────────────────────

func TestWriteFile_NewFile(t *testing.T) {
	tool, dir := mkWriteFileTool(t, 1024)
	path := filepath.Join(dir, "a.txt")
	res, err := callWriteFile(t, tool, writeFileArgs{Path: path, Content: "hello"})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if !res.Created {
		t.Error("created should be true")
	}
	got, _ := os.ReadFile(path)
	if string(got) != "hello" {
		t.Errorf("content = %q", got)
	}
}

func TestWriteFile_Overwrite(t *testing.T) {
	tool, dir := mkWriteFileTool(t, 1024)
	path := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(path, []byte("old"), 0o644)
	res, err := callWriteFile(t, tool, writeFileArgs{Path: path, Content: "new"})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if res.Created {
		t.Error("created should be false on overwrite")
	}
	got, _ := os.ReadFile(path)
	if string(got) != "new" {
		t.Errorf("content = %q", got)
	}
}

func TestWriteFile_OverwriteFalseRejectsExisting(t *testing.T) {
	tool, dir := mkWriteFileTool(t, 1024)
	path := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(path, []byte("old"), 0o644)
	ov := false
	_, err := callWriteFile(t, tool, writeFileArgs{Path: path, Content: "new", Overwrite: &ov})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("err = %v, want already exists", err)
	}
	// content unchanged
	got, _ := os.ReadFile(path)
	if string(got) != "old" {
		t.Errorf("content changed: %q", got)
	}
}

func TestWriteFile_ParentDirMissing(t *testing.T) {
	tool, dir := mkWriteFileTool(t, 1024)
	path := filepath.Join(dir, "nonexist", "a.txt")
	_, err := callWriteFile(t, tool, writeFileArgs{Path: path, Content: "x"})
	if err == nil || !strings.Contains(err.Error(), "parent directory missing") {
		t.Errorf("err = %v, want parent dir missing", err)
	}
}

func TestWriteFile_OutOfSandbox(t *testing.T) {
	tool, _ := mkWriteFileTool(t, 1024)
	raw, _ := json.Marshal(writeFileArgs{Path: "/etc/evil.txt", Content: "x"})
	_, err := tool.Execute(context.Background(), string(raw))
	if err == nil || !strings.Contains(err.Error(), "out of sandbox") {
		t.Errorf("err = %v, want out-of-sandbox", err)
	}
}

func TestWriteFile_ContentTooLarge(t *testing.T) {
	tool, dir := mkWriteFileTool(t, 5)
	_, err := callWriteFile(t, tool, writeFileArgs{
		Path:    filepath.Join(dir, "big.txt"),
		Content: "this is way more than five bytes",
	})
	if err == nil || !strings.Contains(err.Error(), "content too large") {
		t.Errorf("err = %v, want content too large", err)
	}
}

func TestWriteFile_NoTmpLeftOnSuccess(t *testing.T) {
	tool, dir := mkWriteFileTool(t, 1024)
	_, err := callWriteFile(t, tool, writeFileArgs{Path: filepath.Join(dir, "a.txt"), Content: "x"})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".soloqueue-tmp-") {
			t.Errorf("leftover tmp: %s", e.Name())
		}
	}
}

// TestWriteFile_ConcurrentWritesSamePath: two goroutines writing to same file;
// both succeed (last-write-wins), no tmp residue.
func TestWriteFile_ConcurrentWritesSamePath(t *testing.T) {
	tool, dir := mkWriteFileTool(t, 1024)
	path := filepath.Join(dir, "race.txt")
	const N = 10
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			_, err := callWriteFile(t, tool, writeFileArgs{
				Path:    path,
				Content: fmt.Sprintf("content-%d", i),
			})
			if err != nil {
				t.Errorf("goroutine %d: %v", i, err)
			}
		}()
	}
	wg.Wait()

	// file exists, content is one of the N
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("final read: %v", err)
	}
	if !strings.HasPrefix(string(got), "content-") {
		t.Errorf("content = %q, want 'content-N'", got)
	}
	// no tmp leftovers
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".soloqueue-tmp-") {
			t.Errorf("leftover tmp after concurrent writes: %s", e.Name())
		}
	}
}

func TestWriteFile_EmptyPath(t *testing.T) {
	tool, _ := mkWriteFileTool(t, 1024)
	_, err := tool.Execute(context.Background(), `{"path":"","content":"x"}`)
	if err == nil {
		t.Error("empty path should error")
	}
}

func TestWriteFile_InvalidJSON(t *testing.T) {
	tool, _ := mkWriteFileTool(t, 1024)
	_, err := tool.Execute(context.Background(), `{not json`)
	if err == nil {
		t.Error("invalid JSON should error")
	}
}

func TestWriteFile_CtxPreCanceled(t *testing.T) {
	tool, dir := mkWriteFileTool(t, 1024)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	raw, _ := json.Marshal(writeFileArgs{Path: filepath.Join(dir, "a.txt"), Content: "x"})
	_, err := tool.Execute(ctx, string(raw))
	if err != context.Canceled {
		t.Errorf("err = %v, want Canceled", err)
	}
}

func TestWriteFile_MetadataInterface(t *testing.T) {
	tool, _ := mkWriteFileTool(t, 1024)
	if tool.Name() != "write_file" {
		t.Errorf("Name = %q", tool.Name())
	}
	var m map[string]any
	if err := json.Unmarshal(tool.Parameters(), &m); err != nil {
		t.Errorf("Parameters not valid JSON: %v", err)
	}
}

// ─── Multi-write tests ───────────────────────────────────────────────────────

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
