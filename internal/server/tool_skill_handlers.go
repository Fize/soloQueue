package server

import (
	"encoding/json"
	"net/http"
	"sort"

	"github.com/go-chi/chi/v5"
	"github.com/xiaobaitu/soloqueue/internal/agenttools/skill"
	"github.com/xiaobaitu/soloqueue/internal/agenttools/tools"
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

// SkillInfoResponse describes an installed skill. Skill lifecycle operations
// are intentionally outside SoloQueue and are handled by the clawhub CLI.
type SkillInfoResponse struct {
	ID                     string   `json:"id"`
	Name                   string   `json:"name"`
	Description            string   `json:"description"`
	WhenToUse              string   `json:"when_to_use"`
	UserInvocable          bool     `json:"user_invocable"`
	DisableModelInvocation bool     `json:"disable_model_invocation"`
	Context                string   `json:"context"`
	Agent                  string   `json:"agent"`
	FilePath               string   `json:"file_path"`
	AllowedTools           []string `json:"allowed_tools"`
	Triggers               []string `json:"triggers"`
	Body                   string   `json:"body,omitempty"`
	RequiredEnv            []string `json:"required_env,omitempty"`
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
	toolInfos := make([]ToolInfoResponse, 0, len(allTools))
	for _, t := range allTools {
		toolInfos = append(toolInfos, ToolInfoResponse{
			Name:        t.Name(),
			Description: t.Description(),
			Parameters:  t.Parameters(),
		})
	}
	sort.Slice(toolInfos, func(i, j int) bool { return toolInfos[i].Name < toolInfos[j].Name })

	m.writeJSON(w, http.StatusOK, ToolListResponse{Tools: toolInfos, Total: len(toolInfos)})
}

// installedSkills returns the current registry snapshot. The directory
// fallback keeps read-only HTTP tests and lightweight embedders functional;
// production uses the hot-reloaded registry built by runtime.Build.
func (m *Mux) installedSkills() []*skill.Skill {
	if m.skillReg != nil {
		return m.skillReg.Skills()
	}
	if len(m.skillDirs) == 0 {
		return nil
	}
	skills, err := skill.LoadSkillsFromDirs(m.skillDirs)
	if err != nil {
		return nil
	}
	return skills
}

func skillInfo(s *skill.Skill, includeBody bool) SkillInfoResponse {
	info := SkillInfoResponse{
		ID:                     s.ID,
		Name:                   s.Name,
		Description:            s.Description,
		WhenToUse:              s.WhenToUse,
		UserInvocable:          s.UserInvocable,
		DisableModelInvocation: s.DisableModelInvocation,
		Context:                s.Context,
		Agent:                  s.Agent,
		FilePath:               s.FilePath,
		AllowedTools:           s.AllowedTools,
		Triggers:               s.Triggers,
		RequiredEnv:            s.RequiredEnv,
	}
	if includeBody {
		info.Body = s.Instructions
	}
	return info
}

func (m *Mux) installedSkill(id string) (*skill.Skill, bool) {
	if m.skillReg != nil {
		return m.skillReg.GetSkill(id)
	}
	for _, s := range m.installedSkills() {
		if s.ID == id {
			return s, true
		}
	}
	return nil, false
}

// handleListSkills returns all skills installed in the configured skill
// directories. The server does not expose a catalog or lifecycle mutation.
// GET /api/skills
func (m *Mux) handleListSkills(w http.ResponseWriter, _ *http.Request) {
	allSkills := m.installedSkills()
	infos := make([]SkillInfoResponse, 0, len(allSkills))
	for _, s := range allSkills {
		infos = append(infos, skillInfo(s, false))
	}
	m.writeJSON(w, http.StatusOK, SkillListResponse{Skills: infos, Total: len(infos)})
}

// handleGetSkillDetail returns a single installed skill and its markdown body.
// GET /api/skills/{id}
func (m *Mux) handleGetSkillDetail(w http.ResponseWriter, r *http.Request) {
	s, ok := m.installedSkill(chi.URLParam(r, "id"))
	if !ok || s == nil {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": "skill not found"})
		return
	}
	m.writeJSON(w, http.StatusOK, skillInfo(s, true))
}

// handleGetSkillFiles recursively lists files inside an installed skill.
// GET /api/skills/{id}/files
func (m *Mux) handleGetSkillFiles(w http.ResponseWriter, r *http.Request) {
	s, ok := m.installedSkill(chi.URLParam(r, "id"))
	if !ok || s == nil || s.Dir == "" {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": "skill folder not found"})
		return
	}
	files, err := skill.ListSkillFiles(s.Dir)
	if err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	m.writeJSON(w, http.StatusOK, map[string]any{"files": files})
}
