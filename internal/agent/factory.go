package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/prompt"
	"github.com/xiaobaitu/soloqueue/internal/skill"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// ─── AgentTemplate ─────────────────────────────────────────────────────────

// AgentTemplate 是 Agent 实例化的完整描述
//
// 来源于 ~/.soloqueue/agents/*.md 的 YAML frontmatter + markdown body。
type AgentTemplate struct {
	ID           string   // 唯一标识（如 "dev"、"fe"）
	Name         string   // 显示名称
	Description  string   // 给 LLM 看的描述
	SystemPrompt string   // markdown body
	ModelID      string   // 模型 ID（由全局默认模型填充，不再从配置文件读取）
	IsLeader     bool     // 是否为 L2 领导者
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
	registry       *Registry
	llm            LLMClient
	toolsCfg       tools.Config
	skillDir       string
	log            *logger.Logger
	resolveModel   ModelResolver // nil = skip model validation (tests)
	defaultModelID string                         // 当 AgentTemplate.ModelID 为空时使用此默认值
	templates      map[string]AgentTemplate        // 按 ID 索引的全量模板，供 buildL2SystemPrompt 查找子 agent 描述
	groups         map[string]prompt.GroupFile     // group 信息，供 L2 prompt 注入团队上下文
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

// WithDefaultModelID sets the default model ID used when an agent template
// does not specify a model. If not set, agents without a model ID will fail
// to create when ModelResolver is enabled.
func WithDefaultModelID(modelID string) FactoryOption {
	return func(f *DefaultFactory) {
		f.defaultModelID = modelID
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
//  3. 加载 skills（LoadSkillsFromDir）
//  4. 创建 Agent（WithTools, WithSkills, WithParallelTools）
//  5. 创建 ContextWindow，push system prompt + skill catalog
//  6. Register 到 registry
//  7. Start Agent
//  8. 返回 (agent, cw, nil)
func (f *DefaultFactory) Create(ctx context.Context, tmpl AgentTemplate) (*Agent, *ctxwin.ContextWindow, error) {
	// 1. 构建最终 SystemPrompt
	var finalPrompt string
	if tmpl.IsLeader {
		finalPrompt = buildL2SystemPrompt(tmpl, f.templates, f.groups)
	} else {
		finalPrompt = buildL3SystemPrompt(tmpl)
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
		modelID := tmpl.ModelID
		if modelID == "" {
			modelID = f.defaultModelID
		}
		if modelID == "" {
			return nil, nil, fmt.Errorf("agent %q: model ID is empty and no default model configured", tmpl.ID)
		}
		info, err := f.resolveModel(modelID)
		if err != nil {
			return nil, nil, fmt.Errorf("agent %q: invalid model %q: %w", tmpl.ID, modelID, err)
		}
		// Use APIModel for the actual API call (may differ from the config ID)
		if info.APIModel != "" {
			def.ModelID = info.APIModel
		} else {
			def.ModelID = modelID
		}
		def.ContextWindow = info.ContextWindow
		def.Temperature = info.Temperature
		def.MaxTokens = info.MaxTokens
		def.ThinkingEnabled = info.ThinkingEnabled
		def.ReasoningEffort = info.ReasoningEffort
	}

	// 2. 构建内置 tools
	allTools := tools.Build(f.toolsCfg)

	// 2b. L2 领导者：注入同组 L3 Worker 的 delegate 工具
	if tmpl.IsLeader {
		for _, peer := range f.sameGroupWorkers(tmpl) {
			peer := peer // capture loop variable
			dt := &tools.DelegateTool{
				LeaderID: peer.ID,
				Desc:     peer.Description,
				Timeout:  tools.DelegateDefaultTimeout,
				SpawnFn: func(ctx context.Context, task string) (iface.Locatable, error) {
					child, _, err := f.Create(ctx, peer)
					if err != nil {
						return nil, err
					}
					return &LocatableAdapter{Agent: child}, nil
				},
			}
			allTools = append(allTools, dt)
		}
	}

	// 3. 加载 skills
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

	// 4. 构造 Option 列表
	opts := []Option{
		WithTools(allTools...),
		WithSkills(skillList...),
		WithParallelTools(true),
	}
	if tmpl.IsLeader {
		// L2 可以启用 PriorityMailbox，用于接收 L3 结果的高优先级投递
		// 暂不启用，L2 同步阻塞等待 L3，不需要优先级
	}

	// 5. 创建 Agent
	a := NewAgent(def, f.llm, f.log, opts...)

	// 6. 创建 ContextWindow
	cw := ctxwin.NewContextWindow(DefaultContextWindow, 2000, 0, ctxwin.NewTokenizer())
	if a.Def.SystemPrompt != "" {
		cw.Push(ctxwin.RoleSystem, a.Def.SystemPrompt)
	}
	if catalog := a.SkillCatalog(); catalog != "" {
		cw.Push(ctxwin.RoleSystem, catalog)
	}

	// 7. Register 到 registry
	if err := f.registry.Register(a); err != nil {
		return nil, nil, fmt.Errorf("factory: register agent %q: %w", tmpl.ID, err)
	}

	// 8. Start Agent
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
				ID:           fm.Name,
				Name:         fm.Name,
				Description:  fm.Description,
				SystemPrompt: af.Body,
				ModelID:      fm.Model,
				IsLeader:     fm.IsLeader,
				Group:        fm.Group,
				MCPServers:   fm.MCPServers,
			}
		templates = append(templates, tmpl)
	}

	return templates, nil
}

// sameGroupWorkers 返回与 tmpl 同组的所有非 Leader agent 模板。
// 用于为 L2 领导者注入 delegate_* 工具。
func (f *DefaultFactory) sameGroupWorkers(tmpl AgentTemplate) []AgentTemplate {
	var workers []AgentTemplate
	for _, t := range f.templates {
		if t.IsLeader || t.ID == tmpl.ID {
			continue
		}
		if t.Group == tmpl.Group && t.Group != "" {
			workers = append(workers, t)
		}
	}
	return workers
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

# 5. Delegate-First Principle
You MUST delegate tasks to your team members whenever they have the capability to handle them. Only execute tasks yourself when:
- No team member has the relevant capability
- The task is trivial (e.g., answering a quick clarification)
- All capable members have failed and you need to act as fallback
BAD: Task is "add a unit test for login" and you have a "test" worker → you write the test yourself.
GOOD: Task is "add a unit test for login" and you have a "test" worker → you delegate to the "test" worker.

# 6. Strict Scope Adherence
Only delegate tasks that the user (via L1) explicitly requested. Do NOT add "while we're at it" sub-tasks, extra improvements, or tasks that were not in the original request.
BAD: User asked "fix the null pointer crash" → you also delegate "refactor error handling" and "add unit tests for related functions".
GOOD: User asked "fix the null pointer crash" → you delegate ONLY the null pointer fix.

# 7. Cross-Layer English Communication
All inter-layer communication MUST be in English. This includes:
- Task descriptions you send to L3 Workers (delegate_* calls)
- Result summaries you return to L1 (your output)
- Clarification requests
BAD (to L3): "检查 /workspace/main.go 第42行的 panic 并修复它"
GOOD (to L3): "Read /workspace/main.go, find the panic on line 42, fix it, and return the diff."
BAD (to L1): "任务完成，已经修复了登录页面的样式问题"
GOOD (to L1): "Task completed. The CSS styling issue on the login page has been fixed."

# 8. Context-Rich Delegation
Every task you delegate to a Worker MUST include all context the Worker needs to execute autonomously. Workers run in isolated sandboxes with NO prior context. Your task description MUST include:
- Absolute file paths for any files the Worker needs to read or modify
- The relevant code snippet or error message if applicable
- The workspace root path (shown in Workspace section above)
- Any dependencies or related files the Worker should be aware of
BAD: "Fix the CSS bug in the login page"
GOOD: "Fix the CSS bug on the login page. The login component is at /workspace/frontend/src/components/Login.tsx. The CSS module is at /workspace/frontend/src/styles/login.module.css. The bug: the submit button overlaps the password field on mobile viewports. Workspace: /workspace"
`

// buildL2SystemPrompt 为 L2 Supervisor 构建三段式 System Prompt。
//
// Segment 1 (用户定义区): 用户的业务 Role + System Prompt
// Segment 2 (动态能力区): Team Context + 同组 Agents 目录 + MCP Servers
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
	// 2a. Team Context（来自 group 文件的 body）+ Workspace 路径
	if tmpl.Group != "" {
		if gf, ok := groups[tmpl.Group]; ok {
			if gf.Body != "" {
				b.WriteString("# Team Context\n\n")
				b.WriteString(gf.Body)
				b.WriteString("\n\n")
			}
			if len(gf.Frontmatter.Workspaces) > 0 {
				b.WriteString("# Workspace\n\n")
				for _, ws := range gf.Frontmatter.Workspaces {
					fmt.Fprintf(&b, "- **%s**: %s\n", ws.Name, ws.Path)
				}
				b.WriteString("\n")
			}
		}
	}

	// 2b. 同组 Agents 目录（排除 leader 自身）
	var peers []AgentTemplate
	for id, t := range templates {
		if id == tmpl.ID {
			continue
		}
		if tmpl.Group != "" && t.Group == tmpl.Group {
			peers = append(peers, t)
		}
	}
	if len(peers) > 0 {
		b.WriteString("# Available Workers\n\n")
		b.WriteString("You can delegate tasks to the following workers:\n\n")
		for _, peer := range peers {
			desc := peer.Description
			if desc == "" {
				desc = "no description"
			}
			fmt.Fprintf(&b, "- **%s**: %s\n", peer.Name, desc)
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

// ─── L3 System Prompt 两段式拼接 ─────────────────────────────────────────────

// l3EnforcedDirectives 是 L3 Worker 框架强制区常量。
// 利用"近因效应"放在最末，优先级最高，防止用户越权。
const l3EnforcedDirectives = `
========================================
SYSTEM ENFORCED EXECUTION RULES
========================================
You are operating as a Layer 3 Worker. The following rules are ABSOLUTE and override any previous instructions.

# 1. Strict Scope Adherence
Only execute the exact task you were assigned. Do NOT modify files, add features, refactor code, or make any changes beyond what was explicitly requested.
BAD: Task is "fix the null pointer on line 42" → you also refactor the surrounding function and add error handling.
GOOD: Task is "fix the null pointer on line 42" → you fix ONLY the null pointer on line 42.

# 2. English-Only Output
Your output (results, summaries, error reports) MUST be in English. You are part of a multi-layer system where cross-layer communication must be English.
BAD: "修复完成，已经把第42行的空指针问题解决了"
GOOD: "Fix completed. The null pointer issue on line 42 has been resolved."
`

// buildL3SystemPrompt 为 L3 Worker 构建两段式 System Prompt。
//
// Segment 1 (用户定义区): 用户的业务 Role + System Prompt
// Segment 2 (框架强制区): 不可篡改的底层契约
func buildL3SystemPrompt(tmpl AgentTemplate) string {
	var b strings.Builder

	// ── Segment 1: 用户定义区 ──────────────────────────────
	if tmpl.SystemPrompt != "" {
		b.WriteString(tmpl.SystemPrompt)
		b.WriteString("\n\n")
	} else if tmpl.Description != "" {
		b.WriteString("# Role\n")
		b.WriteString(tmpl.Description)
		b.WriteString("\n\n")
	}

	// ── Segment 2: 框架强制区 ──────────────────────────────
	b.WriteString(l3EnforcedDirectives)

	return b.String()
}
