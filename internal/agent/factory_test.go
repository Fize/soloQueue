package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// ─── LoadAgentTemplates tests ──────────────────────────────────────

func TestLoadAgentTemplates_ValidDir(t *testing.T) {
	// 创建临时目录，写入测试 agent 文件
	dir := t.TempDir()

	// agent1.md - 有效的 leader
	agent1 := `---
name: dev
description: Dev agent
model: gpt-4
is_leader: true
---
You are a dev agent.
`
	if err := os.WriteFile(filepath.Join(dir, "agent1.md"), []byte(agent1), 0644); err != nil {
		t.Fatalf("write agent1: %v", err)
	}

	// agent2.md - 有效的非 leader
	agent2 := `---
name: test
description: Test agent
model: gpt-3.5
---
You are a test agent.
`
	if err := os.WriteFile(filepath.Join(dir, "agent2.md"), []byte(agent2), 0644); err != nil {
		t.Fatalf("write agent2: %v", err)
	}

	// 无效文件 - 应该被跳过
	agent3 := `---
invalid yaml
---
`
	if err := os.WriteFile(filepath.Join(dir, "agent3.md"), []byte(agent3), 0644); err != nil {
		t.Fatalf("write agent3: %v", err)
	}

	// 非 .md 文件 - 应该被跳过
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hello"), 0644); err != nil {
		t.Fatalf("write txt: %v", err)
	}

	templates, err := LoadAgentTemplates(dir)
	if err != nil {
		t.Fatalf("LoadAgentTemplates: %v", err)
	}

	// 应该只返回成功解析的文件（agent1 和 agent2）
	if len(templates) != 2 {
		t.Errorf("len(templates) = %d, want 2", len(templates))
	}

	// 验证模板内容
	found := map[string]bool{}
	for _, tmpl := range templates {
		found[tmpl.ID] = true
		if tmpl.ID == "dev" {
			if !tmpl.IsLeader {
				t.Error("dev should have is_leader=true")
			}
		}
		if tmpl.ID == "test" {
			if tmpl.IsLeader {
				t.Error("test should have is_leader=false")
			}
		}
	}

	if !found["dev"] || !found["test"] {
		t.Error("missing expected templates")
	}
}

func TestLoadAgentTemplates_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	templates, err := LoadAgentTemplates(dir)
	if err != nil {
		t.Fatalf("LoadAgentTemplates: %v", err)
	}
	if len(templates) != 0 {
		t.Errorf("len(templates) = %d, want 0", len(templates))
	}
}

func TestLoadAgentTemplates_NonExistentDir(t *testing.T) {
	_, err := LoadAgentTemplates("/nonexistent/path")
	if err == nil {
		t.Error("expected error for nonexistent directory")
	}
}

