package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/xiaobaitu/soloqueue/internal/teamstore"
)

// ─── Response Types ─────────────────────────────────────────────────────────

// TeamResponse is the response for GET/POST/PUT /api/teams/{name}.
type TeamResponse struct {
	ID          string                `json:"id"`
	Name        string                `json:"name"`
	Description string                `json:"description"`
	Workspaces  []teamstore.Workspace `json:"workspaces"`
	Agents      []AgentResponse       `json:"agents,omitempty"`
	CreatedAt   string                `json:"created_at"`
	UpdatedAt   string                `json:"updated_at"`
}

// AgentResponse is the response for agent CRUD endpoints.
type AgentResponse struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	TeamName     string   `json:"team_name"`
	IsLeader     bool     `json:"is_leader"`
	Model        string   `json:"model"`
	SystemPrompt string   `json:"system_prompt"`
	Permission   bool     `json:"permission"`
	MCPServers   []string `json:"mcp_servers"`
	SkillIDs     []string `json:"skill_ids"`
	CreatedAt    string   `json:"created_at"`
	UpdatedAt    string   `json:"updated_at"`
}

// ─── Conversion Helpers ─────────────────────────────────────────────────────

// teamToResponse converts a teamstore.Team to a TeamResponse.
func teamToResponse(t *teamstore.Team, agents []AgentResponse) TeamResponse {
	ws := t.Workspaces
	if ws == nil {
		ws = []teamstore.Workspace{}
	}
	return TeamResponse{
		ID:          t.ID,
		Name:        t.Name,
		Description: t.Description,
		Workspaces:  ws,
		Agents:      agents,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
	}
}

// agentToResponse converts a teamstore.Agent to an AgentResponse.
func agentToResponse(a *teamstore.Agent) AgentResponse {
	mcp := a.MCPServers
	if mcp == nil {
		mcp = []string{}
	}
	skills := a.SkillIDs
	if skills == nil {
		skills = []string{}
	}
	return AgentResponse{
		ID:           a.ID,
		Name:         a.Name,
		Description:  a.Description,
		TeamName:     a.TeamName,
		IsLeader:     a.IsLeader,
		Model:        a.Model,
		SystemPrompt: a.SystemPrompt,
		Permission:   a.Permission,
		MCPServers:   mcp,
		SkillIDs:     skills,
		CreatedAt:    a.CreatedAt,
		UpdatedAt:    a.UpdatedAt,
	}
}

// ─── Team Handlers ──────────────────────────────────────────────────────────

// handleListTeams returns all teams and their agents.
// If teamstore is available, reads from DB; otherwise falls back to
// file-based template loading (backward-compatible).
// GET /api/teams
func (m *Mux) handleListTeams(w http.ResponseWriter, r *http.Request) {
	if m.teamstore != nil {
		m.handleListTeamsFromDB(w, r)
		return
	}
	m.handleListTeamsFromFiles(w)
}

// handleListTeamsFromDB reads teams and agents from the SQLite store.
func (m *Mux) handleListTeamsFromDB(w http.ResponseWriter, r *http.Request) {
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
			Name:        t.Name,
			Description: t.Description,
			Agents:      agtResp,
		})
	}

	m.writeJSON(w, http.StatusOK, TeamListResponse{Teams: result})
}

// handleListTeamsFromFiles falls back to file-based template loading
// when no teamstore is configured.
func (m *Mux) handleListTeamsFromFiles(w http.ResponseWriter) {
	templates := m.reloadTemplates()
	if templates == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "templates not available"})
		return
	}

	teamMap := make(map[string]*TeamInfoResponse)
	groups := m.reloadGroups()

	for _, tmpl := range templates {
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

			if group, ok := groups[groupName]; ok {
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

	teams := make([]TeamInfoResponse, 0, len(teamMap))
	for _, team := range teamMap {
		teams = append(teams, *team)
	}

	m.writeJSON(w, http.StatusOK, TeamListResponse{Teams: teams})
}

// createTeamRequest is the JSON body for POST /api/teams.
type createTeamRequest struct {
	Name        string                `json:"name"`
	Description string                `json:"description"`
	Workspaces  []teamstore.Workspace `json:"workspaces"`
}

// handleCreateTeam creates a new team.
// POST /api/teams
func (m *Mux) handleCreateTeam(w http.ResponseWriter, r *http.Request) {
	if m.teamstore == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "team store not available"})
		return
	}

	var req createTeamRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid request: %v", err)})
		return
	}
	if req.Name == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}

	t := &teamstore.Team{
		Name:        req.Name,
		Description: req.Description,
		Workspaces:  req.Workspaces,
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

	if m.teamstore != nil {
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
		return
	}

	// Fallback to file-based: find matching group and its agents
	groups := m.reloadGroups()
	templates := m.reloadTemplates()
	if groups == nil && templates == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "team store and file templates not available"})
		return
	}

	group, ok := groups[name]
	if !ok {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": fmt.Sprintf("team %q not found", name)})
		return
	}

	agtResp := make([]AgentResponse, 0)
	for _, tmpl := range templates {
		if tmpl.Group == name {
			agtResp = append(agtResp, AgentResponse{
				ID:          tmpl.ID,
				Name:        tmpl.Name,
				Description: tmpl.Description,
				TeamName:    tmpl.Group,
				IsLeader:    tmpl.IsLeader,
				Model:       tmpl.ModelID,
			})
		}
	}

	m.writeJSON(w, http.StatusOK, TeamResponse{
		Name:        name,
		Description: group.Body,
		Agents:      agtResp,
	})
}

