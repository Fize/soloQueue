package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/prompt"
)

// ─── RuntimeMetrics ─────────────────────────────────────────────────────────

// RuntimeMetrics holds live runtime metrics that the TUI writes and the API reads.
// Fields are accessed concurrently — use RWMutex.
type RuntimeMetrics struct {
	mu                sync.RWMutex
	Phase             string
	PromptTokens      int64
	OutputTokens      int64
	CacheHitTokens    int64
	CacheMissTokens   int64
	ContextPct        int
	CurrentIter       int
	ContentDeltas     int
	ActiveDelegations int
	HTTPAddr          string
	onChange          func() // called (under lock) after every setter; notifies Hub

	agentStreamsMu sync.RWMutex
	agentStreams   map[string]*AgentStreamState // instanceID → stream state
	agentCancels   map[string]func()            // instanceID → Watch cancel
}

// SetOnChange sets the callback invoked after every state change.
// Must be called before any setter. The callback is invoked under the write lock.
func (rm *RuntimeMetrics) SetOnChange(fn func()) {
	rm.mu.Lock()
	rm.onChange = fn
	rm.mu.Unlock()
}

// SetPhase updates the phase field (thread-safe).
func (rm *RuntimeMetrics) SetPhase(phase string) {
	rm.mu.Lock()
	rm.Phase = phase
	if rm.onChange != nil {
		rm.onChange()
	}
	rm.mu.Unlock()
}

// SetTokens updates all token counters (thread-safe).
func (rm *RuntimeMetrics) SetTokens(prompt, output, cacheHit, cacheMiss int64) {
	rm.mu.Lock()
	rm.PromptTokens = prompt
	rm.OutputTokens = output
	rm.CacheHitTokens = cacheHit
	rm.CacheMissTokens = cacheMiss
	if rm.onChange != nil {
		rm.onChange()
	}
	rm.mu.Unlock()
}

// SetContext updates context percentage (thread-safe).
func (rm *RuntimeMetrics) SetContext(pct int) {
	rm.mu.Lock()
	rm.ContextPct = pct
	if rm.onChange != nil {
		rm.onChange()
	}
	rm.mu.Unlock()
}

// SetIter updates current iteration (thread-safe).
func (rm *RuntimeMetrics) SetIter(iter int) {
	rm.mu.Lock()
	rm.CurrentIter = iter
	if rm.onChange != nil {
		rm.onChange()
	}
	rm.mu.Unlock()
}

// SetContentDeltas updates the content deltas counter (thread-safe).
func (rm *RuntimeMetrics) SetContentDeltas(n int) {
	rm.mu.Lock()
	rm.ContentDeltas = n
	if rm.onChange != nil {
		rm.onChange()
	}
	rm.mu.Unlock()
}

// SetActiveDelegations updates the active delegations count (thread-safe).
func (rm *RuntimeMetrics) SetActiveDelegations(n int) {
	rm.mu.Lock()
	rm.ActiveDelegations = n
	if rm.onChange != nil {
		rm.onChange()
	}
	rm.mu.Unlock()
}

// Snapshot returns a consistent read of all metrics fields.
func (rm *RuntimeMetrics) Snapshot() (phase string, promptTokens, outputTokens, cacheHit, cacheMiss int64, contextPct, currentIter, contentDeltas, activeDelegations int, httpAddr string) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.Phase, rm.PromptTokens, rm.OutputTokens, rm.CacheHitTokens, rm.CacheMissTokens,
		rm.ContextPct, rm.CurrentIter, rm.ContentDeltas, rm.ActiveDelegations, rm.HTTPAddr
}

// ─── Agent Stream State ─────────────────────────────────────────────────────

// ToolCallState is a JSON-serializable snapshot of a tool call in progress.
type ToolCallState struct {
	CallID     string `json:"call_id"`
	Name       string `json:"name"`
	Args       string `json:"args"`
	Result     string `json:"result"`
	Error      string `json:"error"`
	Done       bool   `json:"done"`
	DurationMs int64  `json:"duration_ms"`
}

// AgentStreamState holds the live streaming output for one agent.
type AgentStreamState struct {
	AgentID    string          `json:"agent_id"`
	Processing bool            `json:"processing"`
	Thinking   string          `json:"thinking"`
	Content    string          `json:"content"`
	ToolCalls  []ToolCallState `json:"tool_calls"`
	Iteration  int             `json:"iteration"`
	Error      string          `json:"error,omitempty"`
}

