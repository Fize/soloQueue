package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// ─── LLM Providers ───────────────────────────────────────────────────────────

// GET /api/config/providers
func (m *Mux) handleListProviders(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}

	providers := m.configSvc.Get().Providers
	m.writeJSON(w, http.StatusOK, providers)
}

// POST /api/config/providers
func (m *Mux) handleCreateProvider(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
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

	if err := m.configSvc.CreateProvider(p); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}


	m.triggerOnConfigChange()
	m.writeJSON(w, http.StatusCreated, p)
}

// PUT /api/config/providers/{id}
func (m *Mux) handleUpdateProvider(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
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

	if err := m.configSvc.UpdateProvider(id, p); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}


	m.triggerOnConfigChange()
	m.writeJSON(w, http.StatusOK, p)
}

// DELETE /api/config/providers/{id}
func (m *Mux) handleDeleteProvider(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provider id is required"})
		return
	}

	if err := m.configSvc.DeleteProvider(id); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}


	m.triggerOnConfigChange()
	m.writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}

// GET /api/config/providers/{id}/remote-models
func (m *Mux) handleListProviderRemoteModels(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provider id is required"})
		return
	}

	var provider *config.LLMProvider
	for _, p := range m.configSvc.Get().Providers {
		if p.ID == id {
			provider = &p
			break
		}
	}
	if provider == nil {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": "provider not found"})
		return
	}

	apiKey := provider.ResolveAPIKey()
	baseURL := strings.TrimRight(provider.BaseURL, "/")
	if baseURL == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provider base URL is empty"})
		return
	}
	url := baseURL + "/models"

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create request: " + err.Error()})
		return
	}

	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	for k, v := range provider.Headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "request failed: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		m.writeJSON(w, resp.StatusCode, map[string]string{
			"error": fmt.Sprintf("provider API returned HTTP %d: %s", resp.StatusCode, string(bodyBytes)),
		})
		return
	}

	var result struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to decode response: " + err.Error()})
		return
	}

	var modelIDs []string
	for _, model := range result.Data {
		if model.ID != "" {
			modelIDs = append(modelIDs, model.ID)
		}
	}

	sort.Strings(modelIDs)
	m.writeJSON(w, http.StatusOK, modelIDs)
}

// ─── LLM Models ──────────────────────────────────────────────────────────────

// GET /api/config/models
func (m *Mux) handleListModels(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}

	models := m.configSvc.Get().Models
	m.writeJSON(w, http.StatusOK, models)
}

// POST /api/config/models
func (m *Mux) handleCreateModel(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
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

	if err := m.configSvc.CreateModel(model); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}


	m.triggerOnConfigChange()
	m.writeJSON(w, http.StatusCreated, model)
}

// PUT /api/config/models/{id}
func (m *Mux) handleUpdateModel(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
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

	if err := m.configSvc.UpdateModel(id, model); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}


	m.triggerOnConfigChange()
	m.writeJSON(w, http.StatusOK, model)
}

// DELETE /api/config/models/{id}
func (m *Mux) handleDeleteModel(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "model id is required"})
		return
	}

	if err := m.configSvc.DeleteModel(id); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}


	m.triggerOnConfigChange()
	m.writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}

// ─── Default Models ──────────────────────────────────────────────────────────

// GET /api/config/default-models
func (m *Mux) handleGetDefaultModels(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}

	defaultModels := m.configSvc.Get().DefaultModels
	m.writeJSON(w, http.StatusOK, defaultModels)
}

// PUT /api/config/default-models
func (m *Mux) handleUpdateDefaultModels(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}

	var dm config.DefaultModelsConfig
	if err := json.NewDecoder(r.Body).Decode(&dm); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if err := m.configSvc.UpdateDefaultModels(dm); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
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

// ─── Tools Config ────────────────────────────────────────────────────────────

