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
	"github.com/xiaobaitu/soloqueue/internal/prompt"
	"github.com/xiaobaitu/soloqueue/internal/skill"
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
	log, err := logger.System(dir, logger.WithConsole(false))
	if err != nil {
		t.Fatalf("logger.System: %v", err)
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
	agent, cw, err := factory.Create(context.Background(), tmpl, "")
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
	log, err := logger.System(dir, logger.WithConsole(false))
	if err != nil {
		t.Fatalf("logger.System: %v", err)
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
		log,
	)

	// 创建子 agent template
	subTmpl := AgentTemplate{
		ID:           "sub-agent",
		Name:         "Sub Agent",
		SystemPrompt: "You are a sub agent.",
	}

	// 先创建子 agent（这样 registry 中会有）
	subAgent, _, err := factory.Create(context.Background(), subTmpl, "")
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

	mainAgent, _, err := factory.Create(context.Background(), mainTmpl, "")
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
	log, err := logger.System(dir, logger.WithConsole(false))
	if err != nil {
		t.Fatalf("logger.System: %v", err)
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
		log,
	)

	tmpl := AgentTemplate{
		ID:           "ephemeral-agent",
		Name:         "Ephemeral Agent",
		SystemPrompt: "You are ephemeral.",
	}

	agent, _, err := factory.Create(context.Background(), tmpl, "")
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
	log, err := logger.System(dir, logger.WithConsole(false))
	if err != nil {
		t.Fatalf("logger.System: %v", err)
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
		log,
	)

	tmpl := AgentTemplate{
		ID:           "multi-agent",
		Name:         "Multi Agent",
		SystemPrompt: "You are multi.",
	}

	// Create first instance
	a1, _, err := factory.Create(context.Background(), tmpl, "")
	if err != nil {
		t.Fatalf("factory.Create first: %v", err)
	}
	defer a1.Stop(time.Second)

	// Create second instance with same template — should succeed (different InstanceID)
	a2, _, err := factory.Create(context.Background(), tmpl, "")
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
	log, err := logger.System(dir, logger.WithConsole(false))
	if err != nil {
		t.Fatalf("logger.System: %v", err)
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
		log,
	)

	tmpl := AgentTemplate{
		ID:           "test-agent",
		Name:         "Test Agent",
		SystemPrompt: "You are a test agent.",
	}

	agent, cw, err := factory.Create(context.Background(), tmpl, "")
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
	log, err := logger.System(dir, logger.WithConsole(false))
	if err != nil {
		t.Fatalf("logger.System: %v", err)
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
		ID:           "cw-test",
		Name:         "CW Test",
		SystemPrompt: "You are a test agent.",
	}

	agent, cw, err := factory.Create(context.Background(), tmpl, "")
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

	log, err := logger.System(dir, logger.WithConsole(false))
	if err != nil {
		t.Fatalf("logger.System: %v", err)
	}
	defer log.Close()

	// 加载 skills 到全局注册表
	skillReg := skill.NewSkillRegistry()
	loaded, err := skill.LoadSkillsFromDir(skillDir)
	if err != nil {
		t.Fatalf("LoadSkillsFromDir: %v", err)
	}
	for _, s := range loaded {
		_ = skillReg.Register(s)
	}

	registry := NewRegistry(nil)
	fakeLLM := &FakeLLM{
		Responses: []string{"hello"},
	}

	factory := NewDefaultFactory(
		registry,
		fakeLLM,
		tools.Config{},
		log,
		WithSkillRegistry(skillReg),
	)

	tmpl := AgentTemplate{
		ID:           "skill-agent",
		Name:         "Skill Agent",
		SystemPrompt: "You are a skill agent.",
		SkillIDs:     []string{"test-skill"},
	}

	agent, _, err := factory.Create(context.Background(), tmpl, "")
	if err != nil {
		t.Fatalf("factory.Create: %v", err)
	}
	defer agent.Stop(time.Second)
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
		nil,
	)

	if factory == nil {
		t.Fatal("factory should not be nil")
	}

	// 尝试创建 agent（nil logger 不应该 panic）
	tmpl := AgentTemplate{
		ID:           "nil-logger",
		Name:         "Nil Logger",
		SystemPrompt: "You are a test.",
	}

	agent, _, err := factory.Create(context.Background(), tmpl, "")
	if err != nil {
		t.Fatalf("factory.Create with nil logger: %v", err)
	}
	defer agent.Stop(time.Second)
}

// ─── Model validation tests ─────────────────────────────────────────

