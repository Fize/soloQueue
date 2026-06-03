package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"

	"github.com/go-chi/chi/v5"
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
	Name                   string        `json:"name"`
	Description            string        `json:"description"`
	WhenToUse              string        `json:"when_to_use"`
	Category               SkillCategory `json:"category"`
	UserInvocable          bool          `json:"user_invocable"`
	DisableModelInvocation bool          `json:"disable_model_invocation"`
	Context                string        `json:"context"`
	Agent                  string        `json:"agent"`
	FilePath               string        `json:"file_path"`
	AllowedTools           []string      `json:"allowed_tools"`
	Triggers               []string      `json:"triggers"`
	Enabled                bool          `json:"enabled"`
	Body                   string        `json:"body,omitempty"`
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
	tmpReg := skill.NewSkillRegistry()
	skill.RegisterBuiltinSkills(tmpReg)

	if len(m.skillDirs) > 0 {
		if userSkills, err := skill.LoadSkillsFromDirs(m.skillDirs); err == nil {
			for _, s := range userSkills {
				_ = tmpReg.Register(s)
			}
		}
	}

	allSkills := tmpReg.Skills()

	skillInfos := make([]SkillInfoResponse, 0, len(allSkills))
	for _, s := range allSkills {
		skillInfos = append(skillInfos, SkillInfoResponse{
			ID:                     s.ID,
			Name:                   s.Name,
			Description:            s.Description,
			WhenToUse:              s.WhenToUse,
			Category:               s.Category,
			UserInvocable:          s.UserInvocable,
			DisableModelInvocation: s.DisableModelInvocation,
			Context:                s.Context,
			Agent:                  s.Agent,
			FilePath:               s.FilePath,
			AllowedTools:           s.AllowedTools,
			Triggers:               s.Triggers,
			Enabled:                !s.Disabled,
		})
	}

	m.writeJSON(w, http.StatusOK, SkillListResponse{
		Skills: skillInfos,
		Total:  len(skillInfos),
	})
}

// getStoreSkills retrieves store skills from local repository folders or fallback to embedded distFS.
func (m *Mux) getStoreSkills() ([]*skill.Skill, error) {
	if _, err := os.Stat("skills"); err == nil {
		return skill.ListStoreSkills("skills")
	}
	if _, err := os.Stat("../skills"); err == nil {
		return skill.ListStoreSkills("../skills")
	}
	fallbackStoreDir := filepath.Join(m.workDir, "store", "skills")
	if _, err := os.Stat(fallbackStoreDir); err == nil {
		return skill.ListStoreSkills(fallbackStoreDir)
	}
	return skill.LoadSkillsFromFS(distFS(), "skills")
}

// installStoreSkill installs a skill from the store (local disk or embedded FS) into userSkillsDir.
func (m *Mux) installStoreSkill(ctx context.Context, userSkillsDir, id string) error {
	// Parse the store skill to inspect its metadata (like Upstream)
	var s *skill.Skill
	var err error

	if _, statErr := os.Stat(filepath.Join("skills", id, "SKILL.md")); statErr == nil {
		s, err = skill.ParseSkillMD(filepath.Join("skills", id, "SKILL.md"))
	} else if _, statErr := os.Stat(filepath.Join("../skills", id, "SKILL.md")); statErr == nil {
		s, err = skill.ParseSkillMD(filepath.Join("../skills", id, "SKILL.md"))
	} else {
		fallbackStoreDir := filepath.Join(m.workDir, "store", "skills")
		if _, statErr := os.Stat(filepath.Join(fallbackStoreDir, id, "SKILL.md")); statErr == nil {
			s, err = skill.ParseSkillMD(filepath.Join(fallbackStoreDir, id, "SKILL.md"))
		} else {
			s, err = skill.ParseSkillMDFromFS(distFS(), filepath.ToSlash(filepath.Join("skills", id, "SKILL.md")))
		}
	}

	// If we found the skill and it has an upstream configured, clone it remotely
	if err == nil && s != nil && s.Upstream != "" {
		return skill.InstallGithubSkill(ctx, s.Upstream, userSkillsDir)
	}

	if _, err := os.Stat(filepath.Join("skills", id)); err == nil {
		return skill.InstallSkill("skills", userSkillsDir, id)
	}
	if _, err := os.Stat(filepath.Join("../skills", id)); err == nil {
		return skill.InstallSkill("../skills", userSkillsDir, id)
	}
	fallbackStoreDir := filepath.Join(m.workDir, "store", "skills")
	if _, err := os.Stat(filepath.Join(fallbackStoreDir, id)); err == nil {
		return skill.InstallSkill(fallbackStoreDir, userSkillsDir, id)
	}
	return skill.InstallSkillFromFS(distFS(), "skills", userSkillsDir, id)
}

