package skill

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenSkillFileNoFollow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte("trusted entry"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := openSkillFileNoFollow(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	content, err := io.ReadAll(f)
	if err != nil || string(content) != "trusted entry" {
		t.Fatalf("regular file read: %q, %v", content, err)
	}
	// Call the open primitive directly: the earlier Lstat check in the reader
	// must not be the only thing that prevents following a swapped symlink.
	link := filepath.Join(dir, "link.md")
	if err := os.Symlink(path, link); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	opened, err := openSkillFileNoFollow(link)
	if err == nil {
		_ = opened.Close()
		t.Fatal("open primitive followed a symlink")
	}
}