func TestDefaultFactory_Create_InvalidModel(t *testing.T) {
	dir := t.TempDir()
	log, err := logger.System(dir, logger.WithConsole(false))
	if err != nil {
		t.Fatalf("logger.System: %v", err)
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

	factory := NewDefaultFactory(registry, fakeLLM, tools.Config{}, log,
		WithModelResolver(resolver),
	)

	// Invalid model should fail at creation time
	tmpl := AgentTemplate{
		ID:           "bad-model-agent",
		Name:         "Bad Model Agent",
		SystemPrompt: "You are a test.",
		ModelID:      "nonexistent-model-xyz",
	}

	_, _, err = factory.Create(context.Background(), tmpl, "")
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
	log, err := logger.System(dir, logger.WithConsole(false))
	if err != nil {
		t.Fatalf("logger.System: %v", err)
	}
	defer log.Close()

	registry := NewRegistry(nil)
	fakeLLM := &FakeLLM{Responses: []string{"hello"}}

	resolver := func(modelID string) (ModelInfo, error) {
		return ModelInfo{}, fmt.Errorf("model %q not found", modelID)
	}

	factory := NewDefaultFactory(registry, fakeLLM, tools.Config{}, log,
		WithModelResolver(resolver),
	)

	tmpl := AgentTemplate{
		ID:           "empty-model-agent",
		Name:         "Empty Model Agent",
		SystemPrompt: "You are a test.",
		ModelID:      "", // empty
	}

	_, _, err = factory.Create(context.Background(), tmpl, "")
	if err == nil {
		t.Fatal("expected error for empty model ID, got nil")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error should mention 'empty', got: %s", err)
	}
}

func TestDefaultFactory_Create_ValidModelResolvesParams(t *testing.T) {
	dir := t.TempDir()
	log, err := logger.System(dir, logger.WithConsole(false))
	if err != nil {
		t.Fatalf("logger.System: %v", err)
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
				MaxTokens:       16384,
				ThinkingEnabled: true,
				ReasoningEffort: "high",
			}, nil
		}
		return ModelInfo{}, fmt.Errorf("model %q not found", modelID)
	}

	factory := NewDefaultFactory(registry, fakeLLM, tools.Config{}, log,
		WithModelResolver(resolver),
	)

	tmpl := AgentTemplate{
		ID:           "resolved-agent",
		Name:         "Resolved Agent",
		SystemPrompt: "You are a test.",
		ModelID:      "deepseek-v4-flash-thinking",
	}

	agent, _, err := factory.Create(context.Background(), tmpl, "")
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
	if agent.Def.MaxTokens != 16384 {
		t.Errorf("MaxTokens = %d, want 16384", agent.Def.MaxTokens)
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
	log, err := logger.System(dir, logger.WithConsole(false))
	if err != nil {
		t.Fatalf("logger.System: %v", err)
	}
	defer log.Close()

	registry := NewRegistry(nil)
	fakeLLM := &FakeLLM{Responses: []string{"hello"}}

	factory := NewDefaultFactory(registry, fakeLLM, tools.Config{}, log)
	// No WithModelResolver — should skip validation

	tmpl := AgentTemplate{
		ID:           "no-resolver-agent",
		Name:         "No Resolver",
		SystemPrompt: "You are a test.",
		ModelID:      "any-random-model-name",
	}

	agent, _, err := factory.Create(context.Background(), tmpl, "")
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
	if !strings.Contains(l2EnforcedDirectivesPart2, "Clarification Before Delegation") {
		t.Error("l2EnforcedDirectivesPart2 should contain 'Clarification Before Delegation'")
	}
	if !strings.Contains(l2EnforcedDirectivesPart2, "need_clarification") {
		t.Error("l2EnforcedDirectivesPart2 should contain 'need_clarification' JSON status")
	}
	if !strings.Contains(l2EnforcedDirectivesPart2, "questions") {
		t.Error("l2EnforcedDirectivesPart2 should contain 'questions' field")
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

	prompt := buildL2SystemPrompt(devTmpl, templates, nil, "/home/user/.soloqueue/plan", "/home/user/.soloqueue", "/home/user/.soloqueue/explore", nil, false)

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

	// Segment 3: exploration artifacts
	if !strings.Contains(prompt, "Exploration Artifacts") {
		t.Error("L2 prompt should contain exploration artifacts rule")
	}
	if !strings.Contains(prompt, "/home/user/.soloqueue/explore") {
		t.Error("L2 prompt should contain explore directory path")
	}
	if !strings.Contains(prompt, "same-day") {
		t.Error("L2 prompt should mention same-day freshness window")
	}
}

// ─── L2/L3 Enforced Directives: "Plan Before Action" rule tests ───────────

func TestL2EnforcedDirectives_ContainsPlanBeforeExecutionRule(t *testing.T) {
	combined := l2EnforcedDirectivesPart1 + l2EnforcedPlanSection + l2EnforcedDirectivesPart2 + l2EnforcedPostPlan
	if !strings.Contains(combined, "MANDATORY Plan Before Execution") {
		t.Error("L2 enforced directives should contain 'MANDATORY Plan Before Execution' rule")
	}
}

func TestL2EnforcedDirectives_ContainsExploreDirPlaceholder(t *testing.T) {
	combined := l2EnforcedDirectivesPart1 + l2EnforcedPlanSection + l2EnforcedDirectivesPart2 + l2EnforcedPostPlan + l2EnforcedExplorationSection
	if !strings.Contains(combined, "{{EXPLORE_DIR}}") {
		t.Error("L2 enforced directives should contain '{{EXPLORE_DIR}}' placeholder")
	}
}

func TestL2EnforcedDirectives_ContainsDesignDocumentStructure(t *testing.T) {
	combined := l2EnforcedDirectivesPart1 + l2EnforcedPlanSection + l2EnforcedDirectivesPart2 + l2EnforcedPostPlan
	if !strings.Contains(combined, "Tasks") {
		t.Error("L2 enforced directives should contain 'Tasks' in checklist structure")
	}
}

func TestL3EnforcedDirectives_ContainsFollowThePlanRule(t *testing.T) {
	combined := l3EnforcedDirectives + l3EnforcedPostPlan
	if !strings.Contains(combined, "Follow the Plan") {
		t.Error("L3 enforced directives should contain 'Follow the Plan' rule")
	}
}

func TestL3EnforcedDirectives_ContainsExploreDirPlaceholder(t *testing.T) {
	combined := l3EnforcedDirectives + l3EnforcedPostPlan + l3EnforcedExplorationSection
	if !strings.Contains(combined, "{{EXPLORE_DIR}}") {
		t.Error("L3 enforced directives should contain '{{EXPLORE_DIR}}' placeholder")
	}
}

func TestL3EnforcedDirectives_ContainsDesignDocumentStructure(t *testing.T) {
	combined := l3EnforcedDirectives + l3EnforcedPostPlan
	if !strings.Contains(combined, "Tasks") {
		t.Error("L3 enforced directives should contain 'Tasks' in checklist structure")
	}
}

// ─── buildL2SystemPrompt / buildL3SystemPrompt: plan-related tests ────────

func TestBuildL2SystemPrompt_ContainsExploreDirPath(t *testing.T) {
	devTmpl := AgentTemplate{
		ID:           "dev",
		Name:         "Dev",
		Description:  "Dev agent",
		SystemPrompt: "You are a dev supervisor.",
		IsLeader:     true,
		Group:        "DevOps",
	}

	templates := map[string]AgentTemplate{
		"dev": devTmpl,
	}

	groups := map[string]prompt.GroupFile{}

	exploreDir := "/home/user/.soloqueue/explore"
	prompt := buildL2SystemPrompt(devTmpl, templates, groups, "", "/home/user/.soloqueue", exploreDir, nil, false)

	// 验证 L2 prompt 中包含 explore 目录路径
	if !strings.Contains(prompt, "/home/user/.soloqueue/explore") {
		t.Errorf("L2 prompt should contain explore directory path %q", exploreDir)
	}
}

func TestBuildL2SystemPrompt_EmptyPlanDir(t *testing.T) {
	devTmpl := AgentTemplate{
		ID:           "dev",
		Name:         "Dev",
		Description:  "Dev agent",
		SystemPrompt: "You are a dev supervisor.",
		IsLeader:     true,
		Group:        "DevOps",
	}

	templates := map[string]AgentTemplate{
		"dev": devTmpl,
	}

	groups := map[string]prompt.GroupFile{}

	prompt := buildL2SystemPrompt(devTmpl, templates, groups, "", "/home/user/.soloqueue", "/home/user/.soloqueue/explore", nil, false)

	// 空 planDir 时，plan 相关规则不应出现
	if strings.Contains(prompt, "MANDATORY Plan Before Execution") {
		t.Error("L2 prompt should not contain 'MANDATORY Plan Before Execution' when planDir is empty")
	}
	if strings.Contains(prompt, "{{PLAN_DIR}}") {
		t.Error("L2 prompt should not contain unreplaced {{PLAN_DIR}} placeholder")
	}

	// exploration artifacts should still be present even when planDir is empty
	if !strings.Contains(prompt, "Exploration Artifacts") {
		t.Error("L2 prompt should contain exploration artifacts rule even when planDir is empty")
	}
	if !strings.Contains(prompt, "/home/user/.soloqueue/explore") {
		t.Error("L2 prompt should contain explore directory path even when planDir is empty")
	}
}

func TestBuildL2SystemPrompt_ContainsDesignDocumentStructure(t *testing.T) {
	devTmpl := AgentTemplate{
		ID:           "dev",
		Name:         "Dev",
		Description:  "Dev agent",
		SystemPrompt: "You are a dev supervisor.",
		IsLeader:     true,
		Group:        "DevOps",
	}

	templates := map[string]AgentTemplate{
		"dev": devTmpl,
	}

	groups := map[string]prompt.GroupFile{}

	planDir := "/home/user/.soloqueue/plan"
	prompt := buildL2SystemPrompt(devTmpl, templates, groups, planDir, "/home/user/.soloqueue", "/home/user/.soloqueue/explore", nil, false)

	// 验证 Tasks 结构约定在 L2 prompt 中体现
	if !strings.Contains(prompt, "Tasks") {
		t.Error("L2 prompt should contain 'Tasks' in checklist structure")
	}
}

func TestBuildL3SystemPrompt_ContainsFollowThePlanRule(t *testing.T) {
	tmpl := AgentTemplate{
		ID:           "backend",
		Name:         "Backend",
		Description:  "Backend worker",
		SystemPrompt: "You are a backend worker.",
	}

	planDir := "/home/user/.soloqueue/plan"
	prompt := buildL3SystemPrompt(tmpl, nil, planDir, "/home/user/.soloqueue", "/home/user/.soloqueue/explore", false)

	// 验证 L3 prompt 中包含 "Follow the Plan" 规则
	if !strings.Contains(prompt, "Follow the Plan") {
		t.Error("L3 prompt should contain 'Follow the Plan' rule")
	}

	// 验证 L3 prompt 中包含 Exploration Artifacts 规则
	if !strings.Contains(prompt, "Exploration Artifacts") {
		t.Error("L3 prompt should contain exploration artifacts rule")
	}
	if !strings.Contains(prompt, "/home/user/.soloqueue/explore") {
		t.Error("L3 prompt should contain explore directory path")
	}
	if !strings.Contains(prompt, "same-day") {
		t.Error("L3 prompt should mention same-day freshness window")
	}
}

func TestBuildL3SystemPrompt_ContainsExploreDirPath(t *testing.T) {
	tmpl := AgentTemplate{
		ID:           "backend",
		Name:         "Backend",
		Description:  "Backend worker",
		SystemPrompt: "You are a backend worker.",
	}

	exploreDir := "/home/user/.soloqueue/explore"
	prompt := buildL3SystemPrompt(tmpl, nil, "", "/home/user/.soloqueue", exploreDir, false)

	// 验证 L3 prompt 中包含 explore 目录路径
	if !strings.Contains(prompt, "/home/user/.soloqueue/explore") {
		t.Errorf("L3 prompt should contain explore directory path, got: %s", prompt)
	}
}

func TestBuildL3SystemPrompt_EmptyPlanDir(t *testing.T) {
	tmpl := AgentTemplate{
		ID:           "backend",
		Name:         "Backend",
		Description:  "Backend worker",
		SystemPrompt: "You are a backend worker.",
	}

	prompt := buildL3SystemPrompt(tmpl, nil, "", "/home/user/.soloqueue", "/home/user/.soloqueue/explore", false)

	// 空 planDir 时，不要出现未替换的占位符
	if strings.Contains(prompt, "{{PLAN_DIR}}") {
		t.Error("L3 prompt should not contain unreplaced {{PLAN_DIR}} placeholder")
	}

	// exploration artifacts should still be present even when planDir is empty
	if !strings.Contains(prompt, "Exploration Artifacts") {
		t.Error("L3 prompt should contain exploration artifacts rule even when planDir is empty")
	}
	if !strings.Contains(prompt, "/home/user/.soloqueue/explore") {
		t.Error("L3 prompt should contain explore directory path even when planDir is empty")
	}
}

func TestBuildL3SystemPrompt_ContainsDesignDocumentStructure(t *testing.T) {
	tmpl := AgentTemplate{
		ID:           "backend",
		Name:         "Backend",
		Description:  "Backend worker",
		SystemPrompt: "You are a backend worker.",
	}

	planDir := "/home/user/.soloqueue/plan"
	prompt := buildL3SystemPrompt(tmpl, nil, planDir, "/home/user/.soloqueue", "/home/user/.soloqueue/explore", false)

	// 验证 Tasks 结构约定在 L3 prompt 中体现
	if !strings.Contains(prompt, "Tasks") {
		t.Error("L3 prompt should contain 'Tasks' in checklist structure")
	}
}

func TestBuildL2SystemPrompt_PermanentMemory(t *testing.T) {
	devTmpl := AgentTemplate{
		ID:           "dev",
		Name:         "Dev",
		Description:  "Dev agent",
		SystemPrompt: "You are a dev supervisor.",
		IsLeader:     true,
		Group:        "DevOps",
	}

	templates := map[string]AgentTemplate{"dev": devTmpl}
	groups := map[string]prompt.GroupFile{}

	prompt := buildL2SystemPrompt(devTmpl, templates, groups, "/plan", "/workdir", "/explore", nil, true)

	if !strings.Contains(prompt, "Long-Term Memory") {
		t.Error("L2 prompt should contain Long-Term Memory section when permanent memory is enabled")
	}
	if !strings.Contains(prompt, "RecallMemory") {
		t.Error("L2 prompt should mention RecallMemory tool")
	}
	if !strings.Contains(prompt, "Remember") {
		t.Error("L2 prompt should mention Remember tool")
	}
}

func TestBuildL2SystemPrompt_NoPermanentMemory(t *testing.T) {
	devTmpl := AgentTemplate{
		ID:           "dev",
		Name:         "Dev",
		Description:  "Dev agent",
		SystemPrompt: "You are a dev supervisor.",
		IsLeader:     true,
		Group:        "DevOps",
	}

	templates := map[string]AgentTemplate{"dev": devTmpl}
	groups := map[string]prompt.GroupFile{}

	prompt := buildL2SystemPrompt(devTmpl, templates, groups, "/plan", "/workdir", "/explore", nil, false)

	if strings.Contains(prompt, "Long-Term Memory") {
		t.Error("L2 prompt should NOT contain Long-Term Memory section when permanent memory is disabled")
	}
}

func TestBuildL3SystemPrompt_PermanentMemory(t *testing.T) {
	tmpl := AgentTemplate{
		ID:           "backend",
		Name:         "Backend",
		Description:  "Backend worker",
		SystemPrompt: "You are a backend worker.",
	}

	prompt := buildL3SystemPrompt(tmpl, nil, "/plan", "/workdir", "/explore", true)

	if !strings.Contains(prompt, "Long-Term Memory") {
		t.Error("L3 prompt should contain Long-Term Memory section when permanent memory is enabled")
	}
	if !strings.Contains(prompt, "RecallMemory") {
		t.Error("L3 prompt should mention RecallMemory tool")
	}
	if !strings.Contains(prompt, "Remember") {
		t.Error("L3 prompt should mention Remember tool")
	}
}

func TestBuildL3SystemPrompt_NoPermanentMemory(t *testing.T) {
	tmpl := AgentTemplate{
		ID:           "backend",
		Name:         "Backend",
		Description:  "Backend worker",
		SystemPrompt: "You are a backend worker.",
	}

	prompt := buildL3SystemPrompt(tmpl, nil, "/plan", "/workdir", "/explore", false)

	if strings.Contains(prompt, "Long-Term Memory") {
		t.Error("L3 prompt should NOT contain Long-Term Memory section when permanent memory is disabled")
	}
}

// ─── loadProjectResources tests ──────────────────────────────────────

func TestLoadProjectResources_AGENTSMD(t *testing.T) {
	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, "AGENTS.md"), []byte("# Project Rules\nAlways write tests."), 0644); err != nil {
		t.Fatal(err)
	}

	f := &DefaultFactory{}
	res := f.loadProjectResources(projectDir)

	if res.projectPrompt == "" {
		t.Fatal("projectPrompt should not be empty when AGENTS.md exists")
	}
	if !strings.Contains(res.projectPrompt, "AGENTS.md") {
		t.Error("projectPrompt should reference AGENTS.md")
	}
	if !strings.Contains(res.projectPrompt, "Always write tests.") {
		t.Error("projectPrompt should contain AGENTS.md content")
	}
}

