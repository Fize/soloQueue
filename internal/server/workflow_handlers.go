package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/xiaobaitu/soloqueue/internal/workflow"
)

func (m *Mux) workflowAvailable(w http.ResponseWriter) bool {
	if m.workflowStore == nil || m.workflowRuns == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "workflow system not configured"})
		return false
	}
	return true
}

func workflowStatus(err error) int {
	if err == nil {
		return http.StatusOK
	}
	msg := err.Error()
	if strings.Contains(msg, "not found") {
		return http.StatusNotFound
	}
	if strings.Contains(msg, "name mismatch") {
		return http.StatusConflict
	}
	return http.StatusUnprocessableEntity
}

func (m *Mux) validateWorkflowAgentTemplates(raw []byte) error {
	parsed, err := workflow.ParseWorkflow(raw)
	if err != nil {
		return err
	}
	// Tests and embedders may intentionally omit the template registry. The
	// production server always supplies it.
	if len(m.templates) == 0 {
		return nil
	}
	available := make(map[string]struct{}, len(m.templates))
	for _, template := range m.templates {
		available[template.ID] = struct{}{}
	}
	for key, ref := range parsed.Agents {
		if _, ok := available[ref.Template]; !ok {
			return fmt.Errorf(
				"workflow: agent %q references missing template %q",
				key,
				ref.Template,
			)
		}
	}
	return nil
}

func (m *Mux) handleListWorkflows(w http.ResponseWriter, r *http.Request) {
	if !m.workflowAvailable(w) {
		return
	}
	metas, err := m.workflowStore.List()
	if err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if metas == nil {
		metas = []workflow.WorkflowMeta{}
	}
	m.writeJSON(w, http.StatusOK, metas)
}

func (m *Mux) handleGetWorkflow(w http.ResponseWriter, r *http.Request) {
	if !m.workflowAvailable(w) {
		return
	}
	name := chi.URLParam(r, "name")
	raw, err := m.workflowStore.ReadRaw(name)
	if err != nil {
		m.writeJSON(w, workflowStatus(err), map[string]string{"error": err.Error()})
		return
	}
	pw, err := m.workflowStore.Load(name)
	if err != nil {
		m.writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	m.writeJSON(w, http.StatusOK, map[string]any{"name": name, "yaml": string(raw), "meta": workflow.WorkflowMeta{Name: pw.Name, Description: pw.Description, Version: pw.Version, Valid: true}})
}

type workflowWriteRequest struct {
	Name string `json:"name"`
	YAML string `json:"yaml"`
}

func (m *Mux) decodeWorkflowWrite(w http.ResponseWriter, r *http.Request) (workflowWriteRequest, bool) {
	var req workflowWriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.YAML) == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and yaml are required"})
		return workflowWriteRequest{}, false
	}
	return req, true
}

func (m *Mux) handleCreateWorkflow(w http.ResponseWriter, r *http.Request) {
	if !m.workflowAvailable(w) {
		return
	}
	req, ok := m.decodeWorkflowWrite(w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if _, err := m.workflowStore.ReadRaw(req.Name); err == nil {
		m.writeJSON(w, http.StatusConflict, map[string]string{"error": "workflow already exists"})
		return
	}
	if err := m.validateWorkflowAgentTemplates([]byte(req.YAML)); err != nil {
		m.writeJSON(w, workflowStatus(err), map[string]string{"error": err.Error()})
		return
	}
	meta, err := m.workflowStore.Save(req.Name, []byte(req.YAML))
	if err != nil {
		m.writeJSON(w, workflowStatus(err), map[string]string{"error": err.Error()})
		return
	}
	m.writeJSON(w, http.StatusCreated, meta)
}

func (m *Mux) handleUpdateWorkflow(w http.ResponseWriter, r *http.Request) {
	if !m.workflowAvailable(w) {
		return
	}
	name := chi.URLParam(r, "name")
	if _, err := m.workflowStore.ReadRaw(name); err != nil {
		m.writeJSON(w, workflowStatus(err), map[string]string{"error": err.Error()})
		return
	}
	req, ok := m.decodeWorkflowWrite(w, r)
	if !ok {
		return
	}
	if err := m.validateWorkflowAgentTemplates([]byte(req.YAML)); err != nil {
		m.writeJSON(w, workflowStatus(err), map[string]string{"error": err.Error()})
		return
	}
	meta, err := m.workflowStore.Save(name, []byte(req.YAML))
	if err != nil {
		m.writeJSON(w, workflowStatus(err), map[string]string{"error": err.Error()})
		return
	}
	m.writeJSON(w, http.StatusOK, meta)
}

func (m *Mux) handleDeleteWorkflow(w http.ResponseWriter, r *http.Request) {
	if !m.workflowAvailable(w) {
		return
	}
	if err := m.workflowStore.Delete(chi.URLParam(r, "name")); err != nil {
		m.writeJSON(w, workflowStatus(err), map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *Mux) handleValidateWorkflow(w http.ResponseWriter, r *http.Request) {
	if !m.workflowAvailable(w) {
		return
	}
	req, ok := m.decodeWorkflowWrite(w, r)
	if !ok {
		return
	}
	if err := m.validateWorkflowAgentTemplates([]byte(req.YAML)); err != nil {
		m.writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"valid": false, "error": err.Error()})
		return
	}
	m.writeJSON(w, http.StatusOK, map[string]any{"valid": true})
}

func (m *Mux) handleStartWorkflowRun(w http.ResponseWriter, r *http.Request) {
	if !m.workflowAvailable(w) {
		return
	}
	name := chi.URLParam(r, "name")
	wf, err := m.workflowStore.Load(name)
	if err != nil {
		m.writeJSON(w, workflowStatus(err), map[string]string{"error": err.Error()})
		return
	}
	var req struct {
		Input string `json:"input"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	// Runs outlive the request that created them; using r.Context() would cancel
	// the workflow as soon as this handler returns.
	id, err := m.workflowRuns.Start(context.Background(), wf, req.Input)
	if err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	m.writeJSON(w, http.StatusAccepted, map[string]string{"run_id": id})
}

func (m *Mux) handleListWorkflowRuns(w http.ResponseWriter, r *http.Request) {
	if !m.workflowAvailable(w) {
		return
	}
	runs, err := m.workflowRuns.List(chi.URLParam(r, "name"))
	if err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	m.writeJSON(w, http.StatusOK, runs)
}

func (m *Mux) handleGetWorkflowRun(w http.ResponseWriter, r *http.Request) {
	if !m.workflowAvailable(w) {
		return
	}
	detail, err := m.workflowRuns.Get(chi.URLParam(r, "runID"))
	if err != nil {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	if detail.WorkflowName != chi.URLParam(r, "name") {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": "workflow run not found"})
		return
	}
	m.writeJSON(w, http.StatusOK, detail)
}

func (m *Mux) handleCancelWorkflowRun(w http.ResponseWriter, r *http.Request) {
	if !m.workflowAvailable(w) {
		return
	}
	id := chi.URLParam(r, "runID")
	detail, err := m.workflowRuns.Get(id)
	if err != nil || detail.WorkflowName != chi.URLParam(r, "name") {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": "workflow run not found"})
		return
	}
	if !m.workflowRuns.Cancel(id) {
		m.writeJSON(w, http.StatusConflict, map[string]string{"error": "workflow run is not active"})
		return
	}
	m.writeJSON(w, http.StatusAccepted, map[string]string{"run_id": id})
}
