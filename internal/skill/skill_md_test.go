package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSkillMD_WithFrontmatter(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "my-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	content := `---
name: my-skill
description: A test skill
allowed-tools: Read,Bash(git:*)
user-invocable: true
---
# My Skill

Do the thing.
`
	path := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	md, err := ParseSkillMD(path)
	if err != nil {
		t.Fatalf("ParseSkillMD: %v", err)
	}
	if md.ID != "my-skill" {
		t.Errorf("ID = %q, want %q", md.ID, "my-skill")
	}
	if md.Description != "A test skill" {
		t.Errorf("Description = %q, want %q", md.Description, "A test skill")
	}
	if md.Instructions != "# My Skill\n\nDo the thing." {
		t.Errorf("Instructions = %q", md.Instructions)
	}
	if md.Category != SkillUser {
		t.Errorf("Category = %v, want %v", md.Category, SkillUser)
	}
	if md.FilePath == "" {
		t.Error("FilePath should not be empty for MDSkill")
	}
	if !strings.HasSuffix(md.FilePath, "my-skill/SKILL.md") {
		t.Errorf("FilePath = %q, should end with my-skill/SKILL.md", md.FilePath)
	}
	if !md.UserInvocable {
		t.Error("UserInvocable should be true")
	}
	if len(md.AllowedTools) != 2 {
		t.Errorf("AllowedTools = %v, want 2 items", md.AllowedTools)
	}
	if md.AllowedTools[0] != "Read" {
		t.Errorf("AllowedTools[0] = %q, want %q", md.AllowedTools[0], "Read")
	}
	if md.AllowedTools[1] != "Bash(git:*)" {
		t.Errorf("AllowedTools[1] = %q, want %q", md.AllowedTools[1], "Bash(git:*)")
	}
}

func TestParseSkillMD_BackwardCompatible(t *testing.T) {
	// 现有 SKILL.md 只有 name 和 description，无新字段
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "old-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	content := `---
name: old-skill
description: An old skill
---
Old instructions.
`
	path := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	md, err := ParseSkillMD(path)
	if err != nil {
		t.Fatalf("ParseSkillMD: %v", err)
	}
	// 新字段应该有正确的默认值
	if md.DisableModelInvocation {
		t.Error("DisableModelInvocation default should be false")
	}
	if !md.UserInvocable {
		t.Error("UserInvocable default should be true")
	}
	if md.Context != "" {
		t.Errorf("Context default should be empty, got %q", md.Context)
	}
	if md.Agent != "" {
		t.Errorf("Agent default should be empty, got %q", md.Agent)
	}
	if len(md.AllowedTools) != 0 {
		t.Errorf("AllowedTools default should be nil, got %v", md.AllowedTools)
	}
}

func TestParseSkillMD_UserInvocableFalse(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "hidden-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	content := `---
name: hidden-skill
description: Not in slash menu
user-invocable: false
---
Hidden instructions.
`
	path := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	md, err := ParseSkillMD(path)
	if err != nil {
		t.Fatalf("ParseSkillMD: %v", err)
	}
	if md.UserInvocable {
		t.Error("UserInvocable should be false when explicitly set")
	}
}

func TestParseSkillMD_ForkMode(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "explore")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	content := `---
name: explore
description: Explore codebase
context: fork
agent: Explore
---
Explore the code.
`
	path := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	md, err := ParseSkillMD(path)
	if err != nil {
		t.Fatalf("ParseSkillMD: %v", err)
	}
	if md.Context != "fork" {
		t.Errorf("Context = %q, want %q", md.Context, "fork")
	}
	if md.Agent != "Explore" {
		t.Errorf("Agent = %q, want %q", md.Agent, "Explore")
	}
}

func TestParseSkillMD_NoFrontmatter(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "bare-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	content := "Just plain instructions here.\n"
	path := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	md, err := ParseSkillMD(path)
	if err != nil {
		t.Fatalf("ParseSkillMD: %v", err)
	}
	if md.ID != "bare-skill" {
		t.Errorf("ID = %q, want %q", md.ID, "bare-skill")
	}
	if md.Description != "Just plain instructions here." {
		t.Errorf("Description = %q", md.Description)
	}
}

func TestParseSkillMD_NameFallbackToDir(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "deploy")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	content := `---
description: Deploy things
---
Deploy now.
`
	path := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	md, err := ParseSkillMD(path)
	if err != nil {
		t.Fatalf("ParseSkillMD: %v", err)
	}
	if md.ID != "deploy" {
		t.Errorf("ID = %q, want %q (from dir name)", md.ID, "deploy")
	}
}

func TestLoadSkillsFromDir(t *testing.T) {
	dir := t.TempDir()

	s1 := filepath.Join(dir, "commit")
	if err := os.MkdirAll(s1, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s1, "SKILL.md"), []byte("---\nname: commit\ndescription: Git commit\n---\nCommit stuff."), 0o644); err != nil {
		t.Fatal(err)
	}

	s2 := filepath.Join(dir, "deploy")
	if err := os.MkdirAll(s2, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s2, "SKILL.md"), []byte("---\nname: deploy\ndescription: Deploy service\n---\nDeploy it."), 0o644); err != nil {
		t.Fatal(err)
	}

	s3 := filepath.Join(dir, "random")
	if err := os.MkdirAll(s3, 0o755); err != nil {
		t.Fatal(err)
	}

	skills, err := LoadSkillsFromDir(dir)
	if err != nil {
		t.Fatalf("LoadSkillsFromDir: %v", err)
	}
	if len(skills) != 2 {
		t.Errorf("got %d skills, want 2", len(skills))
	}

	names := map[string]bool{}
	for _, s := range skills {
		names[s.ID] = true
	}
	if !names["commit"] || !names["deploy"] {
		t.Errorf("expected commit and deploy, got %v", names)
	}
}

func TestLoadSkillsFromDir_NotExist(t *testing.T) {
	skills, err := LoadSkillsFromDir("/nonexistent/path")
	if err != nil {
		t.Errorf("non-existent dir should return nil error, got %v", err)
	}
	if skills != nil {
		t.Errorf("non-existent dir should return nil skills, got %v", skills)
	}
}

func TestBuiltinSkill(t *testing.T) {
	s := NewBuiltinSkill("test", "A test skill", "Do the thing.")
	if s.ID != "test" {
		t.Errorf("ID = %q", s.ID)
	}
	if s.Description != "A test skill" {
		t.Errorf("Description = %q", s.Description)
	}
	if s.Instructions != "Do the thing." {
		t.Errorf("Instructions = %q", s.Instructions)
	}
	if !s.UserInvocable {
		t.Error("UserInvocable default should be true")
	}
	if s.Category != SkillBuiltin {
		t.Errorf("Category = %v", s.Category)
	}
	if s.FilePath != "" {
		t.Errorf("BuiltinSkill FilePath should be empty, got %q", s.FilePath)
	}
}

func TestBuiltinSkill_WithOptions(t *testing.T) {
	s := NewBuiltinSkill("test", "desc", "instructions",
		WithDisableModelInvocation(),
		WithUserInvocable(false),
		WithContext("fork"),
		WithAgent("Explore"),
		WithAllowedTools([]string{"Read", "Grep"}),
	)
	if !s.DisableModelInvocation {
		t.Error("DisableModelInvocation should be true")
	}
	if s.UserInvocable {
		t.Error("UserInvocable should be false")
	}
	if s.Context != "fork" {
		t.Errorf("Context = %q, want %q", s.Context, "fork")
	}
	if s.Agent != "Explore" {
		t.Errorf("Agent = %q, want %q", s.Agent, "Explore")
	}
	if len(s.AllowedTools) != 2 {
		t.Errorf("AllowedTools = %v, want 2 items", s.AllowedTools)
	}
}

func TestSkillRegistry_RegisterAndGet(t *testing.T) {
	r := NewSkillRegistry()
	s := NewBuiltinSkill("test", "desc", "instructions")

	if err := r.Register(s); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got, ok := r.GetSkill("test")
	if !ok {
		t.Fatal("GetSkill not found")
	}
	if got.ID != "test" {
		t.Errorf("got ID = %q", got.ID)
	}
}

func TestSkillRegistry_Duplicate(t *testing.T) {
	r := NewSkillRegistry()
	_ = r.Register(NewBuiltinSkill("x", "d", "i"))
	err := r.Register(NewBuiltinSkill("x", "d2", "i2"))
	if err == nil {
		t.Error("duplicate register should error")
	}
}

func TestSkillRegistry_Nil(t *testing.T) {
	r := NewSkillRegistry()
	err := r.Register(nil)
	if err != ErrSkillNil {
		t.Errorf("Register(nil) = %v, want ErrSkillNil", err)
	}
}

func TestSkillRegistry_EmptyID(t *testing.T) {
	r := NewSkillRegistry()
	err := r.Register(&Skill{ID: ""})
	if err != ErrSkillIDEmpty {
		t.Errorf("Register(empty ID) = %v, want ErrSkillIDEmpty", err)
	}
}

func TestParseAllowedTools(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"", nil},
		{"Read", []string{"Read"}},
		{"Read,Write,Bash(git:*)", []string{"Read", "Write", "Bash(git:*)"}},
		{" Read , Write ", []string{"Read", "Write"}},
		{",,", nil},
	}
	for _, tt := range tests {
		got := ParseAllowedTools(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("ParseAllowedTools(%q) = %v, want %v", tt.input, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("ParseAllowedTools(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}
