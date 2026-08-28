package session

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/agenttools/skill"
	"github.com/xiaobaitu/soloqueue/internal/agenttools/tools"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/infra/db"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/infra/telemetry"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/memory/engine"
	"github.com/xiaobaitu/soloqueue/internal/memory/timeline"
	"github.com/xiaobaitu/soloqueue/internal/prompt"
	"github.com/xiaobaitu/soloqueue/internal/router"
	"github.com/xiaobaitu/soloqueue/internal/runtime"
	"github.com/xiaobaitu/soloqueue/internal/tasktype"
	"github.com/xiaobaitu/soloqueue/internal/team"
)

func timelineTemporalParts(parts []ctxwin.TemporalPart) []timeline.TemporalPartPayload {
	if len(parts) == 0 {
		return nil
	}
	out := make([]timeline.TemporalPartPayload, len(parts))
	for i, part := range parts {
		var timestamp string
		if !part.Timestamp.IsZero() {
			timestamp = part.Timestamp.Format(time.RFC3339Nano)
		}
		out[i] = timeline.TemporalPartPayload{
			Content:         part.Content,
			Timestamp:       timestamp,
			ExposeTimestamp: part.ExposeTimestamp,
		}
	}
	return out
}

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

// newSessionLogger creates a session-level logger from the current settings.
func (b *Builder) newSessionLogger() (*logger.Logger, error) {
	settings := b.Cfg.Get()
	return logger.System(b.WorkDir,
		logger.WithLevel(logger.ParseLogLevel(settings.Log.Level)),
		logger.WithConsole(b.ConsoleLog),
		logger.WithFile(settings.Log.File),
	)
}

// newTimelineWriter creates a timeline writer for the given directory.
func (b *Builder) newTimelineWriter(dir string, sessLog *logger.Logger) (*timeline.Writer, error) {
	settings := b.Cfg.Get()
	tlMaxFileMB := config.DefaultInt(settings.Session.TimelineMaxFileMB, 50)
	if tlMaxFileMB > 50 {
		tlMaxFileMB = 50
	}
	tlMaxBytes := int64(tlMaxFileMB) * 1024 * 1024
	return timeline.NewWriter(dir, "timeline", tlMaxBytes, 15, timeline.WithWriterLogger(sessLog))
}

