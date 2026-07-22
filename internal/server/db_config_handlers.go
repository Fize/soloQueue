package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	if model.ProviderID == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "model provider id is required"})
		return
	}

	if err := m.configSvc.CreateModel(model); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	m.triggerOnConfigChange()
	m.writeJSON(w, http.StatusCreated, model)
}

// PUT /api/config/models/{providerId}/{modelId}
func (m *Mux) handleUpdateModel(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}

	providerID := chi.URLParam(r, "providerId")
	modelID := chi.URLParam(r, "modelId")
	if providerID == "" || modelID == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provider id and model id are required"})
		return
	}

	var model config.LLMModel
	if err := json.NewDecoder(r.Body).Decode(&model); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	model.ID = modelID
	model.ProviderID = providerID

	if err := m.configSvc.UpdateModel(providerID, modelID, model); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	m.triggerOnConfigChange()
	m.writeJSON(w, http.StatusOK, model)
}

// DELETE /api/config/models/{providerId}/{modelId}
func (m *Mux) handleDeleteModel(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}

	providerID := chi.URLParam(r, "providerId")
	modelID := chi.URLParam(r, "modelId")
	if providerID == "" || modelID == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provider id and model id are required"})
		return
	}

	if err := m.configSvc.DeleteModel(providerID, modelID); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	m.triggerOnConfigChange()
	m.writeJSON(w, http.StatusOK, map[string]string{"deleted": modelID, "provider": providerID})
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

type wechatAccountView struct {
	ID                   string   `json:"id"`
	Name                 string   `json:"name"`
	Enabled              bool     `json:"enabled"`
	Connected            bool     `json:"connected"`
	CredentialConfigured bool     `json:"credentialConfigured"`
	BotIDMasked          string   `json:"botIdMasked,omitempty"`
	BaseURL              string   `json:"baseUrl,omitempty"`
	BotAgent             string   `json:"botAgent,omitempty"`
	BindType             string   `json:"bind_type"`
	BindAgent            string   `json:"bind_agent,omitempty"`
	WhitelistEnabled     bool     `json:"whitelist_enabled"`
	Whitelist            []string `json:"whitelist"`
}

func toWechatAccountView(cfg config.WechatBotConfig) wechatAccountView {
	return wechatAccountView{
		ID: cfg.ID, Name: cfg.Name, Enabled: cfg.Enabled,
		Connected: cfg.BotToken != "" && cfg.BotID != "", CredentialConfigured: cfg.BotToken != "",
		BotIDMasked: maskChannelID(cfg.BotID), BaseURL: cfg.BaseURL, BotAgent: cfg.BotAgent,
		BindType: cfg.BindType, BindAgent: cfg.BindAgent,
		WhitelistEnabled: cfg.WhitelistEnabled, Whitelist: cfg.Whitelist,
	}
}

func maskChannelID(value string) string {
	if len(value) <= 8 {
		return value
	}
	return value[:4] + "…" + value[len(value)-4:]
}

// GET /api/config/wechat-bots
func (m *Mux) handleGetWechatBotsConfig(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}
	bots := m.configSvc.Get().WechatBots
	views := make([]wechatAccountView, 0, len(bots))
	for _, bot := range bots {
		views = append(views, toWechatAccountView(bot))
	}
	m.writeJSON(w, http.StatusOK, views)
}

// PUT /api/config/wechat-bots updates non-secret account settings only.
func (m *Mux) handleUpdateWechatBotsConfig(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}
	var cfg []config.WechatBotConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	existing := make(map[string]config.WechatBotConfig)
	for _, bot := range m.configSvc.Get().WechatBots {
		existing[bot.ID] = bot
	}
	for i := range cfg {
		if previous, ok := existing[cfg[i].ID]; ok {
			cfg[i].BotToken = previous.BotToken
			cfg[i].BotID = previous.BotID
		}
	}
	if err := m.configSvc.UpdateWechatBots(cfg); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	m.triggerOnConfigChange()
	views := make([]wechatAccountView, 0, len(cfg))
	for _, bot := range cfg {
		views = append(views, toWechatAccountView(bot))
	}
	m.writeJSON(w, http.StatusOK, views)
}