// StartAgentWatch subscribes to an agent's Watch() and starts a goroutine that
// updates the stream state in real-time. Lazily initializes the stream maps.
func (rm *RuntimeMetrics) StartAgentWatch(a *agent.Agent) {
	if a == nil || a.InstanceID == "" {
		return
	}
	ch, cancel := a.Watch()

	rm.agentStreamsMu.Lock()
	if rm.agentStreams == nil {
		rm.agentStreams = make(map[string]*AgentStreamState)
		rm.agentCancels = make(map[string]func())
	}
	rm.agentCancels[a.InstanceID] = cancel
	rm.agentStreamsMu.Unlock()

	go func() {
		for ev := range ch {
			rm.updateAgentStream(a.InstanceID, ev)
		}
	}()
}

// StopAgentWatch cancels the Watch subscription. The accumulated stream state
// is preserved so the Web UI keeps showing historical output.
func (rm *RuntimeMetrics) StopAgentWatch(instanceID string) {
	var notify func()
	rm.agentStreamsMu.Lock()
	cancel, ok := rm.agentCancels[instanceID]
	if ok {
		cancel()
		delete(rm.agentCancels, instanceID)
	}
	// Preserve stream state for historical display; do NOT delete.
	// New StartAgentWatch for the same instanceID will overwrite it.
	if s, ok := rm.agentStreams[instanceID]; ok {
		s.Processing = false
	}
	notify = rm.onChange
	rm.agentStreamsMu.Unlock()
	if notify != nil {
		notify()
	}
}

// updateAgentStream processes a single AgentEvent and updates the
// corresponding agent's stream state. Triggers onChange on every event.
func (rm *RuntimeMetrics) updateAgentStream(instanceID string, ev agent.AgentEvent) {
	var notify func()

	rm.agentStreamsMu.Lock()
	if rm.agentStreams == nil {
		rm.agentStreamsMu.Unlock()
		return
	}
	s := rm.agentStreams[instanceID]
	if s == nil {
		s = &AgentStreamState{
			AgentID:   instanceID,
			ToolCalls: []ToolCallState{},
		}
		rm.agentStreams[instanceID] = s
	}

	switch e := ev.(type) {
	case agent.ContentDeltaEvent:
		if !s.Processing {
			// New turn: reset content & tool calls but preserve thinking
			// (which may have been set by preceding ReasoningDeltaEvent)
			s.Content = ""
			s.ToolCalls = []ToolCallState{}
			s.Error = ""
		}
		s.Content += e.Delta
		s.Processing = true

	case agent.ReasoningDeltaEvent:
		if !s.Processing {
			// New turn: reset thinking only (content/tool calls preserved
			// for ContentDeltaEvent to handle with same Processing check)
			s.Thinking = ""
		}
		s.Thinking += e.Delta
		s.Processing = true

	case agent.ToolExecStartEvent:
		s.ToolCalls = append(s.ToolCalls, ToolCallState{
			CallID: e.CallID,
			Name:   e.Name,
			Args:   e.Args,
		})

	case agent.ToolExecDoneEvent:
		for i := range s.ToolCalls {
			if s.ToolCalls[i].CallID == e.CallID {
				s.ToolCalls[i].Done = true
				s.ToolCalls[i].Result = e.Result
				s.ToolCalls[i].DurationMs = e.Duration.Milliseconds()
				if e.Err != nil {
					s.ToolCalls[i].Error = e.Err.Error()
				}
				break
			}
		}

	case agent.IterationDoneEvent:
		s.Iteration = e.Iter

	case agent.DoneEvent:
		s.Processing = false

	case agent.ErrorEvent:
		s.Processing = false
		s.Error = e.Err.Error()
	}

	// Release lock before calling onChange to avoid lock-ordering issues;
	// onChange triggers Hub.Notify which eventually reads rm fields.
	notify = rm.onChange
	rm.agentStreamsMu.Unlock()

	if notify != nil {
		notify()
	}
}

// AgentStreams returns a snapshot of all agents' stream states.
func (rm *RuntimeMetrics) AgentStreams() map[string]*AgentStreamState {
	rm.agentStreamsMu.RLock()
	defer rm.agentStreamsMu.RUnlock()
	out := make(map[string]*AgentStreamState, len(rm.agentStreams))
	for id, s := range rm.agentStreams {
		cp := *s
		out[id] = &cp
	}
	return out
}

// ─── Response Types ─────────────────────────────────────────────────────────

