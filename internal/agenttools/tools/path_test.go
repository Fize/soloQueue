package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAbsPath_Happy(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "sub", "a.txt")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	abs, err := absPath(target, "")
	if err != nil {
		t.Fatalf("absPath: %v", err)
	}
	if abs != target {
		t.Errorf("abs = %q, want %q", abs, target)
	}
}

func TestAbsPath_RelativePath(t *testing.T) {
	// relative paths should be resolved against CWD
	abs, err := absPath(".", "")
	if err != nil {
		t.Fatalf("absPath('.'): %v", err)
	}
	if !filepath.IsAbs(abs) {
		t.Errorf("abs = %q, want absolute path", abs)
	}
}

func TestAbsPath_TraversalCleaned(t *testing.T) {
	dir := t.TempDir()
	// ../../etc/passwd from inside dir — filepath.Abs+Clean kills the ..
	traversal := filepath.Join(dir, "..", "..", "etc", "passwd")
	abs, err := absPath(traversal, "")
	if err != nil {
		t.Fatalf("absPath: %v", err)
	}
	// The result should be cleaned (no ..) and absolute
	if !filepath.IsAbs(abs) {
		t.Errorf("abs = %q, want absolute path", abs)
	}
}

func TestAbsPath_EmptyPath(t *testing.T) {
	_, err := absPath("", "")
	if err == nil {
		t.Error("empty path should error")
	}
	_, err = absPath("", "/some/workdir")
	if err == nil {
		t.Error("empty path should error regardless of workDir")
	}
}

func TestAbsPath_RelativeWithWorkDir(t *testing.T) {
	// Relative path resolved against workDir
	abs, err := absPath("report.md", "/home/user/.soloqueue")
	if err != nil {
		t.Fatalf("absPath: %v", err)
	}
	want := "/home/user/.soloqueue/report.md"
	if abs != want {
		t.Errorf("abs = %q, want %q", abs, want)
	}
}

func TestAbsPath_RelativeSubdirWithWorkDir(t *testing.T) {
	// Relative subdirectory path resolved against workDir
	abs, err := absPath("subdir/report.md", "/home/user/.soloqueue")
	if err != nil {
		t.Fatalf("absPath: %v", err)
	}
	want := "/home/user/.soloqueue/subdir/report.md"
	if abs != want {
		t.Errorf("abs = %q, want %q", abs, want)
	}
}

func TestAbsPath_AbsoluteIgnoresWorkDir(t *testing.T) {
	// Absolute path ignores workDir
	abs, err := absPath("/tmp/foo.txt", "/home/user/.soloqueue")
	if err != nil {
		t.Fatalf("absPath: %v", err)
	}
	if abs != "/tmp/foo.txt" {
		t.Errorf("abs = %q, want /tmp/foo.txt", abs)
	}
}

func TestAbsPath_TildeExpansionWithWorkDir(t *testing.T) {
	// ~/ paths expand via home dir, workDir is irrelevant since result is absolute
	abs, err := absPath("~/myfile.txt", "/some/workdir")
	if err != nil {
		t.Fatalf("absPath: %v", err)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, "myfile.txt")
	if abs != want {
		t.Errorf("abs = %q, want %q", abs, want)
	}
}

func TestAbsPath_WorkDirEmptyFallbackCWD(t *testing.T) {
	// Empty workDir falls back to process CWD (matching previous behavior)
	abs, err := absPath(".", "")
	if err != nil {
		t.Fatalf("absPath('.', ''): %v", err)
	}
	cwd, _ := os.Getwd()
	if abs != filepath.Clean(cwd) {
		t.Errorf("abs = %q, want CWD = %q", abs, cwd)
	}
}

func TestAbsPath_TraversalCleanedWithWorkDir(t *testing.T) {
	// Path traversal is cleaned when workDir is provided
	abs, err := absPath("sub/../report.md", "/home/user/.soloqueue")
	if err != nil {
		t.Fatalf("absPath: %v", err)
	}
	want := "/home/user/.soloqueue/report.md"
	if abs != want {
		t.Errorf("abs = %q, want %q", abs, want)
	}
}

func TestAbsPath_WorkDirPathItselfRelative(t *testing.T) {
	// workDir itself might be relative — relative input + relative workDir still resolves via filepath.Join + filepath.Abs
	abs, err := absPath("file.txt", "relative/dir")
	if err != nil {
		t.Fatalf("absPath: %v", err)
	}
	if !filepath.IsAbs(abs) {
		t.Errorf("abs = %q, want absolute path", abs)
	}
	if !strings.HasSuffix(abs, "relative/dir/file.txt") {
		t.Errorf("abs = %q, want suffix 'relative/dir/file.txt'", abs)
	}
}

func TestAbsPath_AnyPathAllowed(t *testing.T) {
	// Any absolute path should be normalizable.
	abs, err := absPath("/etc/passwd", "")
	if err != nil {
		t.Fatalf("absPath('/etc/passwd'): %v", err)
	}
	if abs != "/etc/passwd" {
		t.Errorf("abs = %q, want /etc/passwd", abs)
	}
}