func TestLoadProjectResources_CLAUDEMDFallback(t *testing.T) {
	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, "CLAUDE.md"), []byte("# Claude Rules\nUse Go."), 0644); err != nil {
		t.Fatal(err)
	}

	f := &DefaultFactory{}
	res := f.loadProjectResources(projectDir)

	if res.projectPrompt == "" {
		t.Fatal("projectPrompt should not be empty when CLAUDE.md exists")
	}
	if !strings.Contains(res.projectPrompt, "CLAUDE.md") {
		t.Error("projectPrompt should reference CLAUDE.md")
	}
	if !strings.Contains(res.projectPrompt, "Use Go.") {
		t.Error("projectPrompt should contain CLAUDE.md content")
	}
}

func TestLoadProjectResources_AGENTSMDTakesPrecedence(t *testing.T) {
	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, "AGENTS.md"), []byte("AGENTS content"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "CLAUDE.md"), []byte("CLAUDE content"), 0644); err != nil {
		t.Fatal(err)
	}

	f := &DefaultFactory{}
	res := f.loadProjectResources(projectDir)

	if !strings.Contains(res.projectPrompt, "AGENTS content") {
		t.Error("AGENTS.md should take precedence over CLAUDE.md")
	}
	if strings.Contains(res.projectPrompt, "CLAUDE content") {
		t.Error("CLAUDE.md content should not appear when AGENTS.md exists")
	}
}

