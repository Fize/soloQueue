package server

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenChangeFileNoFollow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "change.txt")
	if err := os.WriteFile(path, []byte("change contents"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := openChangeFileNoFollow(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	content, err := io.ReadAll(f)
	if err != nil || string(content) != "change contents" {
		t.Fatalf("regular file read: %q, %v", content, err)
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(path, link); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	// Bypass caller Lstat on purpose: the final open must reject a swapped link.
	opened, err := openChangeFileNoFollow(link)
	if err == nil {
		_ = opened.Close()
		t.Fatal("open primitive followed a symlink")
	}
}
