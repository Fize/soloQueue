package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/config"
)

func startTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := NewMux(t.TempDir(), nil, nil)
	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		srv.Close()
		mux.Close()
	})
	return srv
}

func TestHTTP_Health(t *testing.T) {
	srv := startTestServer(t)
	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("status = %d", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), "ok") {
		t.Errorf("body = %q", b)
	}
}

func TestHTTP_Auth(t *testing.T) {
	mux := NewMux(t.TempDir(), nil, nil, WithAuthConfig(config.AuthConfig{
		User:     "admin",
		Password: "password123",
	}))
	defer mux.Close()

	// 1. Access via localhost/127.0.0.1 from loopback IP -> Allowed (bypassed)
	{
		req := httptest.NewRequest("GET", "/api/auth/check", nil)
		req.Host = "localhost:8765"
		req.RemoteAddr = "127.0.0.1:12345"
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 OK for localhost loopback, got %d", rec.Code)
		}
	}

	// 2. Access via external IP (e.g. 192.168.1.100) -> 401 Unauthorized
	{
		req := httptest.NewRequest("GET", "/api/auth/check", nil)
		req.Host = "192.168.1.100:8765"
		req.RemoteAddr = "192.168.1.100:12345"
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 Unauthorized for external IP, got %d", rec.Code)
		}
	}

	// 3. Access via external IP spoofing Host header -> 401 Unauthorized
	{
		req := httptest.NewRequest("GET", "/api/auth/check", nil)
		req.Host = "localhost:8765"
		req.RemoteAddr = "192.168.1.100:12345"
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 Unauthorized for spoofed Host header, got %d", rec.Code)
		}
	}

	// 4. Access via external IP with correct Basic Auth -> 200 OK
	{
		req := httptest.NewRequest("GET", "/api/auth/check", nil)
		req.Host = "192.168.1.100:8765"
		req.RemoteAddr = "192.168.1.100:12345"
		req.SetBasicAuth("admin", "password123")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 OK with correct Basic Auth, got %d", rec.Code)
		}
	}
}
