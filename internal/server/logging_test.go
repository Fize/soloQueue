package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHTTPAccessLogger_RotatesBySize(t *testing.T) {
	dir := t.TempDir()
	l, err := newHTTPAccessLogger(dir, 1, 15)
	if err != nil {
		t.Fatalf("newHTTPAccessLogger: %v", err)
	}
	defer l.Close()
	l.writer.SetMaxSize(200)

	h := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	for i := 0; i < 5; i++ {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		h.ServeHTTP(rr, req)
	}

	base := filepath.Join(dir, "access-"+time.Now().Format("2006-01-02"))
	if _, err := os.Stat(base + ".jsonl"); err != nil {
		t.Fatalf("main access log missing: %v", err)
	}
	if _, err := os.Stat(base + "-2.jsonl"); err != nil {
		t.Fatalf("rotated access log missing: %v", err)
	}
}
