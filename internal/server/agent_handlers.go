package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/prompt"
)

// ─── RuntimeMetrics ─────────────────────────────────────────────────────────

// RuntimeMetrics holds live runtime metrics.
// Fields are accessed concurrently — use RWMutex.
type RuntimeMetrics struct {
	mu                sync.RWMutex
	Phase             string
	PromptTokens      int64
	OutputTokens      int64
	CacheHitTokens    int64
	CacheMissTokens   int64
	ContextPct        int
	CurrentTokens     int
	MaxTokens         int
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

// SetCtxwin updates context window metrics (current usage, max capacity, percentage).
// Thread-safe.
func (rm *RuntimeMetrics) SetCtxwin(cur, max int) {
	rm.mu.Lock()
	rm.CurrentTokens = cur
	rm.MaxTokens = max
	if max > 0 {
		rm.ContextPct = cur * 100 / max
	} else {
		rm.ContextPct = 0
	}
	if rm.onChange != nil {
		rm.onChange()
	}
	rm.mu.Unlock()
}

// Snapshot returns a consistent read of all metrics fields.
func (rm *RuntimeMetrics) Snapshot() (phase string, promptTokens, outputTokens, cacheHit, cacheMiss int64, contextPct, currentTokens, maxTokens, currentIter, contentDeltas, activeDelegations int, httpAddr string) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.Phase, rm.PromptTokens, rm.OutputTokens, rm.CacheHitTokens, rm.CacheMissTokens,
		rm.ContextPct, rm.CurrentTokens, rm.MaxTokens, rm.CurrentIter, rm.ContentDeltas, rm.ActiveDelegations, rm.HTTPAddr
}

// ─── Agent Stream State ─────────────────────────────────────────────────────

// SegmentType indicates the kind of an ordered timeline segment.
type SegmentType string

const (
	SegThinking SegmentType = "thinking"
	SegContent  SegmentType = "content"
	SegToolCall SegmentType = "tool_call"
)

// Segment is a single ordered entry in the agent's output timeline.
// For thinking/content segments only Text is populated;
// for tool_call segments the tool-specific fields are used.
type Segment struct {
	Type SegmentType `json:"type"`

	// For thinking / content
	Text string `json:"text,omitempty"`

	// For tool_call
	CallID     string `json:"call_id,omitempty"`
	Name       string `json:"name,omitempty"`
	Args       string `json:"args,omitempty"`
	Result     string `json:"result,omitempty"`
	Error      string `json:"error,omitempty"`
	Done       bool   `json:"done"`
	DurationMs int64  `json:"duration_ms,omitempty"`
}

// AgentStreamState holds the live streaming output for one agent
// as an ordered timeline of segments.
type AgentStreamState struct {
	AgentID    string    `json:"agent_id"`
	Processing bool      `json:"processing"`
	Segments   []Segment `json:"segments"`
	Iteration  int       `json:"iteration"`
	Error      string    `json:"error,omitempty"`
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
	if oldCancel, ok := rm.agentCancels[a.InstanceID]; ok {
		oldCancel()
	}
	rm.agentCancels[a.InstanceID] = cancel
	rm.agentStreamsMu.Unlock()

	go func() {
		for ev := range ch {
			rm.updateAgentStream(a.InstanceID, ev)
		}
	}()
}

