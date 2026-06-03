// Package teamstore manages teams and agents using the filesystem.
// It keeps them as markdown files under groups/ and agents/ directories,
// providing full compatibility with the prompt and supervisor reloading systems.
package teamstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/prompt"
	"github.com/xiaobaitu/soloqueue/internal/sqlitedb"
	"gopkg.in/yaml.v3"
)

// ─── Types ──────────────────────────────────────────────────────────────────

// Team represents a team (group) stored in groups/ directory.
type Team struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Workspaces  []Workspace `json:"workspaces"`
	Projects    []string    `json:"projects"`
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

// Agent represents an agent (team member) stored in agents/ directory.
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

// Store manages team and agent persistence in groups/ and agents/ directories.
type Store struct {
	groupsDir string
	agentsDir string
	db        *sqlitedb.DB
	mu        sync.RWMutex
}

// NewStore creates a new teamstore backed by groups/ and agents/ directories.
func NewStore(groupsDir, agentsDir string, db *sqlitedb.DB) *Store {
	_ = os.MkdirAll(groupsDir, 0755)
	_ = os.MkdirAll(agentsDir, 0755)
	return &Store{
		groupsDir: groupsDir,
		agentsDir: agentsDir,
		db:        db,
	}
}

// ─── Team CRUD ──────────────────────────────────────────────────────────────

// CreateTeam inserts a new team. If ID is empty, lowercase name is used.
func (s *Store) CreateTeam(ctx context.Context, t *Team) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if t.ID == "" {
		t.ID = strings.ToLower(t.Name)
	}
	now := time.Now().Format(time.RFC3339)
	t.CreatedAt = now
	t.UpdatedAt = now

	// Check if already exists
	path := getTeamFilePath(s.groupsDir, t.Name)
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("teamstore: team %q already exists", t.Name)
	}

	return s.writeTeamFile(path, t)
}

// GetTeamByName retrieves a team by its unique name.
func (s *Store) GetTeamByName(ctx context.Context, name string) (*Team, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := getTeamFilePath(s.groupsDir, name)
	info, err := os.Stat(path)
	if err != nil {
		// Try scanning case-insensitively
		foundPath, foundInfo, err2 := s.findFileCaseInsensitive(s.groupsDir, name)
		if err2 != nil {
			return nil, fmt.Errorf("teamstore: team %q not found: %w", name, err)
		}
		path = foundPath
		info = foundInfo
	}

	t, err := parseTeamFile(path, info)
	if err != nil {
		return nil, err
	}

	_ = s.resolveTeamWorkspaces(ctx, t)
	return t, nil
}

// ListTeams returns all teams.
func (s *Store) ListTeams(ctx context.Context) ([]Team, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.groupsDir)
	if err != nil {
		return nil, fmt.Errorf("teamstore: list teams: %w", err)
	}

	var teams []Team
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(s.groupsDir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}
		t, err := parseTeamFile(path, info)
		if err == nil {
			_ = s.resolveTeamWorkspaces(ctx, t)
			teams = append(teams, *t)
		}
	}
	return teams, nil
}

// UpdateTeam updates an existing team by name. Only non-empty fields in t
// are applied. The ID, Name, and CreatedAt fields are preserved from the
// existing record.
func (s *Store) UpdateTeam(ctx context.Context, name string, t *Team) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := getTeamFilePath(s.groupsDir, name)
	info, err := os.Stat(path)
	if err != nil {
		// Try scanning case-insensitively
		foundPath, foundInfo, err2 := s.findFileCaseInsensitive(s.groupsDir, name)
		if err2 != nil {
			return fmt.Errorf("teamstore: team %q not found: %w", name, err)
		}
		path = foundPath
		info = foundInfo
	}

	existing, err := parseTeamFile(path, info)
	if err != nil {
		return err
	}

	existing.Description = t.Description
	existing.Workspaces = t.Workspaces
	existing.Projects = t.Projects
	existing.UpdatedAt = time.Now().Format(time.RFC3339)

	return s.writeTeamFile(path, existing)
}