// handleListStoreSkills returns all available skills in the store catalog.
// GET /api/skills/store
func (m *Mux) handleListStoreSkills(w http.ResponseWriter, _ *http.Request) {
	userSkillsDir := m.skillDirs["user"]

	storeSkills, err := m.getStoreSkills()
	if err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	skillInfos := make([]SkillInfoResponse, 0, len(storeSkills))
	for _, s := range storeSkills {
		installed := false
		if userSkillsDir != "" {
			if _, err := os.Stat(filepath.Join(userSkillsDir, s.ID)); err == nil {
				installed = true
			}
		}

		skillInfos = append(skillInfos, SkillInfoResponse{
			ID:            s.ID,
			Name:          s.Name,
			Description:   s.Description,
			WhenToUse:     s.WhenToUse,
			Category:      s.Category,
			UserInvocable: s.UserInvocable,
			Context:       s.Context,
			Agent:         s.Agent,
			Triggers:      s.Triggers,
			Enabled:       installed, // We reuse 'Enabled' to denote 'Installed' status for store skills
		})
	}

	m.writeJSON(w, http.StatusOK, SkillListResponse{
		Skills: skillInfos,
		Total:  len(skillInfos),
	})
}

// handleInstallSkill installs a skill from the store, a local folder, or a github URL.
// POST /api/skills/install
func (m *Mux) handleInstallSkill(w http.ResponseWriter, r *http.Request) {
	userSkillsDir := m.skillDirs["user"]
	if userSkillsDir == "" {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "user skills directory not configured"})
		return
	}

	var req struct {
		Source string `json:"source"`
		ID     string `json:"id,omitempty"`
		Path   string `json:"path,omitempty"`
		URL    string `json:"url,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	var err error
	switch req.Source {
	case "store":
		err = m.installStoreSkill(r.Context(), userSkillsDir, req.ID)
	case "local":
		err = skill.InstallLocalSkill(req.Path, userSkillsDir)
	case "github":
		err = skill.InstallGithubSkill(r.Context(), req.URL, userSkillsDir)
	default:
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid source type"})
		return
	}

	if err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Rebuild the in-memory skill registry
	if m.skillReg != nil && len(m.skillDirs) > 0 {
		_ = m.skillReg.Rebuild(m.skillDirs)
	}

	m.writeJSON(w, http.StatusOK, map[string]string{"status": "installed"})
}

// handleGetSkillDetail returns a single skill detail with its markdown body.
// GET /api/skills/{id}
func (m *Mux) handleGetSkillDetail(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Look in registry first
	var s *skill.Skill
	var ok bool
	if m.skillReg != nil {
		s, ok = m.skillReg.GetSkill(id)
	}
	if !ok {
		userSkillsDir := m.skillDirs["user"]
		var parsed *skill.Skill
		var err error

		// 1. Try reading from user folder
		if userSkillsDir != "" {
			userSkillPath := filepath.Join(userSkillsDir, id, "SKILL.md")
			if _, statErr := os.Stat(userSkillPath); statErr == nil {
				parsed, err = skill.ParseSkillMD(userSkillPath)
			}
		}

		// 2. Try physical catalog folders
		if parsed == nil {
			if _, statErr := os.Stat("skills"); statErr == nil {
				parsed, err = skill.ParseSkillMD(filepath.Join("skills", id, "SKILL.md"))
			} else if _, statErr := os.Stat("../skills"); statErr == nil {
				parsed, err = skill.ParseSkillMD(filepath.Join("../skills", id, "SKILL.md"))
			} else {
				fallbackStoreDir := filepath.Join(m.workDir, "store", "skills")
				if _, statErr := os.Stat(fallbackStoreDir); statErr == nil {
					parsed, err = skill.ParseSkillMD(filepath.Join(fallbackStoreDir, id, "SKILL.md"))
				}
			}
		}

		// 3. Fallback to embedded filesystem
		if parsed == nil {
			parsed, err = skill.ParseSkillMDFromFS(distFS(), filepath.ToSlash(filepath.Join("skills", id, "SKILL.md")))
		}

		if err == nil && parsed != nil {
			s = parsed
			ok = true
		}
	}

	if !ok || s == nil {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": "skill not found"})
		return
	}

	m.writeJSON(w, http.StatusOK, SkillInfoResponse{
		ID:                     s.ID,
		Name:                   s.Name,
		Description:            s.Description,
		WhenToUse:              s.WhenToUse,
		Category:               s.Category,
		UserInvocable:          s.UserInvocable,
		DisableModelInvocation: s.DisableModelInvocation,
		Context:                s.Context,
		Agent:                  s.Agent,
		FilePath:               s.FilePath,
		AllowedTools:           s.AllowedTools,
		Triggers:               s.Triggers,
		Enabled:                !s.Disabled,
		Body:                   s.Instructions,
	})
}

// handleUpdateSkill updates a user-installed skill's SKILL.md.
// PUT /api/skills/{id}
func (m *Mux) handleUpdateSkill(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	userSkillsDir := m.skillDirs["user"]
	if userSkillsDir == "" {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "user skills directory not configured"})
		return
	}

	var req struct {
		Description string   `json:"description"`
		Body        string   `json:"body"`
		Triggers    []string `json:"triggers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if err := skill.UpdateUserSkill(userSkillsDir, id, req.Description, req.Body, req.Triggers); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if m.skillReg != nil && len(m.skillDirs) > 0 {
		_ = m.skillReg.Rebuild(m.skillDirs)
	}

	m.writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

