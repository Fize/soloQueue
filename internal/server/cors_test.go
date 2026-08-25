package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTP_LoopbackCORS(t *testing.T) {
	mux := NewMux(t.TempDir(), nil)
	defer mux.Close()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Fatalf("allow origin = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("allow credentials = %q, want true", got)
	}
	if got := rec.Header().Get("Vary"); got != "Origin" {
		t.Fatalf("vary = %q, want Origin", got)
	}
}

func TestHTTP_LoopbackCORSPreflight(t *testing.T) {
	mux := NewMux(t.TempDir(), nil)
	defer mux.Close()

	req := httptest.NewRequest(http.MethodOptions, "/api/session/", nil)
	req.Header.Set("Origin", "http://127.0.0.1:5173")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	req.Header.Set("Access-Control-Request-Headers", "content-type")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://127.0.0.1:5173" {
		t.Fatalf("allow origin = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("allow credentials = %q, want true", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Fatal("missing allow methods")
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != "Content-Type, Accept" {
		t.Fatalf("allow headers = %q", got)
	}
}

func TestHTTP_NonLoopbackCORSIsDelegated(t *testing.T) {
	mux := NewMux(t.TempDir(), nil)
	defer mux.Close()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("allow origin = %q, want empty", got)
	}
	if got := rec.Header().Get("Vary"); got != "Origin" {
		t.Fatalf("vary = %q, want Origin", got)
	}
}

func TestWebSocketOriginPolicy(t *testing.T) {
	cases := []struct {
		name   string
		host   string
		origin string
		want   bool
	}{
		{name: "missing origin", host: "127.0.0.1:57647", want: true},
		{name: "standalone loopback", host: "127.0.0.1:57647", origin: "http://127.0.0.1:57648", want: true},
		{name: "nginx same host", host: "app.example.com", origin: "https://app.example.com", want: true},
		{name: "different host", host: "app.example.com", origin: "https://evil.example.com", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/ws", nil)
			req.Host = tc.host
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if got := checkWebSocketOrigin(req); got != tc.want {
				t.Fatalf("checkWebSocketOrigin() = %v, want %v", got, tc.want)
			}
		})
	}
}
