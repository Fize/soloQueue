package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agenttools/mcp"
	"github.com/xiaobaitu/soloqueue/internal/agenttools/skill"
	"github.com/xiaobaitu/soloqueue/internal/agenttools/tools"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/memory/engine"
	"github.com/xiaobaitu/soloqueue/internal/prompt"
	"github.com/xiaobaitu/soloqueue/internal/team/store"
)

// AgentTemplate describes an agent configuration parsed from YAML frontmatter + markdown body.
type AgentTemplate struct {
	ID           string
	Name         string
	Description  string
	SystemPrompt string
	ModelID      string
	IsLeader     bool
	Group        string
	Permission   bool
	MCPServers   []string
	SkillIDs     []string
	// Channels maps channel_type → instance_id.
	Channels map[string]string
	// NotifyChannel is the channel_type for cron notifications. Defaults to first channel.
	NotifyChannel string
}

// ModelInfo holds resolved model configuration.
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
	// ThinkingType is the value for thinking.type sent to the LLM API.
	// "enabled" (default, DeepSeek) or "adaptive" (MiniMax M3, some OpenAI-compatible).
	ThinkingType string

	// Vision indicates the model supports multimodal image_url content.
	Vision bool
}

// ModelResolver resolves a model ID to ModelInfo. Implemented by config layer.
type ModelResolver func(modelID string) (ModelInfo, error)

// AgentFactory creates and starts Agent instances from templates.
type AgentFactory interface {
	// Create creates and starts an Agent instance.
	Create(ctx context.Context, tmpl AgentTemplate, workDir string) (*Agent, *ctxwin.ContextWindow, error)

	CreateWithOptions(ctx context.Context, tmpl AgentTemplate, workDir string, opts CreateOptions) (*Agent, *ctxwin.ContextWindow, error)

	Registry() *Registry

	// ResolveTemplate checks the DB teamstore first, then the in-memory cache.
	ResolveTemplate(ctx context.Context, id string) (AgentTemplate, bool)

	RebuildLeaderPrompt(tmpl AgentTemplate, workDir string) (string, error)
}

// CreateOptions provides optional overrides for agent creation.
type CreateOptions struct {
	ExtraSystemPrompt string
	ExtraTools        []tools.Tool
	MemoryPolicy      MemoryPolicy
}

type MemoryPolicy uint8

const (
	MemoryDisabled MemoryPolicy = iota
	MemoryL2Group
)

// ─── DefaultFactory ────────────────────────────────────────────────────────

// DefaultFactory creates agents, registers them in Registry, and starts them.
type DefaultFactory struct {
	mu sync.RWMutex

	registry       *Registry
	llm            LLMClient
	toolsCfg       tools.Config
	defaultModelID string // Default value used when AgentTemplate.ModelID is empty
	skillRegistry  *skill.SkillRegistry
	skillStats     skill.InvocationStats // Skill invocation telemetry (optional)
	workDir        string                // ~/.soloqueue, used to compute planDir based on team
	log            *logger.Logger
	resolveModel   ModelResolver               // nil = skip model validation (tests)
	templates      map[string]AgentTemplate    // Full templates indexed by ID, used by buildL2SystemPrompt to find sub-agent descriptions
	groups         map[string]prompt.GroupFile // Group information, used to inject team context into L2 prompts
	bypassConfirm  bool                        // global --bypass: skip all confirmations
	mcpManager     *mcp.Manager                // MCP server manager (nil = MCP disabled)
	exploreDir     string                      // exploration artifact directory (platform-appropriate)
	teamstore      *store.Store                // DB-backed team/agent store (nil = disabled)
	memoryEngine   *engine.Engine
}

// NewDefaultFactory creates a DefaultFactory
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

