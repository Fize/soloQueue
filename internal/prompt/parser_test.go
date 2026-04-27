package prompt

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseAgentFile(t *testing.T) {
	content := `---
name: dev
description: 全栈开发工程师
model: glm-5.0-ioa
reasoning: true
group: DevOps
is_leader: true
skills:
  - Agent Browser
  - pua
sub_agents:
  - qa_bot
---
这是 dev 的 system prompt body。
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
	if af.Frontmatter.Description != "全栈开发工程师" {
		t.Errorf("Description = %q, want %q", af.Frontmatter.Description, "全栈开发工程师")
	}
	if !af.Frontmatter.IsLeader {
		t.Error("IsLeader = false, want true")
	}
	if af.Frontmatter.Group != "DevOps" {
		t.Errorf("Group = %q, want %q", af.Frontmatter.Group, "DevOps")
	}
	if len(af.Frontmatter.Skills) != 2 {
		t.Errorf("Skills len = %d, want 2", len(af.Frontmatter.Skills))
	}
	if len(af.Frontmatter.SubAgents) != 1 {
		t.Errorf("SubAgents len = %d, want 1", len(af.Frontmatter.SubAgents))
	}
	if af.Body != "这是 dev 的 system prompt body。" {
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
description: 全栈开发工程师
is_leader: true
group: DevOps
---
body`), 0o644)

	// Non-leader agent
	os.WriteFile(filepath.Join(dir, "helper.md"), []byte(`---
name: helper
description: 辅助助手
is_leader: false
group: DevOps
---
body`), 0o644)

	leaders, err := LoadLeaders(dir)
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

	leaders, err := LoadLeaders(dir)
	if err != nil {
		t.Fatalf("LoadLeaders: %v", err)
	}
	if len(leaders) != 0 {
		t.Errorf("len(leaders) = %d, want 0", len(leaders))
	}
}
