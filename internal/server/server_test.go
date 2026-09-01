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
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/agent/agenttest"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/team/store"
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

func TestTriggerOnConfigChangePassesCommittedSettings(t *testing.T) {
	cfg, err := config.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	committed, err := cfg.Set(func(settings *config.Settings) {
		settings.Log.Level = "debug"
	})
	if err != nil {
		t.Fatal(err)
	}
	var got config.Settings
	mux := &Mux{
		configSvc: cfg,
		onConfigChange: func(settings config.Settings) error {
			got = settings
			return nil
		},
	}
	mux.triggerOnConfigChange()
	if got.Log.Level != committed.Log.Level {
		t.Fatalf("callback log level = %q, want committed %q", got.Log.Level, committed.Log.Level)
	}
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

func TestBuildAgentList_OmitsStoppedUnregisteredSupervisor(t *testing.T) {
	reg := agent.NewRegistry(nil)
	child := agent.NewAgent(agent.Definition{ID: "temporary"}, &agenttest.FakeLLM{Responses: []string{"ok"}}, nil)
	if err := child.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := child.Stop(time.Second); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	sv := agent.NewSupervisor(child, nil, nil)

	mux := NewMux(t.TempDir(), nil,
		WithRegistry(reg),
		WithSupervisors(func() []*agent.Supervisor { return []*agent.Supervisor{sv} }),
	)
	defer mux.Close()

	got := mux.buildAgentList()
	if len(got.Agents) != 0 {
		t.Fatalf("agent count = %d, want 0; stopped unregistered supervisor must be hidden", len(got.Agents))
	}
}

func TestHTTP_AuthRoutesRemoved(t *testing.T) {
	mux := NewMux(t.TempDir(), nil)
	defer mux.Close()

	for _, path := range []string{"/api/auth/status", "/api/auth/check", "/api/auth/token"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
			}
			if rec.Header().Get("WWW-Authenticate") != "" {
				t.Fatal("removed auth route emitted WWW-Authenticate")
			}
		})
	}
}

func TestHTTP_WorkflowRoutesRemoved(t *testing.T) {
	mux := NewMux(t.TempDir(), nil)
	defer mux.Close()

	tests := []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/api/workflows"},
		{method: http.MethodPost, path: "/api/workflows"},
		{method: http.MethodGet, path: "/api/workflows/example"},
		{method: http.MethodPost, path: "/api/workflows/example/run"},
	}
	for _, test := range tests {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			req := httptest.NewRequest(test.method, test.path, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, http.StatusNotFound, rec.Body.String())
			}
		})
	}
}

func TestHTTP_TeamAgents(t *testing.T) {
	tempDir := t.TempDir()
	groupsDir := filepath.Join(tempDir, "groups")
	agentsDir := filepath.Join(tempDir, "agents")
	_ = os.MkdirAll(groupsDir, 0755)
	_ = os.MkdirAll(agentsDir, 0755)

	ts := store.NewStore(groupsDir, agentsDir, nil)
	ctx := context.Background()

	// Create a team
	err := ts.CreateTeam(ctx, &store.Team{
		Name:        "Devs",
		Description: "Dev team",
	})
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}

	// Create an agent
	err = ts.CreateAgent(ctx, &store.Agent{
		Name:        "Alice",
		TeamName:    "Devs",
		Description: "Coder",
	})
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}

	mux := NewMux(tempDir, nil, WithTeamStore(ts))
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