// StopAgentWatch cancels the Watch subscription and deletes the stream state.
func (rm *RuntimeMetrics) StopAgentWatch(instanceID string) {
	var notify func()
	rm.agentStreamsMu.Lock()
	cancel, ok := rm.agentCancels[instanceID]
	if ok {
		cancel()
		delete(rm.agentCancels, instanceID)
	}
	delete(rm.agentStreams, instanceID)
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
			AgentID:  instanceID,
			Segments: []Segment{},
		}
		rm.agentStreams[instanceID] = s
	}

	if !s.Processing {
		switch ev.(type) {
		case agent.DoneEvent, agent.ErrorEvent, agent.IterationDoneEvent:
			// No-op
		default:
			s.Segments = []Segment{}
			s.Error = ""
			s.Processing = true
		}
	}

	switch e := ev.(type) {
	case agent.ContentDeltaEvent:
		n := len(s.Segments)
		if n > 0 && s.Segments[n-1].Type == SegContent {
			s.Segments[n-1].Text += e.Delta
		} else {
			s.Segments = append(s.Segments, Segment{Type: SegContent, Text: e.Delta})
		}

	case agent.ReasoningDeltaEvent:
		n := len(s.Segments)
		if n > 0 && s.Segments[n-1].Type == SegThinking {
			s.Segments[n-1].Text += e.Delta
		} else {
			s.Segments = append(s.Segments, Segment{Type: SegThinking, Text: e.Delta})
		}

	case agent.ToolExecStartEvent:
		s.Segments = append(s.Segments, Segment{
			Type:   SegToolCall,
			CallID: e.CallID,
			Name:   e.Name,
			Args:   e.Args,
		})

	case agent.ToolExecDoneEvent:
		for i := range s.Segments {
			if s.Segments[i].Type == SegToolCall && s.Segments[i].CallID == e.CallID {
				s.Segments[i].Done = true
				s.Segments[i].Result = e.Result
				s.Segments[i].DurationMs = e.Duration.Milliseconds()
				if e.Err != nil {
					s.Segments[i].Error = e.Err.Error()
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

	notify = rm.onChange
	rm.agentStreamsMu.Unlock()

	// Update cumulative metrics under separate lock.
	rm.mu.Lock()
	switch e := ev.(type) {
	case agent.ContentDeltaEvent:
		rm.ContentDeltas++
	case agent.IterationDoneEvent:
		rm.PromptTokens += int64(e.Usage.PromptTokens)
		rm.OutputTokens += int64(e.Usage.CompletionTokens)
		rm.CacheHitTokens += int64(e.Usage.PromptCacheHitTokens)
		rm.CacheMissTokens += int64(e.Usage.PromptCacheMissTokens)
		if e.Iter > rm.CurrentIter {
			rm.CurrentIter = e.Iter
		}
	}
	rm.mu.Unlock()

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
		if len(s.Segments) > 0 {
			cp.Segments = make([]Segment, len(s.Segments))
			copy(cp.Segments, s.Segments)
		}
		out[id] = &cp
	}
	return out
}

// SessionRuntimeInfo holds per-session runtime state for WebSocket state broadcasts.
type SessionRuntimeInfo struct {
	SessionID   string `json:"session_id"`
	RequestID   string `json:"request_id,omitempty"`
	State       string `json:"state"` // "idle", "starting", "streaming", "delegating", "cancelling"
	Revision    uint64 `json:"revision"`
	CtxwinUsed  int    `json:"ctxwin_used"`
	CtxwinLimit int    `json:"ctxwin_limit"`
	Delegating  bool   `json:"delegating"`
}

// RuntimeStatusResponse is the JSON response for GET /api/runtime.
type RuntimeStatusResponse struct {
	Phase             string                        `json:"phase"`
	PromptTokens      int64                         `json:"prompt_tokens"`
	OutputTokens      int64                         `json:"output_tokens"`
	CacheHitTokens    int64                         `json:"cache_hit_tokens"`
	CacheMissTokens   int64                         `json:"cache_miss_tokens"`
	ContextPct        int                           `json:"context_pct"`
	CurrentTokens     int                           `json:"current_tokens"`
	MaxTokens         int                           `json:"max_tokens"`
	CurrentIter       int                           `json:"current_iter"`
	ContentDeltas     int                           `json:"content_deltas"`
	ActiveDelegations int                           `json:"active_delegations"`
	TotalAgents       int                           `json:"total_agents"`
	RunningAgents     int                           `json:"running_agents"`
	IdleAgents        int                           `json:"idle_agents"`
	TotalErrors       int                           `json:"total_errors"`
	HTTPAddr          string                        `json:"http_addr"`
	AgentStreams      map[string]*AgentStreamState  `json:"agent_streams"`
	Sessions          map[string]SessionRuntimeInfo `json:"sessions,omitempty"`
}

// AgentInfoResponse is a single agent in the list.
type AgentInfoResponse struct {
	ID                 string `json:"id"`
	InstanceID         string `json:"instance_id"`
	Name               string `json:"name"`
	State              string `json:"state"`
	ModelID            string `json:"model_id"`
	ProviderID         string `json:"provider_id"`
	Group              string `json:"group"`
	IsLeader           bool   `json:"is_leader"`
	TaskLevel          string `json:"task_level"`
	ThinkingEnabled    bool   `json:"thinking_enabled"`
	ReasoningEffort    string `json:"reasoning_effort"`
	LevelLocked        bool   `json:"level_locked"`
	LastLevel          string `json:"last_level"`
	ErrorCount         int    `json:"error_count"`
	LastError          string `json:"last_error"`
	PendingDelegations int    `json:"pending_delegations"`
	MailboxHigh        int    `json:"mailbox_high"`
	MailboxNormal      int    `json:"mailbox_normal"`
	IsQBot             bool   `json:"is_qbot"`
	Iteration          int    `json:"iteration"`
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
	Soul          string            `json:"soul"`
	Rules         string            `json:"rules"`
	Channels      map[string]string `json:"channels,omitempty"`
	NotifyChannel string            `json:"notify_channel,omitempty"`
}

// AgentConfigResponse is the JSON response for GET /api/agents/{id}/config.
type AgentConfigResponse struct {
	RawConfig     string            `json:"raw_config"`
	SystemPrompt  string            `json:"system_prompt"`
	Name          string            `json:"name"`
	Description   string            `json:"description"`
	Model         string            `json:"model"`
	Group         string            `json:"group"`
	IsLeader      bool              `json:"is_leader"`
	MCPServers    []string          `json:"mcp_servers"`
	Channels      map[string]string `json:"channels,omitempty"`
	NotifyChannel string            `json:"notify_channel,omitempty"`
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
	ID          string                  `json:"id"`
	Name        string                  `json:"name"`
	Description string                  `json:"description"`
	Agents      []AgentTemplateResponse `json:"agents"`
	CreatedAt   string                  `json:"created_at"`
	UpdatedAt   string                  `json:"updated_at"`
}

// TeamListResponse is the response for GET /api/teams.
type TeamListResponse struct {
	Teams []TeamInfoResponse `json:"teams"`
}

// handleGetAgentProfile returns the soul.md and rules.md content for the main agent.
// GET /api/agents/{id}/profile
func (m *Mux) handleGetAgentProfile(w http.ResponseWriter, r *http.Request) {
	rolesDir := filepath.Join(m.workDir, "persona", "roles")

	soulPath := filepath.Join(rolesDir, "soul.md")
	rulesPath := filepath.Join(rolesDir, "rules.md")

	soul, _ := os.ReadFile(soulPath)
	rules, _ := os.ReadFile(rulesPath)

	// Read L1 channel config from channels.yaml
	var channels map[string]string
	var notifyChannel string
	if chData, err := os.ReadFile(filepath.Join(rolesDir, "channels.yaml")); err == nil {
		var l1ch struct {
			Channels      map[string]string `yaml:"channels"`
			NotifyChannel string            `yaml:"notify_channel"`
		}
		if yaml.Unmarshal(chData, &l1ch) == nil {
			channels = l1ch.Channels
			notifyChannel = l1ch.NotifyChannel
		}
	}

	m.writeJSON(w, http.StatusOK, AgentProfileResponse{
		Soul:          string(soul),
		Rules:         string(rules),
		Channels:      channels,
		NotifyChannel: notifyChannel,
	})
}

// UpdateAgentProfileRequest is the request body for PUT /api/agents/{id}/profile.
type UpdateAgentProfileRequest struct {
	Soul          *string            `json:"soul,omitempty"`
	Rules         *string            `json:"rules,omitempty"`
	Channels      *map[string]string `json:"channels,omitempty"`
	NotifyChannel *string            `json:"notify_channel,omitempty"`
}

// handleUpdateAgentProfile updates the soul.md and/or rules.md content for the main agent.
// PUT /api/agents/{id}/profile
func (m *Mux) handleUpdateAgentProfile(w http.ResponseWriter, r *http.Request) {
	var req UpdateAgentProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if req.Soul == nil && req.Rules == nil && req.Channels == nil && req.NotifyChannel == nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "at least one of soul, rules, channels, or notify_channel must be provided"})
		return
	}

	cfg := &prompt.PromptConfig{
		RolesDir: filepath.Join(m.workDir, "persona", "roles"),
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

	// Write L1 channel config to channels.yaml
	if req.Channels != nil || req.NotifyChannel != nil {
		chPath := filepath.Join(m.workDir, "persona", "roles", "channels.yaml")
		// Read existing config to merge
		var l1ch struct {
			Channels      map[string]string `yaml:"channels"`
			NotifyChannel string            `yaml:"notify_channel"`
		}
		if data, err := os.ReadFile(chPath); err == nil {
			yaml.Unmarshal(data, &l1ch)
		}
		if l1ch.Channels == nil {
			l1ch.Channels = make(map[string]string)
		}
		if req.Channels != nil {
			l1ch.Channels = *req.Channels
		}
		if req.NotifyChannel != nil {
			l1ch.NotifyChannel = *req.NotifyChannel
		}
		out, err := yaml.Marshal(l1ch)
		if err != nil {
			m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to marshal channels: " + err.Error()})
			return
		}
		if err := os.WriteFile(chPath, out, 0644); err != nil {
			m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to write channels.yaml: " + err.Error()})
			return
		}
	}

	// Rebuild the system prompt so changes take effect on the next interaction.
	if m.rebuildPrompt != nil {
		if err := m.rebuildPrompt(); err != nil {
			m.log.Warn("failed to rebuild system prompt after profile update", "err", err.Error())
		}
	}

	// Return the updated profile
	rolesDir := filepath.Join(m.workDir, "persona", "roles")
	soul, _ := os.ReadFile(filepath.Join(rolesDir, "soul.md"))
	rules, _ := os.ReadFile(filepath.Join(rolesDir, "rules.md"))

	var channels map[string]string
	var notifyChannel string
	if chData, err := os.ReadFile(filepath.Join(rolesDir, "channels.yaml")); err == nil {
		var l1ch struct {
			Channels      map[string]string `yaml:"channels"`
			NotifyChannel string            `yaml:"notify_channel"`
		}
		if yaml.Unmarshal(chData, &l1ch) == nil {
			channels = l1ch.Channels
			notifyChannel = l1ch.NotifyChannel
		}
	}

	m.writeJSON(w, http.StatusOK, AgentProfileResponse{
		Soul:          string(soul),
		Rules:         string(rules),
		Channels:      channels,
		NotifyChannel: notifyChannel,
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
		RawConfig:     serializeFrontmatter(af.Frontmatter),
		SystemPrompt:  af.Body,
		Name:          af.Frontmatter.Name,
		Description:   af.Frontmatter.Description,
		Model:         af.Frontmatter.Model,
		Group:         af.Frontmatter.Group,
		IsLeader:      af.Frontmatter.IsLeader,
		MCPServers:    af.Frontmatter.MCPServers,
		Channels:      af.Frontmatter.Channels,
		NotifyChannel: af.Frontmatter.NotifyChannel,
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
	RawConfig     *string            `json:"raw_config,omitempty"`
	SystemPrompt  *string            `json:"system_prompt,omitempty"`
	Channels      *map[string]string `json:"channels,omitempty"`
	NotifyChannel *string            `json:"notify_channel,omitempty"`
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

	if req.RawConfig == nil && req.SystemPrompt == nil && req.Channels == nil && req.NotifyChannel == nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "at least one of raw_config, system_prompt, channels, or notify_channel must be provided"})
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

	// Merge channels/notify_channel if provided
	if req.Channels != nil {
		af.Frontmatter.Channels = *req.Channels
	}
	if req.NotifyChannel != nil {
		af.Frontmatter.NotifyChannel = *req.NotifyChannel
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
			m.log.Warn("failed to rebuild system prompt after agent config update", "err", err.Error())
		}
	}

	m.writeJSON(w, http.StatusOK, AgentConfigResponse{
		RawConfig:     serializeFrontmatter(af.Frontmatter),
		SystemPrompt:  af.Body,
		Name:          af.Frontmatter.Name,
		Description:   af.Frontmatter.Description,
		Model:         af.Frontmatter.Model,
		Group:         af.Frontmatter.Group,
		IsLeader:      af.Frontmatter.IsLeader,
		MCPServers:    af.Frontmatter.MCPServers,
		Channels:      af.Frontmatter.Channels,
		NotifyChannel: af.Frontmatter.NotifyChannel,
	})
}