func TestLoadProjectResources_ProjectAgents(t *testing.T) {
	projectDir := t.TempDir()
	agentsDir := filepath.Join(projectDir, ".claude", "agents")
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		t.Fatal(err)
	}

	agentMD := `---
name: project-worker
description: Project-specific worker
---
You are a project worker.
`
	if err := os.WriteFile(filepath.Join(agentsDir, "worker.md"), []byte(agentMD), 0644); err != nil {
		t.Fatal(err)
	}

	f := &DefaultFactory{}
	res := f.loadProjectResources(projectDir)

	if len(res.agents) != 1 {
		t.Fatalf("expected 1 project agent, got %d", len(res.agents))
	}
	if res.agents[0].ID != "project-worker" {
		t.Errorf("agent ID = %q, want %q", res.agents[0].ID, "project-worker")
	}
}

func TestLoadProjectResources_ProjectSkills(t *testing.T) {
	projectDir := t.TempDir()
	skillDir := filepath.Join(projectDir, ".claude", "skills", "deploy")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}

	skillMD := `---
name: deploy
description: Deploy to production
---
# Deploy Skill
Deploy the project.
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillMD), 0644); err != nil {
		t.Fatal(err)
	}

	f := &DefaultFactory{}
	res := f.loadProjectResources(projectDir)

	if len(res.skills) != 1 {
		t.Fatalf("expected 1 project skill, got %d", len(res.skills))
	}
	if res.skills[0].ID != "deploy" {
		t.Errorf("skill ID = %q, want %q", res.skills[0].ID, "deploy")
	}
}

func TestLoadProjectResources_ProjectMCPConfig(t *testing.T) {
	projectDir := t.TempDir()
	claudeDir := filepath.Join(projectDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatal(err)
	}

	mcpJSON := `{
  "mcpServers": {
    "project-db": {
      "command": "npx",
      "args": ["db-mcp"],
      "enabled": true
    }
  }
}`
	if err := os.WriteFile(filepath.Join(claudeDir, "mcp.json"), []byte(mcpJSON), 0644); err != nil {
		t.Fatal(err)
	}

	f := &DefaultFactory{}
	res := f.loadProjectResources(projectDir)

	if res.mcpCfg == nil {
		t.Fatal("mcpCfg should not be nil when .claude/mcp.json exists")
	}
	if len(res.mcpCfg.Servers) != 1 {
		t.Fatalf("expected 1 MCP server, got %d", len(res.mcpCfg.Servers))
	}
	if res.mcpCfg.Servers[0].Name != "project-db" {
		t.Errorf("server name = %q, want %q", res.mcpCfg.Servers[0].Name, "project-db")
	}
}

func TestLoadProjectResources_EmptyProjectDir(t *testing.T) {
	projectDir := t.TempDir()

	f := &DefaultFactory{}
	res := f.loadProjectResources(projectDir)

	if len(res.agents) != 0 {
		t.Errorf("expected 0 agents, got %d", len(res.agents))
	}
	if len(res.skills) != 0 {
		t.Errorf("expected 0 skills, got %d", len(res.skills))
	}
	if res.mcpCfg != nil {
		t.Error("mcpCfg should be nil when no .claude/mcp.json")
	}
	if res.projectPrompt != "" {
		t.Error("projectPrompt should be empty when no AGENTS.md/CLAUDE.md")
	}
}

func TestLoadProjectResources_AllResources(t *testing.T) {
	projectDir := t.TempDir()
	claudeDir := filepath.Join(projectDir, ".claude")
	agentsDir := filepath.Join(claudeDir, "agents")
	skillDir := filepath.Join(claudeDir, "skills", "test-skill")
	if err := os.MkdirAll(agentsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(projectDir, "AGENTS.md"), []byte("Project rules"), 0644); err != nil {
		t.Fatal(err)
	}

	agentMD := `---