// ApplyOption applies a FactoryOption to an already-constructed factory; used
// when the option's value is only known after construction. No-op on nil.
func (f *DefaultFactory) ApplyOption(opt FactoryOption) {
	if opt != nil {
		opt(f)
	}
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

func (f *DefaultFactory) ReplaceTeamCatalog(templates []AgentTemplate, groups map[string]prompt.GroupFile) {
	templateMap := make(map[string]AgentTemplate, len(templates))
	for _, tmpl := range templates {
		templateMap[tmpl.ID] = tmpl
	}
	groupMap := make(map[string]prompt.GroupFile, len(groups))
	for name, group := range groups {
		groupMap[name] = group
	}

	f.mu.Lock()
	f.templates = templateMap
	f.groups = groupMap
	f.mu.Unlock()
}

// RebuildLeaderPrompt rebuilds the L2 leader system prompt from a template,
// using the factory's cached templates and groups. Returns the new prompt
// that the caller should set on the agent and context window.
func (f *DefaultFactory) RebuildLeaderPrompt(tmpl AgentTemplate, workDir string) (string, error) {
	f.mu.RLock()
	templates := make(map[string]AgentTemplate, len(f.templates))
	for id, t := range f.templates {
		templates[id] = t
	}
	groups := make(map[string]prompt.GroupFile, len(f.groups))
	for name, g := range f.groups {
		groups[name] = g
	}
	toolsCfg := f.toolsCfg
	exploreDir := f.exploreDir
	f.mu.RUnlock()

	// Compute planDir and effective workDir (same logic as CreateWithOptions)
	planDir := toolsCfg.PlanDir
	if f.workDir != "" && tmpl.Group != "" {
		teamPlanDir := filepath.Join(f.workDir, "plan", tmpl.Group)
		if err := os.MkdirAll(teamPlanDir, 0o755); err == nil {
			planDir = teamPlanDir
		}
	}
	effectiveWorkDir := workDir
	if effectiveWorkDir == "" || effectiveWorkDir == f.workDir {
		if tmpl.Group != "" && f.workDir != "" {
			effectiveWorkDir = filepath.Join(f.workDir, "workspace", tmpl.Group)
		} else if effectiveWorkDir == "" {
			effectiveWorkDir = f.workDir
		}
	}
	if exploreDir == "" && effectiveWorkDir != "" {
		exploreDir = prompt.ExploreDir(effectiveWorkDir)
	}

	// Load project-level resources
	var projRes projectResources
	if effectiveWorkDir != "" && effectiveWorkDir != f.workDir {
		projRes = f.loadProjectResources(effectiveWorkDir)
	}

	memoryAccess, err := f.memoryAccessFor(context.Background(), tmpl, MemoryL2Group)
	if err != nil {
		return "", err
	}
	hasPermanentMemory := memoryAccess != nil
	newPrompt := buildL2SystemPrompt(tmpl, templates, groups, planDir, effectiveWorkDir, exploreDir, projRes.agents, hasPermanentMemory)
	return newPrompt, nil
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

// WithSkillInvocationStats wires skill telemetry into every SkillTool created
// by this factory. Optional; unset ⇒ tools skip recording.
func WithSkillInvocationStats(stats skill.InvocationStats) FactoryOption {
	return func(f *DefaultFactory) {
		f.skillStats = stats
	}
}

// WithExploreDir sets the exploration artifacts directory (platform-appropriate).
func WithExploreDir(exploreDir string) FactoryOption {
	return func(f *DefaultFactory) {
		f.exploreDir = exploreDir
	}
}

// WithTeamStore sets the DB-backed team/agent store for the factory.
func WithTeamStore(store *store.Store) FactoryOption {
	return func(f *DefaultFactory) {
		f.teamstore = store
	}
}

func WithMemoryEngine(memoryEngine *engine.Engine) FactoryOption {
	return func(f *DefaultFactory) { f.memoryEngine = memoryEngine }
}

func (f *DefaultFactory) UpdateMemoryEngine(memoryEngine *engine.Engine) {
	f.mu.Lock()
	f.memoryEngine = memoryEngine
	f.mu.Unlock()
}

func (f *DefaultFactory) Registry() *Registry {
	return f.registry
}

func (f *DefaultFactory) memoryAccessFor(ctx context.Context, tmpl AgentTemplate, policy MemoryPolicy) (engine.Access, error) {
	f.mu.RLock()
	memoryEngine := f.memoryEngine
	f.mu.RUnlock()
	return f.memoryAccessForEngine(ctx, tmpl, policy, memoryEngine)
}

func (f *DefaultFactory) memoryAccessForEngine(ctx context.Context, tmpl AgentTemplate, policy MemoryPolicy, memoryEngine *engine.Engine) (engine.Access, error) {
	if policy == MemoryDisabled || memoryEngine == nil {
		return nil, nil
	}
	if policy != MemoryL2Group || !tmpl.IsLeader || strings.TrimSpace(tmpl.Group) == "" || f.teamstore == nil {
		return nil, engine.ErrMemoryAccessDenied
	}
	ownerID, err := f.teamstore.EnsureMemoryOwnerID(ctx, tmpl.Group)
	if err != nil {
		if f.log != nil {
			f.log.WarnContext(ctx, logger.CatConfig, "memory owner resolution failed",
				"operation", "bind_l2_group", "error_code", "memory_unavailable")
		}
		return nil, fmt.Errorf("memory_unavailable")
	}
	return memoryEngine.BindL2Group(ownerID)
}

// ResolveTemplate returns the current DB-backed agent template by ID.
// Workflow execution uses this instead of constructing an empty synthetic
// agent, so the selected agent's prompt, model, permissions, skills, and MCP
// servers are preserved.
func (f *DefaultFactory) ResolveTemplate(ctx context.Context, id string) (AgentTemplate, bool) {
	if f.teamstore != nil {
		agents, err := f.teamstore.ListAgents(ctx)
		if err == nil {
			for i := range agents {
				if agents[i].ID != id {
					continue
				}
				t := agents[i].ToAgentTemplate()
				return AgentTemplate{
					ID:            t.ID,
					Name:          t.Name,
					Description:   t.Description,
					SystemPrompt:  t.SystemPrompt,
					ModelID:       t.ModelID,
					IsLeader:      t.IsLeader,
					Group:         t.Group,
					Permission:    t.Permission,
					MCPServers:    t.MCPServers,
					SkillIDs:      t.SkillIDs,
					Channels:      t.Channels,
					NotifyChannel: t.NotifyChannel,
				}, true
			}
		}
	}

	f.mu.RLock()
	defer f.mu.RUnlock()
	t, ok := f.templates[id]
	return t, ok
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

// Create creates and starts an Agent instance based on tmpl
//
// Flow:
//  1. Build the final SystemPrompt (L2 uses three-part concatenation, L3 uses body/description directly)
//  2. Build(toolsCfg) → built-in tools
//  3. Load skills (LoadSkillsFromDir)
//  4. Create Agent (WithTools, WithSkills, WithParallelTools, WithAgentWorkDir)
//  5. Create ContextWindow, push system prompt + skill catalog
//  6. Register to registry
//  7. Start Agent
//  8. Return (agent, cw, nil)
//
// workDir is the project working directory for this agent.
// If empty, the factory's global workDir (~/.soloqueue) is used.
func (f *DefaultFactory) Create(ctx context.Context, tmpl AgentTemplate, workDir string) (*Agent, *ctxwin.ContextWindow, error) {
	opts := CreateOptions{MemoryPolicy: MemoryDisabled}
	if tmpl.IsLeader && tmpl.Group != "" && !strings.HasPrefix(tmpl.ID, "sim-") {
		opts.MemoryPolicy = MemoryL2Group
	}
	return f.CreateWithOptions(ctx, tmpl, workDir, opts)
}

// CreateWithOptions creates an agent with additional configuration.
// ExtraSystemPrompt is appended to the system prompt.
// ExtraTools are added to the agent's tool set after built-in tools.
func (f *DefaultFactory) CreateWithOptions(ctx context.Context, tmpl AgentTemplate, workDir string, opts CreateOptions) (*Agent, *ctxwin.ContextWindow, error) {
	var a *Agent
	// Snapshot hot-reloadable fields under read lock for a consistent agent creation.
	f.mu.RLock()
	llm := f.llm
	toolsCfg := f.toolsCfg
	defaultModelID := f.defaultModelID
	templates := make(map[string]AgentTemplate, len(f.templates))
	for id, template := range f.templates {
		templates[id] = template
	}
	groups := make(map[string]prompt.GroupFile, len(f.groups))
	for name, group := range f.groups {
		groups[name] = group
	}
	memoryEngine := f.memoryEngine
	f.mu.RUnlock()

	memoryAccess, err := f.memoryAccessForEngine(ctx, tmpl, opts.MemoryPolicy, memoryEngine)
	if err != nil {
		return nil, nil, fmt.Errorf("agent %q: configure memory: %w", tmpl.ID, err)
	}

	// 1. Construct the final SystemPrompt
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
	hasPermanentMemory := memoryAccess != nil
	if tmpl.IsLeader {
		finalPrompt = buildL2SystemPrompt(tmpl, templates, groups, planDir, effectiveWorkDir, exploreDir, projRes.agents, hasPermanentMemory)
		if tmpl.Group != "" && toolsCfg.CronStore != nil && toolsCfg.CronScheduler != nil && !iface.IsCronExecution(ctx) {
			finalPrompt += "\n# Cron Jobs\n\n" +
				"You may manage cron jobs only for your own team. Use create_cron_job for new jobs, " +
				"list_cron_jobs to find IDs, update_cron_job to change or pause jobs, and delete_cron_job to remove jobs. " +
				"A new job always requires a user-facing title, a task_type (general, engineering, or research), schedule, and instruction.\n"
		}
	} else {
		finalPrompt = buildL3SystemPrompt(tmpl, groups, planDir, effectiveWorkDir, exploreDir)
	}

	// Inject project instructions (AGENTS.md / CLAUDE.md) into system prompt
	if projRes.projectPrompt != "" {
		finalPrompt = finalPrompt + projRes.projectPrompt
	}

	// 2. Construct the Definition
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
		Channels:        tmpl.Channels,
		NotifyChannel:   tmpl.NotifyChannel,
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
		def.ThinkingType = info.ThinkingType
		def.Vision = info.Vision
	}

	// 2. Build built-in tools
	var allTools []tools.Tool
	if !strings.HasPrefix(tmpl.ID, "sim-") {
		agentToolsCfg := toolsCfg
		agentToolsCfg.WorkDir = effectiveWorkDir
		agentToolsCfg.PlanDir = planDir
		agentToolsCfg.CronScope = tools.CronAccessScope{}
		if tmpl.IsLeader && tmpl.Group != "" && !iface.IsCronExecution(ctx) {
			agentToolsCfg.CronScope = tools.CronAccessScope{
				Mode:  tools.CronAccessTeam,
				Owner: tmpl.Group,
			}
		}
		allTools = tools.BuildBase(agentToolsCfg)
		allTools = append(allTools, tools.BuildMemory(agentToolsCfg, memoryAccess)...)

		// Additionally filter SendFile for L3 workers only
		if !tmpl.IsLeader {
			var filtered []tools.Tool
			for _, t := range allTools {
				if t.Name() != "SendFile" {
					filtered = append(filtered, t)
				}
			}
			allTools = filtered
		}
	}

	// 3. Load skills — merge global registry + project-level skills (project-level overrides global)
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

	// 2b. L2 leader / Top-level agent: inject single unified delegate tool
	if tmpl.IsLeader {
		workDirPolicy := tools.WorkDirInheritOnly
		if tmpl.Group == "" {
			workDirPolicy = tools.WorkDirExplicitOrInherited
		}

		delegateResolver := func(ctx context.Context, targetName, systemPrompt, modelID, task, workDir, skillID string) (iface.Locatable, bool, error) {
			if loc, ok := f.registry.LocateIdleInWorkDir(targetName, effectiveWorkDir); ok {
				return loc, false, nil
			}

			if peerTmpl, ok := findLeaderTemplate(templates, targetName); ok && peerTmpl.ID != tmpl.ID {
				child, _, err := f.CreateWithOptions(ctx, peerTmpl, effectiveWorkDir, CreateOptions{MemoryPolicy: MemoryDisabled})
				if err != nil {
					return nil, false, fmt.Errorf("spawn peer leader %q: %w", targetName, err)
				}
				peerSv := NewSupervisor(child, f, f.log)
				peerSv.WireSpawnFns(templatesSlice(templates))
				peerSv.SetGroup(peerTmpl.Group)
				return NewSelfReapableAdapter(child, peerSv), true, nil
			}

			for _, peer := range visibleWorkers(templates, tmpl, projRes.agents) {
				if strings.EqualFold(peer.ID, targetName) {
					freshTmpl, ok := f.ResolveTemplate(ctx, peer.ID)
					if !ok {
						freshTmpl = peer
					}
					child, _, err := f.Create(ctx, freshTmpl, workDir)
					if err != nil {
						return nil, false, err
					}
					return &LocatableAdapter{Agent: child}, true, nil
				}
			}

			var childTmpl AgentTemplate
			var ok bool
			var baseAgentName string
			var skillDir string

			if skillID != "" && mergedSkillReg != nil {
				if s, okSkill := mergedSkillReg.GetSkill(skillID); okSkill {
					baseAgentName = s.Agent
					skillDir = s.Dir
					if s.Instructions != "" {
						if systemPrompt != "" {
							systemPrompt = systemPrompt + "\n\n# Skill Execution Instructions\n" + s.Instructions
						} else {
							systemPrompt = "# Skill Execution Instructions\n" + s.Instructions
						}
					}
				}
			}

			if skillDir != "" {
				childTmpl, ok = LoadSkillAgentTemplate(skillDir, targetName)
				if !ok && baseAgentName != "" {
					childTmpl, ok = LoadSkillAgentTemplate(skillDir, baseAgentName)
				}
			}
			if !ok && baseAgentName != "" {
				if t, ok2 := templates[strings.ToLower(baseAgentName)]; ok2 {
					childTmpl = t
					ok = true
				}
			}
			if !ok {
				if t, ok2 := templates[strings.ToLower(targetName)]; ok2 {
					childTmpl = t
					ok = true
				}
			}

			childTmpl.ID = strings.ToLower(targetName)
			childTmpl.Name = targetName
			childTmpl.IsLeader = false

			if ok {
				if systemPrompt != "" {
					if childTmpl.SystemPrompt != "" {
						childTmpl.SystemPrompt = childTmpl.SystemPrompt + "\n\n# Skill/Custom execution logic:\n" + systemPrompt
					} else {
						childTmpl.SystemPrompt = systemPrompt
					}
				}
			} else {
				childTmpl.Description = "Dynamic worker agent"
				childTmpl.SystemPrompt = systemPrompt
			}

			if modelID != "" {
				childTmpl.ModelID = modelID
			}

			child, _, err := f.Create(ctx, childTmpl, workDir)
			if err != nil {
				return nil, false, err
			}
			return &LocatableAdapter{Agent: child}, true, nil
		}

		var delegateOpts []tools.DelegateToolOption
		if tmpl.Group == "" {
			delegateOpts = append(delegateOpts, tools.WithAlwaysAsyncDelegation())
		}
		dt := tools.NewDelegateTool(tmpl.ID, 25*time.Minute, delegateResolver, f.registry, f.log, workDirPolicy, delegateOpts...)
		dt.SkillInstructionsLook = func(skillID string) (string, string, string, bool) {
			if s, ok := mergedSkillReg.GetSkill(skillID); ok {
				return s.Instructions, s.Agent, s.Dir, true
			}
			return "", "", "", false
		}
		allTools = append(allTools, dt)
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
			// Fork spawn: create a temporary child agent to execute a skill in fork mode
			forkSpawn := func(ctx context.Context, s *skill.Skill, content, args string) (iface.Locatable, func(), error) {
				var basePrompt string
				if s.Agent != "" {
					// 1. Try loading base agent template from the skill's own agents/ directory
					if baseTmpl, ok := LoadSkillAgentTemplate(s.Dir, s.Agent); ok {
						basePrompt = baseTmpl.SystemPrompt
					} else {
						// 2. Fallback to global templates registry
						if baseTmpl, ok := templates[strings.ToLower(s.Agent)]; ok {
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

				// Build tools and filter out cron tools + SendFile because this is an L3 agent
				forkTools := tools.BuildBase(toolsCfg)
				var filtered []tools.Tool
				for _, t := range forkTools {
					if t.Name() != "SendFile" && !tools.IsCronTool(t.Name()) {
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
				if a != nil {
					child.SetConfirmStore(a.ConfirmStore())
				}
				if err := child.Start(ctx); err != nil {
					return nil, nil, fmt.Errorf("start fork agent: %w", err)
				}
				cleanup := func() { child.Stop(5) }
				return &LocatableAdapter{Agent: child}, cleanup, nil
			}
			skillOpts := []skill.SkillToolOption{skill.WithAgentID(tmpl.ID)}
			if f.skillStats != nil {
				skillOpts = append(skillOpts, skill.WithInvocationStats(f.skillStats))
			}
			skillTool := skill.NewSkillTool(sr, forkSpawn, skillOpts...)
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

	// 4. Build Option list
	agentOpts := []Option{
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
		// L2 can enable PriorityMailbox for high-priority delivery of L3 results
		// Not enabled for now; L2 synchronously blocks waiting for L3, so priority is not needed
	}

	// 4b. Apply CreateOptions: extra tools and system prompt
	if len(opts.ExtraTools) > 0 {
		agentOpts = append(agentOpts, WithTools(opts.ExtraTools...))
	}
	if opts.ExtraSystemPrompt != "" {
		def.SystemPrompt += "\n\n" + opts.ExtraSystemPrompt
	}

	// 5. Create Agent
	a = NewAgent(def, llm, f.log, agentOpts...)

	// 7. Create ContextWindow
	//   Use the context window size from the model config; fallback to DefaultContextWindow if not configured
	maxTokens := def.ContextWindow
	if maxTokens <= 0 {
		maxTokens = DefaultContextWindow
	}
	cw := ctxwin.NewContextWindow(maxTokens, 2000, 0, ctxwin.NewTokenizer())
	if a.Def.SystemPrompt != "" {
		cw.Push(ctxwin.RoleSystem, a.Def.SystemPrompt)
	}

	// 8. Register to registry
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
	agents        []AgentTemplate
	skills        []*skill.Skill
	mcpCfg        *mcp.Config
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

// LoadAgentTemplates scans the agents directory and parses all .md files into AgentTemplate
//
// Returns all agent templates (without filtering IsLeader); the caller decides how to use them.
func LoadAgentTemplates(agentsDir string) ([]AgentTemplate, error) {
	agentFiles, err := prompt.LoadAgentFiles(agentsDir)
	if err != nil {
		return nil, err
	}

	var templates []AgentTemplate
	for _, af := range agentFiles {
		fm := af.Frontmatter
		tmpl := AgentTemplate{
			ID:            strings.ToLower(fm.Name),
			Name:          fm.Name,
			Description:   fm.Description,
			SystemPrompt:  af.Body,
			ModelID:       fm.Model,
			IsLeader:      fm.IsLeader,
			Group:         fm.Group,
			Permission:    fm.Permission,
			MCPServers:    fm.MCPServers,
			SkillIDs:      fm.Skills,
			Channels:      fm.Channels,
			NotifyChannel: fm.NotifyChannel,
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

// visibleWorkers returns the merged list of global agent templates in the same group as tmpl + project-level agent templates.
// Project-level agents override global agents with the same ID.
// Used to inject delegate_* tools for L2 leaders.
func visibleWorkers(templates map[string]AgentTemplate, tmpl AgentTemplate, projectAgents []AgentTemplate) []AgentTemplate {
	merged := make(map[string]AgentTemplate)

	// Global workers from the same group
	for _, t := range templates {
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

// findLeaderTemplate finds a leader template by ID (case-insensitive).
func findLeaderTemplate(templates map[string]AgentTemplate, id string) (AgentTemplate, bool) {
	for _, t := range templates {
		if t.IsLeader && strings.EqualFold(t.ID, id) {
			return t, true
		}
	}
	return AgentTemplate{}, false
}

// templatesSlice converts the internal template map to a slice for APIs
// that accept []AgentTemplate (e.g., Supervisor.WireSpawnFns).
func templatesSlice(templates map[string]AgentTemplate) []AgentTemplate {
	out := make([]AgentTemplate, 0, len(templates))
	for _, t := range templates {
		out = append(out, t)
	}
	return out
}

// ─── L2 System Prompt three-segment concatenation ─────────────────────────────────────────────

// buildL2SystemPrompt builds a three-segment System Prompt for the L2 Supervisor.
//
// Segment 1 (User Defined Area): User's business Role + System Prompt
// Segment 2 (Dynamic Capability Area): Team Context + Same-Group Agents Directory + MCP Servers
// Segment 3 (Framework Mandatory Area): Immutable underlying contract
func buildL2SystemPrompt(tmpl AgentTemplate, templates map[string]AgentTemplate, groups map[string]prompt.GroupFile, planDir, workDir, exploreDir string, projectAgents []AgentTemplate, hasPermanentMemory bool) string {
	var b strings.Builder

	// ── Identity ──────────────────────────────────────────
	b.WriteString("# Identity\n\n")
	fmt.Fprintf(&b, "You are %s.\n\n", tmpl.Name)
	b.WriteString("Your responses must be extremely concise and direct. Answer exactly what is asked without any unnecessary fluff, conversational filler, or pleasantries.\n\n")

	// ── Segment 1: User Defined Area ──────────────────────────────
	// tmpl.SystemPrompt is a markdown body, containing the user's custom full role definition
	// tmpl.Description serves as a fallback only when SystemPrompt is empty
	if tmpl.SystemPrompt != "" {
		b.WriteString(tmpl.SystemPrompt)
		b.WriteString("\n\n")
	} else if tmpl.Description != "" {
		b.WriteString("# Role\n")
		b.WriteString(tmpl.Description)
		b.WriteString("\n\n")
	}

	// ── Segment 2: Dynamic Capability Area ──────────────────────────────
	// 2a. Working Directory (Static instructions for 100% prompt cache hit rate)
	b.WriteString("# Working Directory\n\nAll file and tool operations execute in the current working directory (CWD) by default. Relative file paths and shell commands operate natively in this directory. If needed, you can use `pwd` or directory inspection commands to verify the absolute path.\n\n")

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

	// 2b. Same-group Agents Directory (excluding the leader itself) + merged project-level agents
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
		workerIDs := make([]string, 0, len(mergedWorkers))
		for id := range mergedWorkers {
			workerIDs = append(workerIDs, id)
		}
		sort.Strings(workerIDs)
		for _, id := range workerIDs {
			peer := mergedWorkers[id]
			desc := peer.Description
			if desc == "" {
				desc = "no description"
			}
			fmt.Fprintf(&b, "- **%s**: %s\n", peer.Name, desc)
		}
		b.WriteString("\n")
	}

	// 2b-peer. Peer Teams (cross-team collaboration)
	// List other team leaders so the L2 knows who it can ask for help.
	peerLeaders := make([]AgentTemplate, 0)
	for _, t := range templates {
		if t.IsLeader && t.ID != tmpl.ID {
			peerLeaders = append(peerLeaders, t)
		}
	}
	sort.Slice(peerLeaders, func(i, j int) bool {
		return peerLeaders[i].ID < peerLeaders[j].ID
	})
	if len(peerLeaders) > 0 {
		b.WriteString("# Peer Teams (Cross-Team Collaboration)\n\n")
		b.WriteString("## MANDATORY Delegation Chain\n\n")
		b.WriteString("You MUST follow this exact priority chain, in order, without skipping levels:\n\n")
		b.WriteString("1. **Your Team Workers (FIRST)** — Delegate ALL sub-tasks that match a worker's domain. This is non-negotiable. Self-executing worker-level work is FORBIDDEN.\n\n")
		b.WriteString("2. **Peer Teams (SECOND)** — If NO team worker can handle the sub-task, you MUST check all peer teams listed below. If a peer team's domain matches, you MUST delegate via `request_team_help(name, task, context)`. Skipping peer teams and going directly to self-execution is FORBIDDEN.\n\n")
		b.WriteString("3. **Self-execute (LAST RESORT)** — Only when BOTH team workers AND all peer teams are unsuitable. Self-execution is a delegation failure. Minimize it.\n\n")
		b.WriteString("## Available Peer Teams\n\n")
		for _, peer := range peerLeaders {
			desc := peer.Description
			if desc == "" {
				desc = "no description"
			}
			fmt.Fprintf(&b, "- **%s**: %s\n", peer.Name, desc)
		}
		b.WriteString("\n")
		b.WriteString("## Rules\n")
		b.WriteString("- Peer help is for SUB-TASKS within your current task. Do NOT outsource the entire task.\n")
		b.WriteString("- Provide clear, self-contained context when delegating to peers.\n")
		b.WriteString("- Do NOT form delegation loops (system enforced, auto-rejected).\n")
		b.WriteString("- If a peer team is unreachable, report to the orchestrator with details.\n\n")
	}

	// 2c. MCP Servers
	if len(tmpl.MCPServers) > 0 {
		b.WriteString("# Available MCP Servers\n\n")
		for _, name := range tmpl.MCPServers {
			fmt.Fprintf(&b, "- %s\n", name)
		}
		b.WriteString("\n")
	}

	// ── Segment 3: Framework Mandatory Area ──────────────────────────────
	b.WriteString(prompt.EnvSection(workDir, exploreDir, false, false))
	b.WriteString("\n\n")
	if hasPermanentMemory {
		b.WriteString(prompt.MemoryEngineSection)
		b.WriteString("\n\n")
	}
	b.WriteString(strings.ReplaceAll(prompt.SharedAgentRules, "{{EXPLORE_DIR}}", exploreDir))
	b.WriteString(strings.ReplaceAll(prompt.L2EnforcedDirectivesPart1, "{{PLAN_DIR}}", planDir))
	if planDir != "" {
		planSection := strings.ReplaceAll(prompt.L2EnforcedPlanSection, "{{PLAN_DIR}}", planDir)
		planSection = strings.ReplaceAll(planSection, "{{PLAN_DOC_FORMAT}}", prompt.PlanDocumentFormat)
		b.WriteString(planSection)
	}
	b.WriteString(strings.ReplaceAll(prompt.L2EnforcedDirectivesPart2, "{{PLAN_DIR}}", planDir))
	b.WriteString(strings.ReplaceAll(prompt.L2EnforcedPostPlan, "{{PLAN_DIR}}", planDir))
	if hasMCPServer(tmpl.MCPServers, "builtin-lsp") {
		b.WriteString(prompt.LSPToolAwarenessSection)
	}

	return b.String()
}

// ─── L3 System Prompt Two-Segment Assembly ─────────────────────────────────────────────

func hasMCPServer(servers []string, target string) bool {
	for _, s := range servers {
		if s == target {
			return true
		}
	}
	return false
}

// buildL3SystemPrompt builds a two-segment System Prompt for the L3 Worker.
//
// Segment 1 (User Defined Area): User's business Role + System Prompt
// Segment 2 (Framework Mandatory Area): Immutable underlying contract
func buildL3SystemPrompt(tmpl AgentTemplate, groups map[string]prompt.GroupFile, planDir, workDir, exploreDir string) string {
	var b strings.Builder

	// ── Identity ──────────────────────────────────────────
	b.WriteString("# Identity\n\n")
	fmt.Fprintf(&b, "You are %s.\n\n", tmpl.Name)
	b.WriteString("Your responses must be extremely concise and direct. Answer exactly what is asked without any unnecessary fluff, conversational filler, or pleasantries.\n\n")

	// ── Segment 1: User Defined Area ──────────────────────────────
	if tmpl.SystemPrompt != "" {
		b.WriteString(tmpl.SystemPrompt)
		b.WriteString("\n\n")
	} else if tmpl.Description != "" {
		b.WriteString("# Role\n")
		b.WriteString(tmpl.Description)
		b.WriteString("\n\n")
	}

	// ── Working Directory (Static instructions for 100% prompt cache hit rate) ──
	b.WriteString("# Working Directory\n\nAll file and tool operations execute in the current working directory (CWD) by default. Relative file paths and shell commands operate natively in this directory. If needed, you can use `pwd` or directory inspection commands to verify the absolute path.\n\n")

	// ── Segment 2: Framework Mandatory Area ──────────────────────────────
	b.WriteString(prompt.EnvSection(workDir, exploreDir, false, false))
	b.WriteString("\n\n")
	b.WriteString(strings.ReplaceAll(prompt.SharedAgentRules, "{{EXPLORE_DIR}}", exploreDir))
	b.WriteString(strings.ReplaceAll(prompt.L3EnforcedDirectives, "{{PLAN_DIR}}", planDir))
	b.WriteString(strings.ReplaceAll(prompt.L3EnforcedPostPlan, "{{PLAN_DIR}}", planDir))
	if hasMCPServer(tmpl.MCPServers, "builtin-lsp") {
		b.WriteString(prompt.LSPToolAwarenessSection)
	}

	return b.String()
}
