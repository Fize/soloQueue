package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/prompt"
	"github.com/xiaobaitu/soloqueue/internal/skill"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// ─── AgentTemplate ─────────────────────────────────────────────────────────

// AgentTemplate 是 Agent 实例化的完整描述
//
// 来源于 ~/.soloqueue/agents/*.md 的 YAML frontmatter + markdown body。
// AgentFrontmatter.SubAgents 引用其他 agent 的 name，运行时由 Factory 解析。
type AgentTemplate struct {
	ID           string   // 唯一标识（如 "dev"、"fe"）
	Name         string   // 显示名称
	Description  string   // 给 LLM 看的描述
	SystemPrompt string   // markdown body
	ModelID      string   // 模型 ID（必须匹配 settings.toml 中的 Models[].ID）
	Reasoning    bool     // 是否启用推理
	SubAgents    []string // 子 Agent 名称列表（L2 专用）
	IsLeader     bool     // 是否为 L2 领导者
	Ephemeral    bool     // 是否为阅后即焚的 L3
	Group        string   // 所属 group name
	MCPServers   []string // MCP Server 名称列表
}

// ─── ModelInfo ────────────────────────────────────────────────────────────

// ModelInfo holds the resolved model configuration for an agent.
// Populated by ModelResolver from the settings model registry.
type ModelInfo struct {
	// APIModel is the actual model name sent to the LLM API.
	// Empty means use the model ID itself.
	APIModel string

	// ContextWindow is the model's context window size in tokens.
	ContextWindow int

	// Generation parameters
	Temperature float64
	MaxTokens   int

	// Thinking configuration
	ThinkingEnabled bool
	ReasoningEffort string
}

// ModelResolver looks up a model ID in the settings registry.
//
// Returns (ModelInfo, nil) on success, or (zero, error) if the model ID
// is not found or not enabled. Implemented by the config layer.
type ModelResolver func(modelID string) (ModelInfo, error)

// ─── AgentFactory ──────────────────────────────────────────────────────────

// AgentFactory 从模板实例化 Agent
type AgentFactory interface {
	// Create 根据 tmpl 创建并启动一个 Agent 实例
	Create(ctx context.Context, tmpl AgentTemplate) (*Agent, *ctxwin.ContextWindow, error)

	// Registry 返回内部的 Agent Registry（供 Supervisor 使用）
	Registry() *Registry
}

// ─── DefaultFactory ────────────────────────────────────────────────────────

// DefaultFactory 是 AgentFactory 的默认实现
//
// 包含创建 Agent 所需的所有依赖。创建的 Agent 会自动注册到 Registry 并启动。
type DefaultFactory struct {
	registry     *Registry
	llm          LLMClient
	toolsCfg     tools.Config
	skillDir     string
	log          *logger.Logger
	resolveModel ModelResolver // nil = skip model validation (tests)
	templates    map[string]AgentTemplate        // 按 ID 索引的全量模板，供 buildL2SystemPrompt 查找子 agent 描述
	groups       map[string]prompt.GroupFile     // group 信息，供 L2 prompt 注入团队上下文
}

// NewDefaultFactory 创建 DefaultFactory
func NewDefaultFactory(
	registry *Registry,
	llm LLMClient,
	toolsCfg tools.Config,
	skillDir string,
	log *logger.Logger,
	opts ...FactoryOption,
) *DefaultFactory {
	f := &DefaultFactory{
		registry: registry,
		llm:      llm,
		toolsCfg: toolsCfg,
		skillDir: skillDir,
		log:      log,
	}
	for _, opt := range opts {
		opt(f)
	}
	return f
}

// FactoryOption configures a DefaultFactory.
type FactoryOption func(*DefaultFactory)

// WithModelResolver sets the model resolver for agent model validation.
// When set, Create will validate that the template's ModelID exists in the
// settings model registry, and populate the agent Definition with resolved
// model parameters (APIModel, ContextWindow, Temperature, etc.).
func WithModelResolver(resolver ModelResolver) FactoryOption {
	return func(f *DefaultFactory) {
		f.resolveModel = resolver
	}
}

// WithTemplates sets the template index for L2 system prompt building.
// buildL2SystemPrompt uses this to look up sub-agent descriptions.
func WithTemplates(templates []AgentTemplate) FactoryOption {
	return func(f *DefaultFactory) {
		f.templates = make(map[string]AgentTemplate, len(templates))
		for _, t := range templates {
			f.templates[t.ID] = t
		}
	}
}

// WithGroups sets the group configuration map for L2 system prompt building.
// buildL2SystemPrompt uses this to inject team context into L2 leaders.
func WithGroups(groups map[string]prompt.GroupFile) FactoryOption {
	return func(f *DefaultFactory) {
		f.groups = groups
	}
}

func (f *DefaultFactory) Registry() *Registry {
	return f.registry
}

