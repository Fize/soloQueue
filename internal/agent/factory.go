package agent

import (
	"context"
	"fmt"

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
	ModelID      string   // 模型 ID
	Reasoning    bool     // 是否启用推理
	Skills       []string // 需要加载的 Skill ID
	SubAgents    []string // 子 Agent 名称列表（L2 专用）
	IsLeader     bool     // 是否为 L2 领导者
	Ephemeral    bool     // 是否为阅后即焚的 L3
}

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
	registry *Registry
	llm      LLMClient
	toolsCfg tools.Config
	skillDir string
	log      *logger.Logger
}

// NewDefaultFactory 创建 DefaultFactory
func NewDefaultFactory(
	registry *Registry,
	llm LLMClient,
	toolsCfg tools.Config,
	skillDir string,
	log *logger.Logger,
) *DefaultFactory {
	return &DefaultFactory{
		registry: registry,
		llm:      llm,
		toolsCfg: toolsCfg,
		skillDir: skillDir,
		log:      log,
	}
}

func (f *DefaultFactory) Registry() *Registry {
	return f.registry
}

// Create 根据 tmpl 创建并启动一个 Agent 实例
//
// 流程：
//  1. 构建 Definition（KindCustom, systemPrompt from tmpl）
//  2. Build(toolsCfg) → 内置 tools
//  3. 如果 tmpl.SubAgents 非空，为每个 SubAgent 创建 DelegateTool（同步模式）
//  4. 加载 skills（LoadSkillsFromDir）
//  5. 创建 Agent（WithTools, WithSkills, 可选 WithEphemeral/WithPriorityMailbox）
//  6. 创建 ContextWindow，push system prompt + skill catalog
//  7. Register 到 registry
//  8. Start Agent
//  9. 返回 (agent, cw, nil)
func (f *DefaultFactory) Create(ctx context.Context, tmpl AgentTemplate) (*Agent, *ctxwin.ContextWindow, error) {
	// 1. 构建 Definition
	def := Definition{
		ID:              tmpl.ID,
		Name:            tmpl.Name,
		Role:            RoleUser,
		Kind:            KindCustom,
		ModelID:         tmpl.ModelID,
		SystemPrompt:    tmpl.SystemPrompt,
		ReasoningEffort: "", // TODO: 从 tmpl 推导
	}

	// 2. 构建内置 tools
	allTools := tools.Build(f.toolsCfg)

	// 3. 为每个 SubAgent 创建 DelegateTool（同步模式）
	// 注意：SubAgent 对应的 L3 实例由 Supervisor.SpawnFn 在运行时创建
	// 这里只注册 tool 声明，不启动 Agent
	// SpawnFn 将由 Supervisor 注入（Create 不直接注入）
	for _, subName := range tmpl.SubAgents {
		dt := &tools.DelegateTool{
			LeaderID: subName,
			Desc:     fmt.Sprintf("sub-agent '%s'", subName),
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
	cw := ctxwin.NewContextWindow(128000, 2000, ctxwin.NewTokenizer())
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
	if err := a.Start(context.Background()); err != nil {
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
			Reasoning:    fm.Reasoning,
			Skills:       fm.Skills,
			SubAgents:    fm.SubAgents,
			IsLeader:     fm.IsLeader,
		}
		templates = append(templates, tmpl)
	}

	return templates, nil
}