func (m *Mux) handleDeleteWechatBotConfig(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}
	accountID := chi.URLParam(r, "accountID")
	var confirmation struct {
		ConfirmAccountID string `json:"confirmAccountId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&confirmation); err != nil || confirmation.ConfirmAccountID != accountID {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "account confirmation does not match"})
		return
	}
	bots := m.configSvc.Get().WechatBots
	filtered := bots[:0]
	for _, bot := range bots {
		if bot.ID != accountID {
			filtered = append(filtered, bot)
		}
	}
	if len(filtered) == len(bots) {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": "wechat account not found"})
		return
	}
	if err := m.configSvc.UpdateWechatBots(filtered); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	m.triggerOnConfigChange()
	w.WriteHeader(http.StatusNoContent)
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

// ─── Speech Config ─────────────────────────────────────────────────────────

// GET /api/config/speech
func (m *Mux) handleGetSpeechConfig(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}
	settings := m.configSvc.Get()
	m.writeJSON(w, http.StatusOK, settings.Speech)
}

// PUT /api/config/speech
func (m *Mux) handleUpdateSpeechConfig(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}
	var cfg config.SpeechConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	// Normalize zero values to sensible defaults
	if cfg.Model == "" {
		cfg.Model = "small"
	}
	if err := m.configSvc.UpdateSpeech(cfg); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	m.triggerOnConfigChange()
	m.writeJSON(w, http.StatusOK, cfg)
}

// speechStatusResponse is the response for GET /api/config/speech/status.
type speechStatusResponse struct {
	Enabled          bool   `json:"enabled"`
	Model            string `json:"model"`
	ModelDir         string `json:"modelDir"`
	ModelPath        string `json:"modelPath"`
	ModelExists      bool   `json:"modelExists"`
	WhisperBinary    string `json:"whisperBinary"`
	WhisperAvailable bool   `json:"whisperAvailable"`
	Ready            bool   `json:"ready"`
}

// GET /api/config/speech/status
func (m *Mux) handleGetSpeechStatus(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}
	settings := m.configSvc.Get()
	cfg := settings.Speech

	status := speechStatusResponse{
		Enabled: cfg.Enabled,
		Model:   cfg.Model,
	}

	// Resolve model directory
	modelDir := cfg.ModelDir
	if modelDir == "" {
		workDir, _ := config.DefaultWorkDir()
		if workDir != "" {
			modelDir = filepath.Join(workDir, "models")
		}
	}
	status.ModelDir = modelDir
	modelPath := filepath.Join(modelDir, "ggml-"+cfg.Model+".bin")
	status.ModelPath = modelPath
	if _, err := os.Stat(modelPath); err == nil {
		status.ModelExists = true
	}

	// Check whisper-cli in PATH
	if path, err := exec.LookPath("whisper-cli"); err == nil {
		status.WhisperBinary = path
		status.WhisperAvailable = true
	}

	status.Ready = status.Enabled && status.ModelExists && status.WhisperAvailable
	m.writeJSON(w, http.StatusOK, status)
}

// speechInstallResponse is the response for POST /api/config/speech/install.
type speechInstallResponse struct {
	Success       bool   `json:"success"`
	BinaryPath    string `json:"binaryPath,omitempty"`
	ModelPath     string `json:"modelPath,omitempty"`
	BinaryMessage string `json:"binaryMessage,omitempty"`
	ModelMessage  string `json:"modelMessage,omitempty"`
	Error         string `json:"error,omitempty"`
	Detail        string `json:"detail,omitempty"` // step-by-step instructions on failure
}

// POST /api/config/speech/install
func (m *Mux) handleInstallSpeech(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}

	var req struct {
		Model string `json:"model"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	settings := m.configSvc.Get()
	cfg := settings.Speech

	model := req.Model
	if model == "" {
		model = cfg.Model
	}
	if model == "" {
		model = "small"
	}

	modelDir := cfg.ModelDir
	if modelDir == "" {
		workDir, _ := config.DefaultWorkDir()
		if workDir != "" {
			modelDir = filepath.Join(workDir, "models")
		}
	}

	resp := speechInstallResponse{}

	// ── Step 1: Install whisper-cli binary ──────────────────────────────

	binaryName := "whisper-cli"
	if runtime.GOOS == "windows" {
		binaryName = "whisper-cli.exe"
	}

	if path, err := exec.LookPath(binaryName); err == nil {
		resp.BinaryPath = path
		resp.BinaryMessage = "found in PATH"
	} else {
		installed, msg, detail, err := installWhisperBinary()
		if err != nil {
			resp.Error = err.Error()
			resp.Detail = detail
			m.writeJSON(w, http.StatusInternalServerError, resp)
			return
		}
		resp.BinaryPath = installed
		resp.BinaryMessage = msg
	}

	// ── Step 2: Download model file ────────────────────────────────────

	modelPath := filepath.Join(modelDir, "ggml-"+model+".bin")
	if _, err := os.Stat(modelPath); err == nil {
		resp.ModelPath = modelPath
		resp.ModelMessage = "already exists"
	} else {
		if err := os.MkdirAll(modelDir, 0o755); err != nil {
			resp.Error = fmt.Sprintf("create model dir: %v", err)
			resp.Detail = fmt.Sprintf(
				"无法创建模型目录 %s。\n请检查目录权限后重试。",
				modelDir,
			)
			m.writeJSON(w, http.StatusInternalServerError, resp)
			return
		}
		if detail, err := downloadSpeechModel(model, modelPath); err != nil {
			resp.Error = err.Error()
			resp.Detail = detail
			m.writeJSON(w, http.StatusInternalServerError, resp)
			return
		}
		resp.ModelPath = modelPath
		resp.ModelMessage = "downloaded"
	}

	resp.Success = true
	m.writeJSON(w, http.StatusOK, resp)
}

