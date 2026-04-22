package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mkGrepTool(t *testing.T, max int, lineLen int) (*grepTool, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := Config{
		AllowedDirs: []string{dir},
		MaxMatches:  max,
		MaxLineLen:  lineLen,
	}
	return newGrepTool(cfg), dir
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runGrep(t *testing.T, tool *grepTool, a grepArgs) grepResult {
	t.Helper()
	raw, _ := json.Marshal(a)
	out, err := tool.Execute(context.Background(), string(raw))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var res grepResult
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return res
}

func TestGrep_HappyMatches(t *testing.T) {
	tool, dir := mkGrepTool(t, 100, 200)
	writeFile(t, filepath.Join(dir, "a.go"), "package x\nfunc hello() {}\n")
	writeFile(t, filepath.Join(dir, "b.go"), "package y\nfunc world() {}\n")

	res := runGrep(t, tool, grepArgs{Pattern: `func \w+`, Dir: dir})
	if len(res.Matches) != 2 {
		t.Fatalf("matches = %d, want 2", len(res.Matches))
	}
	if res.Truncated {
		t.Error("should not be truncated")
	}
}

func TestGrep_GlobFilter(t *testing.T) {
	tool, dir := mkGrepTool(t, 100, 200)
	writeFile(t, filepath.Join(dir, "a.go"), "keyword here\n")
	writeFile(t, filepath.Join(dir, "b.py"), "keyword here\n")

	res := runGrep(t, tool, grepArgs{Pattern: "keyword", Dir: dir, Glob: "**/*.go"})
	if len(res.Matches) != 1 {
		t.Fatalf("matches = %d, want 1", len(res.Matches))
	}
	if !strings.HasSuffix(res.Matches[0].File, "a.go") {
		t.Errorf("matched wrong file: %q", res.Matches[0].File)
	}
}

func TestGrep_DirOutsideSandbox(t *testing.T) {
	tool, _ := mkGrepTool(t, 100, 200)
	raw, _ := json.Marshal(grepArgs{Pattern: "x", Dir: "/etc"})
	_, err := tool.Execute(context.Background(), string(raw))
	if err == nil || !strings.Contains(err.Error(), "out of sandbox") {
		t.Errorf("err = %v, want out-of-sandbox", err)
	}
}

func TestGrep_InvalidRegex(t *testing.T) {
	tool, dir := mkGrepTool(t, 100, 200)
	raw, _ := json.Marshal(grepArgs{Pattern: "[unclosed", Dir: dir})
	_, err := tool.Execute(context.Background(), string(raw))
	if err == nil || !strings.Contains(err.Error(), "invalid pattern") {
		t.Errorf("err = %v, want invalid pattern", err)
	}
}

func TestGrep_Truncation(t *testing.T) {
	tool, dir := mkGrepTool(t, 3, 200)
	for i := 0; i < 10; i++ {
		writeFile(t, filepath.Join(dir, fmt.Sprintf("f%d.txt", i)), "hit\n")
	}
	res := runGrep(t, tool, grepArgs{Pattern: "hit", Dir: dir})
	if len(res.Matches) != 3 {
		t.Errorf("matches = %d, want 3", len(res.Matches))
	}
	if !res.Truncated {
		t.Error("truncated should be true")
	}
}

func TestGrep_LineTruncation(t *testing.T) {
	tool, dir := mkGrepTool(t, 10, 20)
	longLine := strings.Repeat("a", 100) + "needle"
	writeFile(t, filepath.Join(dir, "a.txt"), longLine+"\n")
	res := runGrep(t, tool, grepArgs{Pattern: "needle", Dir: dir})
	if len(res.Matches) != 1 {
		t.Fatalf("matches = %d", len(res.Matches))
	}
	// line truncated to MaxLineLen + "…"
	if len(res.Matches[0].Text) > 24 {
		t.Errorf("line not truncated: %q", res.Matches[0].Text)
	}
	if !strings.HasSuffix(res.Matches[0].Text, "…") {
		t.Errorf("missing truncation marker: %q", res.Matches[0].Text)
	}
}

func TestGrep_SkipsBinaryFiles(t *testing.T) {
	tool, dir := mkGrepTool(t, 100, 200)
	writeFile(t, filepath.Join(dir, "text.txt"), "needle in text\n")
	// binary file containing "needle"
	bin := append([]byte("needle"), 0, 1, 2, 3)
	_ = os.WriteFile(filepath.Join(dir, "bin.dat"), bin, 0o644)
	res := runGrep(t, tool, grepArgs{Pattern: "needle", Dir: dir})
	if len(res.Matches) != 1 {
		t.Errorf("matches = %d, want 1 (binary skipped)", len(res.Matches))
	}
	if !strings.HasSuffix(res.Matches[0].File, "text.txt") {
		t.Errorf("matched %q, want text.txt", res.Matches[0].File)
	}
}

func TestGrep_CtxCancel(t *testing.T) {
	tool, dir := mkGrepTool(t, 10000, 200)
	// create lots of files
	for i := 0; i < 2000; i++ {
		writeFile(t, filepath.Join(dir, fmt.Sprintf("f%d.txt", i)), "line\n")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // preemptively canceled
	raw, _ := json.Marshal(grepArgs{Pattern: "line", Dir: dir})
	_, err := tool.Execute(ctx, string(raw))
	if err != context.Canceled {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestGrep_DirMustBeDirectory(t *testing.T) {
	tool, dir := mkGrepTool(t, 100, 200)
	path := filepath.Join(dir, "a.txt")
	writeFile(t, path, "x\n")
	raw, _ := json.Marshal(grepArgs{Pattern: "x", Dir: path})
	_, err := tool.Execute(context.Background(), string(raw))
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("err = %v, want 'not a directory'", err)
	}
}

func TestGrep_MetadataInterface(t *testing.T) {
	tool, _ := mkGrepTool(t, 100, 200)
	if tool.Name() != "grep" {
		t.Errorf("Name = %q", tool.Name())
	}
	if tool.Description() == "" {
		t.Error("Description empty")
	}
	var m map[string]any
	if err := json.Unmarshal(tool.Parameters(), &m); err != nil {
		t.Errorf("Parameters not valid JSON: %v", err)
	}
}