// DeleteTeam removes a team by name.
func (s *Store) DeleteTeam(ctx context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if strings.EqualFold(name, "engineering") {
		return fmt.Errorf("teamstore: deletion of built-in team %q is not allowed", name)
	}

	path := getTeamFilePath(s.groupsDir, name)
	if _, err := os.Stat(path); err != nil {
		foundPath, _, err2 := s.findFileCaseInsensitive(s.groupsDir, name)
		if err2 != nil {
			return fmt.Errorf("teamstore: team %q not found", name)
		}
		path = foundPath
	}

	if err := os.Remove(path); err != nil {
		return fmt.Errorf("teamstore: delete team %q: %w", name, err)
	}
	return nil
}

// ─── Agent CRUD ─────────────────────────────────────────────────────────────

// CreateAgent inserts a new agent. If ID is empty, lowercase name is used.
func (s *Store) CreateAgent(ctx context.Context, a *Agent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if a.ID == "" {
		a.ID = strings.ToLower(a.Name)
	}
	now := time.Now().Format(time.RFC3339)
	a.CreatedAt = now
	a.UpdatedAt = now

	if strings.EqualFold(a.Name, "architect") {
		if a.SystemPrompt != BuiltinLeaderPrompt {
			return fmt.Errorf("teamstore: built-in leader %q must use the built-in prompt", a.Name)
		}
	}

	// Check if already exists
	path := getAgentFilePath(s.agentsDir, a.Name)
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("teamstore: agent %q already exists", a.Name)
	}

	return s.writeAgentFile(path, a)
}

// GetAgentByName retrieves an agent by its unique name.
func (s *Store) GetAgentByName(ctx context.Context, name string) (*Agent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := getAgentFilePath(s.agentsDir, name)
	info, err := os.Stat(path)
	if err != nil {
		foundPath, foundInfo, err2 := s.findFileCaseInsensitive(s.agentsDir, name)
		if err2 != nil {
			return nil, fmt.Errorf("teamstore: agent %q not found: %w", name, err)
		}
		path = foundPath
		info = foundInfo
	}

	return parseAgentFile(path, info)
}

// ListAgents returns all agents.
func (s *Store) ListAgents(ctx context.Context) ([]Agent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.agentsDir)
	if err != nil {
		return nil, fmt.Errorf("teamstore: list agents: %w", err)
	}

	var agents []Agent
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(s.agentsDir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}
		a, err := parseAgentFile(path, info)
		if err == nil {
			agents = append(agents, *a)
		}
	}
	return agents, nil
}

// ListAgentsByTeam returns all agents belonging to a given team.
func (s *Store) ListAgentsByTeam(ctx context.Context, teamName string) ([]Agent, error) {
	agents, err := s.ListAgents(ctx)
	if err != nil {
		return nil, err
	}

	var filtered []Agent
	for _, a := range agents {
		if strings.EqualFold(a.TeamName, teamName) {
			filtered = append(filtered, a)
		}
	}
	return filtered, nil
}

// ListLeaders returns all agents where is_leader = true.
func (s *Store) ListLeaders(ctx context.Context) ([]Agent, error) {
	agents, err := s.ListAgents(ctx)
	if err != nil {
		return nil, err
	}

	var filtered []Agent
	for _, a := range agents {
		if a.IsLeader {
			filtered = append(filtered, a)
		}
	}
	return filtered, nil
}

