package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/xiaobaitu/soloqueue/internal/team/store"
)

type BuiltinTeamMemberResponse struct {
	Name     string `json:"name"`
	IsLeader bool   `json:"is_leader"`
}

type BuiltinTeamResponse struct {
	ID            string                         `json:"id"`
	Name          string                         `json:"name"`
	DisplayName   string                         `json:"display_name"`
	Description   string                         `json:"description"`
	Leader        string                         `json:"leader"`
	Members       []BuiltinTeamMemberResponse    `json:"members"`
	Status        store.BuiltinTeamInstallStatus `json:"status"`
	MissingAgents []string                       `json:"missing_agents"`
	Conflicts     []string                       `json:"conflicts"`
}

// ─── Response Types ─────────────────────────────────────────────────────────

// TeamResponse is the response for GET/POST/PUT /api/teams/{name}.
type TeamResponse struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Agents      []AgentResponse `json:"agents,omitempty"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
}

// AgentResponse is the response for agent CRUD endpoints.
type AgentResponse struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Description   string            `json:"description"`
	TeamName      string            `json:"team_name"`
	IsLeader      bool              `json:"is_leader"`
	Model         string            `json:"model"`
	SystemPrompt  string            `json:"system_prompt"`
	MCPServers    []string          `json:"mcp_servers"`
	SkillIDs      []string          `json:"skill_ids"`
	Channels      map[string]string `json:"channels,omitempty"`
	NotifyChannel string            `json:"notify_channel,omitempty"`
	CreatedAt     string            `json:"created_at"`
	UpdatedAt     string            `json:"updated_at"`
}

// ─── Conversion Helpers ─────────────────────────────────────────────────────

// teamToResponse converts a store.Team to a TeamResponse.
func teamToResponse(t *store.Team, agents []AgentResponse) TeamResponse {
	return TeamResponse{
		ID:          t.ID,
		Name:        t.Name,
		Description: t.Description,
		Agents:      agents,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
	}
}

// agentToResponse converts a store.Agent to an AgentResponse.
func agentToResponse(a *store.Agent) AgentResponse {
	mcp := a.MCPServers
	if mcp == nil {
		mcp = []string{}
	}
	skills := a.SkillIDs
	if skills == nil {
		skills = []string{}
	}
	return AgentResponse{
		ID:            a.ID,
		Name:          a.Name,
		Description:   a.Description,
		TeamName:      a.TeamName,
		IsLeader:      a.IsLeader,
		Model:         a.Model,
		SystemPrompt:  a.SystemPrompt,
		MCPServers:    mcp,
		SkillIDs:      skills,
		Channels:      a.Channels,
		NotifyChannel: a.NotifyChannel,
		CreatedAt:     a.CreatedAt,
		UpdatedAt:     a.UpdatedAt,
	}
}

// ─── Team Handlers ──────────────────────────────────────────────────────────

func (m *Mux) handleListBuiltinTeams(w http.ResponseWriter, r *http.Request) {
	if m.teamstore == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "team store is not available"})
		return
	}
	views, err := m.teamstore.ListBuiltinTeamStatuses(r.Context())
	if err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	result := make([]BuiltinTeamResponse, 0, len(views))
	for _, view := range views {
		item := BuiltinTeamResponse{
			ID:            view.Spec.ID,
			Name:          view.Spec.Name,
			DisplayName:   view.Spec.DisplayName,
			Description:   view.Spec.Description,
			Status:        view.Status,
			MissingAgents: view.MissingAgents,
			Conflicts:     view.Conflicts,
			Members:       make([]BuiltinTeamMemberResponse, 0, len(view.Spec.Agents)),
		}
		for _, member := range view.Spec.Agents {
			item.Members = append(item.Members, BuiltinTeamMemberResponse{Name: member.Name, IsLeader: member.IsLeader})
			if member.IsLeader {
				item.Leader = member.Name
			}
		}
		result = append(result, item)
	}
	m.writeJSON(w, http.StatusOK, map[string]any{"teams": result})
}

func (m *Mux) handleInstallBuiltinTeams(w http.ResponseWriter, r *http.Request) {
	if m.teamstore == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "team store is not available"})
		return
	}
	if m.sessionMgr != nil {
		if sess := m.sessionMgr.Session(); sess != nil && !sess.Idle() {
			m.writeJSON(w, http.StatusConflict, map[string]string{
				"code":  "session_busy",
				"error": "the L1 session is busy",
			})
			return
		}
	}

	var req struct {
		TeamIDs []string `json:"team_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_team_ids", "error": "invalid request"})
		return
	}
	results, err := m.teamstore.InstallBuiltinTeams(r.Context(), req.TeamIDs)
	if err != nil {
		status := http.StatusBadRequest
		code := "invalid_team_ids"
		if errors.Is(err, store.ErrBuiltinTeamConflict) {
			status = http.StatusConflict
			code = "builtin_team_conflict"
		}
		m.writeJSON(w, status, map[string]string{"code": code, "error": err.Error()})
		return
	}

	refreshed := true
	restartRequired := false
	if m.reloadTeamCatalog != nil {
		if err := m.reloadTeamCatalog(); err != nil {
			refreshed = false
			restartRequired = true
			if m.log != nil {
				m.log.Warn("failed to reload runtime team catalog", "err", err.Error())
			}
		}
	}
	m.writeJSON(w, http.StatusCreated, map[string]any{
		"results":           results,
		"runtime_refreshed": refreshed,
		"restart_required":  restartRequired,
	})
}

