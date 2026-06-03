package agent

import (
	"context"
	"encoding/json"
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
	"github.com/xiaobaitu/soloqueue/internal/teamstore"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// ─── AgentTemplate ─────────────────────────────────────────────────────────

// AgentTemplate 是 Agent 实例化的完整描述
//
// 来源于 ~/.soloqueue/agents/*.md 的 YAML frontmatter + markdown body。
type AgentTemplate struct {
	ID           string            // 唯一标识（如 "dev"、"fe"）
	Name         string            // 显示名称
	Description  string            // 给 LLM 看的描述
	SystemPrompt string            // markdown body
	ModelID      string            // 模型 ID（由全局默认模型填充，不再从配置文件读取）
	IsLeader     bool              // 是否为 L2 领导者
	Group        string            // 所属 group name
	Permission   bool              // 特权模式，跳过工具确认
	MCPServers   []string          // MCP Server 名称列表
	SkillIDs     []string          // 该 agent 需要的 skill ID 列表
	ExternalType string            // 外部 Agent 类型 ("claude", "codex", "opencode", "gemini")
	CustomArgs   []string          // 用户自定义 CLI 参数
	CustomEnv    map[string]string // 用户自定义环境变量
}

// ─── ModelInfo ────────────────────────────────────────────────────────────

// ModelInfo holds the resolved model configuration for an agent.
// Populated by ModelResolver from the settings model registry.
type ModelInfo struct {
	ProviderID string

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
	// workDir is the project working directory for this agent.
	// Empty string means use the factory's global workDir (~/.soloqueue).
	Create(ctx context.Context, tmpl AgentTemplate, workDir string) (*Agent, *ctxwin.ContextWindow, error)

	// Registry 返回内部的 Agent Registry（供 Supervisor 使用）
	Registry() *Registry
}

// ─── DefaultFactory ────────────────────────────────────────────────────────

// DefaultFactory 是 AgentFactory 的默认实现
//
// 包含创建 Agent 所需的所有依赖。创建的 Agent 会自动注册到 Registry 并启动。
type DefaultFactory struct {
	mu sync.RWMutex

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
	exploreDir     string                      // exploration artifact directory (platform-appropriate)
	teamstore      *teamstore.Store            // DB-backed team/agent store (nil = disabled)
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

// WithExploreDir sets the exploration artifacts directory (platform-appropriate).
func WithExploreDir(exploreDir string) FactoryOption {
	return func(f *DefaultFactory) {
		f.exploreDir = exploreDir
	}
}

// WithTeamStore sets the DB-backed team/agent store for the factory.
func WithTeamStore(store *teamstore.Store) FactoryOption {
	return func(f *DefaultFactory) {
		f.teamstore = store
	}
}

func (f *DefaultFactory) Registry() *Registry {
	return f.registry
}

// SetToolsConfig updates the tools config of the factory dynamically.
func (f *DefaultFactory) SetToolsConfig(cfg tools.Config) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.toolsCfg = cfg
}

// UpdateLLM updates the LLM Client of the factory dynamically.
func (f *DefaultFactory) UpdateLLM(llm LLMClient) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.llm = llm
}

// UpdateDefaultModelID updates the default model ID of the factory dynamically.
func (f *DefaultFactory) UpdateDefaultModelID(modelID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.defaultModelID = modelID
}

// Log returns the structured logger of the factory.
func (f *DefaultFactory) Log() *logger.Logger {
	return f.log
}