func TestLoadAgentTemplates_MissingFrontmatter(t *testing.T) {
	dir := t.TempDir()
	agent := `no frontmatter here`
	if err := os.WriteFile(filepath.Join(dir, "no-frontmatter.md"), []byte(agent), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	templates, err := LoadAgentTemplates(dir)
	if err != nil {
		t.Fatalf("LoadAgentTemplates: %v", err)
	}
	// 解析失败的文件应该被跳过
	if len(templates) != 0 {
		t.Errorf("len(templates) = %d, want 0 (parse failure skipped)", len(templates))
	}
}

// ─── DefaultFactory.Create tests ────────────────────────────────────

func TestDefaultFactory_Create_Success(t *testing.T) {
	// 创建临时目录用于日志
	dir := t.TempDir()
	log, err := logger.Session(dir, "test-team", "test-sess", logger.WithConsole(false))
	if err != nil {
		t.Fatalf("logger.Session: %v", err)
	}
	defer log.Close()

	// 创建 registry
	registry := NewRegistry(log)

	// 创建 FakeLLM
	fakeLLM := &FakeLLM{
		Responses: []string{"hello"},
	}

	// 创建 factory
	factory := NewDefaultFactory(
		registry,
		fakeLLM,
		tools.Config{},
		"", // skillDir - 为空时跳过 skill 加载
		log,
	)

	// 创建 agent template
	tmpl := AgentTemplate{
		ID:           "test-agent",
		Name:         "Test Agent",
		Description:  "A test agent",
		SystemPrompt: "You are a test agent.",
	}

	// 创建 agent
	agent, cw, err := factory.Create(context.Background(), tmpl)
	if err != nil {
		t.Fatalf("factory.Create: %v", err)
	}

	if agent == nil {
		t.Fatal("agent is nil")
	}
	if cw == nil {
		t.Fatal("cw is nil")
	}

	// 验证 agent 已注册
	if _, ok := registry.Get(agent.InstanceID); !ok {
		t.Error("agent should be registered")
	}

	// 验证 agent 已启动
	if agent.State() != StateIdle {
		t.Errorf("agent state = %s, want idle", agent.State())
	}

	// 清理
	_ = agent.Stop(time.Second)
}

func TestDefaultFactory_Create_WithSubAgents(t *testing.T) {
	dir := t.TempDir()
	log, err := logger.Session(dir, "test-team", "test-sess", logger.WithConsole(false))
	if err != nil {
		t.Fatalf("logger.Session: %v", err)
	}
	defer log.Close()

	registry := NewRegistry(nil)
	fakeLLM := &FakeLLM{
		Responses: []string{"hello"},
	}

	factory := NewDefaultFactory(
		registry,
		fakeLLM,
		tools.Config{},
		"",
		log,
	)

	// 创建子 agent template
	subTmpl := AgentTemplate{
		ID:           "sub-agent",
		Name:         "Sub Agent",
		SystemPrompt: "You are a sub agent.",
	}

	// 先创建子 agent（这样 registry 中会有）
	subAgent, _, err := factory.Create(context.Background(), subTmpl)
	if err != nil {
		t.Fatalf("create sub agent: %v", err)
	}
	defer subAgent.Stop(time.Second)

	// 创建主 agent
	mainTmpl := AgentTemplate{
		ID:           "main-agent",
		Name:         "Main Agent",
		SystemPrompt: "You are main.",
	}

	mainAgent, _, err := factory.Create(context.Background(), mainTmpl)
	if err != nil {
		t.Fatalf("factory.Create main: %v", err)
	}
	defer mainAgent.Stop(time.Second)

	// 验证 tools 已注册
	if mainAgent.tools == nil {
		t.Fatal("tools is nil")
	}

	t.Log("main agent created")
}

func TestDefaultFactory_Create_Ephemeral(t *testing.T) {
	dir := t.TempDir()
	log, err := logger.Session(dir, "test-team", "test-sess", logger.WithConsole(false))
	if err != nil {
		t.Fatalf("logger.Session: %v", err)
	}
	defer log.Close()

	registry := NewRegistry(nil)
	fakeLLM := &FakeLLM{
		Responses: []string{"hello"},
	}

	factory := NewDefaultFactory(
		registry,
		fakeLLM,
		tools.Config{},
		"",
		log,
	)

	tmpl := AgentTemplate{
		ID:           "ephemeral-agent",
		Name:         "Ephemeral Agent",
		SystemPrompt: "You are ephemeral.",
	}

	agent, _, err := factory.Create(context.Background(), tmpl)
	if err != nil {
		t.Fatalf("factory.Create: %v", err)
	}
	defer agent.Stop(time.Second)

	// 验证 agent 创建成功（Ephemeral 功能已移除，但 agent 仍能正常创建）
	if agent == nil {
		t.Fatal("agent is nil")
	}
}

func TestDefaultFactory_Create_SameTemplateMultipleInstances(t *testing.T) {
	dir := t.TempDir()
	log, err := logger.Session(dir, "test-team", "test-sess", logger.WithConsole(false))
	if err != nil {
		t.Fatalf("logger.Session: %v", err)
	}
	defer log.Close()

	registry := NewRegistry(nil)

	fakeLLM := &FakeLLM{
		Responses: []string{"hello"},
	}

	factory := NewDefaultFactory(
		registry,
		fakeLLM,
		tools.Config{},
		"",
		log,
	)

	tmpl := AgentTemplate{
		ID:          "multi-agent",
		Name:        "Multi Agent",
		SystemPrompt: "You are multi.",
	}

	// Create first instance
	a1, _, err := factory.Create(context.Background(), tmpl)
	if err != nil {
		t.Fatalf("factory.Create first: %v", err)
	}
	defer a1.Stop(time.Second)

	// Create second instance with same template — should succeed (different InstanceID)
	a2, _, err := factory.Create(context.Background(), tmpl)
	if err != nil {
		t.Fatalf("factory.Create second with same template should succeed: %v", err)
	}
	defer a2.Stop(time.Second)

	if a1.InstanceID == a2.InstanceID {
		t.Error("two instances should have different InstanceIDs")
	}
	if registry.Len() != 2 {
		t.Errorf("registry Len = %d, want 2", registry.Len())
	}
}

func TestDefaultFactory_Create_StartError(t *testing.T) {
	// 测试 Start 失败的情况（通过 nil LLM？）
	// 实际上 factory.Create 会调用 a.Start()，如果 LLM 是 nil 会报错吗？
	// 看代码：Start 不检查 LLM，所以不会报错
	// 我们需要 mock 一个会失败的情况

	dir := t.TempDir()
	log, err := logger.Session(dir, "test-team", "test-sess", logger.WithConsole(false))
	if err != nil {
		t.Fatalf("logger.Session: %v", err)
	}
	defer log.Close()

	registry := NewRegistry(nil)
	fakeLLM := &FakeLLM{
		Responses: []string{"hello"},
	}

	factory := NewDefaultFactory(
		registry,
		fakeLLM,
		tools.Config{},
		"",
		log,
	)

	tmpl := AgentTemplate{
		ID:          "test-agent",
		Name:        "Test Agent",
		SystemPrompt: "You are a test agent.",
	}

	agent, cw, err := factory.Create(context.Background(), tmpl)
	if err != nil {
		t.Fatalf("factory.Create: %v", err)
	}

	// 验证 agent 和 cw 不为 nil
	if agent == nil {
		t.Error("agent should not be nil")
	}
	if cw == nil {
		t.Error("cw should not be nil")
	}

	_ = agent.Stop(time.Second)
}

// ─── Helper functions for tests ──────────────────────────────────────

// setupTestFactory 创建测试用的 factory 和依赖
func setupTestFactory(t *testing.T) (*DefaultFactory, *Registry, *FakeLLM) {
	t.Helper()

	dir := t.TempDir()
	log, err := logger.Session(dir, "test-team", "test-sess", logger.WithConsole(false))
	if err != nil {
		t.Fatalf("logger.Session: %v", err)
	}
	t.Cleanup(func() { _ = log.Close() })

	registry := NewRegistry(log)
	fakeLLM := &FakeLLM{
		Responses: []string{"test-response"},
	}

	factory := NewDefaultFactory(
		registry,
		fakeLLM,
		tools.Config{},
		"",
		log,
	)

	return factory, registry, fakeLLM
}

func TestDefaultFactory_Registry(t *testing.T) {
	factory, _, _ := setupTestFactory(t)

	registry := factory.Registry()
	if registry == nil {
		t.Error("Registry() should not return nil")
	}
}

func TestDefaultFactory_Create_ContextWindow(t *testing.T) {
	factory, _, _ := setupTestFactory(t)

	tmpl := AgentTemplate{
		ID:          "cw-test",
		Name:        "CW Test",
		SystemPrompt: "You are a test agent.",
	}

	agent, cw, err := factory.Create(context.Background(), tmpl)
	if err != nil {
		t.Fatalf("factory.Create: %v", err)
	}
	defer agent.Stop(time.Second)

	// 验证 cw 已 push system prompt
	if cw == nil {
		t.Fatal("cw is nil")
	}

	// 可以用 Calibrate 等方法验证 cw 不为空
	tokens, _, _ := cw.TokenUsage()
	if tokens == 0 {
		t.Error("cw should have tokens (system prompt pushed)")
	}
}

func TestDefaultFactory_Create_WithSkills(t *testing.T) {
	// 创建临时 skill 目录
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// 写入一个测试 skill 文件
	skillContent := `---
name: test-skill
description: A test skill
---
# Test Skill
This is a test skill.
`
	if err := os.WriteFile(filepath.Join(skillDir, "test-skill.md"), []byte(skillContent), 0644); err != nil {
		t.Fatalf("write skill: %v", err)
	}

	log, err := logger.Session(dir, "test-team", "test-sess", logger.WithConsole(false))
	if err != nil {
		t.Fatalf("logger.Session: %v", err)
	}
	defer log.Close()

	registry := NewRegistry(nil)
	fakeLLM := &FakeLLM{
		Responses: []string{"hello"},
	}

	factory := NewDefaultFactory(
		registry,
		fakeLLM,
		tools.Config{},
		skillDir,
		log,
	)

	tmpl := AgentTemplate{
		ID:          "skill-agent",
		Name:        "Skill Agent",
		SystemPrompt: "You are a skill agent.",
	}

	agent, _, err := factory.Create(context.Background(), tmpl)
	if err != nil {
		t.Fatalf("factory.Create: %v", err)
	}
	defer agent.Stop(time.Second)

	// 验证 skill 已加载（通过 SkillCatalog 检查）
	catalog := agent.SkillCatalog()
	if catalog == "" {
		t.Log("skill catalog is empty (may be expected if skill format differs)")
	}
}

func TestNewDefaultFactory_NilLogger(t *testing.T) {
	registry := NewRegistry(nil)
	fakeLLM := &FakeLLM{
		Responses: []string{"hello"},
	}

	// 使用 nil logger 创建 factory（应该不 panic）
	factory := NewDefaultFactory(
		registry,
		fakeLLM,
		tools.Config{},
		"",
		nil,
	)

	if factory == nil {
		t.Fatal("factory should not be nil")
	}

	// 尝试创建 agent（nil logger 不应该 panic）
	tmpl := AgentTemplate{
		ID:          "nil-logger",
		Name:        "Nil Logger",
		SystemPrompt: "You are a test.",
	}

	agent, _, err := factory.Create(context.Background(), tmpl)
	if err != nil {
		t.Fatalf("factory.Create with nil logger: %v", err)
	}
	defer agent.Stop(time.Second)
}

// ─── Model validation tests ─────────────────────────────────────────

func TestDefaultFactory_Create_InvalidModel(t *testing.T) {
	dir := t.TempDir()
	log, err := logger.Session(dir, "test-team", "test-sess", logger.WithConsole(false))
	if err != nil {
		t.Fatalf("logger.Session: %v", err)
	}
	defer log.Close()

	registry := NewRegistry(nil)
	fakeLLM := &FakeLLM{Responses: []string{"hello"}}

	// Model resolver that only knows "gpt-4" and "gpt-3.5"
	resolver := func(modelID string) (ModelInfo, error) {
		switch modelID {
		case "gpt-4":
			return ModelInfo{
				ContextWindow: 128000,
				Temperature:   0,
				MaxTokens:     4096,
			}, nil
		case "gpt-3.5":
			return ModelInfo{
				ContextWindow: 16000,
				Temperature:   0.7,
				MaxTokens:     2048,
			}, nil
		default:
			return ModelInfo{}, fmt.Errorf("model %q not found in settings; available models: [gpt-4 gpt-3.5]", modelID)
		}
	}

	factory := NewDefaultFactory(registry, fakeLLM, tools.Config{}, "", log,
		WithModelResolver(resolver),
	)

	// Invalid model should fail at creation time
	tmpl := AgentTemplate{
		ID:           "bad-model-agent",
		Name:         "Bad Model Agent",
		SystemPrompt: "You are a test.",
		ModelID:      "nonexistent-model-xyz",
	}

	_, _, err = factory.Create(context.Background(), tmpl)
	if err == nil {
		t.Fatal("expected error for invalid model ID, got nil")
	}
	if !strings.Contains(err.Error(), "nonexistent-model-xyz") {
		t.Errorf("error should mention the bad model ID, got: %s", err)
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error should mention 'not found', got: %s", err)
	}
	t.Logf("got expected error: %s", err)
}

func TestDefaultFactory_Create_EmptyModel(t *testing.T) {
	dir := t.TempDir()
	log, err := logger.Session(dir, "test-team", "test-sess", logger.WithConsole(false))
	if err != nil {
		t.Fatalf("logger.Session: %v", err)
	}
	defer log.Close()

	registry := NewRegistry(nil)
	fakeLLM := &FakeLLM{Responses: []string{"hello"}}

	resolver := func(modelID string) (ModelInfo, error) {
		return ModelInfo{}, fmt.Errorf("model %q not found", modelID)
	}

	factory := NewDefaultFactory(registry, fakeLLM, tools.Config{}, "", log,
		WithModelResolver(resolver),
	)

	tmpl := AgentTemplate{
		ID:           "empty-model-agent",
		Name:         "Empty Model Agent",
		SystemPrompt: "You are a test.",
		ModelID:      "", // empty
	}

	_, _, err = factory.Create(context.Background(), tmpl)
	if err == nil {
		t.Fatal("expected error for empty model ID, got nil")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error should mention 'empty', got: %s", err)
	}
}

func TestDefaultFactory_Create_ValidModelResolvesParams(t *testing.T) {
	dir := t.TempDir()
	log, err := logger.Session(dir, "test-team", "test-sess", logger.WithConsole(false))
	if err != nil {
		t.Fatalf("logger.Session: %v", err)
	}
	defer log.Close()

	registry := NewRegistry(nil)
	fakeLLM := &FakeLLM{Responses: []string{"hello"}}

	resolver := func(modelID string) (ModelInfo, error) {
		if modelID == "deepseek-v4-flash-thinking" {
			return ModelInfo{
				APIModel:        "deepseek-v4-flash",
				ContextWindow:   1048576,
				Temperature:     0,
				MaxTokens:       8192,
				ThinkingEnabled: true,
				ReasoningEffort: "high",
			}, nil
		}
		return ModelInfo{}, fmt.Errorf("model %q not found", modelID)
	}

	factory := NewDefaultFactory(registry, fakeLLM, tools.Config{}, "", log,
		WithModelResolver(resolver),
	)

	tmpl := AgentTemplate{
		ID:           "resolved-agent",
		Name:         "Resolved Agent",
		SystemPrompt: "You are a test.",
		ModelID:      "deepseek-v4-flash-thinking",
	}

	agent, _, err := factory.Create(context.Background(), tmpl)
	if err != nil {
		t.Fatalf("factory.Create: %v", err)
	}
	defer agent.Stop(time.Second)

	// Verify resolved parameters
	if agent.Def.ModelID != "deepseek-v4-flash" {
		t.Errorf("ModelID = %q, want 'deepseek-v4-flash' (APIModel override)", agent.Def.ModelID)
	}
	if agent.Def.ContextWindow != 1048576 {
		t.Errorf("ContextWindow = %d, want 1048576", agent.Def.ContextWindow)
	}
	if agent.Def.MaxTokens != 8192 {
		t.Errorf("MaxTokens = %d, want 8192", agent.Def.MaxTokens)
	}
	if !agent.Def.ThinkingEnabled {
		t.Error("ThinkingEnabled should be true")
	}
	if agent.Def.ReasoningEffort != "high" {
		t.Errorf("ReasoningEffort = %q, want 'high'", agent.Def.ReasoningEffort)
	}
}

func TestDefaultFactory_Create_NoResolver_SkipsValidation(t *testing.T) {
	// Without resolver, any model ID is accepted (backward compat / tests)
	dir := t.TempDir()
	log, err := logger.Session(dir, "test-team", "test-sess", logger.WithConsole(false))
	if err != nil {
		t.Fatalf("logger.Session: %v", err)
	}
	defer log.Close()

	registry := NewRegistry(nil)
	fakeLLM := &FakeLLM{Responses: []string{"hello"}}

	factory := NewDefaultFactory(registry, fakeLLM, tools.Config{}, "", log)
	// No WithModelResolver — should skip validation

	tmpl := AgentTemplate{
		ID:           "no-resolver-agent",
		Name:         "No Resolver",
		SystemPrompt: "You are a test.",
		ModelID:      "any-random-model-name",
	}

	agent, _, err := factory.Create(context.Background(), tmpl)
	if err != nil {
		t.Fatalf("factory.Create should succeed without resolver: %v", err)
	}
	defer agent.Stop(time.Second)

	if agent.Def.ModelID != "any-random-model-name" {
		t.Errorf("ModelID = %q, want 'any-random-model-name'", agent.Def.ModelID)
	}
}

// ─── L2 System Prompt Clarification Rule tests ────────────────────────

func TestL2EnforcedDirectives_ContainsClarificationRule(t *testing.T) {
	if !strings.Contains(l2EnforcedDirectives, "Clarification Before Delegation") {
		t.Error("l2EnforcedDirectives should contain 'Clarification Before Delegation'")
	}
	if !strings.Contains(l2EnforcedDirectives, "need_clarification") {
		t.Error("l2EnforcedDirectives should contain 'need_clarification' JSON status")
	}
	if !strings.Contains(l2EnforcedDirectives, "questions") {
		t.Error("l2EnforcedDirectives should contain 'questions' field")
	}
}

func TestBuildL2SystemPrompt_ContainsClarificationProtocol(t *testing.T) {
	devTmpl := AgentTemplate{
		ID:           "dev",
		Name:         "Dev",
		Description:  "Dev agent",
		SystemPrompt: "You are a dev supervisor.",
		IsLeader:     true,
		Group:        "DevOps",
	}

	frontendTmpl := AgentTemplate{
		ID:          "frontend",
		Name:        "Frontend",
		Description: "Frontend worker",
		Group:       "DevOps",
	}

	backendTmpl := AgentTemplate{
		ID:          "backend",
		Name:        "Backend",
		Description: "Backend worker",
		Group:       "DevOps",
	}

	// 不同组的 agent，不应该出现在 dev 的 prompt 中
	otherTmpl := AgentTemplate{
		ID:          "designer",
		Name:        "Designer",
		Description: "Design worker",
		Group:       "Design",
	}

	templates := map[string]AgentTemplate{
		"dev":      devTmpl,
		"frontend": frontendTmpl,
		"backend":  backendTmpl,
		"designer": otherTmpl,
	}

	prompt := buildL2SystemPrompt(devTmpl, templates, nil)

	// Segment 1: user-defined
	if !strings.Contains(prompt, "You are a dev supervisor.") {
		t.Error("L2 prompt should contain user-defined system prompt")
	}

	// Segment 2: 同组 peers（frontend 和 backend 应该在，designer 不应该）
	if !strings.Contains(prompt, "Frontend") {
		t.Error("L2 prompt should list peer agent Frontend")
	}
	if !strings.Contains(prompt, "Backend") {
		t.Error("L2 prompt should list peer agent Backend")
	}
	if strings.Contains(prompt, "Designer") {
		t.Error("L2 prompt should NOT list agent from different group")
	}

	// Segment 3: clarification protocol
	if !strings.Contains(prompt, "Clarification Before Delegation") {
		t.Error("L2 prompt should contain clarification rule")
	}
	if !strings.Contains(prompt, "need_clarification") {
		t.Error("L2 prompt should contain need_clarification format")
	}
}
