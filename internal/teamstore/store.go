// Package teamstore manages teams and agents in a shared SQLite database.
// It uses *sqlitedb.DB for the connection and serializes writes via its
// shared WMu lock.
package teamstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/xiaobaitu/soloqueue/internal/sqlitedb"
)

// ─── Types ──────────────────────────────────────────────────────────────────

// Team represents a team (group) stored in SQLite.
type Team struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Workspaces  []Workspace `json:"workspaces"`
	CreatedAt   string      `json:"created_at"`
	UpdatedAt   string      `json:"updated_at"`
}

// Workspace describes a working directory associated with a team.
type Workspace struct {
	Name     string         `json:"name"`
	Path     string         `json:"path"`
	AutoWork AutoWorkConfig `json:"autoWork,omitempty"`
}

// AutoWorkConfig describes automatic work scheduling configuration.
type AutoWorkConfig struct {
	Enabled                 bool `json:"enabled"`
	InitialCooldownMinutes  int  `json:"initialCooldownMinutes"`
	PostTaskCooldownMinutes int  `json:"postTaskCooldownMinutes"`
	MaxIntervalsPerDay      int  `json:"maxIntervalsPerDay"`
}

// Agent represents an agent (team member) stored in SQLite.
type Agent struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	TeamName     string   `json:"team_name"`
	IsLeader     bool     `json:"is_leader"`
	Model        string   `json:"model"`
	SystemPrompt string   `json:"system_prompt"`
	Permission   bool     `json:"permission"`
	MCPServers   []string `json:"mcp_servers"`
	SkillIDs     []string `json:"skill_ids"`
	CreatedAt    string   `json:"created_at"`
	UpdatedAt    string   `json:"updated_at"`
}

// AgentTemplate is a flat representation used by the agent factory for
// compatibility with the existing agent template loading system.
type AgentTemplate struct {
	ID           string
	Name         string
	Description  string
	SystemPrompt string
	ModelID      string
	IsLeader     bool
	Group        string // maps to TeamName
	Permission   bool
	MCPServers   []string
	SkillIDs     []string
}

// ─── Store ──────────────────────────────────────────────────────────────────

// Store manages team and agent persistence in a shared SQLite database.
// Writes are serialized via the shared write mutex; reads are concurrent.
type Store struct {
	db *sqlitedb.DB
}

// NewStore creates a new teamstore backed by the given shared database.
// It ensures the required tables exist (CREATE TABLE IF NOT EXISTS).
func NewStore(db *sqlitedb.DB) *Store {
	s := &Store{db: db}
	s.initSchema()
	return s
}

// initSchema creates the teams and agents tables if they don't already exist.
func (s *Store) initSchema() {
	s.db.WMu.Lock()
	defer s.db.WMu.Unlock()

	_, _ = s.db.Exec(`
		CREATE TABLE IF NOT EXISTS teams (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			description TEXT NOT NULL DEFAULT '',
			workspaces TEXT NOT NULL DEFAULT '[]',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
	`)
	_, _ = s.db.Exec(`
		CREATE TABLE IF NOT EXISTS agents (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			description TEXT NOT NULL DEFAULT '',
			team_name TEXT NOT NULL,
			is_leader INTEGER NOT NULL DEFAULT 0,
			model TEXT NOT NULL DEFAULT '',
			system_prompt TEXT NOT NULL DEFAULT '',
			permission INTEGER NOT NULL DEFAULT 0,
			mcp_servers TEXT NOT NULL DEFAULT '[]',
			skill_ids TEXT NOT NULL DEFAULT '[]',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
	`)
}

// ─── Team CRUD ──────────────────────────────────────────────────────────────

// CreateTeam inserts a new team. If ID is empty, a UUID is generated.
func (s *Store) CreateTeam(ctx context.Context, t *Team) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if t.ID == "" {
		t.ID = uuid.New().String()
	}
	now := time.Now().Format(time.RFC3339)
	t.CreatedAt = now
	t.UpdatedAt = now

	wsJSON, err := json.Marshal(t.Workspaces)
	if err != nil {
		return fmt.Errorf("teamstore: marshal workspaces: %w", err)
	}

	s.db.WMu.Lock()
	defer s.db.WMu.Unlock()

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO teams (id, name, description, workspaces, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		t.ID, t.Name, t.Description, string(wsJSON), t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("teamstore: create team %q: %w", t.Name, err)
	}
	return nil
}