// Create 根据 tmpl 创建并启动一个 Agent 实例
//
// 流程：
//  1. 构建最终 SystemPrompt（L2 使用三段式拼接，L3 直接使用 body/description）
//  2. Build(toolsCfg) → 内置 tools
//  3. 如果 tmpl.SubAgents 非空，为每个 SubAgent 创建 DelegateTool（同步模式）
//  4. 加载 skills（LoadSkillsFromDir）
//  5. 创建 Agent（WithTools, WithSkills, 可选 WithEphemeral/WithPriorityMailbox）
//  6. 创建 ContextWindow，push system prompt + skill catalog
//  7. Register 到 registry
//  8. Start Agent
//  9. 返回 (agent, cw, nil)
func (f *DefaultFactory) Create(ctx context.Context, tmpl AgentTemplate) (*Agent, *ctxwin.ContextWindow, error) {
	// 1. 构建最终 SystemPrompt
	var finalPrompt string
	if tmpl.IsLeader {
		finalPrompt = buildL2SystemPrompt(tmpl, f.templates, f.groups)
	} else {
		if tmpl.SystemPrompt != "" {
			finalPrompt = tmpl.SystemPrompt
		} else if tmpl.Description != "" {
			finalPrompt = tmpl.Description
		}
	}

	// 2. 构建 Definition
	def := Definition{
		ID:              tmpl.ID,
		Name:            tmpl.Name,
		Role:            RoleUser,
		Kind:            KindCustom,
		ModelID:         tmpl.ModelID,
		SystemPrompt:    finalPrompt,
		ReasoningEffort: "", // populated below if resolver is set
	}

	// 1b. Validate and resolve model configuration
	if f.resolveModel != nil {
		if tmpl.ModelID == "" {
			return nil, nil, fmt.Errorf("agent %q: model ID is empty", tmpl.ID)
		}
		info, err := f.resolveModel(tmpl.ModelID)
		if err != nil {
			return nil, nil, fmt.Errorf("agent %q: invalid model %q: %w", tmpl.ID, tmpl.ModelID, err)
		}
		// Use APIModel for the actual API call (may differ from the config ID)
		if info.APIModel != "" {
			def.ModelID = info.APIModel
		}
		def.ContextWindow = info.ContextWindow
		def.Temperature = info.Temperature
		def.MaxTokens = info.MaxTokens
		def.ThinkingEnabled = info.ThinkingEnabled
		def.ReasoningEffort = info.ReasoningEffort
	}

	// 2. 构建内置 tools
	allTools := tools.Build(f.toolsCfg)

	// 3. 为每个 SubAgent 创建 DelegateTool（同步模式）
	// 注意：SubAgent 对应的 L3 实例由 Supervisor.SpawnFn 在运行时创建
	// 这里只注册 tool 声明，不启动 Agent
	// SpawnFn 将由 Supervisor 注入（Create 不直接注入）
	for _, subName := range tmpl.SubAgents {
		desc := fmt.Sprintf("sub-agent '%s'", subName)
		if sub, ok := f.templates[subName]; ok && sub.Description != "" {
			desc = sub.Description
		}
		dt := &tools.DelegateTool{
			LeaderID: subName,
			Desc:     desc,
			Locator:  f.registry, // 同步模式：查找已注册的 Agent
			Timeout:  tools.DelegateDefaultTimeout,
		}
		allTools = append(allTools, dt)
	}

	// 4. 加载 skills
	var skillList []skill.Skill
	if f.skillDir != "" {
		loaded, err := skill.LoadSkillsFromDir(f.skillDir)
		if err != nil && f.log != nil {
			f.log.InfoContext(ctx, logger.CatActor, "skill load skipped",
				"err", err,
			)
		}
		skillList = loaded
	}

	// 5. 构造 Option 列表
	opts := []Option{
		WithTools(allTools...),
		WithSkills(skillList...),
		WithParallelTools(true),
	}
	if tmpl.Ephemeral {
		opts = append(opts, WithEphemeral())
	}
	if tmpl.IsLeader {
		// L2 可以启用 PriorityMailbox，用于接收 L3 结果的高优先级投递
		// 暂不启用，L2 同步阻塞等待 L3，不需要优先级
	}

	// 6. 创建 Agent
	a := NewAgent(def, f.llm, f.log, opts...)

	// 7. 创建 ContextWindow
	cw := ctxwin.NewContextWindow(DefaultContextWindow, 2000, ctxwin.NewTokenizer())
	if a.Def.SystemPrompt != "" {
		cw.Push(ctxwin.RoleSystem, a.Def.SystemPrompt)
	}
	if catalog := a.SkillCatalog(); catalog != "" {
		cw.Push(ctxwin.RoleSystem, catalog)
	}

	// 8. Register 到 registry
	if err := f.registry.Register(a); err != nil {
		return nil, nil, fmt.Errorf("factory: register agent %q: %w", tmpl.ID, err)
	}

	// 9. Start Agent
	if err := a.Start(ctx); err != nil {
		f.registry.Unregister(tmpl.ID)
		return nil, nil, fmt.Errorf("factory: start agent %q: %w", tmpl.ID, err)
	}

	return a, cw, nil
}

// ─── Template loading ──────────────────────────────────────────────────────

