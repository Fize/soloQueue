//go:build !windows

package server

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestComputeChangeRevisionDoesNotFollowSymlinkOrReadSpecialFile(t *testing.T) {
	workDir := t.TempDir()
	symlinkPath := filepath.Join(workDir, "link")
	if err := os.Symlink("/dev/zero", symlinkPath); err != nil {
		t.Fatal(err)
	}
	change := FileChange{Path: "link", Status: "modified"}
	first := computeChangeRevision(workDir, "HEAD", change)
	if err := os.Remove(symlinkPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/dev/null", symlinkPath); err != nil {
		t.Fatal(err)
	}
	second := computeChangeRevision(workDir, "HEAD", change)
	if first == second {
		t.Fatal("expected symlink target text to affect revision")
	}

	if err := syscall.Mkfifo(filepath.Join(workDir, "pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if revision := computeChangeRevision(workDir, "HEAD", FileChange{Path: "pipe", Status: "modified"}); revision == "" {
		t.Fatal("expected metadata-only revision for special file")
	}
}
