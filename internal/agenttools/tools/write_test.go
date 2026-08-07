package tools

import (
	"context"
	"encoding/json"
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

func TestWriteFile_PlanDirAutoMkdirAll(t *testing.T) {
	dir := t.TempDir()
	planDir := filepath.Join(dir, "plan")
	cfg := Config{
		MaxWriteSize: 1024,
		PlanDir:      planDir,
	}
	tool := newWriteFileTool(cfg)

	// Write to a subdirectory under plan/ that doesn't exist yet
	path := filepath.Join(planDir, "feature-name", "design.md")
	res, err := callWriteFile(t, tool, writeFileArgs{Path: path, Content: "# Design"})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if !res.Created {
		t.Error("created should be true")
	}
	got, _ := os.ReadFile(path)
	if string(got) != "# Design" {
		t.Errorf("content = %q", got)
	}
}

func TestWriteFile_PlanDirAutoMkdirAll_NestedPath(t *testing.T) {
	dir := t.TempDir()
	planDir := filepath.Join(dir, "plan")
	cfg := Config{
		MaxWriteSize: 1024,
		PlanDir:      planDir,
	}
	tool := newWriteFileTool(cfg)

	// Write to a deeply nested path under plan/
	path := filepath.Join(planDir, "deep", "nested", "subdir", "doc.md")
	res, err := callWriteFile(t, tool, writeFileArgs{Path: path, Content: "deep"})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if !res.Created {
		t.Error("created should be true")
	}
	got, _ := os.ReadFile(path)
	if string(got) != "deep" {
		t.Errorf("content = %q", got)
	}
}

func TestWriteFile_PlanDirNotSet_StillRejectsMissingParent(t *testing.T) {
	// When PlanDir is empty, missing parent should still be rejected
	tool, dir := mkWriteFileTool(t, 1024)
	path := filepath.Join(dir, "nonexist", "a.txt")
	_, err := callWriteFile(t, tool, writeFileArgs{Path: path, Content: "x"})
	if err == nil || !strings.Contains(err.Error(), "parent directory missing") {
		t.Errorf("err = %v, want parent dir missing", err)
	}
}

func TestWriteFile_PlanDir_OutsidePlanDir_StillRejectsMissingParent(t *testing.T) {
	dir := t.TempDir()
	planDir := filepath.Join(dir, "plan")
	cfg := Config{
		MaxWriteSize: 1024,
		PlanDir:      planDir,
	}
	tool := newWriteFileTool(cfg)

	// Writing outside plan dir with missing parent should still fail
	path := filepath.Join(dir, "outside", "a.txt")
	_, err := callWriteFile(t, tool, writeFileArgs{Path: path, Content: "x"})
	if err == nil || !strings.Contains(err.Error(), "parent directory missing") {
		t.Errorf("err = %v, want parent dir missing", err)
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
	if tool.Name() != "Write" {
		t.Errorf("Name = %q", tool.Name())
	}
	var m map[string]any
	if err := json.Unmarshal(tool.Parameters(), &m); err != nil {
		t.Errorf("Parameters not valid JSON: %v", err)
	}
}

// ─── Confirmable interface tests ────────────────────────────────────────────────────

func TestWriteFile_CheckConfirmation_AlwaysNeedsConfirm(t *testing.T) {
	tool, _ := mkWriteFileTool(t, 1024)
	raw, _ := json.Marshal(writeFileArgs{Path: "/tmp/test.go", Content: "hello world"})
	needs, prompt := tool.CheckConfirmation(string(raw))
	if !needs {
		t.Error("Write should always need confirmation")
	}
	if prompt == "" {
		t.Error("expected non-empty prompt")
	}
	if !strings.Contains(prompt, "test.go") {
		t.Errorf("prompt should contain path, got: %s", prompt)
	}
}

func TestWriteFile_CheckConfirmation_InvalidJSON(t *testing.T) {
	tool, _ := mkWriteFileTool(t, 1024)
	needs, prompt := tool.CheckConfirmation(`{not json`)
	if !needs {
		t.Error("should still need confirm even with invalid JSON")
	}
	if prompt == "" {
		t.Error("expected non-empty fallback prompt")
	}
}

func TestWriteFile_ConfirmationOptions_Binary(t *testing.T) {
	tool, _ := mkWriteFileTool(t, 1024)
	raw, _ := json.Marshal(writeFileArgs{Path: "a.go", Content: "x"})
	if opts := tool.ConfirmationOptions(string(raw)); opts != nil {
		t.Errorf("expected nil for binary confirm, got %v", opts)
	}
}

func TestWriteFile_ConfirmArgs_PreservesOriginal(t *testing.T) {
	tool, _ := mkWriteFileTool(t, 1024)
	original := `{"path":"a.go","content":"hello"}`
	for _, choice := range []ConfirmChoice{ChoiceApprove, ChoiceDeny, ChoiceAllowInSession} {
		got := tool.ConfirmArgs(original, choice)
		if got != original {
			t.Errorf("choice=%v: expected original preserved, got %s", choice, got)
		}
	}
}

func TestWriteFile_SupportsSessionWhitelist(t *testing.T) {
	tool, _ := mkWriteFileTool(t, 1024)
	if !tool.SupportsSessionWhitelist() {
		t.Error("should support session whitelist")
	}
}


