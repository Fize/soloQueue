package prompt

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseAgentFile(t *testing.T) {
	content := `---
name: dev
description: Full-stack developer
model: glm-5.0-ioa
group: DevOps
is_leader: true
---
This is dev's system prompt body.
`
	dir := t.TempDir()
	path := filepath.Join(dir, "dev.md")
	os.WriteFile(path, []byte(content), 0o644)

	af, err := ParseAgentFile(path)
	if err != nil {
		t.Fatalf("ParseAgentFile: %v", err)
	}

	if af.Frontmatter.Name != "dev" {
		t.Errorf("Name = %q, want %q", af.Frontmatter.Name, "dev")
	}
	if af.Frontmatter.Description != "Full-stack developer" {
		t.Errorf("Description = %q, want %q", af.Frontmatter.Description, "Full-stack developer")
	}
	if !af.Frontmatter.IsLeader {
		t.Error("IsLeader = false, want true")
	}
	if af.Frontmatter.Group != "DevOps" {
		t.Errorf("Group = %q, want %q", af.Frontmatter.Group, "DevOps")
	}
	if af.Body != "This is dev's system prompt body." {
		t.Errorf("Body = %q, unexpected", af.Body)
	}
}

func TestParseAgentFile_NoFrontmatter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.md")
	os.WriteFile(path, []byte("no frontmatter here"), 0o644)

	_, err := ParseAgentFile(path)
	if err == nil {
		t.Fatal("expected error for missing frontmatter")
	}
}

func TestLoadLeaders(t *testing.T) {
	dir := t.TempDir()

	// Leader agent
	os.WriteFile(filepath.Join(dir, "dev.md"), []byte(`---
name: dev
description: Full-stack developer
is_leader: true
group: DevOps
---
body`), 0o644)

	// Non-leader agent
	os.WriteFile(filepath.Join(dir, "helper.md"), []byte(`---
name: helper
description: Assistant helper
is_leader: false
group: DevOps
---
body`), 0o644)

	leaders, err := LoadLeaders(dir, nil)
	if err != nil {
		t.Fatalf("LoadLeaders: %v", err)
	}

	if len(leaders) != 1 {
		t.Fatalf("len(leaders) = %d, want 1", len(leaders))
	}
	if leaders[0].Name != "dev" {
		t.Errorf("Name = %q, want %q", leaders[0].Name, "dev")
	}
	if leaders[0].Group != "DevOps" {
		t.Errorf("Group = %q, want %q", leaders[0].Group, "DevOps")
	}
}

func TestLoadLeaders_EmptyDir(t *testing.T) {
	dir := t.TempDir()

	leaders, err := LoadLeaders(dir, nil)
	if err != nil {
		t.Fatalf("LoadLeaders: %v", err)
	}
	if len(leaders) != 0 {
		t.Errorf("len(leaders) = %d, want 0", len(leaders))
	}
}

func TestLoadLeaders_WithGroups(t *testing.T) {
	agentsDir := t.TempDir()
	groupsDir := t.TempDir()

	// Create group file
	os.WriteFile(filepath.Join(groupsDir, "DevOps.md"), []byte(`---
name: DevOps
workspaces:
  - name: kumquat
    path: /Users/test/kumquat
    autoWork:
      enabled: false
      initialCooldownMinutes: 1
      postTaskCooldownMinutes: 30
      maxIntervalsPerDay: 24
---
Development team, focused on front-end and back-end development
`), 0o644)

	// Create leader agent
	os.WriteFile(filepath.Join(agentsDir, "dev.md"), []byte(`---
name: dev
description: Full-stack developer
is_leader: true
group: DevOps
---
body`), 0o644)

	// Load groups
	groups, err := LoadGroups(groupsDir)
	if err != nil {
		t.Fatalf("LoadGroups: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("len(groups) = %d, want 1", len(groups))
	}
	if groups["DevOps"].Body != "Development team, focused on front-end and back-end development" {
		t.Errorf("GroupBody = %q, want %q", groups["DevOps"].Body, "Development team, focused on front-end and back-end development")
	}

	// Load leaders (passing groups)
	leaders, err := LoadLeaders(agentsDir, groups)
	if err != nil {
		t.Fatalf("LoadLeaders: %v", err)
	}

	if len(leaders) != 1 {
		t.Fatalf("len(leaders) = %d, want 1", len(leaders))
	}
	l := leaders[0]
	if l.GroupDescription != "Development team, focused on front-end and back-end development" {
		t.Errorf("GroupDescription = %q, want %q", l.GroupDescription, "Development team, focused on front-end and back-end development")
	}
}

func TestLoadGroups(t *testing.T) {
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "DevOps.md"), []byte(`---
name: DevOps
workspaces:
  - name: kumquat
    path: /Users/test/kumquat
---
Development team
`), 0o644)

	os.WriteFile(filepath.Join(dir, "Design.md"), []byte(`---
name: Design
---
Designer team
`), 0o644)

	groups, err := LoadGroups(dir)
	if err != nil {
		t.Fatalf("LoadGroups: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("len(groups) = %d, want 2", len(groups))
	}
	if groups["DevOps"].Body != "Development team" {
		t.Errorf("DevOps body = %q, want %q", groups["DevOps"].Body, "Development team")
	}
	if len(groups["DevOps"].Frontmatter.Workspaces) != 1 {
		t.Errorf("DevOps workspaces len = %d, want 1", len(groups["DevOps"].Frontmatter.Workspaces))
	}
	if groups["Design"].Body != "Designer team" {
		t.Errorf("Design body = %q, want %q", groups["Design"].Body, "Designer team")
	}
}

func TestLoadGroups_NonexistentDir(t *testing.T) {
	groups, err := LoadGroups("/nonexistent/path")
	if err != nil {
		t.Fatalf("LoadGroups on nonexistent dir should not error: %v", err)
	}
	if len(groups) != 0 {
		t.Errorf("len(groups) = %d, want 0", len(groups))
	}
}