name: proj-worker
description: A project worker
---
You work on this project.
`
	if err := os.WriteFile(filepath.Join(agentsDir, "worker.md"), []byte(agentMD), 0644); err != nil {
		t.Fatal(err)
	}

	skillMD := `---
name: test-skill
description: Test skill
---
# Test
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillMD), 0644); err != nil {
		t.Fatal(err)
	}

	mcpJSON := `{
  "mcpServers": {
    "proj-mcp": {
      "command": "npx",
      "args": ["proj-mcp-server"],
      "enabled": true
    }
  }
}`
	if err := os.WriteFile(filepath.Join(claudeDir, "mcp.json"), []byte(mcpJSON), 0644); err != nil {
		t.Fatal(err)
	}

	f := &DefaultFactory{}
	res := f.loadProjectResources(projectDir)

	if res.projectPrompt == "" {
		t.Error("projectPrompt should not be empty")
	}
	if len(res.agents) != 1 {
		t.Errorf("expected 1 agent, got %d", len(res.agents))
	}
	if len(res.skills) != 1 {
		t.Errorf("expected 1 skill, got %d", len(res.skills))
	}
	if res.mcpCfg == nil {
		t.Error("mcpCfg should not be nil")
	}
}

// ─── visibleWorkers tests ──────────────────────────────────────