// UpdateAgent updates an existing agent by name. Only non-empty/relevant
// fields in a are applied. The ID, Name, and CreatedAt fields are preserved
// from the existing record.
func (s *Store) UpdateAgent(ctx context.Context, name string, a *Agent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := getAgentFilePath(s.agentsDir, name)
	info, err := os.Stat(path)
	if err != nil {
		foundPath, foundInfo, err2 := s.findFileCaseInsensitive(s.agentsDir, name)
		if err2 != nil {
			return fmt.Errorf("teamstore: agent %q not found: %w", name, err)
		}
		path = foundPath
		info = foundInfo
	}

	existing, err := parseAgentFile(path, info)
	if err != nil {
		return err
	}

	if strings.EqualFold(name, "architect") {
		if a.SystemPrompt != BuiltinLeaderPrompt {
			// Write the original unmodified prompt back to disk (revert file modification if any)
			existing.SystemPrompt = BuiltinLeaderPrompt
			_ = s.writeAgentFile(path, existing)
			return fmt.Errorf("teamstore: modification of built-in leader %q prompt is not allowed", name)
		}
	}

	existing.Description = a.Description
	existing.TeamName = a.TeamName
	existing.IsLeader = a.IsLeader
	existing.Model = a.Model
	existing.SystemPrompt = a.SystemPrompt
	existing.Permission = a.Permission
	existing.MCPServers = a.MCPServers
	existing.SkillIDs = a.SkillIDs
	existing.UpdatedAt = time.Now().Format(time.RFC3339)

	return s.writeAgentFile(path, existing)
}

