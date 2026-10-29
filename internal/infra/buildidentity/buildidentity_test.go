package buildidentity

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileIdentityChangesWithContents(t *testing.T) {
	path := filepath.Join(t.TempDir(), "program")
	if err := os.WriteFile(path, []byte("first build"), 0600); err != nil {
		t.Fatal(err)
	}
	first, err := File(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("second build"), 0600); err != nil {
		t.Fatal(err)
	}
	second, err := File(path)
	if err != nil || first == second {
		t.Fatalf("identity did not change: %v", err)
	}
}
