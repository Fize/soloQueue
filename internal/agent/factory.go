package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/mcp"
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
	Permission   bool     // 特权模式，跳过工具确认
	MCPServers   []string // MCP Server 名称列表
	SkillIDs     []string // 该 agent 需要的 skill ID 列表
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
	mu sync.RWMutex // protects llm, toolsCfg, defaultModelID for hot-reload

	registry       *Registry
	llm            LLMClient
	toolsCfg       tools.Config
	defaultModelID string // 当 AgentTemplate.ModelID 为空时使用此默认值
	skillRegistry  *skill.SkillRegistry
	workDir        string // ~/.soloqueue, 用于根据 team 计算 planDir
	log            *logger.Logger
	resolveModel   ModelResolver               // nil = skip model validation (tests)
	templates      map[string]AgentTemplate    // 按 ID 索引的全量模板，供 buildL2SystemPrompt 查找子 agent 描述
	groups         map[string]prompt.GroupFile // group 信息，供 L2 prompt 注入团队上下文
	bypassConfirm  bool                        // global --bypass: skip all confirmations
	mcpManager     *mcp.Manager                // MCP server manager (nil = MCP disabled)
}

// NewDefaultFactory 创建 DefaultFactory
func NewDefaultFactory(
	registry *Registry,
	llm LLMClient,
	toolsCfg tools.Config,
	log *logger.Logger,
	opts ...FactoryOption,
) *DefaultFactory {
	f := &DefaultFactory{
		registry: registry,
		llm:      llm,
		toolsCfg: toolsCfg,
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

// WithBypassConfirm sets the global bypass mode — all agents skip confirmations.
func WithBypassConfirm(on bool) FactoryOption {
	return func(f *DefaultFactory) {
		f.bypassConfirm = on
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

// WithWorkDir sets the workDir (~/.soloqueue) for computing team-specific plan directories.
func WithWorkDir(workDir string) FactoryOption {
	return func(f *DefaultFactory) {
		f.workDir = workDir
	}
}

// WithMCPManager sets the MCP manager for MCP tool registration during agent creation.
func WithMCPManager(mgr *mcp.Manager) FactoryOption {
	return func(f *DefaultFactory) {
		f.mcpManager = mgr
	}
}

// WithSkillRegistry sets the global skill registry for skill resolution during agent creation.
// When set, Create() resolves template SkillIDs against this registry.
func WithSkillRegistry(reg *skill.SkillRegistry) FactoryOption {
	return func(f *DefaultFactory) {
		f.skillRegistry = reg
	}
}

func (f *DefaultFactory) Registry() *Registry {
	return f.registry
}

// SetLLMClient updates the LLM client used by future agent creations (hot-reload support).
func (f *DefaultFactory) SetLLMClient(client LLMClient) {
	f.mu.Lock()
	f.llm = client
	f.mu.Unlock()
}

// SetToolsConfig updates the tools config used by future agent creations (hot-reload support).
func (f *DefaultFactory) SetToolsConfig(cfg tools.Config) {
	f.mu.Lock()
	f.toolsCfg = cfg
	f.mu.Unlock()
}

// SetDefaultModelID updates the default model ID used by future agent creations (hot-reload support).
func (f *DefaultFactory) SetDefaultModelID(modelID string) {
	f.mu.Lock()
	f.defaultModelID = modelID
	f.mu.Unlock()
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
	// Snapshot hot-reloadable fields under read lock for a consistent agent creation.
	f.mu.RLock()
	llm := f.llm
	toolsCfg := f.toolsCfg
	defaultModelID := f.defaultModelID
	f.mu.RUnlock()

	// 1. 构建最终 SystemPrompt
	// Compute team-specific planDir: ~/.soloqueue/plan/<group>/
	// L2/L3 agents use team-isolated plan directories; L1 (no group) falls back to global PlanDir.
	planDir := toolsCfg.PlanDir
	if f.workDir != "" && tmpl.Group != "" {
		teamPlanDir := filepath.Join(f.workDir, "plan", tmpl.Group)
		if err := os.MkdirAll(teamPlanDir, 0o755); err == nil {
			planDir = teamPlanDir
		}
	}
	var finalPrompt string
	if tmpl.IsLeader {
		finalPrompt = buildL2SystemPrompt(tmpl, f.templates, f.groups, planDir, f.workDir)
	} else {
		finalPrompt = buildL3SystemPrompt(tmpl, f.groups, planDir, f.workDir)
	}

	// 2. 构建 Definition
	def := Definition{
		ID:              tmpl.ID,
		Name:            tmpl.Name,
		Role:            RoleUser,
		Kind:            KindCustom,
		ModelID:         tmpl.ModelID,
		SystemPrompt:    finalPrompt,
		ReasoningEffort: "",                 // populated below if resolver is set
		ExplicitModel:   tmpl.ModelID != "", // template explicitly set model → don't override
		BypassConfirm:   f.bypassConfirm || tmpl.Permission,
	}

	// 1b. Validate and resolve model configuration
	if f.resolveModel != nil {
		modelID := tmpl.ModelID
		if modelID == "" {
			modelID = defaultModelID
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
	agentToolsCfg := toolsCfg
	allTools := tools.Build(agentToolsCfg)

	// 2b. L2 领导者：注入同组 L3 Worker 的 delegate 工具
	if tmpl.IsLeader {
		for _, peer := range f.sameGroupWorkers(tmpl) {
			peer := peer // capture loop variable
			dt := tools.NewDelegateTool(peer.ID, peer.Description, 25*time.Minute, nil, f.log)
			dt.SpawnFn = func(ctx context.Context, task string) (iface.Locatable, error) {
				child, _, err := f.Create(ctx, peer)
				if err != nil {
					return nil, err
				}
				return &LocatableAdapter{Agent: child}, nil
			}
			allTools = append(allTools, dt)
		}
	}

	// 3. 加载 skills — 从模板的 SkillIDs 在全局注册表中查找
	var skillList []*skill.Skill
	if f.skillRegistry != nil && len(tmpl.SkillIDs) > 0 {
		sr := skill.NewSkillRegistry()
		for _, id := range tmpl.SkillIDs {
			if s, ok := f.skillRegistry.GetSkill(id); ok {
				skillList = append(skillList, s)
				_ = sr.Register(s)
			}
		}
		if sr.Len() > 0 {
			// Fork spawn: 创建临时子 agent 执行 fork 模式的 skill
			forkSpawn := func(ctx context.Context, s *skill.Skill, content, args string) (iface.Locatable, func(), error) {
				forkDef := Definition{
					ID:           fmt.Sprintf("skill-fork-%s", s.ID),
					ModelID:      def.ModelID,
					SystemPrompt: content,
				}
				forkTools := tools.Build(toolsCfg)
				if len(s.AllowedTools) > 0 {
					forkTools = skill.FilterTools(forkTools, s.AllowedTools)
				}
				child := NewAgent(forkDef, llm, f.log,
					WithTools(forkTools...),
					WithParallelTools(true),
				)
				if err := child.Start(ctx); err != nil {
					return nil, nil, fmt.Errorf("start fork agent: %w", err)
				}
				cleanup := func() { child.Stop(5) }
				return &LocatableAdapter{Agent: child}, cleanup, nil
			}
			skillTool := skill.NewSkillTool(sr, forkSpawn)
			allTools = append(allTools, skillTool)
		}
	}

	// 3d. Register MCP tools for servers listed in the agent template.
	if f.mcpManager != nil && len(tmpl.MCPServers) > 0 {
		for _, serverName := range tmpl.MCPServers {
			mcpTools := f.mcpManager.GetTools(ctx, serverName)
			if mcpTools == nil {
				if f.log != nil {
					f.log.WarnContext(ctx, logger.CatMCP, "MCP server not found or disabled",
						"server", serverName, "agent", tmpl.ID,
					)
				}
				continue
			}
			allTools = append(allTools, mcpTools...)
		}
	}

	// 4. 构造 Option 列表
	opts := []Option{
		WithTools(allTools...),
		WithSkills(skillList...),
		WithParallelTools(true),
		// File operation tools: 30s
		WithToolTimeout("Glob", 30*time.Second),
		WithToolTimeout("Grep", 30*time.Second),
		WithToolTimeout("Read", 30*time.Second),
		WithToolTimeout("Write", 30*time.Second),
		WithToolTimeout("Edit", 30*time.Second),
		WithToolTimeout("MultiWrite", 30*time.Second),
		WithToolTimeout("MultiEdit", 30*time.Second),
		// Network tools: 10min
		WithToolTimeout("WebFetch", 10*time.Minute),
		WithToolTimeout("WebSearch", 10*time.Minute),
	}
	if tmpl.IsLeader {
		// L2 可以启用 PriorityMailbox，用于接收 L3 结果的高优先级投递
		// 暂不启用，L2 同步阻塞等待 L3，不需要优先级
	}

	// 5. 创建 Agent
	a := NewAgent(def, llm, f.log, opts...)

	// 6. 创建 ContextWindow
	//   使用模型配置的上下文窗口大小；未配置时使用 DefaultContextWindow
	maxTokens := def.ContextWindow
	if maxTokens <= 0 {
		maxTokens = DefaultContextWindow
	}
	cw := ctxwin.NewContextWindow(maxTokens, 2000, 0, ctxwin.NewTokenizer())
	if a.Def.SystemPrompt != "" {
		cw.Push(ctxwin.RoleSystem, a.Def.SystemPrompt)
	}

	// 7. Register 到 registry
	if err := f.registry.Register(a); err != nil {
		return nil, nil, fmt.Errorf("factory: register agent %q: %w", tmpl.ID, err)
	}

	// 8. Start Agent
	if err := a.Start(ctx); err != nil {
		f.registry.Unregister(a.InstanceID)
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
			Permission:   fm.Permission,
			MCPServers:   fm.MCPServers,
			SkillIDs:     fm.Skills,
		}
		templates = append(templates, tmpl)
	}

	return templates, nil
}

// workspacePaths 返回 group 配置中所有 workspace 的绝对路径。
func workspacePaths(gf prompt.GroupFile) []string {
	paths := make([]string, 0, len(gf.Frontmatter.Workspaces))
	for _, ws := range gf.Frontmatter.Workspaces {
		if ws.Path != "" {
			paths = append(paths, ws.Path)
		}
	}
	return paths
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
const l2EnforcedDirectivesPart1 = `
========================================
SYSTEM ENFORCED EXECUTION RULES
========================================
You are operating as a Layer 2 Supervisor. The following rules are ABSOLUTE and override any previous instructions.

# 1. Context-Rich Delegation
Layer 3 Workers are stateless — they have no memory of prior tasks, no project overview, and no shared state. When delegating, pass ONLY the distilled findings from your own research: the exact file paths, the specific code to modify, the error to fix. Do NOT forward raw context from L1 or the conversation history. Your job is to research, distill, and delegate — each delegation must be self-contained and minimal.

# 2. Atomic Delegation
Tasks MUST be deterministic and executable.
BAD: "Fix the bug in the backend."
GOOD: "Read /workspace/main.go, find the panic on line 42, fix it, and return the diff."
`

const l2EnforcedPlanSection = `
# 3. MANDATORY Plan Before Execution
**Exploratory tasks are EXEMPT.** Reading files, searching code, investigating issues, or answering questions do NOT require a plan. Execute or delegate them without a plan.

**For implementation tasks:**
1. Assess complexity:
   - **Simple task** (single file, narrow change) → delegate directly to L3. L3 will self-plan if needed.
    - **Complex task** (multi-step, multi-file, multiple Workers) → MUST create a plan (steps 2-13).
2. Use CreatePlan to create a plan. Set its content to the absolute path of the design document.
3. Use AddTodoItems + SetTodoDependencies to define concrete steps with dependency relationships. You MUST set dependencies correctly — they drive the execution order.
4. Write the design document to {{PLAN_DIR}}/<feature-name>.md. It MUST contain: Goal, Approach, Impact, and Steps.
5. Present the plan to L1. **MUST include PLAN_ID: <id> in your response.** L1 will reply "PLAN_ID: <id> approved" when ready.
6. After approval, parse the PLAN_ID from L1's reply. If no PLAN_ID found → use ListPlans to find your plan (status="plan"). Then UpdatePlan to "running".

**Execution loop — you MUST follow these steps EXACTLY in order, no skipping:**

7. Read the plan's todos and their dependencies (use ListTodos or ListPlans).
   You MUST check dependencies — they determine what can run in parallel.
8. Identify ALL todos whose dependencies are satisfied (no uncompleted blockers).
9. CRITICAL — Delegate ALL identified todos IN PARALLEL in a SINGLE turn.
   Call multiple delegate_* tools in one response. NEVER delegate them one by one.
   Parallel execution of independent items is MANDATORY, not optional.
10. Wait for ALL parallel delegations in this batch to return results.
11. For EACH completed delegation → call ToggleTodo(id, "done") on success,
    or ToggleTodo(id, "failed") on error. This is REQUIRED after every batch.
12. Repeat from step 7. Find the next batch of todos whose dependencies
    are now satisfied. Continue the loop until no remaining todos.
13. When ALL todos are marked done/failed → call UpdatePlan("done").

**When L3 submits a plan for review:**
- Approve autonomously if straightforward → reply "PLAN_ID: <id> approved"
- Escalate to L1 only for significant trade-offs

BAD: delegate todo1 → wait → ToggleTodo(todo1) → delegate todo2 → wait → ToggleTodo(todo2) → delegate todo3 → wait → ToggleTodo(todo3)
BAD: delegate todo1+todo2+todo3 in parallel → wait → mark ZERO todos → go to step 7
GOOD: delegate todo1+todo2+todo3 (all independent) → wait all → ToggleTodo(1)+ToggleTodo(2)+ToggleTodo(3) → delegate next batch → ...
GOOD: delegate todo1 (only item ready) → wait → ToggleTodo(1) → delegate todo2+todo3 (now unblocked) → wait both → ToggleTodo(2)+ToggleTodo(3) → UpdatePlan("done")
`


const l2EnforcedExplorationSection = `
# 9. Exploration Artifacts
When you perform exploration tasks (reading files, searching code, investigating issues), you SHOULD save a markdown artifact to /tmp/soloqueue-explore if the exploration is complex or the findings are worth sharing with other agents.

## When to Save
- Complex investigations with many files or nuanced conclusions
- Investigations whose results may be reused by other agents in the same session
- Simple one-off lookups can skip saving

## Document Naming
Format: /tmp/soloqueue-explore/<task-slug>_<agent-id>.md
Examples:
- /tmp/soloqueue-explore/explore_auth_flow_dev-leader.md
- /tmp/soloqueue-explore/investigate_race_condition_dev-leader.md

## Document Content
- Agent: your id/name/layer
- Created at: use current time when saving
- Updated at: use current time when updating
- Freshness window: same-day
- Task: the original or summarized task description
- Key Findings, Files Inspected, Reusable Context, Open Questions

## Reuse Rules
1. Before delegating an exploration task to L3, check /tmp/soloqueue-explore for an existing artifact with the same task-slug and your agent-id.
2. If an artifact exists and was created today, read it first and pass its findings to the L3 Worker to avoid redundant exploration.
3. If you create or reuse an artifact, include its path in your response to L1 so other agents can access it.
4. When delegating to L3, you may ask the Worker to write an artifact and return its path.
`

const l2EnforcedPostPlan = `
# 10. Escalation Decision Rule
- If you CAN make a reasonable decision based on context → decide autonomously and proceed.
- If you CANNOT (ambiguous requirements, significant trade-offs, risk of unintended consequences) → escalate to L1 with options and reasoning.
`

const l2EnforcedToolAwareness = `
# 11. Tool Awareness
Before acting, scan ALL available tools and read their descriptions. Each tool's description defines its purpose. Choose the right tool — do not default to a familiar subset when another tool fits better.
`

const l2EnforcedDirectivesPart2 = `
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

# 5. Autonomous Retry
If a Worker returns an error, DO NOT immediately report back to the orchestrator. You must analyze the error, adjust your delegation prompt, and retry.

# 6. Delegate-First Principle
You MUST delegate tasks to your team members whenever they have the capability to handle them. Only execute tasks yourself when:
- No team member has the relevant capability
- The task is trivial (e.g., answering a quick clarification)
- All capable members have failed and you need to act as fallback
BAD: Task is "add a unit test for login" and you have a "test" worker → you write the test yourself.
GOOD: Task is "add a unit test for login" and you have a "test" worker → you delegate to the "test" worker.

# 7. Strict Scope Adherence
Only delegate tasks that the user (via L1) explicitly requested. Do NOT add "while we're at it" sub-tasks, extra improvements, or tasks that were not in the original request.
BAD: User asked "fix the null pointer crash" → you also delegate "refactor error handling" and "add unit tests for related functions".
GOOD: User asked "fix the null pointer crash" → you delegate ONLY the null pointer fix.

# 8. Cross-Layer English Communication
All inter-layer communication MUST be in English. This includes:
- Task descriptions you send to L3 Workers (delegate_* calls)
- Result summaries you return to L1 (your output)
- Clarification requests
BAD (to L3): "检查 /workspace/main.go 第42行的 panic 并修复它"
GOOD (to L3): "Read /workspace/main.go, find the panic on line 42, fix it, and return the diff."
BAD (to L1): "任务完成，已经修复了登录页面的样式问题"
GOOD (to L1): "Task completed. The CSS styling issue on the login page has been fixed."
`

// buildL2SystemPrompt 为 L2 Supervisor 构建三段式 System Prompt。
//
// Segment 1 (用户定义区): 用户的业务 Role + System Prompt
// Segment 2 (动态能力区): Team Context + 同组 Agents 目录 + MCP Servers
// Segment 3 (框架强制区): 不可篡改的底层契约
func buildL2SystemPrompt(tmpl AgentTemplate, templates map[string]AgentTemplate, groups map[string]prompt.GroupFile, planDir, workDir string) string {
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
	// 2a. Working Directory (global + team workspaces)
	b.WriteString("# Working Directory\n\n")
	b.WriteString("Your global working directory is `~/.soloqueue`. All soloQueue configuration, agent definitions, plans, memory, and data files reside under this directory. When writing or reading files within soloQueue's own directories, use this path.\n")
	if tmpl.Group != "" {
		if gf, ok := groups[tmpl.Group]; ok && len(gf.Frontmatter.Workspaces) > 0 {
			b.WriteString("\nTeam workspaces:\n")
			for _, ws := range gf.Frontmatter.Workspaces {
				fmt.Fprintf(&b, "- **%s**: %s\n", ws.Name, ws.Path)
			}
			b.WriteString("\nWhen working in one of the workspaces above, you **must** check its root directory for project-specific instruction files before starting any task:\n")
			b.WriteString("1. `AGENTS.md` — AI agent tactical guidance (if this file exists, read it and skip `CLAUDE.md`)\n")
			b.WriteString("2. `CLAUDE.md` — Project architecture, conventions, and guidelines (only read if `AGENTS.md` does not exist)\n")
			b.WriteString("3. If neither file exists, proceed without project-specific instructions.\n")
		}
	}
	b.WriteString("\n")

	// 2b. Team Context (from group file body)
	if tmpl.Group != "" {
		if gf, ok := groups[tmpl.Group]; ok {
			if gf.Body != "" {
				b.WriteString("# Team Context\n\n")
				b.WriteString(gf.Body)
				b.WriteString("\n\n")
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
	displayPlanDir := planDir
		if workDir != "" && strings.HasPrefix(planDir, workDir) {
			displayPlanDir = "~/.soloqueue" + planDir[len(workDir):]
		}
		b.WriteString(strings.ReplaceAll(l2EnforcedDirectivesPart1, "{{PLAN_DIR}}", displayPlanDir))
		if planDir != "" {
			b.WriteString(strings.ReplaceAll(l2EnforcedPlanSection, "{{PLAN_DIR}}", displayPlanDir))
		}
		b.WriteString(strings.ReplaceAll(l2EnforcedDirectivesPart2, "{{PLAN_DIR}}", displayPlanDir))
		b.WriteString(strings.ReplaceAll(l2EnforcedExplorationSection, "{{PLAN_DIR}}", displayPlanDir))
		b.WriteString(strings.ReplaceAll(l2EnforcedPostPlan, "{{PLAN_DIR}}", displayPlanDir))
		b.WriteString(strings.ReplaceAll(l2EnforcedToolAwareness, "{{PLAN_DIR}}", displayPlanDir))

		return b.String()
	}

	// ─── L3 System Prompt 两段式拼接 ─────────────────────────────────────────────

// l3EnforcedDirectives is the always-included portion of the L3 enforced rules.
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

# 3. Follow the Plan — you MUST execute todos one at a time and mark each:
1. Read the plan's todos. If readable → proceed to step 3.
2. If NO todo items exist → create your own plan: CreatePlan + AddTodoItems + SetTodoDependencies + design doc → present PLAN_ID → wait for approval → UpdatePlan("running").
3. Pick the FIRST uncompleted todo from the list.
4. Execute it using the appropriate tool.
5. IMMEDIATELY after completion → ToggleTodo(id, "done") on success, or ToggleTodo(id, "failed") on error. This step is MANDATORY — you MUST NOT skip it.
6. Repeat from step 3 for the next uncompleted todo.
7. When ALL todos are done → call UpdatePlan("done").

BAD: execute all work → UpdatePlan("done") at the end without per-todo tracking.
GOOD: execute todo1 → ToggleTodo(todo1, "done") → execute todo2 → ToggleTodo(todo2, "done") → ... → UpdatePlan("done").
BAD: execute all work in one shot → no ToggleTodo calls at all.
GOOD: After every completed work item → ToggleTodo(id, "done"). No exceptions.
`

const l3EnforcedExplorationSection = `
# 4. Exploration Artifacts
When you perform exploration tasks (reading files, searching code, investigating issues), you SHOULD save a markdown artifact to /tmp/soloqueue-explore if the exploration is complex or the findings are worth sharing with other agents.

## When to Save
- Complex investigations with many files or nuanced conclusions
- Investigations whose results may be reused by other agents in the same session
- Simple one-off lookups can skip saving

## Document Naming
Format: /tmp/soloqueue-explore/<task-slug>_<agent-id>.md
Examples:
- /tmp/soloqueue-explore/explore_auth_flow_backend-worker.md
- /tmp/soloqueue-explore/investigate_race_condition_backend-worker.md

## Document Content
- Agent: your id/name/layer
- Created at: use current time when saving
- Updated at: use current time when updating
- Freshness window: same-day
- Task: the original or summarized task description
- Key Findings, Files Inspected, Reusable Context, Open Questions

## Reuse Rules
1. Before starting a new exploration, check /tmp/soloqueue-explore for an existing artifact with the same task-slug and your agent-id.
2. If an artifact exists and was created today, read it first and reuse its findings when appropriate.
3. If you create or reuse an artifact, include its path in your response to L2 so other agents can access it.
`

const l3EnforcedPostPlan = `
# 5. Escalation Decision Rule
- If you CAN make a reasonable decision based on context → decide autonomously and proceed.
- If you CANNOT (ambiguous requirements, significant trade-offs) → escalate to L2 with options and reasoning.
`

const l3EnforcedToolAwareness = `
# 6. Tool Awareness
Before executing a task, scan ALL available tools and read their descriptions. Pick the tool that best matches the task. Do not default to a small subset of familiar tools.
`

// buildL3SystemPrompt 为 L3 Worker 构建两段式 System Prompt。
//
// Segment 1 (用户定义区): 用户的业务 Role + System Prompt
// Segment 2 (框架强制区): 不可篡改的底层契约
func buildL3SystemPrompt(tmpl AgentTemplate, groups map[string]prompt.GroupFile, planDir, workDir string) string {
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

	// ── Working Directory ──────────────────────────────────
	b.WriteString("# Working Directory\n\n")
	b.WriteString("Your global working directory is `~/.soloqueue`. All soloQueue configuration, agent definitions, plans, memory, and data files reside under this directory. When writing or reading files within soloQueue's own directories, use this path.\n")
	if tmpl.Group != "" {
		if gf, ok := groups[tmpl.Group]; ok && len(gf.Frontmatter.Workspaces) > 0 {
			b.WriteString("\nTeam workspaces:\n")
			for _, ws := range gf.Frontmatter.Workspaces {
				fmt.Fprintf(&b, "- **%s**: %s\n", ws.Name, ws.Path)
			}
			b.WriteString("\nWhen working in one of the workspaces above, you **must** check its root directory for project-specific instruction files before starting any task:\n")
			b.WriteString("1. `AGENTS.md` — AI agent tactical guidance (if this file exists, read it and skip `CLAUDE.md`)\n")
			b.WriteString("2. `CLAUDE.md` — Project architecture, conventions, and guidelines (only read if `AGENTS.md` does not exist)\n")
			b.WriteString("3. If neither file exists, proceed without project-specific instructions.\n")
		}
	}
	b.WriteString("\n")

	// ── Segment 2: 框架强制区 ──────────────────────────────
	displayPlanDir := planDir
		if workDir != "" && strings.HasPrefix(planDir, workDir) {
			displayPlanDir = "~/.soloqueue" + planDir[len(workDir):]
		}
		b.WriteString(strings.ReplaceAll(l3EnforcedDirectives, "{{PLAN_DIR}}", displayPlanDir))
		b.WriteString(strings.ReplaceAll(l3EnforcedExplorationSection, "{{PLAN_DIR}}", displayPlanDir))
		b.WriteString(strings.ReplaceAll(l3EnforcedPostPlan, "{{PLAN_DIR}}", displayPlanDir))
		b.WriteString(strings.ReplaceAll(l3EnforcedToolAwareness, "{{PLAN_DIR}}", displayPlanDir))

		return b.String()
	}