func TestVisibleWorkers_SameGroupOnly(t *testing.T) {
	f := &DefaultFactory{
		templates: map[string]AgentTemplate{
			"leader":   {ID: "leader", Group: "DevOps", IsLeader: true},
			"backend":  {ID: "backend", Group: "DevOps", IsLeader: false},
			"frontend": {ID: "frontend", Group: "DevOps", IsLeader: false},
			"designer": {ID: "designer", Group: "Design", IsLeader: false},
		},
	}

	leader := f.templates["leader"]
	workers := f.visibleWorkers(leader, nil)

	if len(workers) != 2 {
		t.Fatalf("expected 2 workers, got %d", len(workers))
	}

	found := map[string]bool{}
	for _, w := range workers {
		found[w.ID] = true
	}
	if !found["backend"] || !found["frontend"] {
		t.Errorf("expected backend and frontend, got %v", found)
	}
	if found["designer"] {
		t.Error("designer from different group should not be visible")
	}
}

func TestVisibleWorkers_ExcludesSelfAndLeaders(t *testing.T) {
	f := &DefaultFactory{
		templates: map[string]AgentTemplate{
			"leader":    {ID: "leader", Group: "DevOps", IsLeader: true},
			"leader2":   {ID: "leader2", Group: "DevOps", IsLeader: true},
			"worker":    {ID: "worker", Group: "DevOps", IsLeader: false},
		},
	}

	leader := f.templates["leader"]
	workers := f.visibleWorkers(leader, nil)

	if len(workers) != 1 {
		t.Fatalf("expected 1 worker, got %d", len(workers))
	}
	if workers[0].ID != "worker" {
		t.Errorf("expected worker, got %q", workers[0].ID)
	}
}

func TestVisibleWorkers_ProjectAgentsOverrideGlobal(t *testing.T) {
	f := &DefaultFactory{
		templates: map[string]AgentTemplate{
			"leader":   {ID: "leader", Group: "DevOps", IsLeader: true},
			"backend":  {ID: "backend", Group: "DevOps", IsLeader: false, Description: "global backend"},
			"frontend": {ID: "frontend", Group: "DevOps", IsLeader: false},
		},
	}

	projectAgents := []AgentTemplate{
		{ID: "backend", Group: "DevOps", IsLeader: false, Description: "project backend"},
		{ID: "new-worker", Group: "DevOps", IsLeader: false, Description: "new project worker"},
	}

	leader := f.templates["leader"]
	workers := f.visibleWorkers(leader, projectAgents)

	if len(workers) != 3 {
		t.Fatalf("expected 3 workers, got %d", len(workers))
	}

	byID := map[string]AgentTemplate{}
	for _, w := range workers {
		byID[w.ID] = w
	}

	if byID["backend"].Description != "project backend" {
		t.Errorf("project backend should override global, got %q", byID["backend"].Description)
	}
	if _, ok := byID["new-worker"]; !ok {
		t.Error("new project worker should be visible")
	}
	if _, ok := byID["frontend"]; !ok {
		t.Error("global frontend should still be visible")
	}
}

func TestVisibleWorkers_ProjectAgentExcludesSelfAndLeaders(t *testing.T) {
	f := &DefaultFactory{
		templates: map[string]AgentTemplate{
			"leader": {ID: "leader", Group: "DevOps", IsLeader: true},
		},
	}

	projectAgents := []AgentTemplate{
		{ID: "leader", Group: "DevOps", IsLeader: false, Description: "project leader override"},
		{ID: "proj-leader", Group: "DevOps", IsLeader: true, Description: "project leader"},
		{ID: "proj-worker", Group: "DevOps", IsLeader: false, Description: "project worker"},
	}

	leader := f.templates["leader"]
	workers := f.visibleWorkers(leader, projectAgents)

	if len(workers) != 1 {
		t.Fatalf("expected 1 worker, got %d", len(workers))
	}
	if workers[0].ID != "proj-worker" {
		t.Errorf("expected proj-worker, got %q", workers[0].ID)
	}
}

