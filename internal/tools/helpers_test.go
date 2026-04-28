package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ─── atomicWrite ───────────────────────────────────────────────────────

func TestAtomicWrite_NewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	created, err := atomicWrite(path, []byte("hello"), true)
	if err != nil {
		t.Fatalf("atomicWrite: %v", err)
	}
	if !created {
		t.Errorf("created = false, want true for new file")
	}
	got, _ := os.ReadFile(path)
	if string(got) != "hello" {
		t.Errorf("content = %q", got)
	}
}

func TestAtomicWrite_Overwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	created, err := atomicWrite(path, []byte("new"), true)
	if err != nil {
		t.Fatalf("atomicWrite: %v", err)
	}
	if created {
		t.Errorf("created = true, want false for overwrite")
	}
	got, _ := os.ReadFile(path)
	if string(got) != "new" {
		t.Errorf("content = %q", got)
	}
}

func TestAtomicWrite_OverwriteFalseRejectsExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(path, []byte("old"), 0o644)
	_, err := atomicWrite(path, []byte("new"), false)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("err = %v, want file already exists", err)
	}
	// content unchanged
	got, _ := os.ReadFile(path)
	if string(got) != "old" {
		t.Errorf("content changed: %q", got)
	}
}

func TestAtomicWrite_ParentMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexist", "a.txt")
	_, err := atomicWrite(path, []byte("x"), true)
	if err == nil || !strings.Contains(err.Error(), "parent directory missing") {
		t.Errorf("err = %v, want parent directory missing", err)
	}
}

func TestAtomicWrite_NoTmpResidueOnSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	_, err := atomicWrite(path, []byte("x"), true)
	if err != nil {
		t.Fatalf("atomicWrite: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".soloqueue-tmp-") {
			t.Errorf("leftover tmp file: %s", e.Name())
		}
	}
}

// ─── looksBinary ───────────────────────────────────────────────────────

func TestLooksBinary(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want bool
	}{
		{"empty", []byte(""), false},
		{"utf8", []byte("hello world 中文"), false},
		{"nul", []byte("before\x00after"), true},
		{"nul_late_within_512", append(make([]byte, 500), 0), true},
		{"nul_after_512", append(bytes1k(), 0), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := looksBinary(c.in); got != c.want {
				t.Errorf("looksBinary = %v, want %v", got, c.want)
			}
		})
	}
}

// bytes1k returns 1024 bytes of 'a' for testing
func bytes1k() []byte {
	b := make([]byte, 1024)
	for i := range b {
		b[i] = 'a'
	}
	return b
}

// ─── readFileCapped ────────────────────────────────────────────────────

func TestReadFileCapped_Happy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(path, []byte("hello"), 0o644)
	data, err := readFileCapped(path, 100)
	if err != nil {
		t.Fatalf("readFileCapped: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("content = %q", data)
	}
}

func TestReadFileCapped_TooLarge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.txt")
	_ = os.WriteFile(path, bytes1k(), 0o644)
	_, err := readFileCapped(path, 100)
	if err == nil || !strings.Contains(err.Error(), "file too large") {
		t.Errorf("err = %v, want file too large", err)
	}
}

func TestReadFileCapped_NoLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.txt")
	_ = os.WriteFile(path, bytes1k(), 0o644)
	data, err := readFileCapped(path, 0)
	if err != nil {
		t.Fatalf("readFileCapped no-limit: %v", err)
	}
	if len(data) != 1024 {
		t.Errorf("len = %d, want 1024", len(data))
	}
}

func TestReadFileCapped_Missing(t *testing.T) {
	_, err := readFileCapped("/nonexistent/path/xyz.txt", 100)
	if err == nil {
		t.Error("missing file should error")
	}
}

// ─── ctxErrOrNil ───────────────────────────────────────────────────────

func TestCtxErrOrNil(t *testing.T) {
	if err := ctxErrOrNil(nil); err != nil {
		t.Errorf("ctxErrOrNil(nil) = %v, want nil", err)
	}
}