// GetTeamByName retrieves a team by its unique name.
func (s *Store) GetTeamByName(ctx context.Context, name string) (*Team, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var t Team
	var wsJSON string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, description, workspaces, created_at, updated_at
		 FROM teams WHERE name = ?`, name,
	).Scan(&t.ID, &t.Name, &t.Description, &wsJSON, &t.CreatedAt, &t.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("teamstore: team %q not found", name)
	}
	if err != nil {
		return nil, fmt.Errorf("teamstore: get team %q: %w", name, err)
	}

	if err := json.Unmarshal([]byte(wsJSON), &t.Workspaces); err != nil {
		return nil, fmt.Errorf("teamstore: unmarshal workspaces: %w", err)
	}
	return &t, nil
}

// ListTeams returns all teams.
func (s *Store) ListTeams(ctx context.Context) ([]Team, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, description, workspaces, created_at, updated_at
		 FROM teams ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("teamstore: list teams: %w", err)
	}
	defer rows.Close()

	var teams []Team
	for rows.Next() {
		var t Team
		var wsJSON string
		if err := rows.Scan(&t.ID, &t.Name, &t.Description, &wsJSON, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("teamstore: scan team: %w", err)
		}
		if err := json.Unmarshal([]byte(wsJSON), &t.Workspaces); err != nil {
			return nil, fmt.Errorf("teamstore: unmarshal workspaces: %w", err)
		}
		teams = append(teams, t)
	}
	return teams, rows.Err()
}

// UpdateTeam updates an existing team by name. Only non-empty fields in t
// are applied. The ID, Name, and CreatedAt fields are preserved from the
// existing record.
func (s *Store) UpdateTeam(ctx context.Context, name string, t *Team) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	wsJSON, err := json.Marshal(t.Workspaces)
	if err != nil {
		return fmt.Errorf("teamstore: marshal workspaces: %w", err)
	}

	now := time.Now().Format(time.RFC3339)

	s.db.WMu.Lock()
	defer s.db.WMu.Unlock()

	result, err := s.db.ExecContext(ctx,
		`UPDATE teams SET description = ?, workspaces = ?, updated_at = ?
		 WHERE name = ?`,
		t.Description, string(wsJSON), now, name,
	)
	if err != nil {
		return fmt.Errorf("teamstore: update team %q: %w", name, err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("teamstore: team %q not found", name)
	}
	return nil
}

// DeleteTeam removes a team by name.
func (s *Store) DeleteTeam(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.db.WMu.Lock()
	defer s.db.WMu.Unlock()

	result, err := s.db.ExecContext(ctx, `DELETE FROM teams WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("teamstore: delete team %q: %w", name, err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("teamstore: team %q not found", name)
	}
	return nil
}

// ─── Agent CRUD ─────────────────────────────────────────────────────────────

// CreateAgent inserts a new agent. If ID is empty, a UUID is generated.
func (s *Store) CreateAgent(ctx context.Context, a *Agent) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if a.ID == "" {
		a.ID = uuid.New().String()
	}
	now := time.Now().Format(time.RFC3339)
	a.CreatedAt = now
	a.UpdatedAt = now

	mcpJSON, err := json.Marshal(a.MCPServers)
	if err != nil {
		return fmt.Errorf("teamstore: marshal mcp_servers: %w", err)
	}
	skillsJSON, err := json.Marshal(a.SkillIDs)
	if err != nil {
		return fmt.Errorf("teamstore: marshal skill_ids: %w", err)
	}

	isLeader := 0
	if a.IsLeader {
		isLeader = 1
	}
	permission := 0
	if a.Permission {
		permission = 1
	}

	s.db.WMu.Lock()
	defer s.db.WMu.Unlock()

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO agents (id, name, description, team_name, is_leader, model,
		 system_prompt, permission, mcp_servers, skill_ids, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.Name, a.Description, a.TeamName, isLeader, a.Model,
		a.SystemPrompt, permission, string(mcpJSON), string(skillsJSON),
		a.CreatedAt, a.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("teamstore: create agent %q: %w", a.Name, err)
	}
	return nil
}

// GetAgentByName retrieves an agent by its unique name.
func (s *Store) GetAgentByName(ctx context.Context, name string) (*Agent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var a Agent
	var mcpJSON, skillsJSON string
	var isLeader, permission int
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, description, team_name, is_leader, model,
		 system_prompt, permission, mcp_servers, skill_ids, created_at, updated_at
		 FROM agents WHERE name = ?`, name,
	).Scan(&a.ID, &a.Name, &a.Description, &a.TeamName, &isLeader, &a.Model,
		&a.SystemPrompt, &permission, &mcpJSON, &skillsJSON, &a.CreatedAt, &a.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("teamstore: agent %q not found", name)
	}
	if err != nil {
		return nil, fmt.Errorf("teamstore: get agent %q: %w", name, err)
	}

	a.IsLeader = isLeader != 0
	a.Permission = permission != 0

	if err := json.Unmarshal([]byte(mcpJSON), &a.MCPServers); err != nil {
		return nil, fmt.Errorf("teamstore: unmarshal mcp_servers: %w", err)
	}
	if err := json.Unmarshal([]byte(skillsJSON), &a.SkillIDs); err != nil {
		return nil, fmt.Errorf("teamstore: unmarshal skill_ids: %w", err)
	}
	return &a, nil
}

// ListAgents returns all agents.
func (s *Store) ListAgents(ctx context.Context) ([]Agent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, description, team_name, is_leader, model,
		 system_prompt, permission, mcp_servers, skill_ids, created_at, updated_at
		 FROM agents ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("teamstore: list agents: %w", err)
	}
	defer rows.Close()

	return scanAgents(rows)
}

// ListAgentsByTeam returns all agents belonging to a given team.
func (s *Store) ListAgentsByTeam(ctx context.Context, teamName string) ([]Agent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, description, team_name, is_leader, model,
		 system_prompt, permission, mcp_servers, skill_ids, created_at, updated_at
		 FROM agents WHERE team_name = ? ORDER BY name ASC`, teamName)
	if err != nil {
		return nil, fmt.Errorf("teamstore: list agents by team %q: %w", teamName, err)
	}
	defer rows.Close()

	return scanAgents(rows)
}

