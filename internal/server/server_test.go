package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/teamstore"
)

func startTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := NewMux(t.TempDir(), nil)
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
	mux := NewMux(t.TempDir(), nil, WithAuthConfig(config.AuthConfig{
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

	mux := NewMux(tempDir, nil, WithTeamStore(store))
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



