package server

import (
	"encoding/json"
	"net/http"
	"sort"

	"github.com/xiaobaitu/soloqueue/internal/skill"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// ─── Tools Response Types ──────────────────────────────────────────────────

// ToolInfoResponse is a single tool in the list.
type ToolInfoResponse struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// ToolListResponse is the response for GET /api/tools.
type ToolListResponse struct {
	Tools []ToolInfoResponse `json:"tools"`
	Total int                `json:"total"`
}

// ─── Skills Response Types ─────────────────────────────────────────────────

// SkillCategory is the category of a skill ("builtin" or "user").
type SkillCategory = skill.SkillCategory

// SkillInfoResponse is a single skill in the list.
type SkillInfoResponse struct {
	ID                     string        `json:"id"`
	Description            string        `json:"description"`
	Category               SkillCategory `json:"category"`
	UserInvocable          bool          `json:"user_invocable"`
	DisableModelInvocation bool          `json:"disable_model_invocation"`
	Context                string        `json:"context"`
	Agent                  string        `json:"agent"`
	FilePath               string        `json:"file_path"`
	AllowedTools           []string      `json:"allowed_tools"`
}

// SkillListResponse is the response for GET /api/skills.
type SkillListResponse struct {
	Skills []SkillInfoResponse `json:"skills"`
	Total  int                 `json:"total"`
}

// ─── Handlers ──────────────────────────────────────────────────────────────

// handleListTools returns all built-in tools.
// GET /api/tools
func (m *Mux) handleListTools(w http.ResponseWriter, _ *http.Request) {
	if m.toolsCfg == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "tools config not available"})
		return
	}

	allTools := tools.Build(*m.toolsCfg)

	// Extract only metadata (name, description, parameters) — skip Execute.
	toolInfos := make([]ToolInfoResponse, 0, len(allTools))
	for _, t := range allTools {
		toolInfos = append(toolInfos, ToolInfoResponse{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Parameters(),
		})
	}

	// Sort by name for stable output.
	sort.Slice(toolInfos, func(i, j int) bool {
		return toolInfos[i].Name < toolInfos[j].Name
	})

	m.writeJSON(w, http.StatusOK, ToolListResponse{
		Tools: toolInfos,
		Total: len(toolInfos),
	})
}

// handleListSkills returns all registered skills (builtin + user).
// GET /api/skills
func (m *Mux) handleListSkills(w http.ResponseWriter, _ *http.Request) {
	if m.skillReg == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "skill registry not available"})
		return
	}

	allSkills := m.skillReg.Skills()

	skillInfos := make([]SkillInfoResponse, 0, len(allSkills))
	for _, s := range allSkills {
		skillInfos = append(skillInfos, SkillInfoResponse{
			ID:                     s.ID,
			Description:            s.Description,
			Category:               s.Category,
			UserInvocable:          s.UserInvocable,
			DisableModelInvocation: s.DisableModelInvocation,
			Context:                s.Context,
			Agent:                  s.Agent,
			FilePath:               s.FilePath,
			AllowedTools:           s.AllowedTools,
		})
	}

	m.writeJSON(w, http.StatusOK, SkillListResponse{
		Skills: skillInfos,
		Total:  len(skillInfos),
	})
}
