package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func mkGlobTool(t *testing.T, max int) (*globTool, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := Config{
		AllowedDirs:  []string{dir},
		MaxGlobItems: max,
	}
	return newGlobTool(cfg), dir
}

func runGlob(t *testing.T, tool *globTool, a globArgs) globResult {
	t.Helper()
	raw, _ := json.Marshal(a)
	out, err := tool.Execute(context.Background(), string(raw))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var r globResult
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return r
}

func TestGlob_Happy(t *testing.T) {
	tool, dir := mkGlobTool(t, 1000)
	writeFile(t, filepath.Join(dir, "a.go"), "")
	writeFile(t, filepath.Join(dir, "sub", "b.go"), "")
	writeFile(t, filepath.Join(dir, "sub", "c.py"), "")

	res := runGlob(t, tool, globArgs{Pattern: "**/*.go", Dir: dir})
	if len(res.Files) != 2 {
		t.Fatalf("files = %v", res.Files)
	}
	// ensure paths use / separator
	for _, f := range res.Files {
		if strings.Contains(f, "\\") {
			t.Errorf("path contains backslash: %q", f)
		}
	}
}

func TestGlob_TopLevelOnly(t *testing.T) {
	tool, dir := mkGlobTool(t, 1000)
	writeFile(t, filepath.Join(dir, "a.go"), "")
	writeFile(t, filepath.Join(dir, "sub", "b.go"), "")

	res := runGlob(t, tool, globArgs{Pattern: "*.go", Dir: dir})
	if len(res.Files) != 1 || res.Files[0] != "a.go" {
		t.Errorf("res = %v", res.Files)
	}
}

func TestGlob_NoMatches(t *testing.T) {
	tool, dir := mkGlobTool(t, 1000)
	writeFile(t, filepath.Join(dir, "a.txt"), "")
	res := runGlob(t, tool, globArgs{Pattern: "**/*.go", Dir: dir})
	if len(res.Files) != 0 {
		t.Errorf("expected no matches, got %v", res.Files)
	}
}

func TestGlob_InvalidPattern(t *testing.T) {
	tool, dir := mkGlobTool(t, 1000)
	raw, _ := json.Marshal(globArgs{Pattern: "[unclosed", Dir: dir})
	_, err := tool.Execute(context.Background(), string(raw))
	if err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Errorf("err = %v, want invalid", err)
	}
}

func TestGlob_OutOfSandbox(t *testing.T) {
	tool, _ := mkGlobTool(t, 1000)
	raw, _ := json.Marshal(globArgs{Pattern: "**/*", Dir: "/etc"})
	_, err := tool.Execute(context.Background(), string(raw))
	if err == nil || !strings.Contains(err.Error(), "out of sandbox") {
		t.Errorf("err = %v, want out-of-sandbox", err)
	}
}

func TestGlob_Truncation(t *testing.T) {
	tool, dir := mkGlobTool(t, 5)
	for i := 0; i < 20; i++ {
		writeFile(t, filepath.Join(dir, fmt.Sprintf("f%02d.txt", i)), "")
	}
	res := runGlob(t, tool, globArgs{Pattern: "**/*.txt", Dir: dir})
	if len(res.Files) != 5 {
		t.Errorf("files = %d, want 5", len(res.Files))
	}
	if !res.Truncated {
		t.Error("truncated should be true")
	}
}

func TestGlob_DirMustBeDirectory(t *testing.T) {
	tool, dir := mkGlobTool(t, 1000)
	path := filepath.Join(dir, "a.txt")
	writeFile(t, path, "x")
	raw, _ := json.Marshal(globArgs{Pattern: "*", Dir: path})
	_, err := tool.Execute(context.Background(), string(raw))
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("err = %v, want 'not a directory'", err)
	}
}

func TestGlob_EmptyArgs(t *testing.T) {
	tool, dir := mkGlobTool(t, 1000)
	_, err := tool.Execute(context.Background(), `{"pattern":"","dir":"`+dir+`"}`)
	if err == nil {
		t.Error("empty pattern should error")
	}
	_, err = tool.Execute(context.Background(), `{"pattern":"**/*","dir":""}`)
	if err == nil {
		t.Error("empty dir should error")
	}
}

func TestGlob_InvalidJSON(t *testing.T) {
	tool, _ := mkGlobTool(t, 1000)
	_, err := tool.Execute(context.Background(), `{not json`)
	if err == nil {
		t.Error("invalid JSON should error")
	}
}

func TestGlob_CtxPreCanceled(t *testing.T) {
	tool, dir := mkGlobTool(t, 1000)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	raw, _ := json.Marshal(globArgs{Pattern: "**/*", Dir: dir})
	_, err := tool.Execute(ctx, string(raw))
	if err != context.Canceled {
		t.Errorf("err = %v, want Canceled", err)
	}
}

func TestGlob_MetadataInterface(t *testing.T) {
	tool, _ := mkGlobTool(t, 1000)
	if tool.Name() != "Glob" {
		t.Errorf("Name = %q", tool.Name())
	}
	var m map[string]any
	if err := json.Unmarshal(tool.Parameters(), &m); err != nil {
		t.Errorf("Parameters not valid JSON: %v", err)
	}
}
