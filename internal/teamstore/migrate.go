package teamstore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/prompt"
)

// MigrateFromFiles scans markdown-based team and agent definitions from the
// given directories and imports them into the store. The migration is
// idempotent: teams and agents that already exist (matched by name) are
// skipped without error.
//
// All writes are serialized through the shared database write mutex.
func (s *Store) MigrateFromFiles(groupsDir, agentsDir string) error {
	if err := s.migrateTeams(groupsDir); err != nil {
		return err
	}
	if err := s.migrateAgents(agentsDir); err != nil {
		return err
	}
	return nil
}

// migrateTeams scans *.md files in dir, parses group frontmatter, and
// creates any teams that don't already exist.
func (s *Store) migrateTeams(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // directory missing is not an error
		}
		return fmt.Errorf("teamstore: read groups dir %s: %w", dir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		gf, err := prompt.ParseGroupFile(path)
		if err != nil {
			continue // skip unparseable files
		}

		name := gf.Frontmatter.Name
		if name == "" {
			name = strings.TrimSuffix(entry.Name(), ".md")
		}

		// Idempotency: skip if team already exists
		if _, err := s.GetTeamByName(context.Background(), name); err == nil {
			continue
		}

		// Convert prompt.Workspace → teamstore.Workspace
		workspaces := make([]Workspace, 0, len(gf.Frontmatter.Workspaces))
		for _, pw := range gf.Frontmatter.Workspaces {
			workspaces = append(workspaces, Workspace{
				Name: pw.Name,
				Path: pw.Path,
				AutoWork: AutoWorkConfig{
					Enabled:                 pw.AutoWork.Enabled,
					InitialCooldownMinutes:  pw.AutoWork.InitialCooldownMinutes,
					PostTaskCooldownMinutes: pw.AutoWork.PostTaskCooldownMinutes,
					MaxIntervalsPerDay:      pw.AutoWork.MaxIntervalsPerDay,
				},
			})
		}

		t := &Team{
			Name:        name,
			Description: gf.Body,
			Workspaces:  workspaces,
		}
		// Use context.Background() for migration (no request context).
		if err := s.CreateTeam(context.Background(), t); err != nil {
			return fmt.Errorf("teamstore: migrate team %q: %w", name, err)
		}
	}
	return nil
}

// migrateAgents scans *.md files in dir, parses agent frontmatter, and
// creates any agents that don't already exist.
func (s *Store) migrateAgents(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("teamstore: read agents dir %s: %w", dir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		af, err := prompt.ParseAgentFile(path)
		if err != nil {
			continue
		}

		name := af.Frontmatter.Name
		if name == "" {
			name = strings.TrimSuffix(entry.Name(), ".md")
		}

		// Idempotency: skip if agent already exists
		if _, err := s.GetAgentByName(context.Background(), name); err == nil {
			continue
		}

		a := &Agent{
			Name:         name,
			Description:  af.Frontmatter.Description,
			TeamName:     af.Frontmatter.Group,
			IsLeader:     af.Frontmatter.IsLeader,
			Model:        af.Frontmatter.Model,
			SystemPrompt: af.Body,
			Permission:   af.Frontmatter.Permission,
			MCPServers:   af.Frontmatter.MCPServers,
			SkillIDs:     af.Frontmatter.Skills,
		}
		if err := s.CreateAgent(context.Background(), a); err != nil {
			return fmt.Errorf("teamstore: migrate agent %q: %w", name, err)
		}
	}
	return nil
}
