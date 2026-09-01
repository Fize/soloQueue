package prompt

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureFiles_CreatesRules(t *testing.T) {
	dir := t.TempDir()
	// Pre-create soul.md so that EnsureFiles does not return SoulNeededError.
	rolesDir := filepath.Join(dir, "persona", "roles")
	os.MkdirAll(rolesDir, 0o755)
	os.WriteFile(filepath.Join(rolesDir, "soul.md"), []byte("test soul"), 0o644)

	cfg := &PromptConfig{RolesDir: rolesDir, GlobalDir: filepath.Join(dir, "persona", "global")}
	rulesCreated, err := cfg.EnsureFiles()
	if err != nil {
		t.Fatalf("EnsureFiles: %v", err)
	}
	if !rulesCreated {
		t.Error("rulesCreated should be true when rules.md is newly created")
	}

	// Verify that rules.md was created.
	data, err := os.ReadFile(cfg.RulesPath())
	if err != nil {
		t.Fatalf("read rules.md: %v", err)
	}
	if len(data) == 0 {
		t.Error("rules.md should not be empty")
	}
}

func TestEnsureFiles_SoulNeeded(t *testing.T) {
	dir := t.TempDir()
	cfg := &PromptConfig{RolesDir: filepath.Join(dir, "persona", "roles"), GlobalDir: filepath.Join(dir, "persona", "global")}

	_, err := cfg.EnsureFiles()
	if err == nil {
		t.Fatal("expected SoulNeededError")
	}

	var soulErr *SoulNeededError
	if !errorAs(err, &soulErr) {
		t.Fatalf("expected SoulNeededError, got %T: %v", err, err)
	}
}

func TestEnsureFiles_Idempotent(t *testing.T) {
	dir := t.TempDir()
	cfg := &PromptConfig{RolesDir: filepath.Join(dir, "persona", "roles"), GlobalDir: filepath.Join(dir, "persona", "global")}

	// First: write soul, then EnsureFiles.
	answers := DefaultProfileAnswers()
	cfg.WriteSoul(answers)

	rulesCreated1, err := cfg.EnsureFiles()
	if err != nil {
		t.Fatalf("first EnsureFiles: %v", err)
	}
	if !rulesCreated1 {
		t.Error("first call should create rules")
	}

	// Second: rules already exist.
	rulesCreated2, err := cfg.EnsureFiles()
	if err != nil {
		t.Fatalf("second EnsureFiles: %v", err)
	}
	if rulesCreated2 {
		t.Error("second call should not create rules again")
	}
}

func TestBuildPrompt_Integration(t *testing.T) {
	dir := t.TempDir()
	rolesDir := filepath.Join(dir, "persona", "roles")
	globalDir := filepath.Join(dir, "persona", "global")
	cfg := &PromptConfig{RolesDir: rolesDir, GlobalDir: globalDir}

	// Create all required files.
	cfg.WriteSoul(DefaultProfileAnswers())
	cfg.EnsureFiles()

	// Create user.md
	os.MkdirAll(globalDir, 0o755)
	os.WriteFile(filepath.Join(globalDir, "user.md"), []byte("Test User"), 0o644)

	leaders := []LeaderInfo{
		{Name: "dev", Description: "Development Engineer", Group: "DevOps"},
	}

	result, err := cfg.BuildPrompt(leaders, nil, "", "", "/home/user/.soloqueue/plan", nil)
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}

	// Verify XML assembly.
	if !contains(result, "<identity>") {
		t.Error("missing <identity> tag")
	}
	if !contains(result, "<user_context>") {
		t.Error("missing <user_context> tag")
	}
	if !contains(result, "<available_teams>") {
		t.Error("missing <available_teams> tag")
	}
	if !contains(result, "<rules>") {
		t.Error("missing <rules> tag")
	}
	if !contains(result, "dev (DevOps)") {
		t.Error("missing leader in routing table")
	}
	if !contains(result, "Test User") {
		t.Error("missing user context")
	}
	if !contains(result, "<plan_before_action>") {
		t.Error("missing <plan_before_action> section when planDir is provided")
	}
	if !contains(result, "/home/user/.soloqueue/plan") {
		t.Error("missing plan directory path in plan_before_action section")
	}
}

