package server

import (
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/xiaobaitu/soloqueue/internal/agent"
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

// ─── Response Types ─────────────────────────────────────────────────────────

// RuntimeStatusResponse is the JSON response for GET /api/runtime.
type RuntimeStatusResponse struct {
	Phase             string `json:"phase"`
	PromptTokens      int64  `json:"prompt_tokens"`
	OutputTokens      int64  `json:"output_tokens"`
	CacheHitTokens    int64  `json:"cache_hit_tokens"`
	CacheMissTokens   int64  `json:"cache_miss_tokens"`
	ContextPct        int    `json:"context_pct"`
	CurrentIter       int    `json:"current_iter"`
	ContentDeltas     int    `json:"content_deltas"`
	ActiveDelegations int    `json:"active_delegations"`
	TotalAgents       int    `json:"total_agents"`
	RunningAgents     int    `json:"running_agents"`
	IdleAgents        int    `json:"idle_agents"`
	TotalErrors       int    `json:"total_errors"`
	HTTPAddr          string `json:"http_addr"`
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
	workDir := ".soloqueue" // This should be configured properly
	rolesDir := filepath.Join(workDir, "roles")

	soulPath := filepath.Join(rolesDir, "soul.md")
	rulesPath := filepath.Join(rolesDir, "rules.md")

	soul, _ := os.ReadFile(soulPath)
	rules, _ := os.ReadFile(rulesPath)

	m.writeJSON(w, http.StatusOK, AgentProfileResponse{
		Soul:  string(soul),
		Rules: string(rules),
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
