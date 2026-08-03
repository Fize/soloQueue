package workdir

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeExistingDirExpandsTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	got, err := NormalizeExistingDir("~")
	if err != nil {
		t.Fatalf("NormalizeExistingDir: %v", err)
	}
	want, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Clean(want) {
		t.Fatalf("NormalizeExistingDir(~) = %q, want %q", got, want)
	}
}

func TestExpandHomeSubpath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	got, err := expandHome("~/github.com/soloQueue")
	if err != nil {
		t.Fatalf("expandHome: %v", err)
	}
	want := filepath.Join(home, "github.com/soloQueue")
	if got != want {
		t.Fatalf("expandHome = %q, want %q", got, want)
	}
}

func TestNormalizeExistingDirRejectsMissingAndFiles(t *testing.T) {
	dir := t.TempDir()
	if _, err := NormalizeExistingDir(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("expected missing directory error")
	}

	file := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NormalizeExistingDir(file); err == nil {
		t.Fatal("expected non-directory error")
	}
}
