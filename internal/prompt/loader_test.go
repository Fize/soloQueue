package prompt

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureFiles_CreatesRules(t *testing.T) {
	dir := t.TempDir()
	// 预先创建 profile.md，这样 EnsureFiles 不会返回 ProfileNeededError
	roleDir := filepath.Join(dir, "roles", "main_assistant")
	os.MkdirAll(roleDir, 0o755)
	os.WriteFile(filepath.Join(roleDir, "profile.md"), []byte("test profile"), 0o644)

	cfg := &PromptConfig{RoleID: "main_assistant", BaseDir: dir}
	rulesCreated, err := cfg.EnsureFiles()
	if err != nil {
		t.Fatalf("EnsureFiles: %v", err)
	}
	if !rulesCreated {
		t.Error("rulesCreated should be true when rules.md is newly created")
	}

	// 验证 rules.md 被创建
	data, err := os.ReadFile(cfg.RulesPath())
	if err != nil {
		t.Fatalf("read rules.md: %v", err)
	}
	if len(data) == 0 {
		t.Error("rules.md should not be empty")
	}
}

func TestEnsureFiles_ProfileNeeded(t *testing.T) {
	dir := t.TempDir()
	cfg := &PromptConfig{RoleID: "main_assistant", BaseDir: dir}

	_, err := cfg.EnsureFiles()
	if err == nil {
		t.Fatal("expected ProfileNeededError")
	}

	var profileErr *ProfileNeededError
	if !errorAs(err, &profileErr) {
		t.Fatalf("expected ProfileNeededError, got %T: %v", err, err)
	}
}

func TestEnsureFiles_Idempotent(t *testing.T) {
	dir := t.TempDir()
	cfg := &PromptConfig{RoleID: "main_assistant", BaseDir: dir}

	// 首次：写 profile，然后 EnsureFiles
	answers := DefaultProfileAnswers()
	cfg.WriteProfile(answers)

	rulesCreated1, err := cfg.EnsureFiles()
	if err != nil {
		t.Fatalf("first EnsureFiles: %v", err)
	}
	if !rulesCreated1 {
		t.Error("first call should create rules")
	}

	// 第二次：rules 已存在
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
	cfg := &PromptConfig{RoleID: "main_assistant", BaseDir: dir}

	// 创建所有必要文件
	cfg.WriteProfile(DefaultProfileAnswers())
	cfg.EnsureFiles()

	// 创建 user.md
	os.MkdirAll(filepath.Join(dir, "global"), 0o755)
	os.WriteFile(filepath.Join(dir, "global", "user.md"), []byte("测试用户"), 0o644)

	leaders := []LeaderInfo{
		{Name: "dev", Description: "开发工程师", Group: "DevOps"},
	}

	result, err := cfg.BuildPrompt(leaders)
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}

	// 验证 XML 组装
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
}

func TestBuildPrompt_NoUserCtx(t *testing.T) {
	dir := t.TempDir()
	cfg := &PromptConfig{RoleID: "main_assistant", BaseDir: dir}

	cfg.WriteProfile(DefaultProfileAnswers())
	cfg.EnsureFiles()
	// 不创建 user.md

	result, err := cfg.BuildPrompt(nil)
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}

	if contains(result, "<user_context>") {
		t.Error("should not contain <user_context> when user.md is missing")
	}
	if !contains(result, "No Team Leaders") {
		t.Error("should contain fallback routing message for empty leaders")
	}
}

func TestWriteProfile(t *testing.T) {
	dir := t.TempDir()
	cfg := &PromptConfig{RoleID: "main_assistant", BaseDir: dir}

	answers := ProfileAnswers{
		Name:        "小Q",
		Gender:      "female",
		Personality: "playful",
		CommStyle:   "casual",
	}
	if err := cfg.WriteProfile(answers); err != nil {
		t.Fatalf("WriteProfile: %v", err)
	}

	data, err := os.ReadFile(cfg.profilePath())
	if err != nil {
		t.Fatalf("read profile: %v", err)
	}
	content := string(data)
	if !contains(content, "You are 小Q") {
		t.Error("profile should contain custom name")
	}
}

// helpers

func errorAs(err error, target interface{}) bool {
	return errorAsStd(err, target)
}

func errorAsStd(err error, target interface{}) bool {
	// 简单使用标准 errors.As 的逻辑
	type errorAs interface {
		As(interface{}) bool
	}
	if e, ok := err.(errorAs); ok {
		return e.As(target)
	}
	// Fallback: direct type assertion
	if ptr, ok := target.(**ProfileNeededError); ok {
		if pe, ok := err.(*ProfileNeededError); ok {
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
