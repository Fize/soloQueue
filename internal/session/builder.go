package session

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/prompt"
	"github.com/xiaobaitu/soloqueue/internal/router"
	"github.com/xiaobaitu/soloqueue/internal/runtime"
	"github.com/xiaobaitu/soloqueue/internal/skill"
	"github.com/xiaobaitu/soloqueue/internal/team"
	"github.com/xiaobaitu/soloqueue/internal/timeline"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// Builder encapsulates session creation logic. Each Build() call produces
// an independent session with its own agent, context window, and timeline writer.
type Builder struct {
	RT         *runtime.Stack
	WorkDir    string
	Cfg        *config.GlobalService
	ConsoleLog bool
}

// NewBuilder creates a Builder instance.
func NewBuilder(rt *runtime.Stack, workDir string, cfg *config.GlobalService, consoleLog bool) *Builder {
	return &Builder{
		RT:         rt,
		WorkDir:    workDir,
		Cfg:        cfg,
		ConsoleLog: consoleLog,
	}
}

// Build creates a new session with its own agent, context window, and
// timeline writer. Implements AgentFactory signature.
func (b *Builder) Build(ctx context.Context, teamID string) (*agent.Agent, *ctxwin.ContextWindow, *timeline.Writer, error) {
	agentID := runtime.NewAgentID()

	// Snapshot configuration-derived fields (concurrency-safe, each Build uses latest hot-reload values).
	defModel := b.RT.ReadDefaultModel()
	llmClient := b.RT.ReadLLMClient()
	toolsCfg := b.RT.ReadToolsCfg()

	effectiveModelID := defModel.APIModel
	if effectiveModelID == "" {
		effectiveModelID = defModel.ID
	}
	def := agent.Definition{
		ID:              agentID,
		Name:            prompt.ReadSoulName(b.RT.PromptCfg),
		Kind:            agent.KindCustom,
		ModelID:         effectiveModelID,
		Temperature:     defModel.Generation.Temperature,
		MaxTokens:       defModel.Generation.MaxTokens,
		ReasoningEffort: defModel.Thinking.ReasoningEffort,
		ThinkingEnabled: defModel.Thinking.Enabled,
		MaxIterations:   1000,
		ContextWindow:   defModel.ContextWindow,
		SystemPrompt:    b.RT.SystemPrompt,
	}

	effectiveTeam := teamID
	if effectiveTeam == "" {
		effectiveTeam = "default"
	}
	settings := b.Cfg.Get()
	sessLog, err := logger.System(b.WorkDir,
		logger.WithLevel(logger.ParseLogLevel(settings.Log.Level)),
		logger.WithConsole(b.ConsoleLog),
		logger.WithFile(settings.Log.File),
	)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build session logger: %w", err)
	}

	// Tools: built-in tools (fallback-only for L1) + DelegateTool (async mode: L1 -> L2)
	sessionToolsCfg := toolsCfg
	sessionToolsCfg.Logger = sessLog
	baseTools := tools.Build(sessionToolsCfg)

	// Auto-reload: wrap file-writing tools so writes to agents/ or groups/ dirs
	// trigger automatic parsing and instantiation.
	autoReloadCfg := &team.AutoReloadConfig{
		AgentsDir:    filepath.Join(b.WorkDir, "agents"),
		GroupsDir:    filepath.Join(b.WorkDir, "groups"),
		AgentFactory: b.RT.AgentFactory,
		Logger:       sessLog,
		OnWorkerCreated: func(ctx context.Context, name, group string, ag *agent.Agent, tmpl agent.AgentTemplate) {
			b.RT.CfgMu.RLock()
			supervisors := b.RT.Supervisors
			b.RT.CfgMu.RUnlock()
			for _, sv := range supervisors {
				if sv.Group() == group {
					sv.AdoptChild(ag)
					l2 := sv.Agent()
					// Wire spawn fn on existing delegate tool, or create one
					// for newly added workers not known when L2 was created.
					if !l2.SetDelegateSpawnFn(name, sv.SpawnFnFor(tmpl)) {
						dt := tools.NewDelegateTool(name, tmpl.Description,
							25*time.Minute, nil, sessLog)
						dt.SpawnFn = sv.SpawnFnFor(tmpl)
						if err := l2.RegisterTool(dt); err != nil {
							sessLog.Warn(logger.CatActor,
								"auto-reload: register delegate tool failed",
								"name", name, "err", err.Error())
						}
					}
					sessLog.Info(logger.CatActor, "auto-reload: worker adopted & spawn fn wired",
						"name", name, "group", group)
					return
				}
			}
		},
	}
	for i, t := range baseTools {
		switch t.Name() {
		case "Write", "Edit", "MultiWrite", "MultiEdit":
			baseTools[i] = team.WrapWithAutoReload(t, autoReloadCfg)
		}
	}

	allTools := tools.WithFallbackPrefix(baseTools)
	for _, l := range b.RT.Leaders {
		leader := l // capture loop variable

		// Find the AgentTemplate matching this leader for dynamic creation.
		var leaderTmpl *agent.AgentTemplate
		for i := range b.RT.AllTemplates {
			if b.RT.AllTemplates[i].IsLeader && b.RT.AllTemplates[i].ID == leader.Name {
				leaderTmpl = &b.RT.AllTemplates[i]
				break
			}
		}

		dt := tools.NewDelegateTool(leader.Name, leader.Description, 30*time.Minute, b.RT.AgentRegistry, sessLog)
		dt.SpawnFn = func(ctx context.Context, task string) (iface.Locatable, error) {
			// Prefer an idle instance to avoid cold-start latency.
			if loc, ok := b.RT.AgentRegistry.LocateIdle(leader.Name); ok {
				return loc, nil
			}
			// No idle instance — create a new one with a unique InstanceID.
			if leaderTmpl != nil {
				child, _, err := b.RT.AgentFactory.Create(ctx, *leaderTmpl)
				if err != nil {
					return nil, fmt.Errorf("spawn leader %q: %w", leader.Name, err)
				}

				sv := agent.NewSupervisor(child, b.RT.AgentFactory, sessLog)
				sv.WireSpawnFns(b.RT.AllTemplates)
				sv.SetGroup(leaderTmpl.Group)
				b.RT.AddSupervisor(sv)

				// Register supervisor-scoped inspect_agent for this leader.
				if err := child.RegisterTool(tools.NewInspectAgentTool(agent.SupervisorInspectQuery(sv))); err != nil {
					sessLog.Warn(logger.CatActor, "register inspect_agent for leader failed",
						"name", leader.Name, "err", err.Error())
				}

				sessLog.Info(logger.CatActor, "dynamic L2 supervisor created",
					"instance_id", child.InstanceID,
					"name", leader.Name,
				)
				return agent.NewSelfReapableAdapter(child, sv), nil
			}
			// Fallback: any existing instance (busy but functional).
			if loc, ok := b.RT.AgentRegistry.Locate(leader.Name); ok {
				return loc, nil
			}
			return nil, fmt.Errorf("leader %q not found", leader.Name)
		}
		allTools = append(allTools, dt)
	}

	// Add inspect_agent tool for L1 to query all agent status
	inspectTool := tools.NewInspectAgentTool(agent.RegistryInspectQuery(b.RT.AgentRegistry))
	allTools = append(allTools, inspectTool)

	// Skills: use the global skillRegistry
	skillList := b.RT.SkillRegistry.Skills()

	// SkillTool: only register when skills exist
	if b.RT.SkillRegistry.Len() > 0 {
		// Fork spawn function: creates a temporary child agent to execute a fork-mode skill
		forkSpawn := func(ctx context.Context, s *skill.Skill, content, args string) (iface.Locatable, func(), error) {
			forkDef := agent.Definition{
				ID:           fmt.Sprintf("skill-fork-%s", s.ID),
				ModelID:      def.ModelID,
				SystemPrompt: content,
			}
			forkTools := tools.Build(toolsCfg)
			if len(s.AllowedTools) > 0 {
				forkTools = skill.FilterTools(forkTools, s.AllowedTools)
			}
			child := agent.NewAgent(forkDef, llmClient, sessLog,
				agent.WithTools(forkTools...),
				agent.WithParallelTools(true),
			)
			if err := child.Start(ctx); err != nil {
				return nil, nil, fmt.Errorf("start fork agent: %w", err)
			}
			cleanup := func() { child.Stop(5) }
			return &agent.LocatableAdapter{Agent: child}, cleanup, nil
		}

		skillTool := skill.NewSkillTool(b.RT.SkillRegistry, forkSpawn,
			skill.WithSkillLogger(sessLog))
		allTools = append(allTools, skillTool)
	}

	// MCP tools for L1: register tools from agent.mcpServers whitelist.
	if b.RT.MCPManager != nil {
		for _, name := range b.RT.L1MCPServers() {
			mcpTools := b.RT.MCPManager.GetTools(ctx, name)
			if mcpTools != nil {
				allTools = append(allTools, mcpTools...)
			}
		}
	}

	a := agent.NewAgent(def, llmClient, sessLog,
		agent.WithTools(allTools...),
		agent.WithSkills(skillList...),
		agent.WithParallelTools(true),
		agent.WithPriorityMailbox(),
		agent.WithToolTimeout("Glob", 30*time.Second),
		agent.WithToolTimeout("Grep", 30*time.Second),
		agent.WithToolTimeout("Read", 30*time.Second),
		agent.WithToolTimeout("Write", 30*time.Second),
		agent.WithToolTimeout("Edit", 30*time.Second),
		agent.WithToolTimeout("MultiWrite", 30*time.Second),
		agent.WithToolTimeout("MultiEdit", 30*time.Second),
		agent.WithToolTimeout("WebFetch", 10*time.Minute),
		agent.WithToolTimeout("WebSearch", 10*time.Minute),
	)
	b.RT.AgentRegistry.Register(a)

	// Set the OnLeaderCreated hook after agent construction so the closure
	// can reference 'a'. The hook fires when a leader agent file is written
	// and auto-instantiated — it dynamically registers a delegate_* tool on L1.
	autoReloadCfg.OnLeaderCreated = func(ctx context.Context, name, group string, ag *agent.Agent, _ agent.AgentTemplate) {
		// If a supervisor for this leader template already exists, reap the
		// old leader (stop + unregister) before creating the new one.
		var oldSV *agent.Supervisor
		b.RT.CfgMu.RLock()
		for _, sv := range b.RT.Supervisors {
			if sv.Agent() != nil && sv.Agent().Def.ID == name {
				oldSV = sv
				break
			}
		}
		b.RT.CfgMu.RUnlock()
		if oldSV != nil {
			oldSV.ReapAll(10 * time.Second)
			oldSV.Agent().Stop(10 * time.Second)
			if b.RT.AgentFactory != nil && b.RT.AgentFactory.Registry() != nil {
				b.RT.AgentFactory.Registry().Unregister(oldSV.Agent().InstanceID)
			}
			b.RT.RemoveSupervisor(oldSV)
			sessLog.Info(logger.CatActor, "auto-reload: reaped old leader",
				"name", name, "group", group)
		}

		sv := agent.NewSupervisor(ag, b.RT.AgentFactory, sessLog)
		sv.WireSpawnFns(b.RT.AllTemplates)
		sv.SetGroup(group)
		b.RT.AddSupervisor(sv)
		sessLog.Info(logger.CatActor, "auto-reload: leader supervisor created",
			"name", name, "group", group)

		// Register supervisor-scoped inspect_agent for the auto-reloaded leader.
		if err := ag.RegisterTool(tools.NewInspectAgentTool(agent.SupervisorInspectQuery(sv))); err != nil {
			sessLog.Warn(logger.CatActor, "register inspect_agent for auto-reload leader failed",
				"name", name, "err", err.Error())
		}

		dt := tools.NewDelegateTool(name, name+" team leader", 30*time.Minute, b.RT.AgentRegistry, sessLog)
		dt.SpawnFn = func(ctx context.Context, task string) (iface.Locatable, error) {
			// Prefer an idle instance to enable parallel delegation.
			if loc, ok := b.RT.AgentRegistry.LocateIdle(name); ok {
				return loc, nil
			}
			// Fallback: any instance (even if busy).
			if loc, ok := b.RT.AgentRegistry.Locate(name); ok {
				return loc, nil
			}
			return nil, fmt.Errorf("leader %q not found in registry", name)
		}
		if err := a.RegisterTool(dt); err != nil {
			sessLog.Error(logger.CatActor, "register delegate tool for new leader failed",
				"leader", name, "err", err.Error())
		}
	}

	// Timeline writer + push hook
	tlDir := filepath.Join(b.WorkDir, "logs", "timelines", effectiveTeam)
	tlMaxFileMB := config.DefaultInt(settings.Session.TimelineMaxFileMB, 50)
	if tlMaxFileMB > 50 {
		tlMaxFileMB = 50
	}
	tlMaxBytes := int64(tlMaxFileMB) * 1024 * 1024
	tl, err := timeline.NewWriter(tlDir, "timeline", tlMaxBytes, 15,
		timeline.WithWriterLogger(sessLog))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build timeline writer: %w", err)
	}
	summaryHook := func(summary string, msgs []ctxwin.Message) {
		if err := tl.AppendControl(&timeline.ControlPayload{
			Action:  "summary",
			Reason:  "auto_compact",
			Content: summary,
		}); err != nil {
			sessLog.Error(logger.CatActor, "timeline summary append failed",
				"err", err.Error(), "agent_id", agentID)
		}
		// Record to short-term memory (fire-and-forget, non-blocking).
		// Filter by dedup cursor and group by date to avoid duplicate entries.
		if b.RT.MemoryManager != nil {
			cursor := b.RT.MemoryManager.LastRecordedAt()
			filtered := filterMessagesSince(msgs, cursor)
			if len(filtered) == 0 {
				return
			}
			var latest time.Time
			groups := groupMessagesByDate(filtered)
			for _, g := range groups {
				go func(date time.Time, msgs []ctxwin.Message) {
					defer func() {
						if r := recover(); r != nil {
							sessLog.Error(logger.CatApp, "memory record goroutine panic recovered",
								"panic", fmt.Sprintf("%v", r))
						}
					}()
					text := FormatCtxwinMessages(msgs)
					_ = b.RT.MemoryManager.RecordAt(context.Background(), text, date)
				}(g.date, g.msgs)
				for _, m := range g.msgs {
					if m.Timestamp.After(latest) {
						latest = m.Timestamp
					}
				}
			}
			b.RT.MemoryManager.AdvanceLastRecordedAt(latest)
		}
	}

	pushHook := func(msg ctxwin.Message) {
		var toolCalls []timeline.ToolCallRec
		for _, tc := range msg.ToolCalls {
			toolCalls = append(toolCalls, timeline.ToolCallRec{
				ID:        tc.ID,
				Type:      tc.Type,
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			})
		}
		if err := tl.AppendMessage(&timeline.MessagePayload{
			Role:             string(msg.Role),
			Content:          msg.Content,
			ReasoningContent: msg.ReasoningContent,
			Name:             msg.Name,
			ToolCallID:       msg.ToolCallID,
			ToolCalls:        toolCalls,
			IsEphemeral:      msg.IsEphemeral,
			AgentID:          agentID,
		}); err != nil {
			sessLog.Error(logger.CatActor, "timeline append failed",
				"err", err.Error(), "role", string(msg.Role), "agent_id", agentID)
		}
	}

	// ContextWindow + system prompt
	cw := ctxwin.NewContextWindow(
		defModel.ContextWindow,
		defModel.ContextWindow/10,
		0,
		b.RT.Tokenizer,
		ctxwin.WithPushHook(pushHook),
		ctxwin.WithSummaryHook(summaryHook),
		ctxwin.WithCompactor(b.RT.Compactor),
	)
	// Push system prompt without writing to timeline. The timeline is a
	// conversation event stream; the system prompt is CW metadata, not
	// conversation — persisting it floods the timeline with duplicate
	// copies on every startup, evicting real history during replay.
	cw.SetReplayMode(true)
	if def.SystemPrompt != "" {
		cw.Push(ctxwin.RoleSystem, def.SystemPrompt)
	}

	// Replay the last 10 conversation turns (not the full timeline).
	segments, _, err := timeline.ReadTail(tlDir, "timeline", 10)
	if err != nil {
		sessLog.Warn(logger.CatActor, "builder: ReadTail failed", "err", err.Error(), "dir", tlDir)
	} else if len(segments) == 0 {
		sessLog.Warn(logger.CatActor, "builder: ReadTail returned no segments", "dir", tlDir)
	} else {
		sessLog.Info(logger.CatActor, "builder: ReadTail returned segments, replaying", "segments", len(segments), "msgs", len(segments[0].Messages))
		timeline.ReplayInto(cw, segments)
	}
	cw.SetReplayMode(false)

	if err := a.Start(context.Background()); err != nil {
		tl.Close()
		return nil, nil, nil, err
	}
	return a, cw, tl, nil
}

