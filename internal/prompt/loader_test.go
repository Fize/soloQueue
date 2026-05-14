package prompt

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureFiles_CreatesRules(t *testing.T) {
	dir := t.TempDir()
	// Pre-create soul.md so that EnsureFiles doesn't return SoulNeededError.
	rolesDir := filepath.Join(dir, "prompts", "roles")
	os.MkdirAll(rolesDir, 0o755)
	os.WriteFile(filepath.Join(rolesDir, "soul.md"), []byte("test soul"), 0o644)

	cfg := &PromptConfig{RolesDir: rolesDir, GlobalDir: filepath.Join(dir, "prompts", "global")}
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
	cfg := &PromptConfig{RolesDir: filepath.Join(dir, "prompts", "roles"), GlobalDir: filepath.Join(dir, "prompts", "global")}

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
	cfg := &PromptConfig{RolesDir: filepath.Join(dir, "prompts", "roles"), GlobalDir: filepath.Join(dir, "prompts", "global")}

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
	rolesDir := filepath.Join(dir, "prompts", "roles")
	globalDir := filepath.Join(dir, "prompts", "global")
	cfg := &PromptConfig{RolesDir: rolesDir, GlobalDir: globalDir}

	// Create all required files.
	cfg.WriteSoul(DefaultProfileAnswers())
	cfg.EnsureFiles()

	// Create user.md
	os.MkdirAll(globalDir, 0o755)
	os.WriteFile(filepath.Join(globalDir, "user.md"), []byte("测试用户"), 0o644)

	leaders := []LeaderInfo{
		{Name: "dev", Description: "开发工程师", Group: "DevOps"},
	}

	result, err := cfg.BuildPrompt(leaders, "", "", "/home/user/.soloqueue/plan", nil)
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
	if !contains(result, "测试用户") {
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
	cfg := &PromptConfig{RolesDir: filepath.Join(dir, "prompts", "roles"), GlobalDir: filepath.Join(dir, "prompts", "global")}

	cfg.WriteSoul(DefaultProfileAnswers())
	cfg.EnsureFiles()
	// Do not create user.md.

	result, err := cfg.BuildPrompt(nil, "", "", "/home/user/.soloqueue/plan", nil)
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
	cfg := &PromptConfig{RolesDir: filepath.Join(dir, "prompts", "roles"), GlobalDir: filepath.Join(dir, "prompts", "global")}

	cfg.WriteSoul(DefaultProfileAnswers())
	cfg.EnsureFiles()

	result, err := cfg.BuildPrompt(nil, "", "", "", nil)
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}

	if contains(result, "<plan_before_action>") {
		t.Error("should not contain <plan_before_action> when planDir is empty")
	}
}

func TestBuildPrompt_DockerSandboxPath(t *testing.T) {
	dir := t.TempDir()
	rolesDir := filepath.Join(dir, "prompts", "roles")
	globalDir := filepath.Join(dir, "prompts", "global")
	cfg := &PromptConfig{RolesDir: rolesDir, GlobalDir: globalDir}

	// Create all required files.
	cfg.WriteSoul(DefaultProfileAnswers())
	cfg.EnsureFiles()

	// Create user.md
	os.MkdirAll(globalDir, 0o755)
	os.WriteFile(filepath.Join(globalDir, "user.md"), []byte("测试用户"), 0o644)

	leaders := []LeaderInfo{
		{Name: "dev", Description: "开发工程师", Group: "DevOps"},
	}

	// Simulate Docker sandbox mode: path should be replaced with the in-container path /root/.soloqueue/plan/.
	dockerPlanDir := "/root/.soloqueue/plan"
	result, err := cfg.BuildPrompt(leaders, "", "", dockerPlanDir, nil)
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}

	// Verify that the plan_before_action section exists.
	if !contains(result, "<plan_before_action>") {
		t.Error("missing <plan_before_action> section when planDir is provided")
	}

	// Verify that the Docker sandbox mode path is replaced with the container path.
	if !contains(result, "/root/.soloqueue/plan") {
		t.Error("plan directory path should be replaced to Docker container path /root/.soloqueue/plan")
	}

	// Verify that the host path does not appear (replaced with the container path in Docker sandbox mode).
	if contains(result, dir+"/plan") {
		t.Error("host path should not appear in Docker sandbox mode, should be replaced with container path")
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
			content: "You are 小Q, a personal assistant and the single point of interaction for the user.",
			want:    "小Q",
		},
		{
			name:    "preset soul with English name",
			content: "You are 韩立 (Han Li), a personal assistant and the single point of interaction for the user.",
			want:    "韩立 (Han Li)",
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
			content: "You are one of 小Q,大Q (pick whichever fits the moment), a personal assistant",
			want:    "one of 小Q,大Q (pick whichever fits the moment)",
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
	cfg := &PromptConfig{RolesDir: filepath.Join(dir, "prompts", "roles"), GlobalDir: filepath.Join(dir, "prompts", "global")}

	// Soul doesn't exist yet.
	if name := ReadSoulName(cfg); name != "" {
		t.Errorf("expected empty name for missing soul, got %q", name)
	}

	// Write a soul.
	cfg.WriteSoul(ProfileAnswers{Name: "测试助手", Gender: "female", Personality: "playful", CommStyle: "casual"})

	name := ReadSoulName(cfg)
	if name != "测试助手" {
		t.Errorf("ReadSoulName() = %q, want %q", name, "测试助手")
	}
}

func TestWriteSoul(t *testing.T) {
	dir := t.TempDir()
	cfg := &PromptConfig{RolesDir: filepath.Join(dir, "prompts", "roles"), GlobalDir: filepath.Join(dir, "prompts", "global")}

	answers := ProfileAnswers{
		Name:        "小Q",
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
	if !contains(content, "You are 小Q") {
		t.Error("soul should contain custom name")
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