// RuntimeStatusResponse is the JSON response for GET /api/runtime.
type RuntimeStatusResponse struct {
	Phase             string                       `json:"phase"`
	PromptTokens      int64                        `json:"prompt_tokens"`
	OutputTokens      int64                        `json:"output_tokens"`
	CacheHitTokens    int64                        `json:"cache_hit_tokens"`
	CacheMissTokens   int64                        `json:"cache_miss_tokens"`
	ContextPct        int                          `json:"context_pct"`
	CurrentIter       int                          `json:"current_iter"`
	ContentDeltas     int                          `json:"content_deltas"`
	ActiveDelegations int                          `json:"active_delegations"`
	TotalAgents       int                          `json:"total_agents"`
	RunningAgents     int                          `json:"running_agents"`
	IdleAgents        int                          `json:"idle_agents"`
	TotalErrors       int                          `json:"total_errors"`
	HTTPAddr          string                       `json:"http_addr"`
	AgentStreams      map[string]*AgentStreamState `json:"agent_streams"`
}

// AgentInfoResponse is a single agent in the list.
type AgentInfoResponse struct {
	ID                 string `json:"id"`
	InstanceID         string `json:"instance_id"`
	Name               string `json:"name"`
	State              string `json:"state"`
	ModelID            string `json:"model_id"`
	Group              string `json:"group"`
	IsLeader           bool   `json:"is_leader"`
	TaskLevel          string `json:"task_level"`
	ErrorCount         int    `json:"error_count"`
	LastError          string `json:"last_error"`
	PendingDelegations int    `json:"pending_delegations"`
	MailboxHigh        int    `json:"mailbox_high"`
	MailboxNormal      int    `json:"mailbox_normal"`
}

// SupervisorInfoResponse groups agents into teams.
type SupervisorInfoResponse struct {
	Group       string   `json:"group"`
	LeaderID    string   `json:"leader_id"`
	ChildrenIDs []string `json:"children_ids"`
}

// AgentListResponse is the response for GET /api/agents.
type AgentListResponse struct {
	Agents      []AgentInfoResponse      `json:"agents"`
	Supervisors []SupervisorInfoResponse `json:"supervisors"`
}

// ─── Handlers ───────────────────────────────────────────────────────────────

// AgentProfileResponse is the JSON response for GET /api/agents/{id}/profile.
type AgentProfileResponse struct {
	Soul  string `json:"soul"`
	Rules string `json:"rules"`
}