func TestBuildPrompt_NoUserCtx(t *testing.T) {
	dir := t.TempDir()
	cfg := &PromptConfig{RolesDir: filepath.Join(dir, "persona", "roles"), GlobalDir: filepath.Join(dir, "persona", "global")}

	cfg.WriteSoul(DefaultProfileAnswers())
	cfg.EnsureFiles()
	// Do not create user.md.

	result, err := cfg.BuildPrompt(nil, nil, "", "", "/home/user/.soloqueue/plan", nil)
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}

	if contains(result, "<user_context>") {
		t.Error("should not contain <user_context> when user.md is missing")
	}
	if !contains(result, "No Team Leaders") {
		t.Error("should contain fallback routing message for empty leaders")
	}
	if !contains(result, "<plan_before_action>") {
		t.Error("missing <plan_before_action> section when planDir is provided")
	}
}

func TestBuildPrompt_EmptyPlanDir(t *testing.T) {
	dir := t.TempDir()
	cfg := &PromptConfig{RolesDir: filepath.Join(dir, "persona", "roles"), GlobalDir: filepath.Join(dir, "persona", "global")}

	cfg.WriteSoul(DefaultProfileAnswers())
	cfg.EnsureFiles()

	result, err := cfg.BuildPrompt(nil, nil, "", "", "", nil)
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}

	if contains(result, "<plan_before_action>") {
		t.Error("should not contain <plan_before_action> when planDir is empty")
	}
}

