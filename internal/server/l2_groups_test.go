package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/team/store"
)

// A team created after the mux was built must be selectable without a restart.
func TestListL2GroupsIncludesRuntimeInstalledTeam(t *testing.T) {
	m := NewMux(t.TempDir(), nil)
	defer m.Close()
	ctx := context.Background()

	if err := m.teamstore.CreateTeam(ctx, &store.Team{Name: "engineering"}); err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	if err := m.teamstore.CreateAgent(ctx, &store.Agent{Name: "Lead", TeamName: "engineering", IsLeader: true}); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if err := m.teamstore.CreateTeam(ctx, &store.Team{Name: "worker-only"}); err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	if err := m.teamstore.CreateAgent(ctx, &store.Agent{Name: "Worker", TeamName: "worker-only"}); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}

	recorder := httptest.NewRecorder()
	m.ServeHTTP(recorder, newLocalhostRequest(http.MethodGet, "/api/session/groups", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /api/session/groups status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Groups []string `json:"groups"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	var hasEngineering, hasWorkerOnly bool
	for _, g := range body.Groups {
		switch g {
		case "engineering":
			hasEngineering = true
		case "worker-only":
			hasWorkerOnly = true
		}
	}
	if !hasEngineering {
		t.Fatalf("groups = %v, want engineering", body.Groups)
	}
	if hasWorkerOnly {
		t.Fatalf("groups = %v, must exclude teams without a leader", body.Groups)
	}
}

func TestLeaderAgentNameReadsFromTeamStore(t *testing.T) {
	m := NewMux(t.TempDir(), nil)
	defer m.Close()
	ctx := context.Background()

	if err := m.teamstore.CreateAgent(ctx, &store.Agent{Name: "Lead", TeamName: "engineering", IsLeader: true}); err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}

	if got := m.leaderAgentName(ctx, "engineering"); got != "Lead" {
		t.Fatalf("leaderAgentName = %q, want Lead", got)
	}
	if got := m.leaderAgentName(ctx, "missing"); got != "" {
		t.Fatalf("leaderAgentName for unknown group = %q, want empty", got)
	}
}