// ListLeaders returns all agents where is_leader = 1.
func (s *Store) ListLeaders(ctx context.Context) ([]Agent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, description, team_name, is_leader, model,
		 system_prompt, permission, mcp_servers, skill_ids, created_at, updated_at
		 FROM agents WHERE is_leader = 1 ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("teamstore: list leaders: %w", err)
	}
	defer rows.Close()

	return scanAgents(rows)
}

// UpdateAgent updates an existing agent by name. Only non-empty/relevant
// fields in a are applied. The ID, Name, and CreatedAt fields are preserved
// from the existing record.
func (s *Store) UpdateAgent(ctx context.Context, name string, a *Agent) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	mcpJSON, err := json.Marshal(a.MCPServers)
	if err != nil {
		return fmt.Errorf("teamstore: marshal mcp_servers: %w", err)
	}
	skillsJSON, err := json.Marshal(a.SkillIDs)
	if err != nil {
		return fmt.Errorf("teamstore: marshal skill_ids: %w", err)
	}

	isLeader := 0
	if a.IsLeader {
		isLeader = 1
	}
	permission := 0
	if a.Permission {
		permission = 1
	}

	now := time.Now().Format(time.RFC3339)

	s.db.WMu.Lock()
	defer s.db.WMu.Unlock()

	result, err := s.db.ExecContext(ctx,
		`UPDATE agents SET description = ?, team_name = ?, is_leader = ?,
		 model = ?, system_prompt = ?, permission = ?, mcp_servers = ?,
		 skill_ids = ?, updated_at = ?
		 WHERE name = ?`,
		a.Description, a.TeamName, isLeader, a.Model, a.SystemPrompt,
		permission, string(mcpJSON), string(skillsJSON), now, name,
	)
	if err != nil {
		return fmt.Errorf("teamstore: update agent %q: %w", name, err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("teamstore: agent %q not found", name)
	}
	return nil
}

// DeleteAgent removes an agent by name.
func (s *Store) DeleteAgent(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.db.WMu.Lock()
	defer s.db.WMu.Unlock()

	result, err := s.db.ExecContext(ctx, `DELETE FROM agents WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("teamstore: delete agent %q: %w", name, err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("teamstore: agent %q not found", name)
	}
	return nil
}

// ─── Conversion ─────────────────────────────────────────────────────────────

// ToAgentTemplate converts an Agent to an AgentTemplate for compatibility
// with the agent factory system.
func (a *Agent) ToAgentTemplate() AgentTemplate {
	return AgentTemplate{
		ID:           a.ID,
		Name:         a.Name,
		Description:  a.Description,
		SystemPrompt: a.SystemPrompt,
		ModelID:      a.Model,
		IsLeader:     a.IsLeader,
		Group:        a.TeamName,
		Permission:   a.Permission,
		MCPServers:   a.MCPServers,
		SkillIDs:     a.SkillIDs,
	}
}

// ─── Helpers ────────────────────────────────────────────────────────────────

// scanAgents scans agent rows into a slice. Caller owns rows.Close().
func scanAgents(rows *sql.Rows) ([]Agent, error) {
	var agents []Agent
	for rows.Next() {
		var a Agent
		var mcpJSON, skillsJSON string
		var isLeader, permission int
		if err := rows.Scan(&a.ID, &a.Name, &a.Description, &a.TeamName,
			&isLeader, &a.Model, &a.SystemPrompt, &permission,
			&mcpJSON, &skillsJSON, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, fmt.Errorf("teamstore: scan agent: %w", err)
		}
		a.IsLeader = isLeader != 0
		a.Permission = permission != 0
		if err := json.Unmarshal([]byte(mcpJSON), &a.MCPServers); err != nil {
			return nil, fmt.Errorf("teamstore: unmarshal mcp_servers: %w", err)
		}
		if err := json.Unmarshal([]byte(skillsJSON), &a.SkillIDs); err != nil {
			return nil, fmt.Errorf("teamstore: unmarshal skill_ids: %w", err)
		}
		agents = append(agents, a)
	}
	return agents, rows.Err()
}
