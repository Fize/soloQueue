package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveSandbox_Happy(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "sub", "a.txt")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	abs, err := resolveSandbox([]string{dir}, target)
	if err != nil {
		t.Fatalf("resolveSandbox: %v", err)
	}
	if abs != target {
		t.Errorf("abs = %q, want %q", abs, target)
	}
}

func TestResolveSandbox_Equal(t *testing.T) {
	dir := t.TempDir()
	// requesting the root itself is OK
	abs, err := resolveSandbox([]string{dir}, dir)
	if err != nil {
		t.Fatalf("resolveSandbox(root): %v", err)
	}
	if abs != dir {
		t.Errorf("abs = %q, want %q", abs, dir)
	}
}

func TestResolveSandbox_OutsideRejected(t *testing.T) {
	dir := t.TempDir()
	_, err := resolveSandbox([]string{dir}, "/etc/passwd")
	if err == nil || !strings.Contains(err.Error(), "out of sandbox") {
		t.Errorf("err = %v, want out-of-sandbox", err)
	}
}

func TestResolveSandbox_PrefixMismatch(t *testing.T) {
	// /tmp/foo should NOT match when allowed root is /tmp/foobar
	parent := t.TempDir()
	foobar := filepath.Join(parent, "foobar")
	foo := filepath.Join(parent, "foo")
	for _, d := range []string{foobar, foo} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	target := filepath.Join(foo, "a.txt")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := resolveSandbox([]string{foobar}, target)
	if err == nil {
		t.Error("foo/a.txt should NOT match sandbox /tmp/foobar (prefix confusion)")
	}
}

func TestResolveSandbox_TraversalAttempt(t *testing.T) {
	dir := t.TempDir()
	// ../../etc/passwd from inside dir — filepath.Abs+Clean kills the ..
	traversal := filepath.Join(dir, "..", "..", "etc", "passwd")
	_, err := resolveSandbox([]string{dir}, traversal)
	if err == nil {
		t.Error("traversal should be rejected")
	}
}

func TestResolveSandbox_EmptyPath(t *testing.T) {
	_, err := resolveSandbox([]string{t.TempDir()}, "")
	if err == nil {
		t.Error("empty path should error")
	}
}

func TestResolveSandbox_EmptyAllowList(t *testing.T) {
	_, err := resolveSandbox(nil, "/tmp/anything")
	if err == nil {
		t.Error("empty AllowedDirs should reject all paths")
	}
}