// AgentConfigResponse is the JSON response for GET /api/agents/{id}/config.
type AgentConfigResponse struct {
	RawConfig    string   `json:"raw_config"`
	SystemPrompt string   `json:"system_prompt"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Model        string   `json:"model"`
	Group        string   `json:"group"`
	IsLeader     bool     `json:"is_leader"`
	MCPServers   []string `json:"mcp_servers"`
}

// AgentTemplateResponse is a single agent template in the team list.
type AgentTemplateResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	IsLeader    bool   `json:"is_leader"`
	Group       string `json:"group"`
	ModelID     string `json:"model_id"`
}

// TeamInfoResponse is a single team with its agents.
type TeamInfoResponse struct {
	Name        string                  `json:"name"`
	Description string                  `json:"description"`
	Agents      []AgentTemplateResponse `json:"agents"`
}

// TeamListResponse is the response for GET /api/teams.
type TeamListResponse struct {
	Teams []TeamInfoResponse `json:"teams"`
}

// handleListTeams returns all teams and their agent templates.
// GET /api/teams
func (m *Mux) handleListTeams(w http.ResponseWriter, _ *http.Request) {
	if m.templates == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "templates not available"})
		return
	}

	// Group templates by group name
	teamMap := make(map[string]*TeamInfoResponse)

	for _, tmpl := range m.templates {
		groupName := tmpl.Group
		if groupName == "" {
			groupName = "Default"
		}

		if _, ok := teamMap[groupName]; !ok {
			teamMap[groupName] = &TeamInfoResponse{
				Name:        groupName,
				Description: "",
				Agents:      []AgentTemplateResponse{},
			}

			// Get group description if available
			if group, ok := m.groups[groupName]; ok {
				teamMap[groupName].Description = group.Body
			}
		}

		teamMap[groupName].Agents = append(teamMap[groupName].Agents, AgentTemplateResponse{
			ID:          tmpl.ID,
			Name:        tmpl.Name,
			Description: tmpl.Description,
			IsLeader:    tmpl.IsLeader,
			Group:       tmpl.Group,
			ModelID:     tmpl.ModelID,
		})
	}

	// Convert map to slice
	teams := make([]TeamInfoResponse, 0, len(teamMap))
	for _, team := range teamMap {
		teams = append(teams, *team)
	}

	m.writeJSON(w, http.StatusOK, TeamListResponse{Teams: teams})
}

// handleGetAgentProfile returns the soul.md and rules.md content for the main agent.
// GET /api/agents/{id}/profile
func (m *Mux) handleGetAgentProfile(w http.ResponseWriter, r *http.Request) {
	rolesDir := filepath.Join(m.workDir, "roles")

	soulPath := filepath.Join(rolesDir, "soul.md")
	rulesPath := filepath.Join(rolesDir, "rules.md")

	soul, _ := os.ReadFile(soulPath)
	rules, _ := os.ReadFile(rulesPath)

	m.writeJSON(w, http.StatusOK, AgentProfileResponse{
		Soul:  string(soul),
		Rules: string(rules),
	})
}

// UpdateAgentProfileRequest is the request body for PUT /api/agents/{id}/profile.
type UpdateAgentProfileRequest struct {
	Soul  *string `json:"soul,omitempty"`
	Rules *string `json:"rules,omitempty"`
}

// handleUpdateAgentProfile updates the soul.md and/or rules.md content for the main agent.
// PUT /api/agents/{id}/profile
func (m *Mux) handleUpdateAgentProfile(w http.ResponseWriter, r *http.Request) {
	var req UpdateAgentProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if req.Soul == nil && req.Rules == nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "at least one of soul or rules must be provided"})
		return
	}

	cfg := &prompt.PromptConfig{
		RolesDir: filepath.Join(m.workDir, "roles"),
	}

	if req.Soul != nil {
		if err := cfg.WriteSoulContent(*req.Soul); err != nil {
			m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}

	if req.Rules != nil {
		if err := cfg.WriteRulesContent(*req.Rules); err != nil {
			m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}

	// Rebuild the system prompt so changes take effect on the next interaction.
	if m.rebuildPrompt != nil {
		if err := m.rebuildPrompt(); err != nil {
			m.log.Warn("failed to rebuild system prompt after profile update", "err", err)
		}
	}

	// Return the updated profile
	rolesDir := filepath.Join(m.workDir, "roles")
	soul, _ := os.ReadFile(filepath.Join(rolesDir, "soul.md"))
	rules, _ := os.ReadFile(filepath.Join(rolesDir, "rules.md"))

	m.writeJSON(w, http.StatusOK, AgentProfileResponse{
		Soul:  string(soul),
		Rules: string(rules),
	})
}

// handleGetAgentConfig returns the YAML frontmatter and markdown body from an
// agent's .md file in the agents directory.
// GET /api/agents/{id}/config
func (m *Mux) handleGetAgentConfig(w http.ResponseWriter, r *http.Request) {
	if m.agentsDir == "" {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "agents directory not configured"})
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "agent id is required"})
		return
	}

	path := filepath.Join(m.agentsDir, id+".md")
	af, err := prompt.ParseAgentFile(path)
	if err != nil {
		af, err = findAgentFileByName(m.agentsDir, id)
		if err != nil {
			m.writeJSON(w, http.StatusNotFound, map[string]string{"error": "agent file not found: " + id})
			return
		}
	}

	m.writeJSON(w, http.StatusOK, AgentConfigResponse{
		RawConfig:    serializeFrontmatter(af.Frontmatter),
		SystemPrompt: af.Body,
		Name:         af.Frontmatter.Name,
		Description:  af.Frontmatter.Description,
		Model:        af.Frontmatter.Model,
		Group:        af.Frontmatter.Group,
		IsLeader:     af.Frontmatter.IsLeader,
		MCPServers:   af.Frontmatter.MCPServers,
	})
}

// findAgentFileByName scans the agents directory for a .md file whose frontmatter
// "name" field matches the given name. Returns the parsed file or an error.
func findAgentFileByName(agentsDir, name string) (*prompt.AgentFile, error) {
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		af, err := prompt.ParseAgentFile(filepath.Join(agentsDir, entry.Name()))
		if err != nil {
			continue
		}
		if af.Frontmatter.Name == name {
			return af, nil
		}
	}
	return nil, fmt.Errorf("no agent file with name %q found in %s", name, agentsDir)
}

// serializeFrontmatter serializes AgentFrontmatter back to a YAML string for display.
func serializeFrontmatter(fm prompt.AgentFrontmatter) string {
	data, err := yaml.Marshal(fm)
	if err != nil {
		return fmt.Sprintf("# error serializing frontmatter: %v", err)
	}
	return strings.TrimSpace(string(data))
}

// UpdateAgentConfigRequest is the request body for PUT /api/agents/{id}/config.
type UpdateAgentConfigRequest struct {
	RawConfig    *string `json:"raw_config,omitempty"`
	SystemPrompt *string `json:"system_prompt,omitempty"`
}

// handleUpdateAgentConfig updates an agent's .md file (frontmatter and/or body).
// PUT /api/agents/{id}/config
func (m *Mux) handleUpdateAgentConfig(w http.ResponseWriter, r *http.Request) {
	if m.agentsDir == "" {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "agents directory not configured"})
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "agent id is required"})
		return
	}

	var req UpdateAgentConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if req.RawConfig == nil && req.SystemPrompt == nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "at least one of raw_config or system_prompt must be provided"})
		return
	}

	// Find the .md file
	path := filepath.Join(m.agentsDir, id+".md")
	af, err := prompt.ParseAgentFile(path)
	if err != nil {
		af, err = findAgentFileByName(m.agentsDir, id)
		if err != nil {
			m.writeJSON(w, http.StatusNotFound, map[string]string{"error": "agent file not found: " + id})
			return
		}
		path = filepath.Join(m.agentsDir, af.Frontmatter.Name+".md")
	}

	// Merge frontmatter if raw_config provided
	if req.RawConfig != nil {
		var fm prompt.AgentFrontmatter
		if err := yaml.Unmarshal([]byte(*req.RawConfig), &fm); err != nil {
			m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid yaml in raw_config: " + err.Error()})
			return
		}
		af.Frontmatter = fm
	}

	// Merge body if system_prompt provided
	if req.SystemPrompt != nil {
		af.Body = *req.SystemPrompt
	}

	// Serialize back to .md file
	fmBytes, err := yaml.Marshal(af.Frontmatter)
	if err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to serialize frontmatter: " + err.Error()})
		return
	}

	content := "---\n" + string(fmBytes) + "---\n\n" + af.Body
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to write agent file: " + err.Error()})
		return
	}

	// Rebuild system prompt so changes take effect
	if m.rebuildPrompt != nil {
		if err := m.rebuildPrompt(); err != nil {
			m.log.Warn("failed to rebuild system prompt after agent config update", "err", err)
		}
	}

	m.writeJSON(w, http.StatusOK, AgentConfigResponse{
		RawConfig:    serializeFrontmatter(af.Frontmatter),
		SystemPrompt: af.Body,
		Name:         af.Frontmatter.Name,
		Description:  af.Frontmatter.Description,
		Model:        af.Frontmatter.Model,
		Group:        af.Frontmatter.Group,
		IsLeader:     af.Frontmatter.IsLeader,
		MCPServers:   af.Frontmatter.MCPServers,
	})
}

// ─── Public Builders (shared by REST handlers and WebSocket Hub) ─────────────

// buildRuntimeStatus constructs a RuntimeStatusResponse from the current metrics
// and agent counts. Returns nil if runtimeMetrics is nil.
func (m *Mux) buildRuntimeStatus() *RuntimeStatusResponse {
	if m.runtimeMetrics == nil {
		return nil
	}

	phase, promptTokens, outputTokens, cacheHit, cacheMiss,
		contextPct, currentIter, contentDeltas, activeDelegations, httpAddr := m.runtimeMetrics.Snapshot()

	// Count agents from registry and supervisors.
	var totalAgents, runningAgents, idleAgents, totalErrors int
	if m.registry != nil {
		allAgents := m.collectAllAgents()
		totalAgents = len(allAgents)
		for _, a := range allAgents {
			switch a.State() {
			case agent.StateProcessing:
				runningAgents++
			case agent.StateIdle:
				idleAgents++
			}
			if ec := a.ErrorCount(); ec > 0 {
				totalErrors += int(ec)
			}
		}
	}

	return &RuntimeStatusResponse{
		Phase:             phase,
		PromptTokens:      promptTokens,
		OutputTokens:      outputTokens,
		CacheHitTokens:    cacheHit,
		CacheMissTokens:   cacheMiss,
		ContextPct:        contextPct,
		CurrentIter:       currentIter,
		ContentDeltas:     contentDeltas,
		ActiveDelegations: activeDelegations,
		TotalAgents:       totalAgents,
		RunningAgents:     runningAgents,
		IdleAgents:        idleAgents,
		TotalErrors:       totalErrors,
		HTTPAddr:          httpAddr,
		AgentStreams:      m.runtimeMetrics.AgentStreams(),
	}
}

// buildAgentList constructs an AgentListResponse from the registry and supervisors.
// Returns nil if registry is nil.
func (m *Mux) buildAgentList() *AgentListResponse {
	if m.registry == nil {
		return nil
	}

	registered := m.registry.List()
	var supervisors []*agent.Supervisor
	if m.supervisorsFn != nil {
		supervisors = m.supervisorsFn()
	}

	// Build agent group lookup from supervisors.
	agentGroup := make(map[string]string)
	agentLeader := make(map[string]bool)
	for _, sv := range supervisors {
		if sv == nil {
			continue
		}
		group := sv.Group()
		if a := sv.Agent(); a != nil {
			agentGroup[a.InstanceID] = group
			agentLeader[a.InstanceID] = true
		}
		for _, child := range sv.Children() {
			agentGroup[child.InstanceID] = group
		}
	}

	// Build agent info responses.
	agents := make([]AgentInfoResponse, 0, len(registered))
	for _, a := range registered {
		high, normal := a.MailboxDepth()
		info := AgentInfoResponse{
			ID:                 a.Def.ID,
			InstanceID:         a.InstanceID,
			Name:               a.Def.Name,
			State:              a.State().String(),
			ModelID:            a.EffectiveModelID(),
			Group:              agentGroup[a.InstanceID],
			IsLeader:           agentLeader[a.InstanceID],
			TaskLevel:          a.EffectiveTaskLevel(),
			ErrorCount:         int(a.ErrorCount()),
			LastError:          a.LastError(),
			PendingDelegations: a.PendingDelegations(),
			MailboxHigh:        high,
			MailboxNormal:      normal,
		}
		agents = append(agents, info)
	}

	// Also include L3 children that may not be in the registry.
	registeredIDs := make(map[string]bool, len(registered))
	for _, a := range registered {
		registeredIDs[a.InstanceID] = true
	}
	for _, sv := range supervisors {
		if sv == nil {
			continue
		}
		for _, child := range sv.Children() {
			if registeredIDs[child.InstanceID] {
				continue
			}
			high, normal := child.MailboxDepth()
			info := AgentInfoResponse{
				ID:                 child.Def.ID,
				InstanceID:         child.InstanceID,
				Name:               child.Def.Name,
				State:              child.State().String(),
				ModelID:            child.EffectiveModelID(),
				Group:              agentGroup[child.InstanceID],
				IsLeader:           false,
				TaskLevel:          child.EffectiveTaskLevel(),
				ErrorCount:         int(child.ErrorCount()),
				LastError:          child.LastError(),
				PendingDelegations: child.PendingDelegations(),
				MailboxHigh:        high,
				MailboxNormal:      normal,
			}
			agents = append(agents, info)
		}
	}

	// Build supervisor info responses.
	svInfos := make([]SupervisorInfoResponse, 0, len(supervisors))
	for _, sv := range supervisors {
		if sv == nil {
			continue
		}
		leaderID := ""
		if a := sv.Agent(); a != nil {
			leaderID = a.InstanceID
		}
		children := sv.Children()
		childIDs := make([]string, 0, len(children))
		for _, child := range children {
			childIDs = append(childIDs, child.InstanceID)
		}
		svInfos = append(svInfos, SupervisorInfoResponse{
			Group:       sv.Group(),
			LeaderID:    leaderID,
			ChildrenIDs: childIDs,
		})
	}

	return &AgentListResponse{
		Agents:      agents,
		Supervisors: svInfos,
	}
}

// collectAllAgents returns all unique agents from registry + supervisor children.
func (m *Mux) collectAllAgents() []*agent.Agent {
	seen := make(map[string]bool)
	var out []*agent.Agent

	if m.registry != nil {
		for _, a := range m.registry.List() {
			if !seen[a.InstanceID] {
				seen[a.InstanceID] = true
				out = append(out, a)
			}
		}
	}

	if m.supervisorsFn != nil {
		for _, sv := range m.supervisorsFn() {
			if sv == nil {
				continue
			}
			if a := sv.Agent(); a != nil && !seen[a.InstanceID] {
				seen[a.InstanceID] = true
				out = append(out, a)
			}
			for _, child := range sv.Children() {
				if !seen[child.InstanceID] {
					seen[child.InstanceID] = true
					out = append(out, child)
				}
			}
		}
	}

	return out
}