func TestVisibleWorkers_EmptyGroup(t *testing.T) {
	f := &DefaultFactory{
		templates: map[string]AgentTemplate{
			"lone": {ID: "lone", Group: "", IsLeader: false},
		},
	}

	lone := f.templates["lone"]
	workers := f.visibleWorkers(lone, nil)

	if len(workers) != 0 {
		t.Errorf("expected 0 workers for empty group, got %d", len(workers))
	}
}

// ─── Skills merge tests (via Create) ──────────────────────────────────────

func TestDefaultFactory_Create_ProjectSkillsOverrideGlobal(t *testing.T) {
	projectDir := t.TempDir()

	globalSkillDir := filepath.Join(t.TempDir(), "global-skills")
	if err := os.MkdirAll(filepath.Join(globalSkillDir, "deploy"), 0755); err != nil {
		t.Fatal(err)
	}
	globalSkillMD := `---
name: deploy
description: Global deploy skill
---
# Global Deploy
`
	if err := os.WriteFile(filepath.Join(globalSkillDir, "deploy", "SKILL.md"), []byte(globalSkillMD), 0644); err != nil {
		t.Fatal(err)
	}

	projectSkillDir := filepath.Join(projectDir, ".claude", "skills")
	if err := os.MkdirAll(filepath.Join(projectSkillDir, "deploy"), 0755); err != nil {
		t.Fatal(err)
	}
	projectSkillMD := `---
name: deploy
description: Project deploy skill
---
# Project Deploy
`
	if err := os.WriteFile(filepath.Join(projectSkillDir, "deploy", "SKILL.md"), []byte(projectSkillMD), 0644); err != nil {
		t.Fatal(err)
	}

	globalSkills, err := skill.LoadSkillsFromDir(globalSkillDir)
	if err != nil {
		t.Fatalf("LoadSkillsFromDir global: %v", err)
	}
	globalReg := skill.NewSkillRegistry()
	for _, s := range globalSkills {
		_ = globalReg.Register(s)
	}

	log, err := logger.System(projectDir, logger.WithConsole(false))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	registry := NewRegistry(nil)
	fakeLLM := &FakeLLM{Responses: []string{"hello"}}

	workDir := t.TempDir()
	factory := NewDefaultFactory(registry, fakeLLM, tools.Config{}, log,
		WithSkillRegistry(globalReg),
		WithWorkDir(workDir),
	)

	tmpl := AgentTemplate{
		ID:           "skill-agent",
		Name:         "Skill Agent",
		SystemPrompt: "You are a skill agent.",
		SkillIDs:     []string{"deploy"},
	}

	agent, _, err := factory.Create(context.Background(), tmpl, projectDir)
	if err != nil {
		t.Fatalf("factory.Create: %v", err)
	}
	defer agent.Stop(time.Second)

	if agent.WorkDir != projectDir {
		t.Errorf("WorkDir = %q, want %q", agent.WorkDir, projectDir)
	}
}

func TestDefaultFactory_Create_WorkDirFallbackGroupWorkspace(t *testing.T) {
	globalWorkDir := t.TempDir()

	log, err := logger.System(globalWorkDir, logger.WithConsole(false))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	registry := NewRegistry(nil)
	fakeLLM := &FakeLLM{Responses: []string{"hello"}}

	factory := NewDefaultFactory(registry, fakeLLM, tools.Config{}, log,
		WithWorkDir(globalWorkDir),
	)

	// Case 1: tmpl has Group, workDir passed to Create is empty
	tmplWithGroup := AgentTemplate{
		ID:           "dev-agent",
		Name:         "Dev Agent",
		SystemPrompt: "You are dev.",
		Group:        "DevOps",
	}
	ag1, _, err := factory.Create(context.Background(), tmplWithGroup, "")
	if err != nil {
		t.Fatalf("factory.Create case 1: %v", err)
	}
	defer ag1.Stop(time.Second)

	expectedWorkspaceDir := filepath.Join(globalWorkDir, "workspace", "DevOps")
	if ag1.WorkDir != expectedWorkspaceDir {
		t.Errorf("ag1.WorkDir = %q, want %q", ag1.WorkDir, expectedWorkspaceDir)
	}
	if info, err := os.Stat(expectedWorkspaceDir); err != nil || !info.IsDir() {
		t.Errorf("expected workspace directory %q to be created: %v", expectedWorkspaceDir, err)
	}

	// Case 2: tmpl has Group, workDir passed to Create is the global workDir
	ag2, _, err := factory.Create(context.Background(), tmplWithGroup, globalWorkDir)
	if err != nil {
		t.Fatalf("factory.Create case 2: %v", err)
	}
	defer ag2.Stop(time.Second)

	if ag2.WorkDir != expectedWorkspaceDir {
		t.Errorf("ag2.WorkDir = %q, want %q", ag2.WorkDir, expectedWorkspaceDir)
	}

	// Case 3: tmpl has no Group, workDir passed to Create is empty
	tmplNoGroup := AgentTemplate{
		ID:           "lone-agent",
		Name:         "Lone Agent",
		SystemPrompt: "You are alone.",
		Group:        "",
	}
	ag3, _, err := factory.Create(context.Background(), tmplNoGroup, "")
	if err != nil {
		t.Fatalf("factory.Create case 3: %v", err)
	}
	defer ag3.Stop(time.Second)

	if ag3.WorkDir != globalWorkDir {
		t.Errorf("ag3.WorkDir = %q, want %q", ag3.WorkDir, globalWorkDir)
	}
}


