package teamstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreCRUD(t *testing.T) {
	tempDir := t.TempDir()
	groupsDir := filepath.Join(tempDir, "groups")
	agentsDir := filepath.Join(tempDir, "agents")

	store := NewStore(groupsDir, agentsDir, nil)
	ctx := context.Background()

	// ─── Team Tests ──────────────────────────────────────────────────────────

	// 1. Create Team
	team := &Team{
		Name:        "Devs",
		Description: "Development Team",
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

	// 3. Update Team
	retrievedTeam.Description = "Updated Devs"
	err = store.UpdateTeam(ctx, "Devs", retrievedTeam)
	if err != nil {
		t.Fatalf("failed to update team: %v", err)
	}

	updatedTeam, err := store.GetTeamByName(ctx, "Devs")
	if err != nil {
		t.Fatalf("failed to get updated team: %v", err)
	}
	if updatedTeam.Description != "Updated Devs" {
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

	// 4b. Update Agent with channel binding (regression test)
	updatedAgent.Channels = map[string]string{"qq": "my-qq-bot", "wechat": "default"}
	updatedAgent.NotifyChannel = "qq"
	if err := store.UpdateAgent(ctx, "alice", updatedAgent); err != nil {
		t.Fatalf("UpdateAgent with channels failed: %v", err)
	}
	reloaded, err := store.GetAgentByName(ctx, "alice")
	if err != nil {
		t.Fatalf("GetAgentByName after channel update: %v", err)
	}
	if reloaded.Channels["qq"] != "my-qq-bot" {
		t.Errorf("Channels[qq] = %q, want %q", reloaded.Channels["qq"], "my-qq-bot")
	}
	if reloaded.Channels["wechat"] != "default" {
		t.Errorf("Channels[wechat] = %q, want %q", reloaded.Channels["wechat"], "default")
	}
	if reloaded.NotifyChannel != "qq" {
		t.Errorf("NotifyChannel = %q, want %q", reloaded.NotifyChannel, "qq")
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

func TestBuiltinEngineeringTeam(t *testing.T) {
	tempDir := t.TempDir()
	groupsDir := filepath.Join(tempDir, "groups")
	agentsDir := filepath.Join(tempDir, "agents")

	store := NewStore(groupsDir, agentsDir, nil)
	ctx := context.Background()

	// 1. EnsureBuiltinTechTeam creates engineering and Andrej Karpathy files.
	err := store.EnsureBuiltinTechTeam(ctx)
	if err != nil {
		t.Fatalf("EnsureBuiltinTechTeam failed: %v", err)
	}

	// Verify engineering group file exists.
	groupPath := filepath.Join(groupsDir, "engineering.md")
	if _, err := os.Stat(groupPath); err != nil {
		t.Errorf("engineering.md not created: %v", err)
	}

	// Verify Andrej Karpathy agent file exists.
	agentPath := filepath.Join(agentsDir, "andrej_karpathy.md")
	if _, err := os.Stat(agentPath); err != nil {
		t.Errorf("andrej_karpathy.md not created: %v", err)
	}

	// Verify sub-agent files exist.
	for _, name := range []string{"explorer", "editor", "tester"} {
		saPath := filepath.Join(agentsDir, name+".md")
		if _, err := os.Stat(saPath); err != nil {
			t.Errorf("%s.md not created: %v", name, err)
		}
	}

	// 2. Verify we can modify Andrej Karpathy's system prompt.
	architect, err := store.GetAgentByName(ctx, "Andrej Karpathy")
	if err != nil {
		t.Fatalf("failed to retrieve Andrej Karpathy: %v", err)
	}

	architect.SystemPrompt = "modified prompt"
	err = store.UpdateAgent(ctx, "Andrej Karpathy", architect)
	if err != nil {
		t.Errorf("expected UpdateAgent to succeed when modifying leader prompt, got error: %v", err)
	}

	// Check if prompt was saved on disk.
	architect2, err := store.GetAgentByName(ctx, "Andrej Karpathy")
	if err != nil {
		t.Fatalf("failed to retrieve leader after update: %v", err)
	}
	if architect2.SystemPrompt != "modified prompt" {
		t.Error("expected leader prompt to be updated and saved")
	}

	// Verify we can modify explorer's system prompt.
	explorer, err := store.GetAgentByName(ctx, "explorer")
	if err != nil {
		t.Fatalf("failed to retrieve explorer: %v", err)
	}

	explorer.SystemPrompt = "modified prompt"
	err = store.UpdateAgent(ctx, "explorer", explorer)
	if err != nil {
		t.Errorf("expected UpdateAgent to succeed when modifying explorer prompt, got error: %v", err)
	}

	explorer2, err := store.GetAgentByName(ctx, "explorer")
	if err != nil {
		t.Fatalf("failed to retrieve explorer after update: %v", err)
	}
	if explorer2.SystemPrompt != "modified prompt" {
		t.Error("expected explorer prompt to be updated and saved")
	}

	// 3. Verify we cannot delete Andrej Karpathy or engineering or sub-agents.
	err = store.DeleteAgent(ctx, "Andrej Karpathy")
	if err == nil {
		t.Error("expected DeleteAgent to fail for architect")
	}

	err = store.DeleteTeam(ctx, "engineering")
	if err == nil {
		t.Error("expected DeleteTeam to fail for engineering")
	}

	for _, name := range []string{"explorer", "editor", "tester"} {
		err = store.DeleteAgent(ctx, name)
		if err == nil {
			t.Errorf("expected DeleteAgent to fail for %s", name)
		}
	}
}

func TestBuiltinTeamInstallStatusesAndRepair(t *testing.T) {
	tempDir := t.TempDir()
	store := NewStore(filepath.Join(tempDir, "groups"), filepath.Join(tempDir, "agents"), nil)
	ctx := context.Background()

	views, err := store.ListBuiltinTeamStatuses(ctx)
	if err != nil {
		t.Fatalf("ListBuiltinTeamStatuses: %v", err)
	}
	if len(views) != 1 || views[0].Status != BuiltinTeamAvailable {
		t.Fatalf("initial views = %+v, want one available team", views)
	}

	results, err := store.InstallBuiltinTeams(ctx, []string{"engineering"})
	if err != nil {
		t.Fatalf("InstallBuiltinTeams: %v", err)
	}
	if len(results) != 1 || !results[0].CreatedTeam || len(results[0].CreatedAgents) != 4 {
		t.Fatalf("install results = %+v", results)
	}

	explorer, err := store.GetAgentByName(ctx, "explorer")
	if err != nil {
		t.Fatalf("GetAgentByName explorer: %v", err)
	}
	explorer.SystemPrompt = "custom explorer prompt"
	if err := store.UpdateAgent(ctx, "explorer", explorer); err != nil {
		t.Fatalf("UpdateAgent explorer: %v", err)
	}
	if err := os.Remove(getAgentFilePath(store.agentsDir, "tester")); err != nil {
		t.Fatalf("remove tester: %v", err)
	}

	views, err = store.ListBuiltinTeamStatuses(ctx)
	if err != nil {
		t.Fatalf("ListBuiltinTeamStatuses partial: %v", err)
	}
	if views[0].Status != BuiltinTeamPartial || len(views[0].MissingAgents) != 1 || views[0].MissingAgents[0] != "tester" {
		t.Fatalf("partial view = %+v", views[0])
	}

	results, err = store.InstallBuiltinTeams(ctx, []string{"engineering"})
	if err != nil {
		t.Fatalf("repair built-in team: %v", err)
	}
	if results[0].CreatedTeam || len(results[0].CreatedAgents) != 1 || results[0].CreatedAgents[0] != "tester" {
		t.Fatalf("repair results = %+v", results)
	}
	explorer, err = store.GetAgentByName(ctx, "explorer")
	if err != nil {
		t.Fatalf("GetAgentByName explorer after repair: %v", err)
	}
	if explorer.SystemPrompt != "custom explorer prompt" {
		t.Fatalf("explorer prompt = %q, want preserved custom prompt", explorer.SystemPrompt)
	}

	results, err = store.InstallBuiltinTeams(ctx, []string{"engineering"})
	if err != nil {
		t.Fatalf("idempotent install: %v", err)
	}
	if results[0].CreatedTeam || len(results[0].CreatedAgents) != 0 {
		t.Fatalf("idempotent results = %+v", results)
	}
}

func TestBuiltinTeamInstallRejectsAgentConflict(t *testing.T) {
	tempDir := t.TempDir()
	store := NewStore(filepath.Join(tempDir, "groups"), filepath.Join(tempDir, "agents"), nil)
	ctx := context.Background()

	if err := store.CreateTeam(ctx, &Team{Name: "other"}); err != nil {
		t.Fatalf("CreateTeam other: %v", err)
	}
	if err := store.CreateAgent(ctx, &Agent{
		Name:      "explorer",
		TeamName:  "other",
		IsLeader:  false,
		CreatedAt: time.Now().Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("CreateAgent explorer: %v", err)
	}

	_, err := store.InstallBuiltinTeams(ctx, []string{"engineering"})
	if !errors.Is(err, ErrBuiltinTeamConflict) {
		t.Fatalf("InstallBuiltinTeams error = %v, want ErrBuiltinTeamConflict", err)
	}
	if _, statErr := os.Stat(getTeamFilePath(store.groupsDir, "engineering")); !os.IsNotExist(statErr) {
		t.Fatalf("engineering team should not be created on conflict: %v", statErr)
	}
}