// Create 根据 tmpl 创建并启动一个 Agent 实例
//
// 流程：
//  1. 构建最终 SystemPrompt（L2 使用三段式拼接，L3 直接使用 body/description）
//  2. Build(toolsCfg) → 内置 tools
//  3. 加载 skills（LoadSkillsFromDir）
//  4. 创建 Agent（WithTools, WithSkills, WithParallelTools, WithAgentWorkDir）
//  5. 创建 ContextWindow，push system prompt + skill catalog
//  6. Register 到 registry
//  7. Start Agent
//  8. 返回 (agent, cw, nil)
//
// workDir is the project working directory for this agent.
// If empty, the factory's global workDir (~/.soloqueue) is used.
func (f *DefaultFactory) Create(ctx context.Context, tmpl AgentTemplate, workDir string) (*Agent, *ctxwin.ContextWindow, error) {
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
	// Resolve effective workDir: use project-specific dir or fall back to global/group workspace
	effectiveWorkDir := workDir
	if effectiveWorkDir == "" || effectiveWorkDir == f.workDir {
		if tmpl.Group != "" && f.workDir != "" {
			effectiveWorkDir = filepath.Join(f.workDir, "workspace", tmpl.Group)
		} else if effectiveWorkDir == "" {
			effectiveWorkDir = f.workDir
		}
	}

	// Make sure the effective working directory exists
	if effectiveWorkDir != "" {
		if err := os.MkdirAll(effectiveWorkDir, 0o755); err != nil {
			if f.log != nil {
				f.log.WarnContext(ctx, logger.CatConfig, "failed to create working directory",
					"dir", effectiveWorkDir, "err", err.Error())
			}
		}
	}

	// Compute platform-appropriate explore directory
	exploreDir := f.exploreDir
	if exploreDir == "" && effectiveWorkDir != "" {
		exploreDir = prompt.ExploreDir(effectiveWorkDir)
	}

	// Load project-level resources (.claude/agents, .claude/skills, .claude/mcp.json, AGENTS.md)
	var projRes projectResources
	if effectiveWorkDir != "" && effectiveWorkDir != f.workDir {
		projRes = f.loadProjectResources(effectiveWorkDir)
	}

	var finalPrompt string
	hasPermanentMemory := toolsCfg.PermanentManager != nil
	if tmpl.IsLeader {
		finalPrompt = buildL2SystemPrompt(tmpl, f.templates, f.groups, planDir, effectiveWorkDir, exploreDir, projRes.agents, hasPermanentMemory)
	} else {
		finalPrompt = buildL3SystemPrompt(tmpl, f.groups, planDir, effectiveWorkDir, exploreDir, hasPermanentMemory)
	}

	// Inject project instructions (AGENTS.md / CLAUDE.md) into system prompt
	if projRes.projectPrompt != "" {
		finalPrompt = finalPrompt + projRes.projectPrompt
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

	if tmpl.ExternalType != "" {
		if !tmpl.IsLeader && tmpl.Group == "" {
			return nil, nil, fmt.Errorf("agent %q: external agent type %q is only allowed for L2 leaders or L3 workers", tmpl.ID, tmpl.ExternalType)
		}
		def.Kind = KindExternal
		def.ExternalType = tmpl.ExternalType
		def.CustomArgs = tmpl.CustomArgs
		def.CustomEnv = tmpl.CustomEnv
	}

	// 1b. Validate and resolve model configuration
	if f.resolveModel != nil && tmpl.ExternalType == "" {
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
		def.ProviderID = info.ProviderID
		def.ContextWindow = info.ContextWindow
		def.Temperature = info.Temperature
		def.MaxTokens = info.MaxTokens
		def.ThinkingEnabled = info.ThinkingEnabled
		def.ReasoningEffort = info.ReasoningEffort
	}

	// 2. 构建内置 tools
	agentToolsCfg := toolsCfg
	agentToolsCfg.WorkDir = effectiveWorkDir
	allTools := tools.Build(agentToolsCfg)

	// 2b. L2 领导者：注入同组 L3 Worker + project-level agents 的 delegate 工具
	if tmpl.IsLeader {
		for _, peer := range f.visibleWorkers(tmpl, projRes.agents) {
			peer := peer // capture loop variable
			dt := tools.NewDelegateTool(peer.ID, peer.Description, 25*time.Minute, nil, f.log)
			dt.SpawnFn = func(ctx context.Context, task string, wd string) (iface.Locatable, error) {
				child, _, err := f.Create(ctx, peer, wd)
				if err != nil {
					return nil, err
				}
				return &LocatableAdapter{Agent: child}, nil
			}
			allTools = append(allTools, dt)
		}
	}

	// 3. 加载 skills — 合并全局注册表 + project-level skills（项目级覆盖全局）
	mergedSkillReg := skill.NewSkillRegistry()
	if f.skillRegistry != nil {
		for _, s := range f.skillRegistry.Skills() {
			if !s.Disabled {
				_ = mergedSkillReg.Register(s)
			}
		}
	}
	for _, s := range projRes.skills {
		if !s.Disabled {
			_ = mergedSkillReg.Register(s) // override if same ID
		}
	}

	var skillList []*skill.Skill
	if len(tmpl.SkillIDs) > 0 {
		sr := skill.NewSkillRegistry()
		for _, id := range tmpl.SkillIDs {
			if s, ok := mergedSkillReg.GetSkill(id); ok {
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
	// Project-level MCP config overrides global config for the same server name.
	if f.mcpManager != nil && len(tmpl.MCPServers) > 0 {
		for _, serverName := range tmpl.MCPServers {
			mcpTools := f.mcpManager.GetToolsWithOverride(ctx, serverName, projRes.mcpCfg)
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
		WithAgentWorkDir(effectiveWorkDir),
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

	// 7. 创建 ContextWindow
	//   使用模型配置的上下文窗口大小；未配置时使用 DefaultContextWindow
	maxTokens := def.ContextWindow
	if maxTokens <= 0 {
		maxTokens = DefaultContextWindow
	}
	cw := ctxwin.NewContextWindow(maxTokens, 2000, 0, ctxwin.NewTokenizer())
	if a.Def.SystemPrompt != "" {
		cw.Push(ctxwin.RoleSystem, a.Def.SystemPrompt)
	}

	// 8. Register 到 registry
	if err := f.registry.Register(a); err != nil {
		return nil, nil, fmt.Errorf("factory: register agent %q: %w", tmpl.ID, err)
	}

	// 9. Start Agent
	if err := a.Start(ctx); err != nil {
		f.registry.Unregister(a.InstanceID)
		return nil, nil, fmt.Errorf("factory: start agent %q: %w", tmpl.ID, err)
	}

	return a, cw, nil
}

// projectResources holds all project-level configuration loaded from .claude/.
type projectResources struct {
	agents       []AgentTemplate
	skills       []*skill.Skill
	mcpCfg       *mcp.Config
	projectPrompt string // AGENTS.md or CLAUDE.md content
}

// loadProjectResources loads all project-level configuration from the project
// directory's .claude/ folder (and AGENTS.md/CLAUDE.md in the project root).
//
// Loaded resources:
//   - .claude/agents/*.md   → project-level agent definitions
//   - .claude/skills/*/SKILL.md → project-level skills
//   - .claude/mcp.json      → project-level MCP server config
//   - AGENTS.md / CLAUDE.md → project instructions for system prompt
func (f *DefaultFactory) loadProjectResources(projectDir string) projectResources {
	var res projectResources

	// 1. AGENTS.md or CLAUDE.md (project root)
	agentsMDPath := filepath.Join(projectDir, "AGENTS.md")
	claudeMDPath := filepath.Join(projectDir, "CLAUDE.md")
	if data, err := os.ReadFile(agentsMDPath); err == nil {
		res.projectPrompt = "\n\n# Project Instructions (from AGENTS.md)\n\n" + string(data)
	} else if data, err := os.ReadFile(claudeMDPath); err == nil {
		res.projectPrompt = "\n\n# Project Instructions (from CLAUDE.md)\n\n" + string(data)
	}

	// 2. .claude/agents/*.md
	agentsDir := filepath.Join(projectDir, ".claude", "agents")
	if agents, err := LoadAgentTemplates(agentsDir); err == nil {
		res.agents = agents
		if f.log != nil && len(agents) > 0 {
			f.log.InfoContext(context.Background(), logger.CatConfig,
				"loadProjectResources: loaded project agents",
				"count", len(agents), "project", projectDir)
		}
	}

	// 3. .claude/skills/
	skillsDir := filepath.Join(projectDir, ".claude", "skills")
	if skills, err := skill.LoadSkillsFromDir(skillsDir); err == nil {
		res.skills = skills
		if f.log != nil && len(skills) > 0 {
			f.log.InfoContext(context.Background(), logger.CatConfig,
				"loadProjectResources: loaded project skills",
				"count", len(skills), "project", projectDir)
		}
	}

	// 4. .claude/mcp.json
	mcpPath := filepath.Join(projectDir, ".claude", "mcp.json")
	if data, err := os.ReadFile(mcpPath); err == nil {
		var cfg mcp.Config
		if json.Unmarshal(data, &cfg) == nil && len(cfg.Servers) > 0 {
			res.mcpCfg = &cfg
			if f.log != nil {
				f.log.InfoContext(context.Background(), logger.CatConfig,
					"loadProjectResources: loaded project MCP config",
					"servers", len(cfg.Servers), "project", projectDir)
			}
		}
	}

	return res
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
			ID:           strings.ToLower(fm.Name),
			Name:         fm.Name,
			Description:  fm.Description,
			SystemPrompt: af.Body,
			ModelID:      fm.Model,
			IsLeader:     fm.IsLeader,
			Group:        fm.Group,
			Permission:   fm.Permission,
			MCPServers:   fm.MCPServers,
			SkillIDs:     fm.Skills,
			ExternalType: fm.ExternalType,
			CustomArgs:   fm.CustomArgs,
			CustomEnv:    fm.CustomEnv,
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

// visibleWorkers 返回与 tmpl 同组的全局 agent 模板 + project-level agent 模板的合并列表。
// Project-level agents override global agents with the same ID.
// 用于为 L2 领导者注入 delegate_* 工具。
func (f *DefaultFactory) visibleWorkers(tmpl AgentTemplate, projectAgents []AgentTemplate) []AgentTemplate {
	merged := make(map[string]AgentTemplate)

	// Global workers from the same group
	for _, t := range f.templates {
		if t.IsLeader || t.ID == tmpl.ID {
			continue
		}
		if t.Group == tmpl.Group && t.Group != "" {
			merged[t.ID] = t
		}
	}

	// Project-level workers override global ones with the same ID
	for _, t := range projectAgents {
		if t.IsLeader || t.ID == tmpl.ID {
			continue
		}
		merged[t.ID] = t
	}

	var workers []AgentTemplate
	for _, t := range merged {
		workers = append(workers, t)
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

# 1a. Work Directory Propagation
When delegating tasks to L3 Workers via delegate_* tools, you MUST always include the ` + "`" + `work_dir` + "`" + ` parameter. Set it to your current working directory. This ensures the L3 Worker loads project-specific configuration (AGENTS.md, CLAUDE.md, .claude/) from the correct directory.

BAD: delegate_worker(task="Fix login bug")
GOOD: delegate_worker(task="Fix login bug", work_dir="/path/to/project")

# 2. Atomic Delegation
Tasks MUST be deterministic and executable.
BAD: "Fix the bug in the backend."
GOOD: "Read /workspace/main.go, find the panic on line 42, fix it, and return the diff."
`

const l2EnforcedPlanSection = `
# 3. MANDATORY Plan Before Execution (Plan & Issue Tracking in Kanban)
This rule establishes a **MANDATORY Plan Before Execution** policy for all non-trivial implementation tasks.
**Exploratory tasks are EXEMPT.** Reading files, searching code, investigating issues, or answering questions do NOT require a plan. Execute or delegate them without a plan.

**For implementation tasks:**
1. Assess complexity:
   - **Simple task** (single file, narrow change) → delegate directly to L3. L3 will self-plan if needed.
   - **Complex task** (multi-step, multi-file, multiple Workers) → MUST create a plan (steps 2-12).
2. Use ManageIssue with action="create" to create a new issue:
   - Provide a brief summary in the "description" parameter (this is a short description, not the full plan).
   - Record the detailed technical design / steps (Goal, Approach, Impact, Steps) in the "plan" parameter (this is the markdown document for your implementation plan).
   - Set the "author" parameter to your agent name (e.g. "Dev", "QA", etc.).
3. Use ManageIssue with action="add_task" to define checklist items for the issue. You MUST specify dependencies where applicable to drive the correct execution order.
4. **Approval decision — choose ONE:**
   - **Auto-approve (default for most tasks):** If the plan is straightforward and low-risk → proceed directly to execution without waiting for L1. Update status to "running" using ManageIssue (action="update", status="running", id="<id>").
   - **Escalate to L1 (only for significant trade-offs):** If the plan involves irreversible changes, significant architectural decisions, or conflicts with unclear requirements → return a structured response to L1:
     ` + "`" + `PLAN_REVIEW_REQUIRED
ISSUE_ID: <id>
Summary: <one-line summary of the plan>
Trade-offs: <what requires human decision>` + "`" + `
     Wait for L1 to re-delegate with "ISSUE_ID: <id> approved" before executing.
5. After approval (auto or from L1's re-delegation), update the issue status to "running".

**Execution loop — you MUST follow these steps EXACTLY in order, no skipping:**

6. Read the issue's tasks and their dependencies using ManageIssue with action="get" and id="<id>".
   You MUST check dependencies — they determine what can run in parallel.
7. Identify ALL tasks whose dependencies are satisfied (no uncompleted blockers).
8. CRITICAL — Delegate ALL identified tasks IN PARALLEL in a SINGLE turn.
   Call multiple delegate_* tools in one response. NEVER delegate them one by one.
   Parallel execution of independent items is MANDATORY, not optional.
10. Wait for ALL parallel delegations in this batch to return results.
11. For EACH completed delegation →:
    a. Call ManageIssue with action="toggle_task" and task_id="<task_id>" to mark it complete.
    b. Call ManageIssue with action="add_comment" and set "author" to your agent name to record the results/findings/diffs of that task.
12. Repeat from step 7. Find the next batch of checklist tasks whose dependencies are now satisfied. Continue the loop until no remaining tasks.
13. When ALL checklist tasks are marked completed → call ManageIssue with action="update", id="<id>", and status="done".

**When L3 submits a plan for review:**
- Approve autonomously if straightforward → reply "ISSUE_ID: <id> approved" and proceed.
- Escalate to L1 only for significant trade-offs using the PLAN_REVIEW_REQUIRED format above.

**When L1 re-delegates with "ISSUE_ID: <id> approved":**
- Look up the issue using ManageIssue (action="get", id="<id>") to retrieve the plan and tasks.
- Proceed directly to the execution loop (step 5 onwards).

BAD: delegate task1 → wait → Toggle task1 → delegate task2 → wait ...
BAD: delegate task1+task2+task3 in parallel → wait → mark ZERO tasks complete
GOOD: delegate task1+task2+task3 (all independent) → wait all → toggle_task(1)+toggle_task(2)+toggle_task(3) → delegate next batch.
`


const l2EnforcedExplorationSection = `
# 9. Exploration Artifacts
When you perform exploration tasks (reading files, searching code, investigating issues), you SHOULD save a markdown artifact to {{EXPLORE_DIR}} if the exploration is complex or the findings are worth sharing with other agents.

## When to Save
- Complex investigations with many files or nuanced conclusions
- Investigations whose results may be reused by other agents in the same session
- Simple one-off lookups can skip saving

## Document Naming
Format: {{EXPLORE_DIR}}/<task-slug>_<agent-id>.md
Examples:
- {{EXPLORE_DIR}}/explore_auth_flow_dev-leader.md
- {{EXPLORE_DIR}}/investigate_race_condition_dev-leader.md

## Document Content
- Agent: your id/name/layer
- Created at: use current time when saving
- Updated at: use current time when updating
- Freshness window: same-day
- Task: the original or summarized task description
- Key Findings, Files Inspected, Reusable Context, Open Questions

## Reuse Rules
1. Before delegating an exploration task to L3, check {{EXPLORE_DIR}} for an existing artifact with the same task-slug and your agent-id.
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
Prefer the Read tool for reading files. Using Bash with cat wastes tokens and bypasses the Read tool's size limit. Use Bash for running commands, not for reading text files. If a file exceeds the Read limit, use Bash with head/tail to read portions.

# 12. Prefer Search Before Read
Before reading file contents, you MUST first use Grep or Glob to locate the relevant files and line numbers. Do NOT directly Read large files (>25,000 tokens). If a file exceeds the limit, use the Read tool's offset/limit pagination parameters to read in chunks, or use Grep to narrow the scope first.
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
func buildL2SystemPrompt(tmpl AgentTemplate, templates map[string]AgentTemplate, groups map[string]prompt.GroupFile, planDir, workDir, exploreDir string, projectAgents []AgentTemplate, hasPermanentMemory bool) string {
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
	// 2a. Working Directory
	b.WriteString("# Working Directory\n\n")
	fmt.Fprintf(&b, "Your working directory is `%s`. All project files, source code, and configurations reside under this directory. Use this as the base for all file operations.\n", workDir)
	if tmpl.Group != "" {
		if gf, ok := groups[tmpl.Group]; ok && len(gf.Frontmatter.Workspaces) > 0 {
			b.WriteString("\nTeam workspaces:\n")
			for _, ws := range gf.Frontmatter.Workspaces {
				mark := " "
				if ws.Path == workDir {
					mark = "▶"
				}
				fmt.Fprintf(&b, "- **%s**: %s %s\n", ws.Name, ws.Path, mark)
			}
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

	// 2b. 同组 Agents 目录（排除 leader 自身）+ project-level agents 合并
	mergedWorkers := make(map[string]AgentTemplate)
	for id, t := range templates {
		if id == tmpl.ID {
			continue
		}
		if tmpl.Group != "" && t.Group == tmpl.Group {
			mergedWorkers[id] = t
		}
	}
	// Project-level agents override global agents with the same ID
	for _, t := range projectAgents {
		if t.ID == tmpl.ID {
			continue
		}
		mergedWorkers[t.ID] = t
	}
	if len(mergedWorkers) > 0 {
		b.WriteString("# Available Workers\n\n")
		b.WriteString("You can delegate tasks to the following workers:\n\n")
		for _, peer := range mergedWorkers {
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
	b.WriteString(prompt.EnvSection(workDir, exploreDir, false))
	b.WriteString("\n\n")
	if hasPermanentMemory {
		b.WriteString(permanentMemorySection)
		b.WriteString("\n\n")
	}
	b.WriteString(strings.ReplaceAll(l2EnforcedDirectivesPart1, "{{PLAN_DIR}}", planDir))
	if planDir != "" {
		b.WriteString(strings.ReplaceAll(l2EnforcedPlanSection, "{{PLAN_DIR}}", planDir))
	}
	b.WriteString(strings.ReplaceAll(l2EnforcedDirectivesPart2, "{{PLAN_DIR}}", planDir))
	b.WriteString(strings.ReplaceAll(l2EnforcedExplorationSection, "{{PLAN_DIR}}", planDir))
	b.WriteString(strings.ReplaceAll(l2EnforcedExplorationSection, "{{EXPLORE_DIR}}", exploreDir))
	b.WriteString(strings.ReplaceAll(l2EnforcedPostPlan, "{{PLAN_DIR}}", planDir))
	b.WriteString(strings.ReplaceAll(l2EnforcedToolAwareness, "{{PLAN_DIR}}", planDir))

	return b.String()
}

// ─── L3 System Prompt 两段式拼接 ─────────────────────────────────────────────

// permanentMemorySection is injected into L2/L3 prompts when permanent memory is enabled.
const permanentMemorySection = `
# Long-Term Memory
You have access to long-term memory through the RecallMemory and Remember tools. Long-term memory stores condensed summaries from past conversations.

Use RecallMemory when:
- The user refers to past conversations or previous sessions
- You need historical context about past decisions, preferences, or project history
- The user asks about something discussed before but you can't recall

Use Remember to save important information that may be useful in future conversations.
`

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

# 3. Follow the Plan — you MUST execute tasks one at a time and mark each:
1. Read the issue's checklist tasks. If readable → proceed to step 3.
2. If NO tasks exist → create your own plan:
   a. Use ManageIssue with action="create" to create a new issue:
      - Record a brief summary in the "description" parameter (this is a short description, not the full plan).
      - Record the detailed plan/design (Goal, Approach, Impact, Steps) in the "plan" parameter (this is the markdown document for your implementation plan).
      - Set the "author" parameter to your agent name (e.g. "Refactorer-Worker", "Go-Worker").
   b. Use ManageIssue with action="add_task" to define checklist items.
   c. Present ISSUE_ID → wait for approval → update status to "running" using ManageIssue with action="update" and status="running" and id="<id>".
3. Pick the FIRST uncompleted task from the list.
4. Execute it using the appropriate tool.
5. IMMEDIATELY after completion:
   a. Toggle the task completion status using ManageIssue with action="toggle_task" and task_id="<task_id>". This step is MANDATORY — you MUST NOT skip it.
   b. Call ManageIssue with action="add_comment" and set "author" to your agent name to summarize the changes made and tests run for this task.
6. Repeat from step 3 for the next uncompleted task.
7. When ALL tasks are done → update issue status to "done" using ManageIssue with action="update" and status="done" and id="<id>".

BAD: execute all work → update status to "done" at the end without per-task tracking.
GOOD: execute task1 → ManageIssue(action="toggle_task", task_id="1") → execute task2 → ManageIssue(action="toggle_task", task_id="2") ... → ManageIssue(action="update", status="done", id="<id>").
`

const l3EnforcedExplorationSection = `
# 4. Exploration Artifacts
When you perform exploration tasks (reading files, searching code, investigating issues), you SHOULD save a markdown artifact to {{EXPLORE_DIR}} if the exploration is complex or the findings are worth sharing with other agents.

## When to Save
- Complex investigations with many files or nuanced conclusions
- Investigations whose results may be reused by other agents in the same session
- Simple one-off lookups can skip saving

## Document Naming
Format: {{EXPLORE_DIR}}/<task-slug>_<agent-id>.md
Examples:
- {{EXPLORE_DIR}}/explore_auth_flow_backend-worker.md
- {{EXPLORE_DIR}}/investigate_race_condition_backend-worker.md

## Document Content
- Agent: your id/name/layer
- Created at: use current time when saving
- Updated at: use current time when updating
- Freshness window: same-day
- Task: the original or summarized task description
- Key Findings, Files Inspected, Reusable Context, Open Questions

## Reuse Rules
1. Before starting a new exploration, check {{EXPLORE_DIR}} for an existing artifact with the same task-slug and your agent-id.
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
Prefer the Read tool for reading files. Using Bash with cat wastes tokens and bypasses the Read tool's size limit. Use Bash for running commands, not for reading text files. If a file exceeds the Read limit, use Bash with head/tail to read portions.

# 7. Prefer Search Before Read
Before reading file contents, you MUST first use Grep or Glob to locate the relevant files and line numbers. Do NOT directly Read large files (>25,000 tokens). If a file exceeds the limit, use the Read tool's offset/limit pagination parameters to read in chunks, or use Grep to narrow the scope first.
`

// buildL3SystemPrompt 为 L3 Worker 构建两段式 System Prompt。
//
// Segment 1 (用户定义区): 用户的业务 Role + System Prompt
// Segment 2 (框架强制区): 不可篡改的底层契约
func buildL3SystemPrompt(tmpl AgentTemplate, groups map[string]prompt.GroupFile, planDir, workDir, exploreDir string, hasPermanentMemory bool) string {
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
	fmt.Fprintf(&b, "Your working directory is `%s`. All project files, source code, and configurations reside under this directory. Use this as the base for all file operations.\n", workDir)
	if tmpl.Group != "" {
		if gf, ok := groups[tmpl.Group]; ok && len(gf.Frontmatter.Workspaces) > 0 {
			b.WriteString("\nTeam workspaces:\n")
			for _, ws := range gf.Frontmatter.Workspaces {
				mark := " "
				if ws.Path == workDir {
					mark = "▶"
				}
				fmt.Fprintf(&b, "- **%s**: %s %s\n", ws.Name, ws.Path, mark)
			}
		}
	}
	b.WriteString("\n")

	// ── Segment 2: 框架强制区 ──────────────────────────────
	b.WriteString(prompt.EnvSection(workDir, exploreDir, false))
	b.WriteString("\n\n")
	if hasPermanentMemory {
		b.WriteString(permanentMemorySection)
		b.WriteString("\n\n")
	}
	b.WriteString(strings.ReplaceAll(l3EnforcedDirectives, "{{PLAN_DIR}}", planDir))
	b.WriteString(strings.ReplaceAll(l3EnforcedExplorationSection, "{{PLAN_DIR}}", planDir))
	b.WriteString(strings.ReplaceAll(l3EnforcedExplorationSection, "{{EXPLORE_DIR}}", exploreDir))
	b.WriteString(strings.ReplaceAll(l3EnforcedPostPlan, "{{PLAN_DIR}}", planDir))
	b.WriteString(strings.ReplaceAll(l3EnforcedToolAwareness, "{{PLAN_DIR}}", planDir))

	return b.String()
	}