// installWhisperBinary tries to install whisper-cli.
// On success returns (binaryPath, humanMessage, "", nil).
// On failure returns ("", "", detailInstructions, error).
func installWhisperBinary() (binaryPath, message, detail string, err error) {
	binaryName := "whisper-cli"
	if runtime.GOOS == "windows" {
		binaryName = "whisper-cli.exe"
	}

	switch runtime.GOOS {
	case "darwin":
		if _, lookErr := exec.LookPath("brew"); lookErr == nil {
			cmd := exec.Command("brew", "install", "whisper-cpp")
			cmd.Stdout = io.Discard
			cmd.Stderr = io.Discard
			if runErr := cmd.Run(); runErr != nil {
				detail := `brew install 失败，请手动安装:

1. 打开终端，运行:
   brew install whisper-cpp

2. 如果 brew 报错，尝试更新:
   brew update && brew install whisper-cpp

3. 如果仍然失败，从源码编译:
   git clone https://github.com/ggerganov/whisper.cpp
   cd whisper.cpp && make
   sudo cp build/bin/whisper-cli /usr/local/bin/`
				return "", "", detail, fmt.Errorf("brew install whisper-cpp failed: %w", runErr)
			}
			path, lookErr := exec.LookPath(binaryName)
			if lookErr != nil {
				detail := fmt.Sprintf(
					"brew install 成功，但 %s 未加入 PATH。\n请运行: brew link whisper-cpp",
					binaryName,
				)
				return "", "", detail, fmt.Errorf("brew install succeeded but %s not found in PATH", binaryName)
			}
			return path, "installed via brew", "", nil
		}
		detail := `未找到 Homebrew，无法自动安装 whisper-cli。

请选择以下方式之一安装:

方式 1 — 安装 Homebrew 后自动安装:
  /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
  brew install whisper-cpp

方式 2 — 下载预编译二进制:
  访问 https://github.com/ggerganov/whisper.cpp/releases/latest
  下载适用于 macOS 的 whisper-cli 文件
  将其放入 /usr/local/bin/ 并添加执行权限:
    chmod +x /usr/local/bin/whisper-cli`
		return "", "", detail, fmt.Errorf("Homebrew not found")

	case "linux":
		if _, lookErr := exec.LookPath("apt-get"); lookErr == nil {
			detail := `请在终端中运行以下命令安装 whisper-cpp:

  sudo apt-get update
  sudo apt-get install -y whisper-cpp

如果软件源中没有 whisper-cpp，请从源码编译:
  git clone https://github.com/ggerganov/whisper.cpp
  cd whisper.cpp && make
  sudo cp build/bin/whisper-cli /usr/local/bin/`
			return "", "", detail, fmt.Errorf("需要手动安装 whisper-cpp")
		}
		detail := `请从源码编译安装 whisper-cpp:

  git clone https://github.com/ggerganov/whisper.cpp
  cd whisper.cpp && make
  sudo cp build/bin/whisper-cli /usr/local/bin/

或下载预编译二进制:
  访问 https://github.com/ggerganov/whisper.cpp/releases/latest
  下载适用于 Linux 的二进制文件`
		return "", "", detail, fmt.Errorf("需要手动安装 whisper-cpp")

	default: // windows
		detail := `请手动安装 whisper-cpp:

1. 访问 https://github.com/ggerganov/whisper.cpp/releases/latest
2. 下载 Windows 版本的 whisper-cli.exe
3. 将其放入 PATH 中的任意目录（如 C:\Windows\System32）

或使用 vcpkg:
  vcpkg install whisper-cpp`
		return "", "", detail, fmt.Errorf("需要手动安装 whisper-cpp")
	}
}

