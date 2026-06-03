package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/proxy"
	"github.com/xiaobaitu/soloqueue/internal/teamstore"
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

	// 5. WebSocket: Access via localhost loopback -> Bypasses auth (returns 503 Service Unavailable because hub is nil, not 401)
	{
		req := httptest.NewRequest("GET", "/ws", nil)
		req.Host = "localhost:8765"
		req.RemoteAddr = "127.0.0.1:12345"
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("expected 503 Service Unavailable for localhost WebSocket loopback, got %d", rec.Code)
		}
	}

	// 6. WebSocket: Access via external IP -> 401 Unauthorized
	{
		req := httptest.NewRequest("GET", "/ws", nil)
		req.Host = "192.168.1.100:8765"
		req.RemoteAddr = "192.168.1.100:12345"
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 Unauthorized for external WebSocket access, got %d", rec.Code)
		}
	}

	// 7. WebSocket: Access via external IP with correct Basic Auth -> Bypasses auth (returns 503 Service Unavailable because hub is nil, not 401)
	{
		req := httptest.NewRequest("GET", "/ws", nil)
		req.Host = "192.168.1.100:8765"
		req.RemoteAddr = "192.168.1.100:12345"
		req.SetBasicAuth("admin", "password123")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("expected 503 Service Unavailable for authenticated external WebSocket access, got %d", rec.Code)
		}
	}
}

func TestHTTP_TeamAgents(t *testing.T) {
	tempDir := t.TempDir()
	groupsDir := filepath.Join(tempDir, "groups")
	agentsDir := filepath.Join(tempDir, "agents")
	_ = os.MkdirAll(groupsDir, 0755)
	_ = os.MkdirAll(agentsDir, 0755)

	store := teamstore.NewStore(groupsDir, agentsDir, nil)
	ctx := context.Background()

	// Create a team
	err := store.CreateTeam(ctx, &teamstore.Team{
		Name:        "Devs",
		Description: "Dev team",
	})
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}

	// Create an agent
	err = store.CreateAgent(ctx, &teamstore.Agent{
		Name:        "Alice",
		TeamName:    "Devs",
		Description: "Coder",
	})
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}

	mux := NewMux(tempDir, nil, nil, WithTeamStore(store))
	defer mux.Close()

	// 1. Test GET /api/teams
	{
		req := httptest.NewRequest("GET", "/api/teams", nil)
		req.Host = "localhost:8765"
		req.RemoteAddr = "127.0.0.1:12345"
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("GET /api/teams status = %d", rec.Code)
		}

		body, _ := io.ReadAll(rec.Body)
		var resp struct {
			Teams []struct {
				Name   string `json:"name"`
				Agents []struct {
					Name string `json:"name"`
				} `json:"agents"`
			} `json:"teams"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			t.Fatalf("Unmarshal teams: %v, body = %s", err, body)
		}

		if len(resp.Teams) != 1 || resp.Teams[0].Name != "Devs" {
			t.Errorf("expected team Devs, got %+v", resp.Teams)
		}
		if len(resp.Teams[0].Agents) != 1 || resp.Teams[0].Agents[0].Name != "Alice" {
			t.Errorf("expected agent Alice in team Devs, got %+v", resp.Teams[0].Agents)
		}
	}

	// 2. Test GET /api/agents
	{
		req := httptest.NewRequest("GET", "/api/agents", nil)
		req.Host = "localhost:8765"
		req.RemoteAddr = "127.0.0.1:12345"
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("GET /api/agents status = %d", rec.Code)
		}

		body, _ := io.ReadAll(rec.Body)
		var resp struct {
			Agents []struct {
				Name string `json:"name"`
			} `json:"agents"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			t.Fatalf("Unmarshal agents: %v, body = %s", err, body)
		}

		if len(resp.Agents) != 1 || resp.Agents[0].Name != "Alice" {
			t.Errorf("expected agent Alice, got %+v", resp.Agents)
		}
	}
}

func TestHTTP_ProxyCookieHijackDefense(t *testing.T) {
	tempDir := t.TempDir()

	// Create a dummy target server that the proxy forwards to
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("proxied_response"))
	}))
	defer targetServer.Close()

	pm, err := proxy.NewProxyManager(tempDir, filepath.Join(tempDir, "proxies.json"))
	if err != nil {
		t.Fatalf("failed to create proxy manager: %v", err)
	}
	_, err = pm.AddProxy("test-proxy", targetServer.URL)
	if err != nil {
		t.Fatalf("failed to add proxy: %v", err)
	}

	mux := NewMux(tempDir, nil, nil, WithProxyManager(pm))
	defer mux.Close()

	// 1. Request GET / with Cookie set and Accept: text/html -> Should NOT be proxied (should fall through to FileServer/dashboard)
	{
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Accept", "text/html")
		req.AddCookie(&http.Cookie{Name: "soloqueue_active_proxy", Value: "test-proxy"})
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		body := rec.Body.String()
		if strings.Contains(body, "proxied_response") {
			t.Errorf("GET / with Accept: text/html was hijacked by proxy")
		}
	}

	// 2. Request GET /something-else with Cookie set and Accept: */* (non-HTML asset) -> Should be proxied
	{
		req := httptest.NewRequest("GET", "/something-else", nil)
		req.Header.Set("Accept", "*/*")
		req.AddCookie(&http.Cookie{Name: "soloqueue_active_proxy", Value: "test-proxy"})
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		body := rec.Body.String()
		if !strings.Contains(body, "proxied_response") {
			t.Errorf("expected proxied response for asset request, got %q (status %d)", body, rec.Code)
		}
	}

	// 3. Request GET /favicon.ico (an asset that exists in fsys) with Cookie set -> Should NOT be proxied
	{
		req := httptest.NewRequest("GET", "/favicon.ico", nil)
		req.AddCookie(&http.Cookie{Name: "soloqueue_active_proxy", Value: "test-proxy"})
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		body := rec.Body.String()
		if strings.Contains(body, "proxied_response") {
			t.Errorf("GET /favicon.ico (existing fsys asset) was hijacked by proxy")
		}
	}
}