// handleDeleteSkill deletes a user-installed skill.
// DELETE /api/skills/{id}
func (m *Mux) handleDeleteSkill(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	userSkillsDir := m.skillDirs["user"]
	if userSkillsDir == "" {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "user skills directory not configured"})
		return
	}

	if err := skill.UninstallSkill(userSkillsDir, id); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if m.skillReg != nil && len(m.skillDirs) > 0 {
		_ = m.skillReg.Rebuild(m.skillDirs)
	}

	m.writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// handleGetSkillFiles recursively lists all files inside the skill directory.
// GET /api/skills/{id}/files
func (m *Mux) handleGetSkillFiles(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	userSkillsDir := m.skillDirs["user"]

	// 1. Try reading from user folder
	if userSkillsDir != "" {
		dir := filepath.Join(userSkillsDir, id)
		if _, err := os.Stat(dir); err == nil {
			files, err := skill.ListSkillFiles(dir)
			if err != nil {
				m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			m.writeJSON(w, http.StatusOK, map[string]any{"files": files})
			return
		}
	}

	// 2. Try physical catalog folders
	var diskDir string
	if _, statErr := os.Stat(filepath.Join("skills", id)); statErr == nil {
		diskDir = filepath.Join("skills", id)
	} else if _, statErr := os.Stat(filepath.Join("../skills", id)); statErr == nil {
		diskDir = filepath.Join("../skills", id)
	} else {
		fallbackStoreDir := filepath.Join(m.workDir, "store", "skills")
		if _, statErr := os.Stat(filepath.Join(fallbackStoreDir, id)); statErr == nil {
			diskDir = filepath.Join(fallbackStoreDir, id)
		}
	}

	if diskDir != "" {
		files, err := skill.ListSkillFiles(diskDir)
		if err != nil {
			m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		m.writeJSON(w, http.StatusOK, map[string]any{"files": files})
		return
	}

	// 3. Fallback to embedded filesystem
	virtualDir := filepath.ToSlash(filepath.Join("skills", id))
	files, err := skill.ListSkillFilesFromFS(distFS(), virtualDir)
	if err != nil {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": "skill folder not found"})
		return
	}

	// Map virtual file entries to JSON response format
	var outFiles []any
	for _, entry := range files {
		outFiles = append(outFiles, map[string]any{
			"path": entry.Path,
			"kind": entry.Kind,
			"size": entry.Size,
		})
	}

	m.writeJSON(w, http.StatusOK, map[string]any{"files": outFiles})
}

// handleToggleSkill toggles the enabled/disabled state of a skill by creating/removing .disabled.
// POST /api/skills/{id}/toggle
func (m *Mux) handleToggleSkill(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	userSkillsDir := m.skillDirs["user"]
	if userSkillsDir == "" {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "user skills directory not configured"})
		return
	}

	skillDir := filepath.Join(userSkillsDir, id)
	if _, err := os.Stat(skillDir); os.IsNotExist(err) {
		// If it doesn't exist in user skills, it might be a built-in skill from store.
		// To disable it, we must copy it to user skills first, then disable it (shadowing).
		if err := m.installStoreSkill(r.Context(), userSkillsDir, id); err != nil {
			m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to override built-in skill: " + err.Error()})
			return
		}
	}

	disabledFile := filepath.Join(skillDir, ".disabled")
	enabled := false

	if _, err := os.Stat(disabledFile); err == nil {
		// Currently disabled -> enable it (remove file)
		if err := os.Remove(disabledFile); err != nil {
			m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		enabled = true
	} else {
		// Currently enabled -> disable it (create file)
		if err := os.WriteFile(disabledFile, []byte(""), 0o644); err != nil {
			m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		enabled = false
	}

	if m.skillReg != nil && len(m.skillDirs) > 0 {
		_ = m.skillReg.Rebuild(m.skillDirs)
	}

	m.writeJSON(w, http.StatusOK, map[string]any{"id": id, "enabled": enabled})
}

// handleImportSkill imports a new user-created skill.
// POST /api/skills
func (m *Mux) handleImportSkill(w http.ResponseWriter, r *http.Request) {
	userSkillsDir := m.skillDirs["user"]
	if userSkillsDir == "" {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "user skills directory not configured"})
		return
	}

	var req struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Body        string   `json:"body"`
		Triggers    []string `json:"triggers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if err := skill.ImportUserSkill(userSkillsDir, req.Name, req.Description, req.Body, req.Triggers); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if m.skillReg != nil && len(m.skillDirs) > 0 {
		_ = m.skillReg.Rebuild(m.skillDirs)
	}

	m.writeJSON(w, http.StatusCreated, map[string]string{"status": "imported", "id": req.Name})
}