func TestExtractSoulName(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "custom soul",
			content: "You are XiaoQ, a personal assistant and the single point of interaction for the user.",
			want:    "XiaoQ",
		},
		{
			name:    "preset soul with English name",
			content: "You are Han Li (Han Li), a personal assistant and the single point of interaction for the user.",
			want:    "Han Li (Han Li)",
		},
		{
			name:    "default name",
			content: "You are SoloQueue, a personal assistant and the single point of interaction for the user.",
			want:    "SoloQueue",
		},
		{
			name:    "no You are prefix",
			content: "This is a plain text without soul format.",
			want:    "",
		},
		{
			name:    "no comma after name",
			content: "You are SoloQueue a personal assistant",
			want:    "",
		},
		{
			name:    "empty content",
			content: "",
			want:    "",
		},
		{
			name:    "multi-name with comma separator",
			content: "You are one of XiaoQ,DaQ (pick whichever fits the moment), a personal assistant",
			want:    "one of XiaoQ,DaQ (pick whichever fits the moment)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractSoulName(tt.content)
			if got != tt.want {
				t.Errorf("extractSoulName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReadSoulName(t *testing.T) {
	dir := t.TempDir()
	cfg := &PromptConfig{RolesDir: filepath.Join(dir, "persona", "roles"), GlobalDir: filepath.Join(dir, "persona", "global")}

	// Soul does not exist yet.
	if name := ReadSoulName(cfg); name != "" {
		t.Errorf("expected empty name for missing soul, got %q", name)
	}

	// Write a soul.
	cfg.WriteSoul(ProfileAnswers{Name: "Test Assistant", Gender: "female", Personality: "playful", CommStyle: "casual"})

	name := ReadSoulName(cfg)
	if name != "Test Assistant" {
		t.Errorf("ReadSoulName() = %q, want %q", name, "Test Assistant")
	}
}

func TestWriteSoul(t *testing.T) {
	dir := t.TempDir()
	cfg := &PromptConfig{RolesDir: filepath.Join(dir, "persona", "roles"), GlobalDir: filepath.Join(dir, "persona", "global")}

	answers := ProfileAnswers{
		Name:        "XiaoQ",
		Gender:      "female",
		Personality: "playful",
		CommStyle:   "casual",
	}
	if err := cfg.WriteSoul(answers); err != nil {
		t.Fatalf("WriteSoul: %v", err)
	}

	data, err := os.ReadFile(cfg.soulPath())
	if err != nil {
		t.Fatalf("read soul: %v", err)
	}
	content := string(data)
	if !contains(content, "You are XiaoQ") {
		t.Error("soul should contain custom name")
	}
}

func TestEnsureFiles_MigratesOldPromptsDir(t *testing.T) {
	dir := t.TempDir()
	oldRolesDir := filepath.Join(dir, "prompts", "roles")
	if err := os.MkdirAll(oldRolesDir, 0o755); err != nil {
		t.Fatalf("mkdir old roles dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(oldRolesDir, "soul.md"), []byte("legacy soul content"), 0o644); err != nil {
		t.Fatalf("write legacy soul: %v", err)
	}

	cfg := &PromptConfig{
		RolesDir:  filepath.Join(dir, "persona", "roles"),
		GlobalDir: filepath.Join(dir, "persona", "global"),
	}

	_, err := cfg.EnsureFiles()
	if err != nil {
		t.Fatalf("EnsureFiles error: %v", err)
	}

	// Verify persona directory was created via migration
	migratedSoul, err := os.ReadFile(filepath.Join(dir, "persona", "roles", "soul.md"))
	if err != nil {
		t.Fatalf("read migrated soul: %v", err)
	}
	if string(migratedSoul) != "legacy soul content" {
		t.Errorf("migrated soul content mismatch, got %q", string(migratedSoul))
	}
}

func TestEnsureFiles_BothDirectoriesExist(t *testing.T) {
	dir := t.TempDir()

	// 1. Create old prompts directory with missing_in_persona.md and soul.md
	oldRolesDir := filepath.Join(dir, "prompts", "roles")
	os.MkdirAll(oldRolesDir, 0o755)
	os.WriteFile(filepath.Join(oldRolesDir, "soul.md"), []byte("old soul"), 0o644)
	os.WriteFile(filepath.Join(oldRolesDir, "extra.md"), []byte("extra content"), 0o644)

	// 2. Create new persona directory with new soul.md
	newRolesDir := filepath.Join(dir, "persona", "roles")
	os.MkdirAll(newRolesDir, 0o755)
	os.WriteFile(filepath.Join(newRolesDir, "soul.md"), []byte("new soul"), 0o644)

	cfg := &PromptConfig{
		RolesDir:  newRolesDir,
		GlobalDir: filepath.Join(dir, "persona", "global"),
	}

	_, err := cfg.EnsureFiles()
	if err != nil {
		t.Fatalf("EnsureFiles error: %v", err)
	}

	// 3. Verify persona/roles/soul.md preserved "new soul"
	newSoul, err := os.ReadFile(filepath.Join(newRolesDir, "soul.md"))
	if err != nil || string(newSoul) != "new soul" {
		t.Errorf("persona/roles/soul.md should preserve new content, got %q, err %v", string(newSoul), err)
	}

	// 4. Verify persona/roles/extra.md was merged from prompts
	extra, err := os.ReadFile(filepath.Join(newRolesDir, "extra.md"))
	if err != nil || string(extra) != "extra content" {
		t.Errorf("extra.md should be merged from old prompts, got %q, err %v", string(extra), err)
	}

	// 5. Verify old prompts directory is preserved to prevent unintended data deletion
	if _, err := os.Stat(filepath.Join(dir, "prompts")); os.IsNotExist(err) {
		t.Error("old prompts directory should be preserved safely without accidental deletion")
	}
}

// helpers

func errorAs(err error, target interface{}) bool {
	return errorAsStd(err, target)
}

func errorAsStd(err error, target interface{}) bool {
	// Simple reuse of the standard errors.As logic.
	type errorAs interface {
		As(interface{}) bool
	}
	if e, ok := err.(errorAs); ok {
		return e.As(target)
	}
	// Fallback: direct type assertion.
	if ptr, ok := target.(**SoulNeededError); ok {
		if pe, ok := err.(*SoulNeededError); ok {
			*ptr = pe
			return true
		}
	}
	return false
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && stringContains(s, substr)))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestLoadGlobalRules(t *testing.T) {
	dir := t.TempDir()
	globalDir := filepath.Join(dir, "global")
	os.MkdirAll(globalDir, 0o755)

	os.WriteFile(filepath.Join(globalDir, "user.md"), []byte("user content"), 0o644)
	os.WriteFile(filepath.Join(globalDir, "rules1.md"), []byte("rule 1"), 0o644)
	os.WriteFile(filepath.Join(globalDir, "rules2.md"), []byte("rule 2"), 0o644)
	os.WriteFile(filepath.Join(globalDir, "not-md.txt"), []byte("not md"), 0o644)
	os.MkdirAll(filepath.Join(globalDir, "subdir.md"), 0o755)

	rules, err := LoadGlobalRules(globalDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(rules) != 2 {
		t.Errorf("expected 2 rules, got %d", len(rules))
	}

	if rules["rules1.md"] != "rule 1" {
		t.Errorf("expected rule 1 content, got %s", rules["rules1.md"])
	}

	if rules["rules2.md"] != "rule 2" {
		t.Errorf("expected rule 2 content, got %s", rules["rules2.md"])
	}

	if _, ok := rules["user.md"]; ok {
		t.Error("user.md should be excluded")
	}

	// Test non-existent dir
	rules, err = LoadGlobalRules(filepath.Join(dir, "non-existent"))
	if err != nil {
		t.Fatalf("unexpected error for non-existent dir: %v", err)
	}
	if rules != nil {
		t.Error("expected nil rules for non-existent dir")
	}
}