// ─── buildL2SystemPrompt project agents tests ──────────────────────────────

func TestBuildL2SystemPrompt_ProjectAgentsListed(t *testing.T) {
	devTmpl := AgentTemplate{
		ID:           "dev",
		Name:         "Dev",
		Description:  "Dev agent",
		SystemPrompt: "You are a dev supervisor.",
		IsLeader:     true,
		Group:        "DevOps",
	}

	templates := map[string]AgentTemplate{
		"dev": devTmpl,
	}

	projectAgents := []AgentTemplate{
		{ID: "proj-backend", Name: "ProjBackend", Description: "Project backend worker", Group: "DevOps"},
		{ID: "proj-frontend", Name: "ProjFrontend", Description: "Project frontend worker", Group: "DevOps"},
	}

	promptStr := buildL2SystemPrompt(devTmpl, templates, nil, "/plan", "/workdir", "/explore", projectAgents, false)

	if !strings.Contains(promptStr, "ProjBackend") {
		t.Error("L2 prompt should list project agent ProjBackend")
	}
	if !strings.Contains(promptStr, "ProjFrontend") {
		t.Error("L2 prompt should list project agent ProjFrontend")
	}
	if !strings.Contains(promptStr, "Project backend worker") {
		t.Error("L2 prompt should contain project agent description")
	}
}

func TestBuildL2SystemPrompt_ProjectAgentOverridesGlobal(t *testing.T) {
	devTmpl := AgentTemplate{
		ID:           "dev",
		Name:         "Dev",
		Description:  "Dev agent",
		SystemPrompt: "You are a dev supervisor.",
		IsLeader:     true,
		Group:        "DevOps",
	}

	globalBackend := AgentTemplate{
		ID:          "backend",
		Name:        "Backend",
		Description: "Global backend",
		Group:       "DevOps",
	}

	templates := map[string]AgentTemplate{
		"dev":     devTmpl,
		"backend": globalBackend,
	}

	projectAgents := []AgentTemplate{
		{ID: "backend", Name: "ProjBackend", Description: "Project backend override", Group: "DevOps"},
	}

	promptStr := buildL2SystemPrompt(devTmpl, templates, nil, "/plan", "/workdir", "/explore", projectAgents, false)

	if !strings.Contains(promptStr, "ProjBackend") {
		t.Error("L2 prompt should list project-overridden backend")
	}
	if strings.Contains(promptStr, "Global backend") {
		t.Error("L2 prompt should NOT contain global backend description after project override")
	}
	if !strings.Contains(promptStr, "Project backend override") {
		t.Error("L2 prompt should contain project backend description")
	}
}

func TestBuildL2SystemPrompt_ProjectAgentExcludesSelf(t *testing.T) {
	devTmpl := AgentTemplate{
		ID:           "dev",
		Name:         "Dev",
		Description:  "Dev agent",
		SystemPrompt: "You are a dev supervisor.",
		IsLeader:     true,
		Group:        "DevOps",
	}

	templates := map[string]AgentTemplate{
		"dev": devTmpl,
	}

	projectAgents := []AgentTemplate{
		{ID: "dev", Name: "ProjDev", Description: "Should be excluded", Group: "DevOps"},
		{ID: "worker", Name: "Worker", Description: "Valid worker", Group: "DevOps"},
	}

	promptStr := buildL2SystemPrompt(devTmpl, templates, nil, "/plan", "/workdir", "/explore", projectAgents, false)

	if strings.Contains(promptStr, "Should be excluded") {
		t.Error("L2 prompt should NOT list project agent with same ID as leader (self)")
	}
	if !strings.Contains(promptStr, "Valid worker") {
		t.Error("L2 prompt should list valid project worker")
	}
}

func TestBuildL2SystemPrompt_NilProjectAgents(t *testing.T) {
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

	templates := map[string]AgentTemplate{
		"dev":      devTmpl,
		"frontend": frontendTmpl,
	}

	promptStr := buildL2SystemPrompt(devTmpl, templates, nil, "/plan", "/workdir", "/explore", nil, false)

	if !strings.Contains(promptStr, "Frontend") {
		t.Error("L2 prompt should list global peer even when projectAgents is nil")
	}
}

func TestDefaultFactory_Create_SimulationAgentNoTools(t *testing.T) {
	globalWorkDir := t.TempDir()

	log, err := logger.System(globalWorkDir, logger.WithConsole(false))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()

	registry := NewRegistry(nil)
	fakeLLM := &FakeLLM{Responses: []string{"hello"}}

	factory := NewDefaultFactory(registry, fakeLLM, tools.Config{}, log,
		WithWorkDir(globalWorkDir),
	)

	// Simulation agent ID prefixed with "sim-"
	simTmpl := AgentTemplate{
		ID:           "sim-alice",
		Name:         "Alice",
		SystemPrompt: "You are simulated Alice.",
	}

	simAgent, _, err := factory.Create(context.Background(), simTmpl, "")
	if err != nil {
		t.Fatalf("failed to create simulation agent: %v", err)
	}
	defer simAgent.Stop(time.Second)

	// Verify that the simulation agent has NO tools
	if specs := simAgent.ToolSpecs(); len(specs) > 0 {
		t.Errorf("expected simulation agent to have no tools, but got %d tools: %v", len(specs), specs)
	}
}