// handleListTeams returns all teams and their agents.
// GET /api/teams
func (m *Mux) handleListTeams(w http.ResponseWriter, r *http.Request) {
	teams, err := m.teamstore.ListTeams(r.Context())
	if err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	result := make([]TeamInfoResponse, 0, len(teams))
	for _, t := range teams {
		agents, err := m.teamstore.ListAgentsByTeam(r.Context(), t.Name)
		if err != nil {
			m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		agtResp := make([]AgentTemplateResponse, 0, len(agents))
		for _, a := range agents {
			agtResp = append(agtResp, AgentTemplateResponse{
				ID:          a.ID,
				Name:        a.Name,
				Description: a.Description,
				IsLeader:    a.IsLeader,
				Group:       a.TeamName,
				ModelID:     a.Model,
			})
		}
		result = append(result, TeamInfoResponse{
			ID:          t.ID,
			Name:        t.Name,
			Description: t.Description,
			Agents:      agtResp,
			CreatedAt:   t.CreatedAt,
			UpdatedAt:   t.UpdatedAt,
		})
	}

	m.writeJSON(w, http.StatusOK, TeamListResponse{Teams: result})
}

// createTeamRequest is the JSON body for POST /api/teams.
type createTeamRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// handleCreateTeam creates a new team.
// POST /api/teams
func (m *Mux) handleCreateTeam(w http.ResponseWriter, r *http.Request) {
	var req createTeamRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid request: %v", err)})
		return
	}
	if req.Name == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}

	t := &store.Team{
		Name:        req.Name,
		Description: req.Description,
	}
	if err := m.teamstore.CreateTeam(r.Context(), t); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	m.writeJSON(w, http.StatusCreated, teamToResponse(t, nil))
}

// handleGetTeam returns a single team with its agents.
// GET /api/teams/{name}
func (m *Mux) handleGetTeam(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	t, err := m.teamstore.GetTeamByName(r.Context(), name)
	if err != nil {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	agents, err := m.teamstore.ListAgentsByTeam(r.Context(), name)
	if err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	agtResp := make([]AgentResponse, 0, len(agents))
	for i := range agents {
		agtResp = append(agtResp, agentToResponse(&agents[i]))
	}
	m.writeJSON(w, http.StatusOK, teamToResponse(t, agtResp))
}

// updateTeamRequest is the JSON body for PUT /api/teams/{name}.
type updateTeamRequest struct {
	Description *string `json:"description,omitempty"`
}

// handleUpdateTeam updates an existing team.
// PUT /api/teams/{name}
func (m *Mux) handleUpdateTeam(w http.ResponseWriter, r *http.Request) {

	name := chi.URLParam(r, "name")

	// Fetch existing team first to preserve fields
	existing, err := m.teamstore.GetTeamByName(r.Context(), name)
	if err != nil {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	var req updateTeamRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid request: %v", err)})
		return
	}

	if req.Description != nil {
		existing.Description = *req.Description
	}

	if err := m.teamstore.UpdateTeam(r.Context(), name, existing); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	// Re-fetch to get updated timestamps
	updated, _ := m.teamstore.GetTeamByName(r.Context(), name)
	if updated == nil {
		updated = existing
	}

	// Rebuild prompt if callback is set
	m.maybeRebuildPrompt(w)

	m.writeJSON(w, http.StatusOK, teamToResponse(updated, nil))
}

// handleDeleteTeam removes a team by name.
// DELETE /api/teams/{name}
func (m *Mux) handleDeleteTeam(w http.ResponseWriter, r *http.Request) {

	name := chi.URLParam(r, "name")
	if err := m.teamstore.DeleteTeam(r.Context(), name); err != nil {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	m.maybeRebuildPrompt(w)

	m.writeJSON(w, http.StatusOK, map[string]string{"deleted": name})
}

// ─── Agent Handlers ─────────────────────────────────────────────────────────

// handleListAgents returns all agents, optionally filtered by team.
// GET /api/agents?team=<name>
func (m *Mux) handleListAgents(w http.ResponseWriter, r *http.Request) {
	teamName := r.URL.Query().Get("team")

	var agents []store.Agent
	var err error
	if teamName != "" {
		agents, err = m.teamstore.ListAgentsByTeam(r.Context(), teamName)
	} else {
		agents, err = m.teamstore.ListAgents(r.Context())
	}
	if err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	result := make([]AgentResponse, 0, len(agents))
	for i := range agents {
		result = append(result, agentToResponse(&agents[i]))
	}
	m.writeJSON(w, http.StatusOK, map[string]any{"agents": result})
}

// createAgentRequest is the JSON body for POST /api/agents.
type createAgentRequest struct {
	Name          string            `json:"name"`
	Description   string            `json:"description"`
	TeamName      string            `json:"team_name"`
	IsLeader      bool              `json:"is_leader"`
	Model         string            `json:"model"`
	SystemPrompt  string            `json:"system_prompt"`
	MCPServers    []string          `json:"mcp_servers"`
	SkillIDs      []string          `json:"skill_ids"`
	Channels      map[string]string `json:"channels,omitempty"`
	NotifyChannel string            `json:"notify_channel,omitempty"`
}

// handleCreateAgent creates a new agent.
// POST /api/agents
func (m *Mux) handleCreateAgent(w http.ResponseWriter, r *http.Request) {
	var req createAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid request: %v", err)})
		return
	}
	if req.Name == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if req.TeamName == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "team_name is required"})
		return
	}

	a := &store.Agent{
		Name:          req.Name,
		Description:   req.Description,
		TeamName:      req.TeamName,
		IsLeader:      req.IsLeader,
		Model:         req.Model,
		SystemPrompt:  req.SystemPrompt,
		MCPServers:    req.MCPServers,
		SkillIDs:      req.SkillIDs,
		Channels:      req.Channels,
		NotifyChannel: req.NotifyChannel,
	}
	if err := m.teamstore.CreateAgent(r.Context(), a); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	m.maybeRebuildPrompt(w)

	m.writeJSON(w, http.StatusCreated, agentToResponse(a))
}

