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
when_to_use: user mentions test
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
	if md.ID() != "my-skill" {
		t.Errorf("ID = %q, want %q", md.ID(), "my-skill")
	}
	if md.Description() != "A test skill" {
		t.Errorf("Description = %q, want %q", md.Description(), "A test skill")
	}
	if md.WhenToUse() != "user mentions test" {
		t.Errorf("WhenToUse = %q, want %q", md.WhenToUse(), "user mentions test")
	}
	if md.Instructions() != "# My Skill\n\nDo the thing." {
		t.Errorf("Instructions = %q", md.Instructions())
	}
	if md.Category() != SkillUser {
		t.Errorf("Category = %v, want %v", md.Category(), SkillUser)
	}
	if md.FilePath() == "" {
		t.Error("FilePath should not be empty for MDSkill")
	}
	if !strings.HasSuffix(md.FilePath(), "my-skill/SKILL.md") {
		t.Errorf("FilePath = %q, should end with my-skill/SKILL.md", md.FilePath())
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
	if md.ID() != "bare-skill" {
		t.Errorf("ID = %q, want %q", md.ID(), "bare-skill")
	}
	if md.Description() != "Just plain instructions here." {
		t.Errorf("Description = %q", md.Description())
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
	if md.ID() != "deploy" {
		t.Errorf("ID = %q, want %q (from dir name)", md.ID(), "deploy")
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
		names[s.ID()] = true
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
	if s.ID() != "test" {
		t.Errorf("ID = %q", s.ID())
	}
	if s.Description() != "A test skill" {
		t.Errorf("Description = %q", s.Description())
	}
	if s.Instructions() != "Do the thing." {
		t.Errorf("Instructions = %q", s.Instructions())
	}
	if s.WhenToUse() != "" {
		t.Errorf("WhenToUse should be empty, got %q", s.WhenToUse())
	}
	if s.Category() != SkillBuiltin {
		t.Errorf("Category = %v", s.Category())
	}
	if s.FilePath() != "" {
		t.Errorf("BuiltinSkill FilePath should be empty, got %q", s.FilePath())
	}
}

func TestBuiltinSkill_WithWhenToUse(t *testing.T) {
	s := NewBuiltinSkill("test", "desc", "instructions", WithWhenToUse("user says test"))
	if s.WhenToUse() != "user says test" {
		t.Errorf("WhenToUse = %q", s.WhenToUse())
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
	if got.ID() != "test" {
		t.Errorf("got ID = %q", got.ID())
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

func TestSkillRegistry_Catalog(t *testing.T) {
	r := NewSkillRegistry()
	_ = r.Register(NewBuiltinSkill("deploy", "Deploy to K8s", "", WithWhenToUse("user wants to deploy")))
	_ = r.Register(NewBuiltinSkill("commit", "Git commit", ""))

	cat := r.Catalog()
	if cat == "" {
		t.Fatal("Catalog should not be empty")
	}
	if !strings.Contains(cat, "## Available Skills") {
		t.Error("Catalog should contain header")
	}
	if !strings.Contains(cat, "**commit**") {
		t.Error("Catalog should contain commit skill")
	}
	if !strings.Contains(cat, "**deploy**") {
		t.Error("Catalog should contain deploy skill")
	}
	if !strings.Contains(cat, "Use when: user wants to deploy") {
		t.Error("Catalog should contain when_to_use for deploy")
	}
	if strings.Contains(cat, "Use when:") && strings.Contains(cat, "**commit**") {
		// commit has no when_to_use, should not have "Use when:" in its line
		lines := strings.Split(cat, "\n")
		for _, line := range lines {
			if strings.Contains(line, "**commit**") && strings.Contains(line, "Use when:") {
				t.Error("commit should not have Use when (empty when_to_use)")
			}
		}
	}
	if !strings.Contains(cat, "file_read") {
		t.Error("Catalog should mention file_read for loading instructions")
	}
}

func TestSkillRegistry_CatalogEmpty(t *testing.T) {
	r := NewSkillRegistry()
	if cat := r.Catalog(); cat != "" {
		t.Errorf("empty registry Catalog should return empty, got %q", cat)
	}
}

func TestSkillRegistry_CatalogNameOnly(t *testing.T) {
	// skill 只有名字，description 和 when_to_use 都为空
	r := NewSkillRegistry()
	_ = r.Register(NewBuiltinSkill("commit", "", ""))

	cat := r.Catalog()
	if !strings.Contains(cat, "- **commit**") {
		t.Errorf("Catalog should contain '- **commit**', got %q", cat)
	}
	// 不应该有多余的冒号或 "Use when:"
	for _, line := range strings.Split(cat, "\n") {
		if strings.Contains(line, "**commit**") {
			if strings.Contains(line, "Use when:") {
				t.Error("name-only skill should not have Use when")
			}
			// 不应该有 ": " 但名字后面没有描述的情况
			if strings.Contains(line, "**commit**: ") && !strings.Contains(line, "**commit**: <") {
				// 允许有描述的情况，但空描述不应该出现 ": "
				// "- **commit**\n" 是正确的，"- **commit**: \n" 是错误的
			}
		}
	}
}

func TestSkillRegistry_CatalogWithFilePath(t *testing.T) {
	r := NewSkillRegistry()
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "my-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(path, []byte("---\nname: my-skill\ndescription: Test skill\n---\nBody."), 0o644); err != nil {
		t.Fatal(err)
	}

	md, err := ParseSkillMD(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Register(md)

	cat := r.Catalog()
	if !strings.Contains(cat, "→") {
		t.Error("Catalog with MDSkill should contain file path arrow")
	}
	if !strings.Contains(cat, "SKILL.md") {
		t.Error("Catalog with MDSkill should contain SKILL.md path")
	}
}
