package server

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// ─── LLM Providers ───────────────────────────────────────────────────────────

// GET /api/config/providers
func (m *Mux) handleListProviders(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil || m.configSvc.GetDB() == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "configuration database not available"})
		return
	}

	providers, err := config.LoadProviders(r.Context(), m.configSvc.GetDB())
	if err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	m.writeJSON(w, http.StatusOK, providers)
}

// POST /api/config/providers
func (m *Mux) handleCreateProvider(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil || m.configSvc.GetDB() == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "configuration database not available"})
		return
	}

	var p config.LLMProvider
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if p.ID == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provider id is required"})
		return
	}

	if err := config.SaveProvider(r.Context(), m.configSvc.GetDB(), p); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if err := m.configSvc.ReloadFromDB(); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to reload config: " + err.Error()})
		return
	}

	m.triggerOnConfigChange()
	m.writeJSON(w, http.StatusCreated, p)
}

// PUT /api/config/providers/{id}
func (m *Mux) handleUpdateProvider(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil || m.configSvc.GetDB() == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "configuration database not available"})
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provider id is required"})
		return
	}

	var p config.LLMProvider
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	p.ID = id

	if err := config.SaveProvider(r.Context(), m.configSvc.GetDB(), p); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if err := m.configSvc.ReloadFromDB(); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to reload config: " + err.Error()})
		return
	}

	m.triggerOnConfigChange()
	m.writeJSON(w, http.StatusOK, p)
}

// DELETE /api/config/providers/{id}
func (m *Mux) handleDeleteProvider(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil || m.configSvc.GetDB() == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "configuration database not available"})
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provider id is required"})
		return
	}

	if err := config.DeleteProvider(r.Context(), m.configSvc.GetDB(), id); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if err := m.configSvc.ReloadFromDB(); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to reload config: " + err.Error()})
		return
	}

	m.triggerOnConfigChange()
	m.writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}

// ─── LLM Models ──────────────────────────────────────────────────────────────

// GET /api/config/models
func (m *Mux) handleListModels(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil || m.configSvc.GetDB() == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "configuration database not available"})
		return
	}

	models, err := config.LoadModels(r.Context(), m.configSvc.GetDB())
	if err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	m.writeJSON(w, http.StatusOK, models)
}

// POST /api/config/models
func (m *Mux) handleCreateModel(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil || m.configSvc.GetDB() == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "configuration database not available"})
		return
	}

	var model config.LLMModel
	if err := json.NewDecoder(r.Body).Decode(&model); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if model.ID == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "model id is required"})
		return
	}

	if err := config.SaveModel(r.Context(), m.configSvc.GetDB(), model); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if err := m.configSvc.ReloadFromDB(); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to reload config: " + err.Error()})
		return
	}

	m.triggerOnConfigChange()
	m.writeJSON(w, http.StatusCreated, model)
}

// PUT /api/config/models/{id}
func (m *Mux) handleUpdateModel(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil || m.configSvc.GetDB() == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "configuration database not available"})
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "model id is required"})
		return
	}

	var model config.LLMModel
	if err := json.NewDecoder(r.Body).Decode(&model); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	model.ID = id

	if err := config.SaveModel(r.Context(), m.configSvc.GetDB(), model); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if err := m.configSvc.ReloadFromDB(); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to reload config: " + err.Error()})
		return
	}

	m.triggerOnConfigChange()
	m.writeJSON(w, http.StatusOK, model)
}

// DELETE /api/config/models/{id}
func (m *Mux) handleDeleteModel(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil || m.configSvc.GetDB() == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "configuration database not available"})
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "model id is required"})
		return
	}

	if err := config.DeleteModel(r.Context(), m.configSvc.GetDB(), id); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if err := m.configSvc.ReloadFromDB(); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to reload config: " + err.Error()})
		return
	}

	m.triggerOnConfigChange()
	m.writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}

// ─── Default Models ──────────────────────────────────────────────────────────

// GET /api/config/default-models
func (m *Mux) handleGetDefaultModels(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil || m.configSvc.GetDB() == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "configuration database not available"})
		return
	}

	defaultModels, err := config.LoadDefaultModels(r.Context(), m.configSvc.GetDB())
	if err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	m.writeJSON(w, http.StatusOK, defaultModels)
}

// PUT /api/config/default-models
func (m *Mux) handleUpdateDefaultModels(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil || m.configSvc.GetDB() == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "configuration database not available"})
		return
	}

	var dm config.DefaultModelsConfig
	if err := json.NewDecoder(r.Body).Decode(&dm); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if err := config.SaveDefaultModels(r.Context(), m.configSvc.GetDB(), dm); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if err := m.configSvc.ReloadFromDB(); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to reload config: " + err.Error()})
		return
	}

	m.triggerOnConfigChange()
	m.writeJSON(w, http.StatusOK, dm)
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func (m *Mux) triggerOnConfigChange() {
	if m.onConfigChange != nil {
		if err := m.onConfigChange(); err != nil && m.log != nil {
			m.log.WarnContext(rContext(), logger.CatConfig, "onConfigChange callback failed", "err", err.Error())
		}
	}
}

// fallback context since some background changes might not carry request ctx
func rContext() context.Context {
	return context.Background()
}
