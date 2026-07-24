// Package teamstore manages teams and agents using the filesystem.
// It keeps them as markdown files under groups/ and agents/ directories,
// providing full compatibility with the prompt and supervisor reloading systems.
package teamstore

import (
	"context"
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
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// Agent represents an agent (team member) stored in agents/ directory.
type Agent struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Description   string            `json:"description"`
	TeamName      string            `json:"team_name"`
	IsLeader      bool              `json:"is_leader"`
	Model         string            `json:"model"`
	SystemPrompt  string            `json:"system_prompt"`
	Permission    bool              `json:"permission"`
	MCPServers    []string          `json:"mcp_servers"`
	SkillIDs      []string          `json:"skill_ids"`
	Channels      map[string]string `json:"channels,omitempty"`
	NotifyChannel string            `json:"notify_channel,omitempty"`
	CreatedAt     string            `json:"created_at"`
	UpdatedAt     string            `json:"updated_at"`
}

// AgentTemplate is a flat representation used by the agent factory for
// compatibility with the existing agent template loading system.
type AgentTemplate struct {
	ID            string
	Name          string
	Description   string
	SystemPrompt  string
	ModelID       string
	IsLeader      bool
	Group         string // maps to TeamName
	Permission    bool
	MCPServers    []string
	SkillIDs      []string
	Channels      map[string]string
	NotifyChannel string
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

	existing.Description = a.Description
	existing.TeamName = a.TeamName
	existing.IsLeader = a.IsLeader
	existing.Model = a.Model
	existing.SystemPrompt = a.SystemPrompt
	existing.Permission = a.Permission
	existing.MCPServers = a.MCPServers
	existing.SkillIDs = a.SkillIDs
	existing.Channels = a.Channels
	existing.NotifyChannel = a.NotifyChannel
	existing.UpdatedAt = time.Now().Format(time.RFC3339)

	return s.writeAgentFile(path, existing)
}

// DeleteAgent removes an agent by name.
func (s *Store) DeleteAgent(ctx context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if strings.EqualFold(name, "Andrej Karpathy") ||
		strings.EqualFold(name, "explorer") ||
		strings.EqualFold(name, "editor") ||
		strings.EqualFold(name, "tester") {
		return fmt.Errorf("teamstore: deletion of built-in agent %q is not allowed", name)
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
		ID:            a.ID,
		Name:          a.Name,
		Description:   a.Description,
		SystemPrompt:  a.SystemPrompt,
		ModelID:       a.Model,
		IsLeader:      a.IsLeader,
		Group:         a.TeamName,
		Permission:    a.Permission,
		MCPServers:    a.MCPServers,
		SkillIDs:      a.SkillIDs,
		Channels:      a.Channels,
		NotifyChannel: a.NotifyChannel,
	}
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func getTeamFilePath(groupsDir, name string) string {
	return filepath.Join(groupsDir, strings.ToLower(name)+".md")
}

func getAgentFilePath(agentsDir, name string) string {
	if strings.EqualFold(name, "Andrej Karpathy") || strings.EqualFold(name, "AndrejKarpathy") {
		return filepath.Join(agentsDir, "AndrejKarpathy.md")
	}
	return filepath.Join(agentsDir, strings.ToLower(name)+".md")
}

func (s *Store) findFileCaseInsensitive(dir, name string) (string, os.FileInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", nil, err
	}
	target := strings.ToLower(name) + ".md"
	if strings.EqualFold(name, "Andrej Karpathy") || strings.EqualFold(name, "AndrejKarpathy") {
		target = "andrejkarpathy.md"
	}
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
		CreatedAt: t.CreatedAt,
		UpdatedAt: t.UpdatedAt,
	}

	fmBytes, err := yaml.Marshal(fm)
	if err != nil {
		return fmt.Errorf("teamstore: marshal team frontmatter: %w", err)
	}

	content := fmt.Sprintf("---\n%s\n---\n%s\n", strings.TrimSpace(string(fmBytes)), strings.TrimSpace(t.Description))
	return os.WriteFile(path, []byte(content), 0644)
}

func (s *Store) writeAgentFile(path string, a *Agent) error {
	fm := prompt.AgentFrontmatter{
		ID:            a.ID,
		Name:          a.Name,
		Description:   a.Description,
		Model:         a.Model,
		Group:         a.TeamName,
		IsLeader:      a.IsLeader,
		Permission:    a.Permission,
		MCPServers:    a.MCPServers,
		Skills:        a.SkillIDs,
		Channels:      a.Channels,
		NotifyChannel: a.NotifyChannel,
		CreatedAt:     a.CreatedAt,
		UpdatedAt:     a.UpdatedAt,
	}

	fmBytes, err := yaml.Marshal(fm)
	if err != nil {
		return fmt.Errorf("teamstore: marshal agent frontmatter: %w", err)
	}

	content := fmt.Sprintf("---\n%s\n---\n%s\n", strings.TrimSpace(string(fmBytes)), strings.TrimSpace(a.SystemPrompt))
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

	return &Team{
		ID:          id,
		Name:        name,
		Description: gf.Body,
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
		ID:            id,
		Name:          name,
		Description:   af.Frontmatter.Description,
		TeamName:      af.Frontmatter.Group,
		IsLeader:      af.Frontmatter.IsLeader,
		Model:         af.Frontmatter.Model,
		SystemPrompt:  af.Body,
		Permission:    af.Frontmatter.Permission,
		MCPServers:    af.Frontmatter.MCPServers,
		SkillIDs:      af.Frontmatter.Skills,
		Channels:      af.Frontmatter.Channels,
		NotifyChannel: af.Frontmatter.NotifyChannel,
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
	}, nil
}