func TestHTTP_BuiltinTeamCatalogAndInstall(t *testing.T) {
	tempDir := t.TempDir()
	ts := store.NewStore(filepath.Join(tempDir, "groups"), filepath.Join(tempDir, "agents"), nil)
	reloads := 0
	mux := NewMux(tempDir, nil,
		WithTeamStore(ts),
		WithTeamCatalogReload(func() error {
			reloads++
			return nil
		}),
	)
	defer mux.Close()

	getReq := httptest.NewRequest(http.MethodGet, "/api/builtin-teams", nil)
	getReq.Host = "localhost:8765"
	getReq.RemoteAddr = "127.0.0.1:12345"
	getRec := httptest.NewRecorder()
	mux.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET /api/builtin-teams status = %d, body = %s", getRec.Code, getRec.Body.String())
	}
	var catalog struct {
		Teams []BuiltinTeamResponse `json:"teams"`
	}
	if err := json.Unmarshal(getRec.Body.Bytes(), &catalog); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	if len(catalog.Teams) != 1 || catalog.Teams[0].ID != "engineering" ||
		catalog.Teams[0].Status != store.BuiltinTeamAvailable {
		t.Fatalf("catalog = %+v", catalog.Teams)
	}

	postReq := httptest.NewRequest(http.MethodPost, "/api/builtin-teams/install",
		strings.NewReader(`{"team_ids":["engineering"]}`))
	postReq.Header.Set("Content-Type", "application/json")
	postReq.Host = "localhost:8765"
	postReq.RemoteAddr = "127.0.0.1:12345"
	postRec := httptest.NewRecorder()
	mux.ServeHTTP(postRec, postReq)
	if postRec.Code != http.StatusCreated {
		t.Fatalf("POST /api/builtin-teams/install status = %d, body = %s", postRec.Code, postRec.Body.String())
	}
	if reloads != 1 {
		t.Fatalf("catalog reloads = %d, want 1", reloads)
	}

	getReq = httptest.NewRequest(http.MethodGet, "/api/builtin-teams", nil)
	getReq.Host = "localhost:8765"
	getReq.RemoteAddr = "127.0.0.1:12345"
	getRec = httptest.NewRecorder()
	mux.ServeHTTP(getRec, getReq)
	if err := json.Unmarshal(getRec.Body.Bytes(), &catalog); err != nil {
		t.Fatalf("decode installed catalog: %v", err)
	}
	if catalog.Teams[0].Status != store.BuiltinTeamInstalled {
		t.Fatalf("installed status = %q", catalog.Teams[0].Status)
	}
}

func TestHTTP_ListProviderRemoteModels(t *testing.T) {
	// 1. Create a mock remote provider server
	mockRemoteSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer mock-api-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("X-Custom-Test") != "test-val" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"object": "list",
			"data": [
				{"id": "gpt-4-turbo", "object": "model"},
				{"id": "gpt-3.5-turbo", "object": "model"},
				{"id": "dall-e-3", "object": "model"}
			]
		}`))
	}))
	defer mockRemoteSrv.Close()

	// 2. Start local test server
	tempDir := t.TempDir()
	configSvc, err := config.New(tempDir)
	if err != nil {
		t.Fatalf("config.New(): %v", err)
	}
	err = configSvc.Watch()
	if err != nil {
		t.Fatalf("configSvc.Watch(): %v", err)
	}

	// Create LLMProvider config that uses the mockRemoteSrv URL
	p := config.LLMProvider{
		ID:      "mock-provider",
		Name:    "Mock Provider",
		BaseURL: mockRemoteSrv.URL,
		APIKey:  "mock-api-key",
		Enabled: true,
		Headers: map[string]string{"X-Custom-Test": "test-val"},
	}
	if err := configSvc.CreateProvider(p); err != nil {
		t.Fatalf("CreateProvider: %v", err)
	}

	mux := NewMux(tempDir, nil, WithConfigService(configSvc))
	defer mux.Close()

	// 3. Test remote models GET endpoint
	req := httptest.NewRequest("GET", "/api/config/providers/mock-provider/remote-models", nil)
	req.Host = "localhost:8765"
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/config/providers/mock-provider/remote-models status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var modelIDs []string
	if err := json.Unmarshal(rec.Body.Bytes(), &modelIDs); err != nil {
		t.Fatalf("Unmarshal response: %v, body = %s", err, rec.Body.String())
	}

	expected := []string{"dall-e-3", "gpt-3.5-turbo", "gpt-4-turbo"}
	if len(modelIDs) != len(expected) {
		t.Fatalf("expected %d models, got %d: %+v", len(expected), len(modelIDs), modelIDs)
	}
	for i, id := range expected {
		if modelIDs[i] != id {
			t.Errorf("at index %d: expected %s, got %s", i, id, modelIDs[i])
		}
	}
}
