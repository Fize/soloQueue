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

	// Vision indicates the model supports multimodal image_url content.
	Vision bool
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
	hasPermanentMemory := toolsCfg.MemoryEngine != nil
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
		def.ProviderID = info.ProviderID
		def.ContextWindow = info.ContextWindow
		def.Temperature = info.Temperature
		def.MaxTokens = info.MaxTokens
		def.ThinkingEnabled = info.ThinkingEnabled
		def.ReasoningEffort = info.ReasoningEffort
		def.Vision = info.Vision
	}

	// 2. 构建内置 tools
	var allTools []tools.Tool
	if !strings.HasPrefix(tmpl.ID, "sim-") {
		agentToolsCfg := toolsCfg
		agentToolsCfg.WorkDir = effectiveWorkDir
		allTools = tools.Build(agentToolsCfg)

		// Filter out SendFile and schedule_task for L3 workers
		if !tmpl.IsLeader {
			var filtered []tools.Tool
			for _, t := range allTools {
				if t.Name() != "SendFile" && t.Name() != "schedule_task" {
					filtered = append(filtered, t)
				}
			}
			allTools = filtered
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

	// 2b. L2 领导者：注入同组 L3 Worker + project-level agents 的 delegate 工具
	if tmpl.IsLeader {
		// Inject the generic delegate_agent tool for dynamic L3 delegation
		dat := tools.NewDelegateAgentTool(f.log, func(ctx context.Context, name, systemPrompt, modelID, task, workDir string, baseAgentName string, skillDir string) (iface.Locatable, error) {
			var childTmpl AgentTemplate
			var ok bool

			// 1. Try loading matching agent template from the skill's agents directory
			if skillDir != "" {
				childTmpl, ok = LoadSkillAgentTemplate(skillDir, name)
				if !ok && baseAgentName != "" {
					childTmpl, ok = LoadSkillAgentTemplate(skillDir, baseAgentName)
				}
			}

			// 2. Fallback to global templates registry
			if !ok && baseAgentName != "" {
				if t, ok2 := f.templates[strings.ToLower(baseAgentName)]; ok2 {
					childTmpl = t
					ok = true
				}
			}
			if !ok {
				if t, ok2 := f.templates[strings.ToLower(name)]; ok2 {
					childTmpl = t
					ok = true
				}
			}

			// Configure template fields
			childTmpl.ID = strings.ToLower(name)
			childTmpl.Name = name
			childTmpl.IsLeader = false // All dynamically delegated agents are L3 workers

			if ok {
				// Combine base agent's system prompt with skill instructions / custom prompt
				if systemPrompt != "" {
					if childTmpl.SystemPrompt != "" {
						childTmpl.SystemPrompt = childTmpl.SystemPrompt + "\n\n# Skill/Custom execution logic:\n" + systemPrompt
					} else {
						childTmpl.SystemPrompt = systemPrompt
					}
				}
			} else {
				childTmpl.Description = "Dynamic skill agent"
				childTmpl.SystemPrompt = systemPrompt
			}

			if modelID != "" {
				childTmpl.ModelID = modelID
			}

			child, _, err := f.Create(ctx, childTmpl, workDir)
			if err != nil {
				return nil, err
			}
			return &LocatableAdapter{Agent: child}, nil
		})
		dat.SkillInstructionsLook = func(skillID string) (string, string, string, bool) {
			if s, ok := mergedSkillReg.GetSkill(skillID); ok {
				return s.Instructions, s.Agent, s.Dir, true
			}
			return "", "", "", false
		}
		allTools = append(allTools, dat)

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

		// 2c. L2 领导者：注入横向协作工具（list_peer_teams + request_team_help）
		// 仅当存在其他团队时才注入，避免单团队场景下给 LLM 无意义的工具。
		peerCatalog := f.peerTeamsCatalog(tmpl)
		if len(peerCatalog) > 0 {
			listTool := tools.NewListPeerTeamsTool(peerCatalog, tmpl.ID)
			allTools = append(allTools, listTool)

			// locateOrSpawn: 复用 LocateIdle 找空闲 peer leader，找不到则 spawn 新实例。
			locateOrSpawn := func(ctx context.Context, teamName string) (iface.Locatable, bool, error) {
				if loc, ok := f.registry.LocateIdle(teamName); ok {
					return loc, false, nil
				}
				// 找不到空闲实例 → spawn 新的 peer leader
				peerTmpl, ok := f.findLeaderTemplate(teamName)
				if !ok {
					return nil, false, fmt.Errorf("peer leader %q not found", teamName)
				}
				child, _, err := f.Create(ctx, peerTmpl, effectiveWorkDir)
				if err != nil {
					return nil, false, fmt.Errorf("spawn peer leader %q: %w", teamName, err)
				}
				// 新 spawn 的 peer leader 需要自己的 supervisor 来管理它的 L3 children
				peerSv := NewSupervisor(child, f, f.log)
				peerSv.WireSpawnFns(f.templatesSlice())
				peerSv.SetGroup(peerTmpl.Group)
				return NewSelfReapableAdapter(child, peerSv), true, nil
			}
			// reap: 对于 spawn 的新实例，OnDelegationDone 已经由 SelfReapableAdapter
			// 处理；这里不需要额外 reap（DoneNotifier 路径已经覆盖）。
			helpTool := tools.NewRequestTeamHelpTool(tmpl.ID, locateOrSpawn, nil, 25*time.Minute)
			if f.log != nil {
				helpTool.SetLogger(f.log)
			}
			allTools = append(allTools, helpTool)
		}
	}

	var skillList []*skill.Skill
	if !strings.HasPrefix(tmpl.ID, "sim-") && len(tmpl.SkillIDs) > 0 {
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
				var basePrompt string
				if s.Agent != "" {
					// 1. Try loading base agent template from the skill's own agents/ directory
					if baseTmpl, ok := LoadSkillAgentTemplate(s.Dir, s.Agent); ok {
						basePrompt = baseTmpl.SystemPrompt
					} else {
						// 2. Fallback to global templates registry
						if baseTmpl, ok := f.templates[strings.ToLower(s.Agent)]; ok {
							basePrompt = baseTmpl.SystemPrompt
						}
					}
				}

				finalSystemPrompt := content
				if basePrompt != "" {
					finalSystemPrompt = basePrompt + "\n\n# Skill Execution Instructions\n" + content
				}

				forkDef := Definition{
					ID:           fmt.Sprintf("skill-fork-%s", s.ID),
					ModelID:      def.ModelID,
					SystemPrompt: finalSystemPrompt,
				}

				// Build tools and filter out SendFile/schedule_task because this is an L3 agent
				forkTools := tools.Build(toolsCfg)
				var filtered []tools.Tool
				for _, t := range forkTools {
					if t.Name() != "SendFile" && t.Name() != "schedule_task" {
						filtered = append(filtered, t)
					}
				}
				forkTools = filtered

				if len(s.AllowedTools) > 0 {
					forkTools = skill.FilterTools(forkTools, s.AllowedTools)
				}
				child := NewAgent(forkDef, llm, f.log,
					WithTools(forkTools...),
					WithParallelTools(true),
					WithAgentWorkDir(effectiveWorkDir),
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
	if !strings.HasPrefix(tmpl.ID, "sim-") && f.mcpManager != nil && len(tmpl.MCPServers) > 0 {
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
		}
		templates = append(templates, tmpl)
	}

	return templates, nil
}

// LoadSkillAgentTemplate attempts to load an agent template from a skill's agents/ subdirectory.
func LoadSkillAgentTemplate(skillDir string, agentName string) (AgentTemplate, bool) {
	if skillDir == "" || agentName == "" {
		return AgentTemplate{}, false
	}
	agentsDir := filepath.Join(skillDir, "agents")
	if fi, err := os.Stat(agentsDir); err == nil && fi.IsDir() {
		if tmpls, err := LoadAgentTemplates(agentsDir); err == nil {
			for _, t := range tmpls {
				if strings.EqualFold(t.ID, agentName) || strings.EqualFold(t.Name, agentName) {
					return t, true
				}
			}
		}
	}
	return AgentTemplate{}, false
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

// peerTeamsCatalog builds the read-only peer team catalog for an L2 leader.
// Excludes the caller's own team. Returns teams that have a leader (is_leader)
// template and belong to a different group.
func (f *DefaultFactory) peerTeamsCatalog(selfTmpl AgentTemplate) []tools.PeerTeamInfo {
	var catalog []tools.PeerTeamInfo
	seen := make(map[string]bool) // dedup by leader ID

	for _, t := range f.templates {
		if !t.IsLeader || t.ID == selfTmpl.ID {
			continue
		}
		if seen[t.ID] {
			continue
		}
		seen[t.ID] = true

		// Count workers in this leader's group
		workerCount := 0
		for _, w := range f.templates {
			if !w.IsLeader && w.Group == t.Group && w.Group != "" {
				workerCount++
			}
		}

		desc := t.Description
		if desc == "" {
			desc = "no description"
		}

		catalog = append(catalog, tools.PeerTeamInfo{
			Name:              t.ID,
			Group:             t.Group,
			LeaderDescription: desc,
			WorkerCount:       workerCount,
		})
	}
	return catalog
}

// findLeaderTemplate finds a leader template by ID (case-insensitive).
func (f *DefaultFactory) findLeaderTemplate(id string) (AgentTemplate, bool) {
	for _, t := range f.templates {
		if t.IsLeader && strings.EqualFold(t.ID, id) {
			return t, true
		}
	}
	return AgentTemplate{}, false
}

// templatesSlice converts the internal template map to a slice for APIs
// that accept []AgentTemplate (e.g., Supervisor.WireSpawnFns).
func (f *DefaultFactory) templatesSlice() []AgentTemplate {
	out := make([]AgentTemplate, 0, len(f.templates))
	for _, t := range f.templates {
		out = append(out, t)
	}
	return out
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
# 3. MANDATORY Plan Before Execution (Plan & Todo File Tracking)
This rule establishes a **MANDATORY Plan Before Execution** policy for all non-trivial implementation tasks.
**Exploratory tasks are EXEMPT.** Reading files, searching code, investigating issues, or answering questions do NOT require a plan. Execute or delegate them without a plan.

**For implementation tasks:**
1. Assess complexity:
   - **Simple task** (single file, narrow change) → delegate directly to L3. L3 will self-plan if needed.
   - **Complex task** (multi-step, multi-file, multiple Workers) → MUST create a plan.
2. Create a markdown plan file under the project-specific path: ` + "`" + `{{PLAN_DIR}}/YYYY-MM-DD/<slug>.md` + "`" + ` (where YYYY-MM-DD is today's date). If not inside a project workspace, use the home directory fallback ` + "`" + `~/.soloqueue/plan/YYYY-MM-DD/<slug>.md` + "`" + `.
3. Structuring the file:
   - Must start with an H1 header ('# Title') containing the name of the plan.
   - Must contain a '# Tasks' header. Under this header, list all checklist tasks using standard checkboxes ('- [ ]', '- [/]', '- [x]').
   - Use indentations to denote sub-tasks. You can add notes or dependency labels in task descriptions.
4. **Approval decision — choose ONE:**
   - **Auto-approve (default for most tasks):** If the plan is straightforward and low-risk → proceed directly to execution without waiting for L1.
   - **Escalate to L1 (only for significant trade-offs):** If the plan involves irreversible changes or trade-offs → return a structured response to L1:
     ` + "`" + `PLAN_REVIEW_REQUIRED
Path: <path_to_plan_file>
Summary: <one-line summary of the plan>
Trade-offs: <what requires human decision>` + "`" + `
     Wait for L1 to re-delegate with "Plan <path> approved" before executing.

**Execution loop — you MUST follow these steps EXACTLY in order, no skipping:**

5. Read the tasks and their statuses directly from the plan file.
6. Identify all tasks whose blockers/parent tasks are completed.
7. CRITICAL — Delegate ALL identified tasks IN PARALLEL in a SINGLE turn.
   Call multiple delegate_* tools in one response. Set the ` + "`" + `work_dir` + "`" + ` parameter in each tool call so the worker runs in the same workspace. Pass the plan file path to the workers in the task prompt.
   Parallel execution of independent items is MANDATORY, not optional.
8. Wait for all parallel delegations in this batch to return results.
9. For each completed task, update the checkbox in the plan file to ` + "`" + `- [x]` + "`" + ` using standard file editing tools.
10. Repeat from step 5. Find the next batch of checklist tasks whose dependencies are now satisfied. Continue the loop until no remaining tasks.
11. When ALL tasks in the checklist are marked completed, your job is complete.

**When L3 submits a plan for review:**
- Approve autonomously if straightforward → reply 'Plan <path> approved' and proceed.
- Escalate to L1 only for significant trade-offs using the PLAN_REVIEW_REQUIRED format above.

**When L1 re-delegates with "Plan <path> approved":**
- Read the plan file at '<path>' to retrieve the tasks.
- Proceed directly to the execution loop (step 5 onwards).

BAD: delegate task1 → wait → mark done → delegate task2 → wait ...
BAD: delegate task1+task2+task3 in parallel → wait → update zero tasks in the file.
GOOD: delegate task1+task2+task3 (all independent) → wait all → update plan file marking task1, task2, task3 as done → delegate next batch.
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

	// ── Identity ──────────────────────────────────────────
	b.WriteString("# Identity\n\n")
	fmt.Fprintf(&b, "You are %s.\n\n", tmpl.Name)
	b.WriteString("Your responses must be extremely concise and direct. Answer exactly what is asked without any unnecessary fluff, conversational filler, or pleasantries.\n\n")

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

	// 2b-peer. Peer Teams (cross-team help)
	// List other team leaders so the L2 knows who it can ask for help.
	peerLeaderCount := 0
	for _, t := range templates {
		if t.IsLeader && t.ID != tmpl.ID {
			peerLeaderCount++
		}
	}
	if peerLeaderCount > 0 {
		b.WriteString("# Peer Teams (Cross-Team Help)\n\n")
		b.WriteString("You can request help from peer team leaders when your team lacks a capability.\n\n")
		b.WriteString("## When to use peer help\n")
		b.WriteString("- Call `list_peer_teams` first to see which teams exist and what they can do.\n")
		b.WriteString("- Use `request_team_help(team_name, task, context)` to delegate a sub-task to a peer team.\n")
		b.WriteString("- Only request help when your team genuinely cannot handle the sub-task.\n")
		b.WriteString("- Provide clear, self-contained task descriptions and sufficient context.\n\n")
		b.WriteString("## Rules\n")
		b.WriteString("- Do NOT form delegation loops. The system will reject cyclic requests.\n")
		b.WriteString("- If a peer team is unavailable or refuses, fall back to handling it yourself or report to the user.\n")
		b.WriteString("- Peer help is for *sub-tasks* within your current task, not for replacing your own work.\n")
		b.WriteString("- Delegation depth is limited to 2 hops. If you need deeper collaboration, the task is too complex for lateral help — escalate.\n\n")
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
	b.WriteString(prompt.EnvSection(workDir, exploreDir, false, false))
	b.WriteString("\n\n")
	if hasPermanentMemory {
		b.WriteString(memoryEngineSection)
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

// memoryEngineSection is injected into L2/L3 prompts when the memory engine is enabled.
const memoryEngineSection = `
# Long-Term Memory Usage — MANDATORY WORKFLOW

You have access to a memory engine with hybrid search (BM25 keyword + Knowledge Graph).
Memory usage is NOT optional — it is part of your core workflow. Follow this three-phase
process for EVERY non-trivial task.

## Phase 1 — RECALL: Before ANY analysis or action
Before you start planning, analyzing, or delegating, you MUST:

1. Call RecallMemory with the task description as the query to find relevant past experiences,
   previous analyses, decisions, and results.

2. If recalled results reference specific entities (stocks, projects, people, strategies),
   call RecallEntity on each key entity to explore the knowledge graph and find all
   related memories.

3. If the task involves the same topic at a different time (e.g., "analyze stock X now"
   when you previously analyzed it in Q1), call MemoryTimeline to see how understanding,
   decisions, and context evolved over time.

4. Only after reviewing the historical context, proceed with your analysis. Explicitly
   reference and compare past findings with the current situation.

CRITICAL: Do NOT skip this phase. The user expects you to leverage historical experience.
A blind analysis without historical context is incomplete and will be rejected.

## Phase 2 — ANALYZE: Compare past and present
When performing analysis:

- Explicitly reference what was known before vs. what is new now.
- Note any contradictions or changes since the last analysis.
- Use ConnectEntities(entity1, entity2) to discover hidden relationships between
  different concepts, projects, or investment targets.
- Consult the knowledge graph when you need to understand how entities relate.

## Phase 3 — REMEMBER: After EVERY task completion
After completing your task, you MUST call Remember to save:

1. Your key findings and conclusions.
2. All entities involved (stocks, projects, people, strategies, tools) with their
   types and relationships in the "entities" field. This builds the knowledge graph
   for future RecallEntity and ConnectEntities queries.
3. Include an event_time matching the data or decision date so future time-travel
   queries (MemoryTimeline) work correctly.

Entity types and relationship types are open — you define them based on your domain.
Examples: stock, project, strategy, person, metric, decision, risk, opportunity.

## Maintenance
Call ConsolidateMemories periodically to clean up stale edges and decayed memories.

## Tool Reference
- **RecallMemory(query, limit=10)**: Hybrid search by text query. ALWAYS call first.
- **RecallEntity(entity, max_hops=2)**: Explore KG from a specific entity.
- **ConnectEntities(source, target)**: Find paths between two entities.
- **MemoryTimeline(from, to, limit=50)**: Chronological review over a date range.
- **Remember(content, entities[], event_time)**: Save findings to long-term memory.
- **KGIndex(entities[])**: Bulk-index entities and relationships into the KG.
- **ConsolidateMemories()**: Run maintenance (edge decay, stale cleanup).
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
1. Locate the plan file path. If L2 provided a plan path, read that file. If no plan file path was provided, check the workspace for an existing plan or create your own:
   - Create a markdown plan file under ` + "`" + `{{PLAN_DIR}}/YYYY-MM-DD/<slug>.md` + "`" + ` (use fallback ` + "`" + `~/.soloqueue/plan/YYYY-MM-DD/<slug>.md` + "`" + ` if no workspace is active).
    - Write an H1 header ('# Title') and a '# Tasks' section containing checklist items ('- [ ]', '- [/]', '- [x]').
    - If creating your own plan, present the path to L2, wait for approval, and then proceed.
2. Pick the FIRST uncompleted task from the checklist in the plan file.
3. Mark it in-progress by replacing '- [ ]' with '- [/' + ']' in the file.
4. Execute it using the appropriate tool.
5. IMMEDIATELY after completion:
   - Replace the task's checkbox with '- [x]' in the file. This step is MANDATORY — you MUST NOT skip it.
6. Repeat from step 2 for the next uncompleted task.
7. When ALL tasks in the checklist are marked completed, report the completion to L2.

BAD: execute all work → report done at the end without updating the plan file per task.
GOOD: execute task1 → mark done in file → execute task2 → mark done in file ... → report completion.
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

	// ── Identity ──────────────────────────────────────────
	b.WriteString("# Identity\n\n")
	fmt.Fprintf(&b, "You are %s.\n\n", tmpl.Name)
	b.WriteString("Your responses must be extremely concise and direct. Answer exactly what is asked without any unnecessary fluff, conversational filler, or pleasantries.\n\n")

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
	b.WriteString(prompt.EnvSection(workDir, exploreDir, false, false))
	b.WriteString("\n\n")
	if hasPermanentMemory {
		b.WriteString(memoryEngineSection)
		b.WriteString("\n\n")
	}
	b.WriteString(strings.ReplaceAll(l3EnforcedDirectives, "{{PLAN_DIR}}", planDir))
	b.WriteString(strings.ReplaceAll(l3EnforcedExplorationSection, "{{PLAN_DIR}}", planDir))
	b.WriteString(strings.ReplaceAll(l3EnforcedExplorationSection, "{{EXPLORE_DIR}}", exploreDir))
	b.WriteString(strings.ReplaceAll(l3EnforcedPostPlan, "{{PLAN_DIR}}", planDir))
	b.WriteString(strings.ReplaceAll(l3EnforcedToolAwareness, "{{PLAN_DIR}}", planDir))

	return b.String()
	}