// downloadSpeechModel downloads a GGML model file from HuggingFace.
// On success returns ("", nil). On failure returns (detailInstructions, error).
func downloadSpeechModel(model, destPath string) (string, error) {
	url := fmt.Sprintf("https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-%s.bin", model)

	resp, err := http.Get(url)
	if err != nil {
		detail := fmt.Sprintf(`无法连接到 HuggingFace，请手动下载模型文件。

1. 浏览器打开:
   https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-%s.bin

2. 下载后移动到:
   %s

3. 或使用镜像站下载:
   https://hf-mirror.com/ggerganov/whisper.cpp/resolve/main/ggml-%s.bin

网络错误: %v`, model, destPath, model, err)
		return detail, fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			detail := fmt.Sprintf(`模型 "ggml-%s.bin" 不存在。

可用模型: tiny, base, small, medium（注意: large 模型需要下载 large-v3）。

确认模型名称后重试，或手动下载:
  curl -L -o %s https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-%s.bin`, model, destPath, model)
			return detail, fmt.Errorf("model %q not found (HTTP 404)", model)
		}
		detail := fmt.Sprintf(`下载模型失败 (HTTP %d)。

请手动下载:
  curl -L -o %s https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-%s.bin

如果网络受限，可使用国内镜像:
  curl -L -o %s https://hf-mirror.com/ggerganov/whisper.cpp/resolve/main/ggml-%s.bin`, resp.StatusCode, destPath, model, destPath, model)
		return detail, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Sprintf("无法写入文件 %s。\n请检查目录权限。", destPath), err
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		os.Remove(destPath)
		detail := fmt.Sprintf(`下载中断，文件不完整已删除。

请重试一键安装，或手动下载:
  curl -L -o %s https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-%s.bin`, destPath, model)
		return detail, fmt.Errorf("download interrupted: %w", err)
	}

	return "", nil
}
