package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/xiaobaitu/soloqueue/internal/teamstore"
)

type createProjectRequest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	Description string `json:"description"`
}

func (m *Mux) handleListProjects(w http.ResponseWriter, r *http.Request) {
	if m.teamstore == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "team store not available"})
		return
	}
	projects, err := m.teamstore.ListProjects(r.Context())
	if err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if projects == nil {
		projects = []teamstore.Project{}
	}
	m.writeJSON(w, http.StatusOK, map[string]any{"projects": projects})
}

func (m *Mux) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	if m.teamstore == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "team store not available"})
		return
	}

	var req createProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid request: %v", err)})
		return
	}
	if req.ID == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "id is required"})
		return
	}
	if req.Name == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if req.Path == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path is required"})
		return
	}

	p := &teamstore.Project{
		ID:          req.ID,
		Name:        req.Name,
		Path:        req.Path,
		Description: req.Description,
	}

	if err := m.teamstore.CreateProject(r.Context(), p); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	m.writeJSON(w, http.StatusCreated, p)
}

func (m *Mux) handleGetProject(w http.ResponseWriter, r *http.Request) {
	if m.teamstore == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "team store not available"})
		return
	}
	id := chi.URLParam(r, "id")
	p, err := m.teamstore.GetProject(r.Context(), id)
	if err != nil {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	m.writeJSON(w, http.StatusOK, p)
}

func (m *Mux) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	if m.teamstore == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "team store not available"})
		return
	}
	id := chi.URLParam(r, "id")

	var req createProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid request: %v", err)})
		return
	}

	p, err := m.teamstore.GetProject(r.Context(), id)
	if err != nil {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	if req.Name != "" {
		p.Name = req.Name
	}
	if req.Path != "" {
		p.Path = req.Path
	}
	if req.Description != "" {
		p.Description = req.Description
	}

	if err := m.teamstore.UpdateProject(r.Context(), id, p); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	m.writeJSON(w, http.StatusOK, p)
}

func (m *Mux) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	if m.teamstore == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "team store not available"})
		return
	}
	id := chi.URLParam(r, "id")
	if err := m.teamstore.DeleteProject(r.Context(), id); err != nil {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	m.writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}

func (m *Mux) handleGetProjectBranches(w http.ResponseWriter, r *http.Request) {
	if m.teamstore == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "team store not available"})
		return
	}
	id := chi.URLParam(r, "id")
	p, err := m.teamstore.GetProject(r.Context(), id)
	if err != nil {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	// Get current active branch
	activeCmd := exec.Command("git", "-C", p.Path, "rev-parse", "--abbrev-ref", "HEAD")
	activeBranchBytes, err := activeCmd.Output()
	activeBranch := "main"
	if err == nil {
		activeBranch = strings.TrimSpace(string(activeBranchBytes))
	}

	cmd := exec.Command("git", "-C", p.Path, "branch", "--format=%(refname:short)")
	output, err := cmd.Output()
	if err != nil {
		m.writeJSON(w, http.StatusOK, map[string]any{"branches": []string{activeBranch}})
		return
	}

	lines := strings.Split(string(output), "\n")
	var branches []string
	branches = append(branches, activeBranch)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && line != activeBranch {
			branches = append(branches, line)
		}
	}

	m.writeJSON(w, http.StatusOK, map[string]any{"branches": branches})
}
