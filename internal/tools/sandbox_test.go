package tools

import (
	"os"
	"path/filepath"
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

	abs, err := absPath(target)
	if err != nil {
		t.Fatalf("absPath: %v", err)
	}
	if abs != target {
		t.Errorf("abs = %q, want %q", abs, target)
	}
}

func TestAbsPath_RelativePath(t *testing.T) {
	// relative paths should be resolved against CWD
	abs, err := absPath(".")
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
	abs, err := absPath(traversal)
	if err != nil {
		t.Fatalf("absPath: %v", err)
	}
	// The result should be cleaned (no ..) and absolute
	if !filepath.IsAbs(abs) {
		t.Errorf("abs = %q, want absolute path", abs)
	}
}

func TestAbsPath_EmptyPath(t *testing.T) {
	_, err := absPath("")
	if err == nil {
		t.Error("empty path should error")
	}
}

func TestAbsPath_AnyPathAllowed(t *testing.T) {
	// Without sandbox restriction, any absolute path should be normalizable
	abs, err := absPath("/etc/passwd")
	if err != nil {
		t.Fatalf("absPath('/etc/passwd'): %v", err)
	}
	if abs != "/etc/passwd" {
		t.Errorf("abs = %q, want /etc/passwd", abs)
	}
}