func TestHTTP_ProxyErrorHandling(t *testing.T) {
	tempDir := t.TempDir()

	// Use an invalid/unreachable target server to trigger proxy error
	targetURL := "http://127.0.0.1:57648" // hopefully unreachable

	pm, err := proxy.NewProxyManager(tempDir, filepath.Join(tempDir, "proxies.json"))
	if err != nil {
		t.Fatalf("failed to create proxy manager: %v", err)
	}
	_, err = pm.AddProxy("error-proxy", targetURL)
	if err != nil {
		t.Fatalf("failed to add proxy: %v", err)
	}

	// Create logger that captures log records to file
	sysLog, err := logger.System(tempDir, logger.WithConsole(false), logger.WithLevel(slog.LevelDebug))
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	defer sysLog.Close()

	mux := NewMux(tempDir, sysLog, nil, WithProxyManager(pm))
	defer mux.Close()

	// 1. Initially it is healthy (by default). Request it and expect 502 Bad Gateway.
	{
		// Redirect stderr to verify nothing is printed to console
		oldStderr := os.Stderr
		pipeR, pipeW, err := os.Pipe()
		if err != nil {
			t.Fatalf("pipe: %v", err)
		}
		os.Stderr = pipeW

		// Request proxy path to trigger connection refused error
		req := httptest.NewRequest("GET", "/unreachable-path", nil)
		req.Header.Set("Accept", "*/*")
		req.AddCookie(&http.Cookie{Name: "soloqueue_active_proxy", Value: "error-proxy"})
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		// Restore stderr
		pipeW.Close()
		os.Stderr = oldStderr

		if rec.Code != http.StatusBadGateway {
			t.Errorf("expected 502 Bad Gateway, got %d", rec.Code)
		}

		// Read captured stderr
		var stderrBuf strings.Builder
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			var buf [256]byte
			for {
				n, err := pipeR.Read(buf[:])
				if n > 0 {
					stderrBuf.Write(buf[:n])
				}
				if err != nil {
					break
				}
			}
		}()
		wg.Wait()

		capturedStderr := stderrBuf.String()
		if strings.Contains(capturedStderr, "proxy error") {
			t.Errorf("did not expect standard library log output to print 'proxy error' to stderr, got:\n%s", capturedStderr)
		}
	}

	// 2. Start the proxy manager and trigger health checks to mark it unhealthy.
	if err := pm.Start(); err != nil {
		t.Fatalf("failed to start proxy manager: %v", err)
	}
	// Trigger checks to reach 3 failures
	// (Start() runs checkHealth() once on startup, which is 1st failure)
	pm.Shutdown() // stop background checking loop to avoid race, but structure remains
	
	// Manually trigger remaining CheckHealth calls to safely simulate 3 failures
	pm.CheckHealth() // 2nd failure
	pm.CheckHealth() // 3rd failure

	// Verify health status is now false
	_, healthy, _ := pm.GetProxyStatus("error-proxy")
	if healthy {
		t.Fatalf("expected proxy to be marked unhealthy")
	}

	// 3. Request proxy path now that it is unhealthy -> should get 503 Service Unavailable
	{
		req := httptest.NewRequest("GET", "/unreachable-path", nil)
		req.Header.Set("Accept", "*/*")
		req.AddCookie(&http.Cookie{Name: "soloqueue_active_proxy", Value: "error-proxy"})
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("expected 503 Service Unavailable, got %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "Proxy target is unhealthy") {
			t.Errorf("expected body to mention 'Proxy target is unhealthy', got %q", rec.Body.String())
		}
	}

	// Verify log file contains the debug proxy error log
	sysLog.Close() // Flush logs
	httpLogFile := filepath.Join(tempDir, "logs", "system", "http-"+time.Now().Format("2006-01-02")+".jsonl")
	data, err := os.ReadFile(httpLogFile)
	if err != nil {
		t.Fatalf("failed to read http log file: %v", err)
	}
	logContent := string(data)
	if !strings.Contains(logContent, "http: proxy error") {
		t.Errorf("expected http log file to contain 'http: proxy error', got log content:\n%s", logContent)
	}
	if !strings.Contains(logContent, "DEBUG") {
		t.Errorf("expected http log file to contain 'DEBUG' level log, got log content:\n%s", logContent)
	}
}