// Build creates a new session with its own agent, context window, and
// timeline writer. Implements AgentFactory signature.
func (b *Builder) Build(ctx context.Context, teamID string) (*agent.Agent, *ctxwin.ContextWindow, *timeline.Writer, error) {
	var a *agent.Agent
	// L1 orchestrator uses a fixed agent ID so timeline replays are deterministic
	// across restarts and never mix with old sessions.
	agentID := "l1-agent"

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
		MaxIterations:   200,
		ProviderID:      defModel.ProviderID,
		ContextWindow:   defModel.ContextWindow,
		SystemPrompt:    b.RT.SystemPrompt,
		Channels:        b.RT.L1Channels,
		NotifyChannel:   b.RT.L1NotifyChannel,
	}

	effectiveTeam := teamID
	if effectiveTeam == "" {
		effectiveTeam = "default"
	}
	sessLog, err := b.newSessionLogger()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build session logger: %w", err)
	}

	// Tools: built-in tools (fallback-only for L1) + DelegateTool (async mode: L1 -> L2)
	sessionToolsCfg := toolsCfg
	sessionToolsCfg.Logger = sessLog
	sessionToolsCfg.TeamStore = b.RT.TeamStore
	sessionToolsCfg.CronScope = tools.CronAccessScope{Mode: tools.CronAccessGlobal}
	baseTools := tools.BuildBase(sessionToolsCfg)
	if b.RT.MemoryEngine != nil {
		scopeType, scopeID := memoryScopeForL1(b.WorkDir)
		access := b.RT.MemoryEngine.BindL1(scopeType, scopeID, true)
		baseTools = append(baseTools, tools.BuildMemory(sessionToolsCfg, access)...)
	}

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
					if l2.HasTool("delegate") {
						sessLog.Info(logger.CatActor, "auto-reload: worker adopted & delegate tool active",
							"name", name, "group", group)
						return
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

	// Add inspect_agent tool for L1 to query all agent status
	inspectTool := tools.NewInspectAgentTool(agent.RegistryInspectQuery(b.RT.AgentRegistry))
	allTools = append(allTools, inspectTool)

	// Skills: use the global skillRegistry, filtering out disabled ones
	var skillList []*skill.Skill
	for _, s := range b.RT.SkillRegistry.Skills() {
		if !s.Disabled {
			skillList = append(skillList, s)
		}
	}

	// SkillTool: only register when skills exist
	if b.RT.SkillRegistry.Len() > 0 {
		// Fork spawn function: creates a temporary child agent to execute a fork-mode skill
		forkSpawn := func(ctx context.Context, s *skill.Skill, content, args string) (iface.Locatable, func(), error) {
			var basePrompt string
			if s.Agent != "" {
				// 1. Try loading base agent template from the skill's own agents/ directory
				if baseTmpl, ok := agent.LoadSkillAgentTemplate(s.Dir, s.Agent); ok {
					basePrompt = baseTmpl.SystemPrompt
				} else {
					// 2. Fallback to templates stack
					for i := range b.RT.AllTemplates {
						if strings.EqualFold(b.RT.AllTemplates[i].ID, s.Agent) {
							basePrompt = b.RT.AllTemplates[i].SystemPrompt
							break
						}
					}
				}
			}

			finalSystemPrompt := content
			if basePrompt != "" {
				finalSystemPrompt = basePrompt + "\n\n# Skill Execution Instructions\n" + content
			}

			forkDef := agent.Definition{
				ID:           fmt.Sprintf("skill-fork-%s", s.ID),
				ModelID:      def.ModelID,
				SystemPrompt: finalSystemPrompt,
			}
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
			child := agent.NewAgent(forkDef, llmClient, sessLog,
				agent.WithTools(forkTools...),
				agent.WithParallelTools(true),
			)
			if a != nil {
				child.SetConfirmStore(a.ConfirmStore())
			}
			if err := child.Start(ctx); err != nil {
				return nil, nil, fmt.Errorf("start fork agent: %w", err)
			}
			cleanup := func() { child.Stop(5) }
			return &agent.LocatableAdapter{Agent: child}, cleanup, nil
		}

		skillOpts := []skill.SkillToolOption{skill.WithSkillLogger(sessLog), skill.WithAgentID(def.ID)}
		if b.RT.SharedDB != nil {
			skillOpts = append(skillOpts, skill.WithInvocationStats(skill.NewSQLiteInvocationStats(b.RT.SharedDB)))
		}
		skillTool := skill.NewSkillTool(b.RT.SkillRegistry, forkSpawn, skillOpts...)
		allTools = append(allTools, skillTool)
	}

	// Inject unified delegate tool for L1 agent
	delegateResolver := func(ctx context.Context, name, systemPrompt, modelID, task, workDir, skillID string) (iface.Locatable, bool, error) {
		if loc, ok := b.RT.AgentRegistry.LocateIdleInWorkDir(name, workDir); ok {
			return loc, false, nil
		}

		var tmpl agent.AgentTemplate
		var ok bool
		var baseAgentName string
		var skillDir string

		if skillID != "" && b.RT.SkillRegistry != nil {
			if s, okSkill := b.RT.SkillRegistry.GetSkill(skillID); okSkill {
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
			tmpl, ok = agent.LoadSkillAgentTemplate(skillDir, name)
			if !ok && baseAgentName != "" {
				tmpl, ok = agent.LoadSkillAgentTemplate(skillDir, baseAgentName)
			}
		}

		if !ok && baseAgentName != "" {
			for i := range b.RT.AllTemplates {
				if strings.EqualFold(b.RT.AllTemplates[i].ID, baseAgentName) {
					tmpl = b.RT.AllTemplates[i]
					ok = true
					break
				}
			}
		}

		if !ok {
			for i := range b.RT.AllTemplates {
				if strings.EqualFold(b.RT.AllTemplates[i].ID, name) {
					tmpl = b.RT.AllTemplates[i]
					ok = true
					break
				}
			}
		}

		tmpl.ID = strings.ToLower(name)
		tmpl.Name = name
		tmpl.IsLeader = false

		if ok {
			if systemPrompt != "" {
				if tmpl.SystemPrompt != "" {
					tmpl.SystemPrompt = tmpl.SystemPrompt + "\n\n# Skill/Custom execution logic:\n" + systemPrompt
				} else {
					tmpl.SystemPrompt = systemPrompt
				}
			}
		} else {
			tmpl.Description = "Dynamic worker agent"
			tmpl.SystemPrompt = systemPrompt
		}

		if modelID != "" {
			tmpl.ModelID = modelID
		}

		child, _, err := b.RT.AgentFactory.Create(ctx, tmpl, workDir)
		if err != nil {
			return nil, false, err
		}
		sv := agent.NewSupervisor(child, b.RT.AgentFactory, sessLog)
		sv.WireSpawnFns(b.RT.AllTemplates)
		b.RT.AddSupervisor(sv)

		return agent.NewSelfReapableAdapterWithCleanup(child, sv, func() {
			b.RT.RemoveSupervisor(sv)
		}), true, nil
	}

	dt := tools.NewDelegateTool("L1", 30*time.Minute, delegateResolver, b.RT.AgentRegistry, sessLog, tools.WorkDirExplicitOrInherited, tools.WithAlwaysAsyncDelegation())
	dt.SkillInstructionsLook = func(skillID string) (string, string, string, bool) {
		if s, ok := b.RT.SkillRegistry.GetSkill(skillID); ok {
			return s.Instructions, s.Agent, s.Dir, true
		}
		return "", "", "", false
	}
	allTools = append(allTools, dt)

	// MCP tools for L1: register tools from agent.mcpServers whitelist.
	if b.RT.MCPManager != nil {
		for _, name := range b.RT.L1MCPServers() {
			mcpTools := b.RT.MCPManager.GetTools(ctx, name)
			if mcpTools != nil {
				allTools = append(allTools, mcpTools...)
			}
		}
	}

	a = agent.NewAgent(def, llmClient, sessLog,
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
		cleanupSupervisor := func(sv *agent.Supervisor) {
			if sv == nil || sv.Agent() == nil {
				return
			}
			_ = sv.Agent().Stop(10 * time.Second)
			_ = sv.ReapAll(10 * time.Second)
			if b.RT.AgentFactory != nil && b.RT.AgentFactory.Registry() != nil {
				b.RT.AgentFactory.Registry().Unregister(sv.Agent().InstanceID)
			}
			b.RT.RemoveSupervisor(sv)
		}

		// If a supervisor for this leader template already exists, reap the
		// old leader (stop + unregister) before creating the new one.
		var oldSV *agent.Supervisor
		b.RT.CfgMu.RLock()
		for _, sv := range b.RT.Supervisors {
			if sv.Agent() != nil && strings.EqualFold(sv.Agent().Def.ID, name) {
				oldSV = sv
				break
			}
		}
		b.RT.CfgMu.RUnlock()
		if oldSV != nil {
			cleanupSupervisor(oldSV)
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

		if a.HasTool("delegate") {
			return
		}
	}

	// Timeline writer + push hook
	tlDir := filepath.Join(b.WorkDir, "logs", "timelines", effectiveTeam)
	tl, err := b.newTimelineWriter(tlDir, sessLog)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build timeline writer: %w", err)
	}
	summaryHook := func(segments []ctxwin.SummarySegment, finalSummary string) {
		// Nightly persona reflection: runs async before the per-segment memory
		// routing so the raw conversation is still available for evidence.
		go func() {
			defer func() {
				if r := recover(); r != nil {
					sessLog.Error(logger.CatApp, "persona reflection: panic recovered",
						"panic", fmt.Sprintf("%v", r))
				}
			}()
			name := prompt.ReadSoulName(b.RT.PromptCfg)
			var raw strings.Builder
			for _, seg := range segments {
				raw.WriteString(FormatCtxwinMessages(seg.Msgs))
				raw.WriteString("\n")
			}
			var daily string
			if b.RT.MemoryManager != nil {
				daily, _ = b.RT.MemoryManager.ReadRecentMemory(1)
			}
			statePath := filepath.Join(b.WorkDir, "persona", "roles", "state.md")
			if err := UpdatePersonaState(context.Background(), sessLog, b.RT.ReadLLMClient(), statePath, name, raw.String(), daily, time.Now(), b.RT.FastModelProviderID, b.RT.FastModelID); err != nil {
				sessLog.Error(logger.CatApp, "persona reflection failed", "err", err.Error())
			}
		}()

		cutoff := time.Now().AddDate(0, 0, -7)
		cursor := time.Time{}
		if b.RT.MemoryManager != nil {
			cursor = b.RT.MemoryManager.LastRecordedAt()
		}
		var latest time.Time

		// Strip <memories> from the merged final summary once, so the timeline
		// record matches what the CW now stores. Per-segment memory routing
		// below still works off the per-segment summaries (their memories).
		_, finalClean := extractMemoriesFromSummary(finalSummary)

		// Emit a single timeline summary event from the merged finalSummary.
		// Per-segment summaries are still routed to long-term / short-term
		// memory below, but the chat UI now sees one consolidated segment.
		if finalClean != "" {
			if err := tl.AppendControl(&timeline.ControlPayload{
				Action:  "summary",
				Reason:  "auto_compact",
				Content: finalClean,
			}); err != nil {
				sessLog.Error(logger.CatActor, "timeline summary append failed",
					"err", err.Error(), "agent_id", agentID)
			}
		}

		for _, seg := range segments {
			// Dedup: skip messages already recorded by cursor
			filtered := filterMessagesSince(seg.Msgs, cursor)
			if len(filtered) == 0 {
				continue
			}

			// Extract <memories> block from the per-segment summary and save
			// separately to the long-term memory engine.
			memories, _ := extractMemoriesFromSummary(seg.Summary)
			for _, mem := range memories {
				if b.RT.MemoryEngine != nil {
					scopeType, scopeID := engine.ScopeGlobal, ""
					if effectiveTeam != "default" {
						scopeType, scopeID = engine.ScopeTeam, effectiveTeam
					}
					access := b.RT.MemoryEngine.BindL1(scopeType, scopeID, true)
					_, err := access.Ingest(context.Background(), engine.MemoryCandidate{
						Content:    mem,
						MemoryType: engine.MemoryTypeStableFact,
						ScopeType:  scopeType,
						ScopeID:    scopeID,
						SourceType: engine.SourceCompaction,
						SourceID:   effectiveTeam,
						Date:       seg.Date.Format("2006-01-02"),
						EventTime:  seg.Date.Format(time.RFC3339),
						Confidence: 0.8,
					})
					if err != nil {
						sessLog.Error(logger.CatActor, "memory extraction: save failed",
							"err", err.Error())
					}
				}
			}

			if seg.Date.Before(cutoff) {
				// >7 days old: the durable facts above may enter long-term
				// memory, while the full task summary remains in the timeline.
			} else if b.RT.MemoryManager != nil {
				// ≤7 days: short-term memory only (timeline event already
				// emitted above from the merged finalSummary).
				go func(date time.Time, msgs []ctxwin.Message) {
					defer func() {
						if r := recover(); r != nil {
							sessLog.Error(logger.CatApp, "memory record goroutine panic recovered",
								"panic", fmt.Sprintf("%v", r))
						}
					}()
					text := FormatCtxwinMessages(msgs)
					_ = b.RT.MemoryManager.RecordAt(context.Background(), text, date)
				}(seg.Date, filtered)
			}

			for _, m := range filtered {
				if m.Timestamp.After(latest) {
					latest = m.Timestamp
				}
			}
		}

		if b.RT.MemoryManager != nil && !latest.IsZero() {
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
		var files []timeline.FileAttachment
		for _, f := range msg.Files {
			files = append(files, timeline.FileAttachment{
				Name: f.Name,
				Path: f.Path,
			})
		}
		if err := tl.AppendMessage(&timeline.MessagePayload{
			Role:             string(msg.Role),
			Content:          msg.Content,
			Timestamp:        msg.Timestamp.Format(time.RFC3339Nano),
			ReasoningContent: msg.ReasoningContent,
			Name:             msg.Name,
			ToolCallID:       msg.ToolCallID,
			ToolCalls:        toolCalls,
			Files:            files,
			IsEphemeral:      msg.IsEphemeral,
			AgentID:          agentID,
			ExposeTimestamp:  msg.ExposeTimestamp,
			TemporalParts:    timelineTemporalParts(msg.TemporalParts),
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

	// Replay all conversation turns since the last compact/clear boundary (maxTurns=0 = no limit).
	// This ensures ctxwin_used accurately reflects the real token count after restart.
	// UI history rendering uses its own paginated ReadTail calls with explicit limits.
	segments, _, err := timeline.ReadTail(tlDir, "timeline", 0, agentID)
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

func memoryScopeForL1(workDir string) (string, string) {
	workDir = filepath.Clean(strings.TrimSpace(workDir))
	if workDir == "." || workDir == "" || strings.HasSuffix(workDir, string(filepath.Separator)+".soloqueue") {
		return engine.ScopeGlobal, ""
	}
	return engine.ScopeProject, workDir
}

func (b *Builder) ReconcileL1TeamCatalog(sess *Session, systemPrompt string) error {
	if sess == nil || sess.Agent == nil {
		return nil
	}
	if !sess.Idle() {
		return ErrSessionBusy
	}

	sess.ContextWindow().ReplacePrimarySystem(systemPrompt)
	return nil
}

// BuildFactory constructs the AgentFactory function used by SessionManager.
//
// consoleLog controls whether the session logger outputs to stderr
// (serve=settings.Log.Console).
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
	return func(ctx context.Context, prompt string, priorLevel string, history []ctxwin.PayloadMessage) (RouteResult, error) {
		prior, _ := tasktype.Parse(priorLevel)
		decision, err := rtr.Route(ctx, router.ClassifyInput{Text: prompt, PreviousTaskType: prior}, history)

		// Always record classification in usage_metrics for observability.
		// On error the level is "Unknown" and source is "error" — still
		// valuable for diagnosing classifier failures.
		if rt.SharedDB != nil {
			metadata := telemetry.MetadataFromContext(ctx)
			bgCtx := context.Background()

			level := decision.TaskType.String()
			source := string(decision.Classification.Source)
			if err != nil {
				source = "error"
			}

			go func() {
				_ = rt.SharedDB.InsertRouterClassification(
					bgCtx,
					telemetry.UsageRouter, // usageType
					metadata.TeamID,
					level,
					source,
				)
				status := telemetry.StatusSuccess
				if err != nil {
					status = telemetry.StatusError
				}
				_ = rt.SharedDB.InsertRouteDecisionMetric(bgCtx, db.RouteDecisionMetric{
					DecisionID:           uuid.NewString(),
					RequestID:            metadata.RequestID,
					SessionID:            metadata.SessionID,
					RunID:                metadata.RunID,
					TeamID:               metadata.TeamID,
					TaskType:             level,
					ProviderID:           decision.ProviderID,
					ModelID:              decision.ModelID,
					ClassificationSource: source,
					Status:               status,
					DecidedAt:            time.Now().UTC(),
				})
			}()
		}

		if err != nil {
			return RouteResult{}, err
		}

		return RouteResult{
			ProviderID:        decision.ProviderID,
			ModelID:           decision.ModelID,
			ThinkingEnabled:   decision.ThinkingEnabled,
			ReasoningEffort:   decision.ReasoningEffort,
			ThinkingType:      decision.ThinkingType,
			Level:             decision.TaskType.String(),
			ContextWindow:     decision.ContextWindow,
			Vision:            decision.Vision,
			ClassifierWarning: "",
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

// extractMemoriesFromSummary parses a <memories> block from the compactor's
// output and returns:
//   - extracted: individual memory statements (each a concise fact worth saving)
//   - cleaned:   the summary with the <memories> block removed
//
// If no <memories> block is found, returns nil and the original text unchanged.
func extractMemoriesFromSummary(summary string) (extracted []string, cleaned string) {
	startTag := "<memories>"
	endTag := "</memories>"

	start := strings.Index(summary, startTag)
	if start < 0 {
		return nil, summary
	}
	end := strings.Index(summary[start+len(startTag):], endTag)
	if end < 0 {
		return nil, summary
	}
	end = start + len(startTag) + end + len(endTag)

	// Extract content between tags
	block := summary[start+len(startTag) : end-len(endTag)]
	lines := strings.Split(block, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Lines starting with "- " are memory items
		if strings.HasPrefix(trimmed, "- ") {
			item := strings.TrimSpace(trimmed[2:])
			if item != "" {
				extracted = append(extracted, item)
			}
		}
	}

	// Remove the <memories> block from the summary
	cleaned = strings.TrimSpace(summary[:start] + summary[end:])
	return extracted, cleaned
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

// BuildL2 creates a standalone L2 session with its own agent, context window,
// and timeline writer. It uses the leader template matching the given group.
//
// The returned Session has full infrastructure: timeline persistence at
// logs/timelines/l2-<id>/, context window compaction, memory hooks, idle reaper,
// and timeline replay on restart.
//
// L1→L2 delegation is NOT affected — this creates an independent session
// for direct user conversation.
func (b *Builder) BuildL2(ctx context.Context, id, group, workDir string) (*Session, error) {
	agentID := "l2-" + id + "-agent"
	sessionID := "l2-" + id + "-session"

	// Find the leader template matching the group.
	var leaderTmpl *agent.AgentTemplate
	for i := range b.RT.AllTemplates {
		t := &b.RT.AllTemplates[i]
		if t.IsLeader && strings.EqualFold(t.Group, group) {
			leaderTmpl = t
			break
		}
	}
	if leaderTmpl == nil {
		return nil, fmt.Errorf("no leader template found for group %q", group)
	}

	sessLog, err := b.newSessionLogger()
	if err != nil {
		return nil, fmt.Errorf("build L2 session logger: %w", err)
	}

	// Create the L2 agent via the factory — this gets the correct L2 system
	// prompt, delegate tools for workers in this group, MCP tools, and skills.
	// Pass the project workDir so tools operate in the project directory.
	agentWorkDir := workDir
	if agentWorkDir == "" {
		agentWorkDir = b.WorkDir
	}
	childAgent, _, err := b.RT.AgentFactory.Create(ctx, *leaderTmpl, agentWorkDir)
	if err != nil {
		return nil, fmt.Errorf("create L2 agent for group %q: %w", group, err)
	}

	// Create a Supervisor to track L3 children spawned by this L2 session.
	sv := agent.NewSupervisor(childAgent, b.RT.AgentFactory, sessLog)
	sv.WireSpawnFns(b.RT.AllTemplates)
	sv.SetGroup(group)

	// Register supervisor-scoped inspect_agent for this L2.
	if err := childAgent.RegisterTool(tools.NewInspectAgentTool(agent.SupervisorInspectQuery(sv))); err != nil {
		sessLog.Warn(logger.CatActor, "register inspect_agent for L2 failed",
			"name", leaderTmpl.ID, "err", err.Error())
	}

	// Timeline writer.
	tlDir := filepath.Join(b.WorkDir, "logs", "timelines", "l2-"+id)
	tl, err := b.newTimelineWriter(tlDir, sessLog)
	if err != nil {
		_ = childAgent.Stop(5 * time.Second)
		b.RT.AgentRegistry.Unregister(childAgent.InstanceID)
		return nil, fmt.Errorf("build L2 timeline writer: %w", err)
	}

	// Persist session metadata (group, work_dir, git_base_ref, baseline) into
	// the unified meta.json. Subsequent writes (name, plans, level) also go
	// through metastore.MergeAndSave.
	baseline := CaptureBaseline(agentWorkDir)
	if err := MergeAndSave(b.WorkDir, id, func(m *SessionMeta) {
		if m.Group == "" {
			m.Group = group
			m.WorkDir = workDir
			m.GitBaseRef = baseline.GitBaseRef
			if baseline.Snapshot != nil {
				m.Baseline = baseline.Snapshot
			}
		}
	}); err != nil {
		sessLog.Warn(logger.CatApp, "BuildL2: initial meta.json write failed",
			"id", id, "err", err.Error())
	}

	// Context window model config — use the L2 leader's resolved model.
	effectiveCW := childAgent.Def.ContextWindow
	if effectiveCW <= 0 {
		effectiveCW = agent.DefaultContextWindow
	}

	// Summary hook (timeline-only, no memory writes).
	summaryHook := func(_ []ctxwin.SummarySegment, finalSummary string) {
		if finalSummary == "" {
			return
		}
		if err := tl.AppendControl(&timeline.ControlPayload{
			Action:  "summary",
			Reason:  "auto_compact",
			Content: finalSummary,
		}); err != nil {
			sessLog.Error(logger.CatActor, "timeline summary append failed",
				"err", err.Error(), "agent_id", agentID)
		}
	}

	// Push hook: writes every message to timeline.
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
		var files []timeline.FileAttachment
		for _, f := range msg.Files {
			files = append(files, timeline.FileAttachment{
				Name: f.Name,
				Path: f.Path,
			})
		}
		if err := tl.AppendMessage(&timeline.MessagePayload{
			Role:             string(msg.Role),
			Content:          msg.Content,
			Timestamp:        msg.Timestamp.Format(time.RFC3339Nano),
			ReasoningContent: msg.ReasoningContent,
			Name:             msg.Name,
			ToolCallID:       msg.ToolCallID,
			ToolCalls:        toolCalls,
			Files:            files,
			IsEphemeral:      msg.IsEphemeral,
			AgentID:          agentID,
			ExposeTimestamp:  msg.ExposeTimestamp,
			TemporalParts:    timelineTemporalParts(msg.TemporalParts),
		}); err != nil {
			sessLog.Error(logger.CatActor, "timeline append failed",
				"err", err.Error(), "role", string(msg.Role), "agent_id", agentID)
		}
	}

	cw := ctxwin.NewContextWindow(
		effectiveCW,
		effectiveCW/10,
		0,
		b.RT.Tokenizer,
		ctxwin.WithPushHook(pushHook),
		ctxwin.WithSummaryHook(summaryHook),
		ctxwin.WithCompactor(b.RT.Compactor),
	)

	// Push L2 system prompt without writing to timeline.
	cw.SetReplayMode(true)
	if childAgent.Def.SystemPrompt != "" {
		cw.Push(ctxwin.RoleSystem, childAgent.Def.SystemPrompt)
	}

	// Replay all conversation turns since the last compact/clear boundary (maxTurns=0 = no limit).
	// This ensures ctxwin_used accurately reflects the real token count after restart.
	segments, _, err := timeline.ReadTail(tlDir, "timeline", 0, agentID)
	if err != nil {
		sessLog.Warn(logger.CatActor, "BuildL2: ReadTail failed", "err", err.Error(), "dir", tlDir)
	} else if len(segments) > 0 {
		sessLog.Info(logger.CatActor, "BuildL2: replaying timeline segments",
			"segments", len(segments), "msgs", len(segments[0].Messages))
		timeline.ReplayInto(cw, segments)
	}
	cw.SetReplayMode(false)

	// Register the supervisor in the runtime (agent already registered by factory).
	b.RT.AddSupervisor(sv)

	// Build the Session.
	sessLogger := sessLog.Child()
	s := NewSession(sessionID, group, childAgent, cw, tl, sessLogger)
	s.Supervisor = sv
	// Enable auto-compression for idle L2 sessions (same thresholds as L1).
	s.idleTimeout = 30 * time.Minute
	s.compactThreshold = 200000

	// Wire router (same as L1).
	if b.RT.TaskRouter != nil {
		s.Router = BuildRouterFunc(b.RT)
	}

	// Wire L2 meta.json coordinates so router-driven level writes (and any
	// other session code path) can persist without re-deriving paths.
	s.metaWorkDir = b.WorkDir
	s.metaL2ID = id
	s.gitBaseRef = baseline.GitBaseRef
	if baseline.Snapshot != nil {
		s.metaBaseline = baseline.Snapshot
	}

	// Restore last task level from meta.json so follow-up questions after
	// restart don't lose their task context (e.g., "is this feature completed?" staying
	// at L0).
	if persisted, lerr := LoadMeta(b.WorkDir, id); lerr == nil && persisted.Level != "" {
		s.SetLastLevel(persisted.Level)
		sessLog.Debug(logger.CatApp, "BuildL2: restored task level from meta.json",
			"level", persisted.Level,
		)
	} else if lerr != nil && !errors.Is(lerr, ErrNoGroup) {
		sessLog.Warn(logger.CatApp, "BuildL2: meta.json read failed",
			"id", id, "err", lerr.Error())
	}

	sessLog.Info(logger.CatActor, "BuildL2: session created",
		"target_id", sessionID,
		"group", group,
		"agent_id", agentID,
	)

	return s, nil
}

// BuildL2ForCron builds a simplified L2 session for cron/scheduled tasks.
// Compared to BuildL2, it skips timeline replay, idle reaper, memory hooks,
// supervisor registration, router, level file restore, meta file, and baseline
// capture — cron sessions are short-lived and start fresh every time.
func (b *Builder) BuildL2ForCron(ctx context.Context, id, group, cronLogDir string) (*Session, error) {
	agentID := "cron-l2-" + id + "-agent"
	sessionID := "cron-l2-" + id + "-session"

	// Find the leader template matching the group.
	var leaderTmpl *agent.AgentTemplate
	for i := range b.RT.AllTemplates {
		t := &b.RT.AllTemplates[i]
		if t.IsLeader && strings.EqualFold(t.Group, group) {
			leaderTmpl = t
			break
		}
	}
	if leaderTmpl == nil {
		return nil, fmt.Errorf("no leader template found for group %q", group)
	}

	sessLog, err := b.newSessionLogger()
	if err != nil {
		return nil, fmt.Errorf("build L2 cron session logger: %w", err)
	}

	// Create the L2 agent via the factory.
	// Scheduled-task level selection owns the per-run model. Clear a template
	// model pin for this temporary agent so the scheduler override can apply;
	// normal interactive team sessions retain the template pin.
	cronTmpl := *leaderTmpl
	cronTmpl.ModelID = ""
	ctx = iface.ContextWithCronExecution(ctx)
	childAgent, _, err := b.RT.AgentFactory.Create(ctx, cronTmpl, b.WorkDir)
	if err != nil {
		return nil, fmt.Errorf("create L2 agent for cron group %q: %w", group, err)
	}

	// Create a Supervisor to track L3 children spawned by this L2 session.
	sv := agent.NewSupervisor(childAgent, b.RT.AgentFactory, sessLog)
	sv.WireSpawnFns(b.RT.AllTemplates)
	sv.SetGroup(group)

	// Register supervisor-scoped inspect_agent.
	if err := childAgent.RegisterTool(tools.NewInspectAgentTool(agent.SupervisorInspectQuery(sv))); err != nil {
		sessLog.Warn(logger.CatActor, "register inspect_agent for cron L2 failed",
			"name", leaderTmpl.ID, "err", err.Error())
	}

	// Timeline writer — use the caller-supplied cronLogDir.
	tl, err := b.newTimelineWriter(cronLogDir, sessLog)
	if err != nil {
		_ = childAgent.Stop(5 * time.Second)
		b.RT.AgentRegistry.Unregister(childAgent.InstanceID)
		return nil, fmt.Errorf("build L2 cron timeline writer: %w", err)
	}

	// Context window model config.
	effectiveCW := childAgent.Def.ContextWindow
	if effectiveCW <= 0 {
		effectiveCW = agent.DefaultContextWindow
	}

	// Summary hook (timeline-only).
	summaryHook := func(_ []ctxwin.SummarySegment, finalSummary string) {
		if finalSummary == "" {
			return
		}
		if err := tl.AppendControl(&timeline.ControlPayload{
			Action:  "summary",
			Reason:  "auto_compact",
			Content: finalSummary,
		}); err != nil {
			sessLog.Error(logger.CatActor, "timeline summary append failed",
				"err", err.Error(), "agent_id", agentID)
		}
	}

	// Push hook: writes every message to timeline.
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
		var files []timeline.FileAttachment
		for _, f := range msg.Files {
			files = append(files, timeline.FileAttachment{
				Name: f.Name,
				Path: f.Path,
			})
		}
		if err := tl.AppendMessage(&timeline.MessagePayload{
			Role:             string(msg.Role),
			Content:          msg.Content,
			Timestamp:        msg.Timestamp.Format(time.RFC3339Nano),
			ReasoningContent: msg.ReasoningContent,
			Name:             msg.Name,
			ToolCallID:       msg.ToolCallID,
			ToolCalls:        toolCalls,
			Files:            files,
			IsEphemeral:      msg.IsEphemeral,
			AgentID:          agentID,
			ExposeTimestamp:  msg.ExposeTimestamp,
			TemporalParts:    timelineTemporalParts(msg.TemporalParts),
		}); err != nil {
			sessLog.Error(logger.CatActor, "timeline append failed",
				"err", err.Error(), "role", string(msg.Role), "agent_id", agentID)
		}
	}

	cw := ctxwin.NewContextWindow(
		effectiveCW,
		effectiveCW/10,
		0,
		b.RT.Tokenizer,
		ctxwin.WithPushHook(pushHook),
		ctxwin.WithSummaryHook(summaryHook),
		ctxwin.WithCompactor(b.RT.Compactor),
	)

	// Push L2 system prompt without writing to timeline.
	cw.SetReplayMode(true)
	if childAgent.Def.SystemPrompt != "" {
		cw.Push(ctxwin.RoleSystem, childAgent.Def.SystemPrompt)
	}
	cw.SetReplayMode(false)

	// Build the Session — no idle reaper, no router, no level file, no supervisor
	// registration, no memory hooks. Cron sessions are short-lived.
	sessLogger := sessLog.Child()
	s := NewSession(sessionID, group, childAgent, cw, tl, sessLogger)
	s.Supervisor = sv

	sessLog.Info(logger.CatActor, "BuildL2ForCron: session created",
		"target_id", sessionID,
		"group", group,
		"agent_id", agentID,
	)

	return s, nil
}