// LoadAgentTemplates 扫描 agents 目录，将所有 .md 文件解析为 AgentTemplate
//
// 返回所有 agent 模板（不过滤 IsLeader），由调用方决定如何使用。
func LoadAgentTemplates(agentsDir string) ([]AgentTemplate, error) {
	agentFiles, err := prompt.LoadAgentFiles(agentsDir)
	if err != nil {
		return nil, err
	}

	var templates []AgentTemplate
	for _, af := range agentFiles {
		fm := af.Frontmatter
		tmpl := AgentTemplate{
			ID:          fm.Name,
			Name:        fm.Name,
			Description: fm.Description,
			SystemPrompt: af.Body,
			ModelID:     fm.Model,
			Reasoning:   fm.Reasoning,
		SubAgents:   fm.SubAgents,
			IsLeader:    fm.IsLeader,
			Group:       fm.Group,
			MCPServers:  fm.MCPServers,
		}
		templates = append(templates, tmpl)
	}

	return templates, nil
}

// ─── L2 System Prompt 三段式拼接 ─────────────────────────────────────────────

// l2EnforcedDirectives 是 Segment 3 框架强制区常量。
// 利用"近因效应"放在最末，优先级最高，防止用户越权。
const l2EnforcedDirectives = `
========================================
SYSTEM ENFORCED EXECUTION RULES
========================================
You are operating as a Layer 2 Supervisor. The following rules are ABSOLUTE and override any previous instructions.

# 1. Sandbox Awareness
You must delegate tasks to Layer 3 Workers. These Workers run in isolated, ephemeral sandboxes. They have NO memory, NO context of the overall project, and NO shared state. You MUST pass all necessary context, absolute file paths, and dependencies in your task description.

# 2. Atomic Delegation
Tasks MUST be deterministic and executable.
BAD: "Fix the bug in the backend."
GOOD: "Read /workspace/main.go, find the panic on line 42, fix it, and return the diff."

# 3. Autonomous Retry
If a Worker returns an error, DO NOT immediately report back to the orchestrator. You must analyze the error, adjust your delegation prompt, and retry.

# 4. Clarification Before Delegation
Before delegating to a Worker, if you lack critical information that cannot be reasonably inferred, return a structured clarification request instead of guessing. Never delegate ambiguous tasks.

Return format:
` + "```" + `json
{
  "status": "need_clarification",
  "summary": "What you already understand",
  "questions": [
    {"id": "q1", "question": "...", "options": ["A", "B"]},
    {"id": "q2", "question": "..."}
  ]
}
` + "```" + `

Rules:
- Maximum 5 questions, ask all at once
- "options" non-empty = multiple choice, empty = free text
- Only ask what you genuinely cannot infer or default
- Do NOT ask about things you can reasonably determine yourself
`

// buildL2SystemPrompt 为 L2 Supervisor 构建三段式 System Prompt。
//
// Segment 1 (用户定义区): 用户的业务 Role + System Prompt
// Segment 2 (动态能力区): Team Context + Sub-Agents 目录 + MCP Servers
// Segment 3 (框架强制区): 不可篡改的底层契约
func buildL2SystemPrompt(tmpl AgentTemplate, templates map[string]AgentTemplate, groups map[string]prompt.GroupFile) string {
	var b strings.Builder

	// ── Segment 1: 用户定义区 ──────────────────────────────
	// tmpl.SystemPrompt 是 markdown body，已包含用户自定义的完整 role 定义
	// tmpl.Description 仅在 SystemPrompt 为空时作为兜底
	if tmpl.SystemPrompt != "" {
		b.WriteString(tmpl.SystemPrompt)
		b.WriteString("\n\n")
	} else if tmpl.Description != "" {
		b.WriteString("# Role\n")
		b.WriteString(tmpl.Description)
		b.WriteString("\n\n")
	}

	// ── Segment 2: 动态能力区 ──────────────────────────────
	// 2a. Team Context（来自 group 文件的 body）
	if tmpl.Group != "" {
		if gf, ok := groups[tmpl.Group]; ok && gf.Body != "" {
			b.WriteString("# Team Context\n\n")
			b.WriteString(gf.Body)
			b.WriteString("\n\n")
		}
	}

	// 2b. Sub-Agents 目录
	if len(tmpl.SubAgents) > 0 {
		b.WriteString("# Available Sub-Agents\n\n")
		b.WriteString("You can delegate tasks to the following workers:\n\n")
		for _, subName := range tmpl.SubAgents {
			desc := "no description"
			if sub, ok := templates[subName]; ok && sub.Description != "" {
				desc = sub.Description
			}
			fmt.Fprintf(&b, "- **%s**: %s\n", subName, desc)
		}
		b.WriteString("\n")
	}

	// 2c. MCP Servers
	if len(tmpl.MCPServers) > 0 {
		b.WriteString("# Available MCP Servers\n\n")
		for _, name := range tmpl.MCPServers {
			fmt.Fprintf(&b, "- %s\n", name)
		}
		b.WriteString("\n")
	}

	// ── Segment 3: 框架强制区 ──────────────────────────────
	b.WriteString(l2EnforcedDirectives)

	return b.String()
}