// BuildFactory constructs the AgentFactory function used by SessionManager.
//
// consoleLog controls whether the session logger outputs to stderr
// (TUI=false, serve=settings.Log.Console).
func BuildFactory(rt *runtime.Stack, workDir string, cfg *config.GlobalService, consoleLog bool) AgentFactory {
	b := NewBuilder(rt, workDir, cfg, consoleLog)
	return b.Build
}

// BuildRouterFunc creates a TaskRouterFunc from the runtime Stack's task router.
// Returns nil if no router is configured (routing disabled).
func BuildRouterFunc(rt *runtime.Stack) TaskRouterFunc {
	if rt.TaskRouter == nil {
		return nil
	}
	rtr := rt.TaskRouter
	return func(ctx context.Context, prompt string, priorLevel string) (RouteResult, error) {
		// Use the router package's Route method directly
		decision, err := rtr.Route(ctx, prompt, parseClassificationLevel(priorLevel))
		if err != nil {
			return RouteResult{}, err
		}
		return RouteResult{
			ProviderID:      decision.ProviderID,
			ModelID:         decision.ModelID,
			ThinkingEnabled: decision.ThinkingEnabled,
			ReasoningEffort: decision.ReasoningEffort,
			Level:           decision.Level.String(),
		}, nil
	}
}