// GET /api/config/tools
func (m *Mux) handleGetToolsConfig(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}
	settings := m.configSvc.Get()
	m.writeJSON(w, http.StatusOK, settings.Tools)
}

// PUT /api/config/tools
func (m *Mux) handleUpdateToolsConfig(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}
	var cfg config.ToolsConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := m.configSvc.UpdateTools(cfg); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	m.triggerOnConfigChange()
	m.writeJSON(w, http.StatusOK, cfg)
}

// ─── QQ Bot Config ───────────────────────────────────────────────────────────

// GET /api/config/qqbot
func (m *Mux) handleGetQQBotsConfig(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}
	settings := m.configSvc.Get()
	m.writeJSON(w, http.StatusOK, settings.QQBots)
}

// PUT /api/config/qqbot
func (m *Mux) handleUpdateQQBotsConfig(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}
	var cfg []config.QQBotConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := m.configSvc.UpdateQQBots(cfg); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	m.triggerOnConfigChange()
	m.writeJSON(w, http.StatusOK, cfg)
}

// ─── LSP MCP Config ──────────────────────────────────────────────────────────

// GET /api/config/lspmcp
func (m *Mux) handleGetLSPMCPConfig(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}
	settings := m.configSvc.Get()
	m.writeJSON(w, http.StatusOK, settings.LSPMCP)
}

// PUT /api/config/lspmcp
func (m *Mux) handleUpdateLSPMCPConfig(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}
	var cfg config.LSPMCPConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := m.configSvc.UpdateLSPMCP(cfg); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	m.triggerOnConfigChange()
	m.writeJSON(w, http.StatusOK, cfg)
}

// ─── Embedding Config ────────────────────────────────────────────────────────

// GET /api/config/embedding
func (m *Mux) handleGetEmbeddingConfig(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}
	settings := m.configSvc.Get()
	m.writeJSON(w, http.StatusOK, settings.Embedding)
}

// PUT /api/config/embedding
func (m *Mux) handleUpdateEmbeddingConfig(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}
	var cfg config.EmbeddingConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := m.configSvc.UpdateEmbedding(cfg); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	m.triggerOnConfigChange()
	m.writeJSON(w, http.StatusOK, cfg)
}

// ─── Session Config ──────────────────────────────────────────────────────────

// GET /api/config/session
func (m *Mux) handleGetSessionConfig(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}
	settings := m.configSvc.Get()
	m.writeJSON(w, http.StatusOK, settings.Session)
}

// PUT /api/config/session
func (m *Mux) handleUpdateSessionConfig(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}
	var cfg config.SessionConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := m.configSvc.UpdateSession(cfg); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	m.triggerOnConfigChange()
	m.writeJSON(w, http.StatusOK, cfg)
}

// ─── Simulation Config ───────────────────────────────────────────────────────

// GET /api/config/simulation
func (m *Mux) handleGetSimulationConfig(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}
	settings := m.configSvc.Get()
	m.writeJSON(w, http.StatusOK, settings.Simulation)
}

// PUT /api/config/simulation
func (m *Mux) handleUpdateSimulationConfig(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}
	var cfg config.SimulationConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	// Normalize zero values to sensible defaults
	if cfg.SimulatedHours <= 0 {
		cfg.SimulatedHours = 168
	}
	if cfg.TickIntervalMs <= 0 {
		cfg.TickIntervalMs = 1000
	}
	if cfg.TimeScale <= 0 {
		cfg.TimeScale = 300
	}
	if cfg.Language == "" {
		cfg.Language = "zh"
	}
	if err := m.configSvc.UpdateSimulation(cfg); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if cfg.DBPath != "" && m.simEngine != nil {
		if err := m.simEngine.SetDBPath(cfg.DBPath); err != nil {
			m.log.WarnContext(r.Context(), logger.CatSimulation, "failed to update simulation engine DB path", "err", err.Error())
		}
	}
	m.triggerOnConfigChange()
	m.writeJSON(w, http.StatusOK, cfg)
}