// updateTeamRequest is the JSON body for PUT /api/teams/{name}.
type updateTeamRequest struct {
	Description *string                `json:"description,omitempty"`
	Workspaces  *[]teamstore.Workspace `json:"workspaces,omitempty"`
}

// handleUpdateTeam updates an existing team.
// PUT /api/teams/{name}
func (m *Mux) handleUpdateTeam(w http.ResponseWriter, r *http.Request) {
	if m.teamstore == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "team store not available"})
		return
	}

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
	if req.Workspaces != nil {
		existing.Workspaces = *req.Workspaces
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
	if m.teamstore == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "team store not available"})
		return
	}

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
	if m.teamstore != nil {
		m.handleListAgentsFromDB(w, r)
		return
	}
	m.handleListAgentsFromFiles(w)
}

// handleListAgentsFromDB reads agents from the SQLite store.
func (m *Mux) handleListAgentsFromDB(w http.ResponseWriter, r *http.Request) {
	teamName := r.URL.Query().Get("team")

	var agents []teamstore.Agent
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
	m.writeJSON(w, http.StatusOK, result)
}

// handleListAgentsFromFiles falls back to file-based template loading.
func (m *Mux) handleListAgentsFromFiles(w http.ResponseWriter) {
	templates := m.reloadTemplates()
	if templates == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "agent templates not available"})
		return
	}

	result := make([]AgentResponse, 0, len(templates))
	for _, tmpl := range templates {
		result = append(result, AgentResponse{
			ID:          tmpl.ID,
			Name:        tmpl.Name,
			Description: tmpl.Description,
			TeamName:    tmpl.Group,
			IsLeader:    tmpl.IsLeader,
			Model:       tmpl.ModelID,
		})
	}
	m.writeJSON(w, http.StatusOK, result)
}

// createAgentRequest is the JSON body for POST /api/agents.
type createAgentRequest struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	TeamName     string   `json:"team_name"`
	IsLeader     bool     `json:"is_leader"`
	Model        string   `json:"model"`
	SystemPrompt string   `json:"system_prompt"`
	Permission   bool     `json:"permission"`
	MCPServers   []string `json:"mcp_servers"`
	SkillIDs     []string `json:"skill_ids"`
}

// handleCreateAgent creates a new agent.
// POST /api/agents
func (m *Mux) handleCreateAgent(w http.ResponseWriter, r *http.Request) {
	if m.teamstore == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "agent store not available"})
		return
	}

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

	a := &teamstore.Agent{
		Name:         req.Name,
		Description:  req.Description,
		TeamName:     req.TeamName,
		IsLeader:     req.IsLeader,
		Model:        req.Model,
		SystemPrompt: req.SystemPrompt,
		Permission:   req.Permission,
		MCPServers:   req.MCPServers,
		SkillIDs:     req.SkillIDs,
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

	if m.teamstore != nil {
		a, err := m.teamstore.GetAgentByName(r.Context(), name)
		if err != nil {
			m.writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		m.writeJSON(w, http.StatusOK, agentToResponse(a))
		return
	}

	// Fallback to file-based
	templates := m.reloadTemplates()
	if templates == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "agent templates not available"})
		return
	}
	for _, tmpl := range templates {
		if tmpl.Name == name {
			m.writeJSON(w, http.StatusOK, AgentResponse{
				ID:           tmpl.ID,
				Name:         tmpl.Name,
				Description:  tmpl.Description,
				TeamName:     tmpl.Group,
				IsLeader:     tmpl.IsLeader,
				Model:        tmpl.ModelID,
				SystemPrompt: tmpl.SystemPrompt,
				Permission:   tmpl.Permission,
				MCPServers:   tmpl.MCPServers,
				SkillIDs:     tmpl.SkillIDs,
			})
			return
		}
	}
	m.writeJSON(w, http.StatusNotFound, map[string]string{"error": fmt.Sprintf("agent %q not found", name)})
}

// updateAgentRequest is the JSON body for PUT /api/agents/{name}.
type updateAgentRequest struct {
	Description  *string   `json:"description,omitempty"`
	TeamName     *string   `json:"team_name,omitempty"`
	IsLeader     *bool     `json:"is_leader,omitempty"`
	Model        *string   `json:"model,omitempty"`
	SystemPrompt *string   `json:"system_prompt,omitempty"`
	Permission   *bool     `json:"permission,omitempty"`
	MCPServers   *[]string `json:"mcp_servers,omitempty"`
	SkillIDs     *[]string `json:"skill_ids,omitempty"`
}

// handleUpdateAgent updates an existing agent.
// PUT /api/agents/{name}
func (m *Mux) handleUpdateAgent(w http.ResponseWriter, r *http.Request) {
	if m.teamstore == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "agent store not available"})
		return
	}

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
	if req.Permission != nil {
		existing.Permission = *req.Permission
	}
	if req.MCPServers != nil {
		existing.MCPServers = *req.MCPServers
	}
	if req.SkillIDs != nil {
		existing.SkillIDs = *req.SkillIDs
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
	if m.teamstore == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "agent store not available"})
		return
	}

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
