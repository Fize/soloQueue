package teamstore

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestStoreCRUD(t *testing.T) {
	tempDir := t.TempDir()
	groupsDir := filepath.Join(tempDir, "groups")
	agentsDir := filepath.Join(tempDir, "agents")

	store := NewStore(groupsDir, agentsDir)
	ctx := context.Background()

	// ─── Team Tests ──────────────────────────────────────────────────────────

	// 1. Create Team
	team := &Team{
		Name:        "Devs",
		Description: "Development Team",
		Workspaces: []Workspace{
			{Name: "code", Path: "/workspace/code"},
		},
	}
	err := store.CreateTeam(ctx, team)
	if err != nil {
		t.Fatalf("failed to create team: %v", err)
	}

	// Verify file exists
	teamPath := filepath.Join(groupsDir, "devs.md")
	if _, err := os.Stat(teamPath); err != nil {
		t.Errorf("team file not created: %v", err)
	}

	// 2. Get Team
	retrievedTeam, err := store.GetTeamByName(ctx, "Devs")
	if err != nil {
		t.Fatalf("failed to get team: %v", err)
	}
	if retrievedTeam.Name != "Devs" || retrievedTeam.Description != "Development Team" {
		t.Errorf("mismatch in retrieved team: %+v", retrievedTeam)
	}
	if len(retrievedTeam.Workspaces) != 1 || retrievedTeam.Workspaces[0].Name != "code" {
		t.Errorf("mismatch in workspaces: %+v", retrievedTeam.Workspaces)
	}

	// 3. Update Team
	retrievedTeam.Description = "Updated Devs"
	retrievedTeam.Workspaces = append(retrievedTeam.Workspaces, Workspace{Name: "docs", Path: "/workspace/docs"})
	err = store.UpdateTeam(ctx, "Devs", retrievedTeam)
	if err != nil {
		t.Fatalf("failed to update team: %v", err)
	}

	updatedTeam, err := store.GetTeamByName(ctx, "Devs")
	if err != nil {
		t.Fatalf("failed to get updated team: %v", err)
	}
	if updatedTeam.Description != "Updated Devs" || len(updatedTeam.Workspaces) != 2 {
		t.Errorf("updated team values mismatch: %+v", updatedTeam)
	}

	// ─── Agent Tests ─────────────────────────────────────────────────────────

	// 1. Create Agent
	agent := &Agent{
		Name:         "alice",
		Description:  "Lead Developer",
		TeamName:     "Devs",
		IsLeader:     true,
		Model:        "gpt-4o",
		SystemPrompt: "You are Alice.",
		Permission:   true,
		MCPServers:   []string{"git-mcp"},
		SkillIDs:     []string{"bash"},
	}
	err = store.CreateAgent(ctx, agent)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// Verify file exists
	agentPath := filepath.Join(agentsDir, "alice.md")
	if _, err := os.Stat(agentPath); err != nil {
		t.Errorf("agent file not created: %v", err)
	}

	// 2. Get Agent
	retrievedAgent, err := store.GetAgentByName(ctx, "alice")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}
	if retrievedAgent.Name != "alice" || retrievedAgent.TeamName != "Devs" || !retrievedAgent.IsLeader {
		t.Errorf("mismatch in retrieved agent: %+v", retrievedAgent)
	}

	// 3. List Agents and Leaders
	agents, err := store.ListAgents(ctx)
	if err != nil || len(agents) != 1 {
		t.Errorf("list agents failed: %v, count: %d", err, len(agents))
	}
	leaders, err := store.ListLeaders(ctx)
	if err != nil || len(leaders) != 1 || leaders[0].Name != "alice" {
		t.Errorf("list leaders failed: %v", err)
	}

	// 4. Update Agent
	retrievedAgent.SystemPrompt = "You are Alice, the lead developer."
	retrievedAgent.IsLeader = false
	err = store.UpdateAgent(ctx, "alice", retrievedAgent)
	if err != nil {
		t.Fatalf("failed to update agent: %v", err)
	}

	updatedAgent, err := store.GetAgentByName(ctx, "alice")
	if err != nil {
		t.Fatalf("failed to get updated agent: %v", err)
	}
	if updatedAgent.SystemPrompt != "You are Alice, the lead developer." || updatedAgent.IsLeader {
		t.Errorf("updated agent values mismatch: %+v", updatedAgent)
	}

	// ─── Deletion Tests ──────────────────────────────────────────────────────

	err = store.DeleteAgent(ctx, "alice")
	if err != nil {
		t.Errorf("failed to delete agent: %v", err)
	}
	if _, err := os.Stat(agentPath); !os.IsNotExist(err) {
		t.Error("agent file still exists after deletion")
	}

	err = store.DeleteTeam(ctx, "Devs")
	if err != nil {
		t.Errorf("failed to delete team: %v", err)
	}
	if _, err := os.Stat(teamPath); !os.IsNotExist(err) {
		t.Error("team file still exists after deletion")
	}
}
