package lsp

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFileURIEncodesAndRoundTripsPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "目录 with space.go")
	uri := PathToURI(path)
	if strings.Contains(uri, " ") {
		t.Fatalf("URI contains unescaped space: %q", uri)
	}
	if got := uriToPath(uri); got != path {
		t.Fatalf("round trip = %q, want %q", got, path)
	}
}
