package speechinstall

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallDecoderCopiesBundledExecutable(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "release", "silk-decoder.exe")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("decoder"), 0o755); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "work", "bin")
	dest, err := InstallDecoder(src, root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != "decoder" {
		t.Fatalf("contents %q: %v", got, err)
	}
}
