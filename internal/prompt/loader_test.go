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

	result, err := cfg.BuildPrompt(leaders, "", "", "/home/user/.soloqueue/plan")
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
	if !contains(result, "<plan_before_action>") {
		t.Error("missing <plan_before_action> section when planDir is provided")
	}
	if !contains(result, "/home/user/.soloqueue/plan") {
		t.Error("missing plan directory path in plan_before_action section")
	}
}

func TestBuildPrompt_NoUserCtx(t *testing.T) {
	dir := t.TempDir()
	cfg := &PromptConfig{RoleID: "main_assistant", BaseDir: dir}

	cfg.WriteProfile(DefaultProfileAnswers())
	cfg.EnsureFiles()
	// 不创建 user.md

	result, err := cfg.BuildPrompt(nil, "", "", "/home/user/.soloqueue/plan")
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
	cfg := &PromptConfig{RoleID: "main_assistant", BaseDir: dir}

	cfg.WriteProfile(DefaultProfileAnswers())
	cfg.EnsureFiles()

	result, err := cfg.BuildPrompt(nil, "", "", "")
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}

	if contains(result, "<plan_before_action>") {
		t.Error("should not contain <plan_before_action> when planDir is empty")
	}
}

func TestBuildPrompt_DockerSandboxPath(t *testing.T) {
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

	// 模拟 Docker 沙箱模式：路径应替换为容器内路径 /root/.soloqueue/plan/
	dockerPlanDir := "/root/.soloqueue/plan"
	result, err := cfg.BuildPrompt(leaders, "", "", dockerPlanDir)
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}

	// 验证 plan_before_action 段存在
	if !contains(result, "<plan_before_action>") {
		t.Error("missing <plan_before_action> section when planDir is provided")
	}

	// 验证 Docker 沙箱模式下路径替换为容器内路径
	if !contains(result, "/root/.soloqueue/plan") {
		t.Error("plan directory path should be replaced to Docker container path /root/.soloqueue/plan")
	}

	// 验证宿主机路径不应出现（Docker 沙箱模式下替换为容器路径）
	if contains(result, dir+"/plan") {
		t.Error("host path should not appear in Docker sandbox mode, should be replaced with container path")
	}
}

func TestExtractProfileName(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "custom profile",
			content: "You are 小Q, a personal assistant and the single point of interaction for the user.",
			want:    "小Q",
		},
		{
			name:    "preset profile with English name",
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
			content: "This is a plain text without profile format.",
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
			got := extractProfileName(tt.content)
			if got != tt.want {
				t.Errorf("extractProfileName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReadProfileName(t *testing.T) {
	dir := t.TempDir()
	cfg := &PromptConfig{RoleID: "main_assistant", BaseDir: dir}

	// Profile doesn't exist yet
	if name := ReadProfileName(cfg); name != "" {
		t.Errorf("expected empty name for missing profile, got %q", name)
	}

	// Write a profile
	cfg.WriteProfile(ProfileAnswers{Name: "测试助手", Gender: "female", Personality: "playful", CommStyle: "casual"})

	name := ReadProfileName(cfg)
	if name != "测试助手" {
		t.Errorf("ReadProfileName() = %q, want %q", name, "测试助手")
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