// DeleteAgent removes an agent by name.
func (s *Store) DeleteAgent(ctx context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if strings.EqualFold(name, "architect") {
		return fmt.Errorf("teamstore: deletion of built-in leader %q is not allowed", name)
	}

	path := getAgentFilePath(s.agentsDir, name)
	if _, err := os.Stat(path); err != nil {
		foundPath, _, err2 := s.findFileCaseInsensitive(s.agentsDir, name)
		if err2 != nil {
			return fmt.Errorf("teamstore: agent %q not found", name)
		}
		path = foundPath
	}

	if err := os.Remove(path); err != nil {
		return fmt.Errorf("teamstore: delete agent %q: %w", name, err)
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

func getTeamFilePath(groupsDir, name string) string {
	return filepath.Join(groupsDir, strings.ToLower(name)+".md")
}

func getAgentFilePath(agentsDir, name string) string {
	return filepath.Join(agentsDir, strings.ToLower(name)+".md")
}

func (s *Store) findFileCaseInsensitive(dir, name string) (string, os.FileInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", nil, err
	}
	target := strings.ToLower(name) + ".md"
	for _, entry := range entries {
		if strings.EqualFold(entry.Name(), target) {
			path := filepath.Join(dir, entry.Name())
			info, err := entry.Info()
			if err != nil {
				return "", nil, err
			}
			return path, info, nil
		}
	}
	return "", nil, os.ErrNotExist
}

func (s *Store) writeTeamFile(path string, t *Team) error {
	fm := prompt.GroupFrontmatter{
		ID:        t.ID,
		Name:      t.Name,
		Projects:  t.Projects,
		CreatedAt: t.CreatedAt,
		UpdatedAt: t.UpdatedAt,
	}
	if t.Workspaces != nil {
		workspaces := make([]prompt.Workspace, len(t.Workspaces))
		for i, w := range t.Workspaces {
			workspaces[i] = prompt.Workspace{
				Name: w.Name,
				Path: w.Path,
				AutoWork: prompt.AutoWorkConfig{
					Enabled:                 w.AutoWork.Enabled,
					InitialCooldownMinutes:  w.AutoWork.InitialCooldownMinutes,
					PostTaskCooldownMinutes: w.AutoWork.PostTaskCooldownMinutes,
					MaxIntervalsPerDay:      w.AutoWork.MaxIntervalsPerDay,
				},
			}
		}
		fm.Workspaces = workspaces
	}

	fmBytes, err := yaml.Marshal(fm)
	if err != nil {
		return fmt.Errorf("teamstore: marshal team frontmatter: %w", err)
	}

	content := fmt.Sprintf("---\n%s---\n%s\n", strings.TrimSpace(string(fmBytes)), strings.TrimSpace(t.Description))
	return os.WriteFile(path, []byte(content), 0644)
}

func (s *Store) writeAgentFile(path string, a *Agent) error {
	fm := prompt.AgentFrontmatter{
		ID:          a.ID,
		Name:        a.Name,
		Description: a.Description,
		Model:       a.Model,
		Group:       a.TeamName,
		IsLeader:    a.IsLeader,
		Permission:  a.Permission,
		MCPServers:  a.MCPServers,
		Skills:      a.SkillIDs,
		CreatedAt:   a.CreatedAt,
		UpdatedAt:   a.UpdatedAt,
	}

	fmBytes, err := yaml.Marshal(fm)
	if err != nil {
		return fmt.Errorf("teamstore: marshal agent frontmatter: %w", err)
	}

	content := fmt.Sprintf("---\n%s---\n%s\n", strings.TrimSpace(string(fmBytes)), strings.TrimSpace(a.SystemPrompt))
	return os.WriteFile(path, []byte(content), 0644)
}

func parseTeamFile(path string, info os.FileInfo) (*Team, error) {
	gf, err := prompt.ParseGroupFile(path)
	if err != nil {
		return nil, fmt.Errorf("teamstore: parse team file %s: %w", path, err)
	}

	name := gf.Frontmatter.Name
	if name == "" {
		name = strings.TrimSuffix(info.Name(), ".md")
	}

	id := strings.ToLower(name)

	createdAt := gf.Frontmatter.CreatedAt
	updatedAt := gf.Frontmatter.UpdatedAt
	if createdAt == "" {
		createdAt = info.ModTime().Format(time.RFC3339)
	}
	if updatedAt == "" {
		updatedAt = info.ModTime().Format(time.RFC3339)
	}

	var workspaces []Workspace
	if gf.Frontmatter.Workspaces != nil {
		workspaces = make([]Workspace, len(gf.Frontmatter.Workspaces))
		for i, w := range gf.Frontmatter.Workspaces {
			workspaces[i] = Workspace{
				Name: w.Name,
				Path: w.Path,
				AutoWork: AutoWorkConfig{
					Enabled:                 w.AutoWork.Enabled,
					InitialCooldownMinutes:  w.AutoWork.InitialCooldownMinutes,
					PostTaskCooldownMinutes: w.AutoWork.PostTaskCooldownMinutes,
					MaxIntervalsPerDay:      w.AutoWork.MaxIntervalsPerDay,
				},
			}
		}
	}

	return &Team{
		ID:          id,
		Name:        name,
		Description: gf.Body,
		Workspaces:  workspaces,
		Projects:    gf.Frontmatter.Projects,
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}, nil
}

func parseAgentFile(path string, info os.FileInfo) (*Agent, error) {
	af, err := prompt.ParseAgentFile(path)
	if err != nil {
		return nil, fmt.Errorf("teamstore: parse agent file %s: %w", path, err)
	}

	name := af.Frontmatter.Name
	if name == "" {
		name = strings.TrimSuffix(info.Name(), ".md")
	}

	id := strings.ToLower(name)

	createdAt := af.Frontmatter.CreatedAt
	updatedAt := af.Frontmatter.UpdatedAt
	if createdAt == "" {
		createdAt = info.ModTime().Format(time.RFC3339)
	}
	if updatedAt == "" {
		updatedAt = info.ModTime().Format(time.RFC3339)
	}

	return &Agent{
		ID:           id,
		Name:         name,
		Description:  af.Frontmatter.Description,
		TeamName:     af.Frontmatter.Group,
		IsLeader:     af.Frontmatter.IsLeader,
		Model:        af.Frontmatter.Model,
		SystemPrompt: af.Body,
		Permission:   af.Frontmatter.Permission,
		MCPServers:   af.Frontmatter.MCPServers,
		SkillIDs:     af.Frontmatter.Skills,
		CreatedAt:    createdAt,
		UpdatedAt:    updatedAt,
	}, nil
}

func (s *Store) resolveTeamWorkspaces(ctx context.Context, t *Team) error {
	if s.db == nil || len(t.Projects) == 0 {
		return nil
	}

	for _, projID := range t.Projects {
		p, err := s.GetProject(ctx, projID)
		if err != nil {
			continue
		}
		ws := Workspace{
			Name: p.Name,
			Path: p.Path,
		}
		exists := false
		for _, existing := range t.Workspaces {
			if existing.Path == ws.Path || existing.Name == ws.Name {
				exists = true
				break
			}
		}
		if !exists {
			t.Workspaces = append(t.Workspaces, ws)
		}
	}
	return nil
}

// MigrateWorkspacesToProjects scans all group markdown files. If any team has workspaces
// directly defined, it creates corresponding project records in the database, associates
// them with the team, and updates the group markdown file (migrating them from workspaces to projects).
// It also cleans up any orphaned/non-existent project IDs from the team's associated projects list.
func (s *Store) MigrateWorkspacesToProjects(ctx context.Context) error {
	if s.db == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if database projects table already has data.
	// If it does, we do NOT perform any workspace merging/creation.
	var count int
	errCount := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects`).Scan(&count)
	dbHasData := (errCount == nil && count > 0)

	entries, err := os.ReadDir(s.groupsDir)
	if err != nil {
		return fmt.Errorf("migrate: read groups dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(s.groupsDir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}
		t, err := parseTeamFile(path, info)
		if err != nil {
			continue
		}

		workspacesPresent := len(t.Workspaces) > 0

		// 1. If database has NO data, migrate workspaces to projects in database and associate them.
		if !dbHasData && workspacesPresent {
			for _, ws := range t.Workspaces {
				// Check if a project with this name or path already exists in database
				var existingID string
				err := s.db.QueryRowContext(ctx,
					`SELECT id FROM projects WHERE path = ? OR name = ?`,
					ws.Path, ws.Name,
				).Scan(&existingID)

				if err != nil {
					if errors.Is(err, sql.ErrNoRows) {
						// Create a new project
						projID := strings.ToLower(t.Name + "-" + ws.Name)
						// Ensure unique ID in database by appending timestamp if it already exists
						var tempID string
						if errID := s.db.QueryRowContext(ctx, `SELECT id FROM projects WHERE id = ?`, projID).Scan(&tempID); errID == nil {
							projID = fmt.Sprintf("%s-%d", projID, time.Now().UnixNano())
						}

						proj := &Project{
							ID:          projID,
							Name:        ws.Name,
							Path:        ws.Path,
							Description: fmt.Sprintf("Migrated from team %s", t.Name),
						}

						func() {
							s.db.WMu.Lock()
							defer s.db.WMu.Unlock()
							now := time.Now().Format(time.RFC3339)
							proj.CreatedAt = now
							proj.UpdatedAt = now
							_, _ = s.db.ExecContext(ctx,
								`INSERT INTO projects (id, name, path, description, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
								proj.ID, proj.Name, proj.Path, proj.Description, proj.CreatedAt, proj.UpdatedAt,
							)
						}()
						existingID = proj.ID
					} else {
						continue
					}
				}

				// Associate project with the team
				existsInTeam := false
				for _, pID := range t.Projects {
					if pID == existingID {
						existsInTeam = true
						break
					}
				}
				if !existsInTeam {
					t.Projects = append(t.Projects, existingID)
				}
			}
		}

		// 2. Clean up non-existent project IDs from the team's projects list.
		// This runs regardless of dbHasData.
		var validProjects []string
		projectsChanged := false
		for _, pID := range t.Projects {
			var tempID string
			errVal := s.db.QueryRowContext(ctx, `SELECT id FROM projects WHERE id = ?`, pID).Scan(&tempID)
			if errVal == nil {
				validProjects = append(validProjects, pID)
			} else {
				// Project doesn't exist in DB, filter it out
				projectsChanged = true
			}
		}

		// 3. Clear workspaces and rewrite file if we modified projects or need to clear obsolete workspaces
		if (workspacesPresent && !dbHasData) || projectsChanged || (workspacesPresent && dbHasData) {
			t.Workspaces = nil
			if projectsChanged {
				t.Projects = validProjects
			}
			_ = s.writeTeamFile(path, t)
		}
	}

	return nil
}
