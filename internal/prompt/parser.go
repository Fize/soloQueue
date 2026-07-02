package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// AgentFrontmatter corresponds to the YAML frontmatter of ~/.soloqueue/agents/*.md.
type AgentFrontmatter struct {
	ID           string            `yaml:"id,omitempty"`
	Name         string            `yaml:"name"`
	Description  string            `yaml:"description"`
	Model        string            `yaml:"model"`
	Group        string            `yaml:"group"`
	IsLeader     bool              `yaml:"is_leader"`
	Permission   bool              `yaml:"permission"`
	MCPServers   []string          `yaml:"mcp_servers,omitempty"`
	Skills       []string          `yaml:"skills,omitempty"`
	CreatedAt    string            `yaml:"created_at,omitempty"`
	UpdatedAt    string            `yaml:"updated_at,omitempty"`
}

// GroupFrontmatter corresponds to the YAML frontmatter of ~/.soloqueue/groups/*.md.
type GroupFrontmatter struct {
	ID         string      `yaml:"id,omitempty"`
	Name       string      `yaml:"name"`
	Workspaces []Workspace `yaml:"workspaces,omitempty"`
	Projects   []string    `yaml:"projects,omitempty"`
	CreatedAt  string      `yaml:"created_at,omitempty"`
	UpdatedAt  string      `yaml:"updated_at,omitempty"`
}

// Workspace describes a team's associated workspace.
type Workspace struct {
	Name     string         `yaml:"name"`
	Path     string         `yaml:"path"`
	AutoWork AutoWorkConfig `yaml:"autoWork"`
}

// AutoWorkConfig describes the automatic work configuration.
type AutoWorkConfig struct {
	Enabled                 bool `yaml:"enabled"`
	InitialCooldownMinutes  int  `yaml:"initialCooldownMinutes"`
	PostTaskCooldownMinutes int  `yaml:"postTaskCooldownMinutes"`
	MaxIntervalsPerDay      int  `yaml:"maxIntervalsPerDay"`
}

// GroupFile parse result: frontmatter + markdown body (group description).
type GroupFile struct {
	Frontmatter GroupFrontmatter
	Body        string
}

// AgentFile parse result: frontmatter + markdown body.
type AgentFile struct {
	Frontmatter AgentFrontmatter
	Body        string
}

// ParseAgentFile parses a single agent markdown file (YAML frontmatter + body).
func ParseAgentFile(path string) (*AgentFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read agent file %s: %w", path, err)
	}

	content := string(data)

	// Extract the frontmatter between ---
	if !strings.HasPrefix(content, "---") {
		return nil, fmt.Errorf("agent file %s: missing frontmatter delimiter", path)
	}

	end := strings.Index(content[3:], "---")
	if end < 0 {
		return nil, fmt.Errorf("agent file %s: unclosed frontmatter", path)
	}

	fmContent := strings.TrimSpace(content[3 : end+3])
	body := strings.TrimSpace(content[end+6:])

	var fm AgentFrontmatter
	if err := yaml.Unmarshal([]byte(fmContent), &fm); err != nil {
		return nil, fmt.Errorf("parse frontmatter %s: %w", path, err)
	}

	return &AgentFile{Frontmatter: fm, Body: body}, nil
}

// LoadLeaders scans the agents directory and returns all agents with is_leader=true.
// Only extracts Name/Description/Group, not Skills (main Agent doesn't need to know tool details).
// If groups are provided, GroupDescription will be populated.
func LoadLeaders(agentsDir string, groups map[string]GroupFile) ([]LeaderInfo, error) {
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return nil, fmt.Errorf("read agents dir %s: %w", agentsDir, err)
	}

	var leaders []LeaderInfo
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		path := filepath.Join(agentsDir, entry.Name())
		af, err := ParseAgentFile(path)
		if err != nil {
			continue // Skip files that failed to parse
		}

		if af.Frontmatter.IsLeader {
			li := LeaderInfo{
				Name:        af.Frontmatter.Name,
				Description: af.Frontmatter.Description,
				Group:       af.Frontmatter.Group,
			}

			// Populate group information
			if gf, ok := groups[af.Frontmatter.Group]; ok {
				li.GroupDescription = gf.Body
			}

			leaders = append(leaders, li)
		}
	}

	return leaders, nil
}

// LoadAgentFiles scans the agents directory and returns all parsed AgentFiles.
//
// Does not filter by IsLeader, returns all .md files. Files that failed to parse are skipped (does not interrupt the process).
func LoadAgentFiles(agentsDir string) ([]AgentFile, error) {
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return nil, fmt.Errorf("read agents dir %s: %w", agentsDir, err)
	}

	var files []AgentFile
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		path := filepath.Join(agentsDir, entry.Name())
		af, err := ParseAgentFile(path)
		if err != nil {
			continue // Skip files that failed to parse
		}

		files = append(files, *af)
	}

	return files, nil
}

// ParseGroupFile parses a single group markdown file (YAML frontmatter + body).
func ParseGroupFile(path string) (*GroupFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read group file %s: %w", path, err)
	}

	content := string(data)

	if !strings.HasPrefix(content, "---") {
		return nil, fmt.Errorf("group file %s: missing frontmatter delimiter", path)
	}

	end := strings.Index(content[3:], "---")
	if end < 0 {
		return nil, fmt.Errorf("group file %s: unclosed frontmatter", path)
	}

	fmContent := strings.TrimSpace(content[3 : end+3])
	body := strings.TrimSpace(content[end+6:])

	var fm GroupFrontmatter
	if err := yaml.Unmarshal([]byte(fmContent), &fm); err != nil {
		return nil, fmt.Errorf("parse group frontmatter %s: %w", path, err)
	}

	return &GroupFile{Frontmatter: fm, Body: body}, nil
}

// LoadGroups scans the groups directory and returns a map of name -> GroupFile.
// If the directory does not exist, an empty map is returned instead of an error (for backward compatibility).
func LoadGroups(groupsDir string) (map[string]GroupFile, error) {
	entries, err := os.ReadDir(groupsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]GroupFile), nil
		}
		return nil, fmt.Errorf("read groups dir %s: %w", groupsDir, err)
	}

	groups := make(map[string]GroupFile)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		path := filepath.Join(groupsDir, entry.Name())
		gf, err := ParseGroupFile(path)
		if err != nil {
			continue // Skip files that failed to parse
		}

		name := gf.Frontmatter.Name
		if name == "" {
			// Use the filename (without .md) as a fallback
			name = strings.TrimSuffix(entry.Name(), ".md")
		}
		groups[name] = *gf
	}

	return groups, nil
}