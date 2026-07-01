package teamstore

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/sqlitedb"
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

func TestStoreWorkspaceMigration(t *testing.T) {
	tempDir := t.TempDir()
	groupsDir := filepath.Join(tempDir, "groups")
	agentsDir := filepath.Join(tempDir, "agents")
	dbPath := filepath.Join(tempDir, "entries.db")

	db, err := sqlitedb.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer db.Close()

	store := NewStore(groupsDir, agentsDir, db)
	ctx := context.Background()

	// 1. Create a raw team file with direct workspaces under groupsDir.
	teamMD := `---
name: QA
workspaces:
  - name: qacode
    path: /workspace/qa
---
QA team description.
`
	_ = os.MkdirAll(groupsDir, 0755)
	err = os.WriteFile(filepath.Join(groupsDir, "qa.md"), []byte(teamMD), 0644)
	if err != nil {
		t.Fatalf("failed to write raw team file: %v", err)
	}

	// 2. Run the migration.
	err = store.MigrateWorkspacesToProjects(ctx)
	if err != nil {
		t.Fatalf("failed to run workspaces migration: %v", err)
	}

	// 3. Check if the project was inserted into the database.
	proj, err := store.GetProject(ctx, "qa-qacode")
	if err != nil {
		t.Fatalf("project was not migrated to DB: %v", err)
	}
	if proj.Name != "qacode" || proj.Path != "/workspace/qa" {
		t.Errorf("mismatch in migrated project: %+v", proj)
	}

	// 4. Check if the team file has been updated to remove Workspaces and add Projects.
	retrievedTeam, err := store.GetTeamByName(ctx, "QA")
	if err != nil {
		t.Fatalf("failed to get migrated team: %v", err)
	}
	if len(retrievedTeam.Workspaces) != 1 || retrievedTeam.Workspaces[0].Path != "/workspace/qa" {
		t.Errorf("mismatch in team workspaces: %+v", retrievedTeam.Workspaces)
	}

	// Verify project association is stored in team record.
	if len(retrievedTeam.Projects) != 1 || retrievedTeam.Projects[0] != "qa-qacode" {
		t.Errorf("mismatch in team projects association: %+v", retrievedTeam.Projects)
	}

	// 5. If database is not empty, running migration again should NOT create new projects or modify file projects if we add a new workspace.
	// Write a new workspace to qa.md.
	teamMD2 := `---
name: QA
workspaces:
  - name: extra
    path: /workspace/extra
projects:
  - qa-qacode
---
QA team description.
`
	err = os.WriteFile(filepath.Join(groupsDir, "qa.md"), []byte(teamMD2), 0644)
	if err != nil {
		t.Fatalf("failed to write raw team file: %v", err)
	}

	// Run migration again. Since DB is not empty, it should NOT migrate "extra" into DB.
	err = store.MigrateWorkspacesToProjects(ctx)
	if err != nil {
		t.Fatalf("failed to run migration again: %v", err)
	}

	// Verify "qa-extra" does NOT exist in DB.
	_, err = store.GetProject(ctx, "qa-extra")
	if err == nil {
		t.Error("expected qa-extra project not to be created since DB is not empty")
	}

	// Verify the qa.md file workspaces block is cleared, but projects is still only ["qa-qacode"].
	retrievedTeam2, err := store.GetTeamByName(ctx, "QA")
	if err != nil {
		t.Fatalf("failed to get migrated team: %v", err)
	}
	if len(retrievedTeam2.Projects) != 1 || retrievedTeam2.Projects[0] != "qa-qacode" {
		t.Errorf("expected projects list to remain unchanged, got: %+v", retrievedTeam2.Projects)
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
	agentPath := filepath.Join(agentsDir, "AndrejKarpathy.md")
	if _, err := os.Stat(agentPath); err != nil {
		t.Errorf("AndrejKarpathy.md not created: %v", err)
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

