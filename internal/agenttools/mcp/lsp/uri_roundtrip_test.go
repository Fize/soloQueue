package lsp

import "github.com/xiaobaitu/soloqueue/internal/testfixtures"
import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFileURIEncodesAndRoundTripsPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), testfixtures.Chinese("uri_roundtrip_test_001"))
	uri := PathToURI(path)
	if strings.Contains(uri, " ") {
		t.Fatalf("URI contains unescaped space: %q", uri)
	}
	if got := uriToPath(uri); got != path {
		t.Fatalf("round trip = %q, want %q", got, path)
	}
}
