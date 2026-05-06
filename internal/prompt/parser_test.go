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
group: DevOps
is_leader: true
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

	// 创建 group 文件
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
开发团队，专注前后端开发
`), 0o644)

	// 创建 leader agent
	os.WriteFile(filepath.Join(agentsDir, "dev.md"), []byte(`---
name: dev
description: 全栈开发工程师
is_leader: true
group: DevOps
---
body`), 0o644)

	// 加载 groups
	groups, err := LoadGroups(groupsDir)
	if err != nil {
		t.Fatalf("LoadGroups: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("len(groups) = %d, want 1", len(groups))
	}
	if groups["DevOps"].Body != "开发团队，专注前后端开发" {
		t.Errorf("GroupBody = %q, want %q", groups["DevOps"].Body, "开发团队，专注前后端开发")
	}

	// 加载 leaders（传入 groups）
	leaders, err := LoadLeaders(agentsDir, groups)
	if err != nil {
		t.Fatalf("LoadLeaders: %v", err)
	}

	if len(leaders) != 1 {
		t.Fatalf("len(leaders) = %d, want 1", len(leaders))
	}
	l := leaders[0]
	if l.GroupDescription != "开发团队，专注前后端开发" {
		t.Errorf("GroupDescription = %q, want %q", l.GroupDescription, "开发团队，专注前后端开发")
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
开发团队
`), 0o644)

	os.WriteFile(filepath.Join(dir, "Design.md"), []byte(`---
name: Design
---
设计师团队
`), 0o644)

	groups, err := LoadGroups(dir)
	if err != nil {
		t.Fatalf("LoadGroups: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("len(groups) = %d, want 2", len(groups))
	}
	if groups["DevOps"].Body != "开发团队" {
		t.Errorf("DevOps body = %q, want %q", groups["DevOps"].Body, "开发团队")
	}
	if len(groups["DevOps"].Frontmatter.Workspaces) != 1 {
		t.Errorf("DevOps workspaces len = %d, want 1", len(groups["DevOps"].Frontmatter.Workspaces))
	}
	if groups["Design"].Body != "设计师团队" {
		t.Errorf("Design body = %q, want %q", groups["Design"].Body, "设计师团队")
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