// handleGetAgent returns a single agent by name.
// GET /api/agents/{name}
func (m *Mux) handleGetAgent(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	a, err := m.teamstore.GetAgentByName(r.Context(), name)
	if err != nil {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	m.writeJSON(w, http.StatusOK, agentToResponse(a))
}

// updateAgentRequest is the JSON body for PUT /api/agents/{name}.
type updateAgentRequest struct {
	Description   *string            `json:"description,omitempty"`
	TeamName      *string            `json:"team_name,omitempty"`
	IsLeader      *bool              `json:"is_leader,omitempty"`
	Model         *string            `json:"model,omitempty"`
	SystemPrompt  *string            `json:"system_prompt,omitempty"`
	MCPServers    *[]string          `json:"mcp_servers,omitempty"`
	SkillIDs      *[]string          `json:"skill_ids,omitempty"`
	Channels      *map[string]string `json:"channels,omitempty"`
	NotifyChannel *string            `json:"notify_channel,omitempty"`
}

// handleUpdateAgent updates an existing agent.
// PUT /api/agents/{name}
func (m *Mux) handleUpdateAgent(w http.ResponseWriter, r *http.Request) {

	name := chi.URLParam(r, "name")

	// Fetch existing agent first to preserve fields
	existing, err := m.teamstore.GetAgentByName(r.Context(), name)
	if err != nil {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	var req updateAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid request: %v", err)})
		return
	}

	if req.Description != nil {
		existing.Description = *req.Description
	}
	if req.TeamName != nil {
		existing.TeamName = *req.TeamName
	}
	if req.IsLeader != nil {
		existing.IsLeader = *req.IsLeader
	}
	if req.Model != nil {
		existing.Model = *req.Model
	}
	if req.SystemPrompt != nil {
		existing.SystemPrompt = *req.SystemPrompt
	}
	if req.MCPServers != nil {
		existing.MCPServers = *req.MCPServers
	}
	if req.SkillIDs != nil {
		existing.SkillIDs = *req.SkillIDs
	}
	if req.Channels != nil {
		existing.Channels = *req.Channels
	}
	if req.NotifyChannel != nil {
		existing.NotifyChannel = *req.NotifyChannel
	}

	if err := m.teamstore.UpdateAgent(r.Context(), name, existing); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	// Re-fetch to get updated timestamps
	updated, _ := m.teamstore.GetAgentByName(r.Context(), name)
	if updated == nil {
		updated = existing
	}

	m.maybeRebuildPrompt(w)

	m.writeJSON(w, http.StatusOK, agentToResponse(updated))
}

// handleDeleteAgent removes an agent by name.
// DELETE /api/agents/{name}
func (m *Mux) handleDeleteAgent(w http.ResponseWriter, r *http.Request) {

	name := chi.URLParam(r, "name")
	if err := m.teamstore.DeleteAgent(r.Context(), name); err != nil {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	m.maybeRebuildPrompt(w)

	m.writeJSON(w, http.StatusOK, map[string]string{"deleted": name})
}

// maybeRebuildPrompt triggers a prompt rebuild if the callback is set.
// Logs a warning on failure but does not fail the request.
func (m *Mux) maybeRebuildPrompt(w http.ResponseWriter) {
	if m.rebuildPrompt != nil {
		if err := m.rebuildPrompt(); err != nil {
			if m.log != nil {
				m.log.Warn("failed to rebuild system prompt after team/agent change", "err", err.Error())
			}
		}
	}
}