// ─── Public Builders (shared by REST handlers and WebSocket Hub) ─────────────

// buildRuntimeStatus constructs a RuntimeStatusResponse from current metrics,
// agent counts, and per-session runtime states. Returns nil if runtimeMetrics is nil.
func (m *Mux) buildRuntimeStatus(hub *Hub) *RuntimeStatusResponse {
	if m.runtimeMetrics == nil {
		return nil
	}

	_, promptTokens, outputTokens, cacheHit, cacheMiss,
		contextPct, currentTokens, maxTokens, currentIter, contentDeltas, _, httpAddr := m.runtimeMetrics.Snapshot()

	// Count agents from registry and supervisors.
	var totalAgents, runningAgents, idleAgents, totalErrors, activeDelegations int
	phase := "idle"
	if m.registry != nil {
		allAgents := m.collectAllAgents()
		totalAgents = len(allAgents)
		for _, a := range allAgents {
			switch a.State() {
			case agent.StateProcessing:
				runningAgents++
			case agent.StateStopping:
				if phase != "processing" {
					phase = "stopping"
				}
			case agent.StateStopped:
				if phase != "processing" && phase != "stopping" {
					phase = "stopped"
				}
			}
			activeDelegations += a.PendingDelegations()
			if ec := a.ErrorCount(); ec > 0 {
				totalErrors += int(ec)
			}
		}
		if runningAgents > 0 {
			phase = "processing"
		}
	}

	sessions := make(map[string]SessionRuntimeInfo)
	if hub != nil && hub.requests != nil {
		// Populate L1 session runtime if initialized
		if m.sessionMgr != nil {
			if l1Sess := m.sessionMgr.Session(); l1Sess != nil {
				used, limit := 0, 0
				if l1Sess.CW() != nil {
					used, limit, _ = l1Sess.CW().TokenUsage()
				}
				info := SessionRuntimeInfo{
					SessionID:   "l1",
					State:       "idle",
					Revision:    hub.GetSessionRevision("l1"),
					CtxwinUsed:  used,
					CtxwinLimit: limit,
				}
				if req, active := hub.requests.GetBySession("l1"); active {
					info.RequestID = req.RequestID
					info.State = string(req.State)
					info.Delegating = req.Delegating
				}
				sessions["l1"] = info
			}
		}

		// Populate L2 session runtimes
		if m.l2Store != nil {
			for _, entry := range m.l2Store.List() {
				if entry.AgentInstanceID == "" {
					continue
				}
				sid := "l2:" + entry.ID
				info := SessionRuntimeInfo{
					SessionID:   sid,
					State:       "idle",
					Revision:    hub.GetSessionRevision(sid),
					CtxwinUsed:  entry.CtxwinUsed,
					CtxwinLimit: entry.CtxwinLimit,
				}
				if req, active := hub.requests.GetBySession(sid); active {
					info.RequestID = req.RequestID
					info.State = string(req.State)
					info.Delegating = req.Delegating
				}
				sessions[sid] = info
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
		CurrentTokens:     currentTokens,
		MaxTokens:         maxTokens,
		CurrentIter:       currentIter,
		ContentDeltas:     contentDeltas,
		ActiveDelegations: activeDelegations,
		TotalAgents:       totalAgents,
		RunningAgents:     runningAgents,
		IdleAgents:        idleAgents,
		TotalErrors:       totalErrors,
		HTTPAddr:          httpAddr,
		AgentStreams:      m.runtimeMetrics.AgentStreams(),
		Sessions:          sessions,
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
		isQBot := false
		levelLocked := false
		lastLevel := ""
		if m.sessionMgr != nil && m.sessionMgr.Session() != nil {
			sess := m.sessionMgr.Session()
			if a.Def.ID == "l1-agent" || (sess.Agent != nil && a.Def.ID == sess.Agent.Def.ID) {
				isQBot = sess.IsQBot()
				levelLocked = sess.LevelLocked()
				lastLevel = sess.CurrentLevel()
			}
		}
		if lastLevel == "" && m.l2Store != nil {
			if sess := m.l2Store.FindByAgentInstanceID(a.InstanceID); sess != nil {
				isQBot = sess.IsQBot()
				levelLocked = sess.LevelLocked()
				lastLevel = sess.CurrentLevel()
			}
		}
		mp := a.ModelOverride()
		te := mp != nil && mp.ThinkingEnabled
		var re string
		if mp != nil {
			re = mp.ReasoningEffort
		}
		info := AgentInfoResponse{
			ID:                 a.Def.ID,
			InstanceID:         a.InstanceID,
			Name:               a.Def.Name,
			State:              a.State().String(),
			ModelID:            a.EffectiveModelID(),
			ProviderID:         a.EffectiveProviderID(),
			Group:              agentGroup[a.InstanceID],
			IsLeader:           agentLeader[a.InstanceID],
			TaskLevel:          a.EffectiveTaskLevel(),
			ThinkingEnabled:    te,
			ReasoningEffort:    re,
			LevelLocked:        levelLocked,
			LastLevel:          lastLevel,
			ErrorCount:         int(a.ErrorCount()),
			LastError:          a.LastError(),
			PendingDelegations: a.PendingDelegations(),
			MailboxHigh:        high,
			MailboxNormal:      normal,
			IsQBot:             isQBot,
			Iteration:          a.CurrentWork().Iteration,
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
		if a := sv.Agent(); a != nil && !registeredIDs[a.InstanceID] {
			high, normal := a.MailboxDepth()
			mp := a.ModelOverride()
			te := mp != nil && mp.ThinkingEnabled
			var re string
			if mp != nil {
				re = mp.ReasoningEffort
			}

			isQBot := false
			levelLocked := false
			lastLevel := ""
			if m.l2Store != nil {
				if sess := m.l2Store.FindByAgentInstanceID(a.InstanceID); sess != nil {
					isQBot = sess.IsQBot()
					levelLocked = sess.LevelLocked()
					lastLevel = sess.CurrentLevel()
				}
			}

			info := AgentInfoResponse{
				ID:                 a.Def.ID,
				InstanceID:         a.InstanceID,
				Name:               a.Def.Name,
				State:              a.State().String(),
				ModelID:            a.EffectiveModelID(),
				ProviderID:         a.EffectiveProviderID(),
				Group:              agentGroup[a.InstanceID],
				IsLeader:           agentLeader[a.InstanceID],
				TaskLevel:          a.EffectiveTaskLevel(),
				ThinkingEnabled:    te,
				ReasoningEffort:    re,
				LevelLocked:        levelLocked,
				LastLevel:          lastLevel,
				ErrorCount:         int(a.ErrorCount()),
				LastError:          a.LastError(),
				PendingDelegations: a.PendingDelegations(),
				MailboxHigh:        high,
				MailboxNormal:      normal,
				IsQBot:             isQBot,
				Iteration:          a.CurrentWork().Iteration,
			}
			agents = append(agents, info)
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
				Iteration:          child.CurrentWork().Iteration,
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

func (m *Mux) handleGetLiveAgents(w http.ResponseWriter, r *http.Request) {
	liveAgents := m.buildAgentList()
	if liveAgents == nil {
		m.writeJSON(w, http.StatusOK, AgentListResponse{
			Agents:      []AgentInfoResponse{},
			Supervisors: []SupervisorInfoResponse{},
		})
		return
	}
	m.writeJSON(w, http.StatusOK, liveAgents)
}

// ─── Global Rules CRUD for L1 Agent ──────────────────────────────────────────

var validGlobalRuleFilename = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*\.md$`)

func isValidGlobalRuleFilename(name string) bool {
	return validGlobalRuleFilename.MatchString(name) && name != "user.md"
}

// GlobalRuleFileResponse represents a file in the global rules directory.
type GlobalRuleFileResponse struct {
	Filename string `json:"filename"`
	Size     int64  `json:"size"`
}

// GlobalRuleContentResponse represents the content of a global rule file.
type GlobalRuleContentResponse struct {
	Filename string `json:"filename"`
	Content  string `json:"content"`
}

// handleListGlobalRules returns a list of custom rule files in persona/global.
// GET /api/agents/l1/global-rules
func (m *Mux) handleListGlobalRules(w http.ResponseWriter, r *http.Request) {
	globalDir := filepath.Join(m.workDir, "persona", "global")
	entries, err := os.ReadDir(globalDir)
	if err != nil {
		if os.IsNotExist(err) {
			m.writeJSON(w, http.StatusOK, []GlobalRuleFileResponse{})
			return
		}
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read global directory"})
		return
	}

	var files []GlobalRuleFileResponse
	for _, entry := range entries {
		if entry.IsDir() || !isValidGlobalRuleFilename(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, GlobalRuleFileResponse{
			Filename: entry.Name(),
			Size:     info.Size(),
		})
	}

	m.writeJSON(w, http.StatusOK, files)
}

// handleGetGlobalRule returns the content of a specific global rule file.
// GET /api/agents/l1/global-rules/{filename}
func (m *Mux) handleGetGlobalRule(w http.ResponseWriter, r *http.Request) {
	filename := chi.URLParam(r, "filename")
	if !isValidGlobalRuleFilename(filename) {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid filename"})
		return
	}

	path := filepath.Join(m.workDir, "persona", "global", filename)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			m.writeJSON(w, http.StatusNotFound, map[string]string{"error": "file not found"})
			return
		}
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read file"})
		return
	}

	m.writeJSON(w, http.StatusOK, GlobalRuleContentResponse{
		Filename: filename,
		Content:  string(data),
	})
}

// handleSaveGlobalRule creates or updates a global rule file.
// PUT /api/agents/l1/global-rules/{filename}
func (m *Mux) handleSaveGlobalRule(w http.ResponseWriter, r *http.Request) {
	filename := chi.URLParam(r, "filename")
	if !isValidGlobalRuleFilename(filename) {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid filename"})
		return
	}

	var req struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	globalDir := filepath.Join(m.workDir, "persona", "global")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create global directory"})
		return
	}

	path := filepath.Join(globalDir, filename)
	if err := os.WriteFile(path, []byte(req.Content), 0o644); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to write file"})
		return
	}

	if m.rebuildPrompt != nil {
		if err := m.rebuildPrompt(); err != nil {
			m.log.Warn("failed to rebuild system prompt after saving global rule", "err", err.Error())
		}
	}

	info, _ := os.Stat(path)
	var size int64
	if info != nil {
		size = info.Size()
	}

	m.writeJSON(w, http.StatusOK, GlobalRuleFileResponse{
		Filename: filename,
		Size:     size,
	})
}

// handleDeleteGlobalRule deletes a global rule file.
// DELETE /api/agents/l1/global-rules/{filename}
func (m *Mux) handleDeleteGlobalRule(w http.ResponseWriter, r *http.Request) {
	filename := chi.URLParam(r, "filename")
	if !isValidGlobalRuleFilename(filename) {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid filename"})
		return
	}

	path := filepath.Join(m.workDir, "persona", "global", filename)
	if err := os.Remove(path); err != nil {
		if !os.IsNotExist(err) {
			m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete file"})
			return
		}
	}

	if m.rebuildPrompt != nil {
		if err := m.rebuildPrompt(); err != nil {
			m.log.Warn("failed to rebuild system prompt after deleting global rule", "err", err.Error())
		}
	}

	w.WriteHeader(http.StatusNoContent)
}