// BuildMemoryHook creates a MemoryHook that records conversation segments
// to the short-term memory system.
func BuildMemoryHook(rt *runtime.Stack) MemoryHook {
	if rt.MemoryManager == nil {
		return nil
	}
	return func(ctx context.Context, conversationText string, recordedAt time.Time) {
		_ = rt.MemoryManager.RecordAt(ctx, conversationText, recordedAt)
	}
}

// parseClassificationLevel converts a string level to the router package's ClassificationLevel.
func parseClassificationLevel(level string) router.ClassificationLevel {
	switch level {
	case "L0-Conversation":
		return router.LevelConversation
	case "L1-SimpleSingleFile":
		return router.LevelSimpleSingleFile
	case "L2-MediumMultiFile":
		return router.LevelMediumMultiFile
	case "L3-ComplexRefactoring":
		return router.LevelComplexRefactoring
	default:
		return router.LevelUnknown
	}
}

// FormatCtxwinMessages converts ctxwin messages to a plain-text representation
// suitable for memory summarization. Skips system messages.
func FormatCtxwinMessages(msgs []ctxwin.Message) string {
	var buf strings.Builder
	var lastTS time.Time
	for _, m := range msgs {
		if m.Role == ctxwin.RoleSystem {
			continue
		}
		if !m.Timestamp.IsZero() && !m.Timestamp.Equal(lastTS) {
			buf.WriteString("[" + m.Timestamp.Format("2006-01-02 15:04") + "]\n")
			lastTS = m.Timestamp
		}
		switch m.Role {
		case ctxwin.RoleUser:
			buf.WriteString("User: ")
		case ctxwin.RoleAssistant:
			buf.WriteString("Assistant: ")
		case ctxwin.RoleTool:
			buf.WriteString("Tool(" + m.Name + "): ")
		default:
			buf.WriteString(string(m.Role) + ": ")
		}
		content := m.Content
		if len(content) > 2000 {
			content = content[:2000] + "...(truncated)"
		}
		buf.WriteString(content)
		buf.WriteString("\n\n")
	}
	return buf.String()
}
