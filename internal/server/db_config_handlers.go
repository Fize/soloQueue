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
	qqbot "github.com/xiaobaitu/soloqueue/internal/channel/qq"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
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

// ─── Model Routes ────────────────────────────────────────────────────────────

// GET /api/config/model-routes
func (m *Mux) handleGetModelRoutes(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}

	m.writeJSON(w, http.StatusOK, m.configSvc.Get().ModelRoutes)
}

// PUT /api/config/model-routes
func (m *Mux) handleUpdateModelRoutes(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}

	var routes config.ModelRoutesConfig
	if err := json.NewDecoder(r.Body).Decode(&routes); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if err := m.configSvc.UpdateModelRoutes(routes); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	m.triggerOnConfigChange()
	m.writeJSON(w, http.StatusOK, routes)
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func (m *Mux) triggerOnConfigChange() {
	if m.onConfigChange != nil {
		if err := m.onConfigChange(m.configSvc.Get()); err != nil && m.log != nil {
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
	SilkDecoder      string `json:"silkDecoder"`
	SilkAvailable    bool   `json:"silkAvailable"`
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
	status.SilkDecoder = qqbot.NewTranscriber("", "").SilkDecoder()
	status.SilkAvailable = status.SilkDecoder != ""

	status.Ready = status.Enabled && status.ModelExists && status.WhisperAvailable && status.SilkAvailable
	m.writeJSON(w, http.StatusOK, status)
}

// speechInstallResponse is the response for POST /api/config/speech/install.
type speechInstallResponse struct {
	Success       bool   `json:"success"`
	BinaryPath    string `json:"binaryPath,omitempty"`
	ModelPath     string `json:"modelPath,omitempty"`
	SilkPath      string `json:"silkPath,omitempty"`
	BinaryMessage string `json:"binaryMessage,omitempty"`
	ModelMessage  string `json:"modelMessage,omitempty"`
	SilkMessage   string `json:"silkMessage,omitempty"`
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

	// ── Step 2: Install SILK decoder ────────────────────────────────────
	if decoder := qqbot.NewTranscriber("", "").SilkDecoder(); decoder != "" {
		resp.SilkPath = decoder
		resp.SilkMessage = "already exists"
	} else {
		installed, msg, detail, err := installSilkDecoder()
		if err != nil {
			resp.Error = err.Error()
			resp.Detail = detail
			m.writeJSON(w, http.StatusInternalServerError, resp)
			return
		}
		resp.SilkPath = installed
		resp.SilkMessage = msg
	}

	// ── Step 3: Download model file ────────────────────────────────────

	modelPath := filepath.Join(modelDir, "ggml-"+model+".bin")
	if _, err := os.Stat(modelPath); err == nil {
		resp.ModelPath = modelPath
		resp.ModelMessage = "already exists"
	} else {
		if err := os.MkdirAll(modelDir, 0o755); err != nil {
			resp.Error = fmt.Sprintf("create model dir: %v", err)
			resp.Detail = fmt.Sprintf(
				"Cannot create model directory %s.\nPlease check directory permissions and try again.",
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

const silkDecoderRepo = "https://github.com/kn007/silk-v3-decoder.git"

// installSilkDecoder installs the decoder into ~/.soloqueue/bin. macOS and
// Linux build the upstream SDK; Windows uses the upstream prebuilt executable.
func installSilkDecoder() (binaryPath, message, detail string, err error) {
	workDir, err := config.DefaultWorkDir()
	if err != nil {
		return "", "", "Cannot determine SoloQueue working directory.", err
	}
	binDir := filepath.Join(workDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return "", "", fmt.Sprintf("Cannot create tool directory %s.", binDir), err
	}
	name := "silk-decoder"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	destPath := filepath.Join(binDir, name)

	if _, err := exec.LookPath("git"); err != nil {
		return "", "", "git not found. Cannot download SILK decoder. Please install git and try again.", err
	}
	tmpDir, err := os.MkdirTemp("", "soloqueue-silk-*")
	if err != nil {
		return "", "", "Cannot create temporary installation directory.", err
	}
	defer os.RemoveAll(tmpDir)
	repoDir := filepath.Join(tmpDir, "silk-v3-decoder")
	if out, err := exec.Command("git", "clone", "--depth", "1", silkDecoderRepo, repoDir).CombinedOutput(); err != nil {
		return "", "", fmt.Sprintf("Failed to download SILK decoder: %s", strings.TrimSpace(string(out))), err
	}

	var sourcePath string
	if runtime.GOOS == "windows" {
		sourcePath = filepath.Join(repoDir, "windows", "silk_v3_decoder.exe")
	} else {
		if out, err := exec.Command("make", "-C", filepath.Join(repoDir, "silk")).CombinedOutput(); err != nil {
			return "", "", fmt.Sprintf("Failed to compile SILK decoder. Please install C compiler and make: %s", strings.TrimSpace(string(out))), err
		}
		sourcePath = filepath.Join(repoDir, "silk", "decoder")
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return "", "", "SILK decoder binary missing after build.", err
	}
	defer source.Close()
	dest, err := os.OpenFile(destPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return "", "", fmt.Sprintf("Cannot write to %s.", destPath), err
	}
	_, copyErr := io.Copy(dest, source)
	closeErr := dest.Close()
	if copyErr != nil || closeErr != nil {
		return "", "", "Failed to write SILK decoder binary.", firstError(copyErr, closeErr)
	}
	return destPath, "installed to SoloQueue tools directory", "", nil
}

func firstError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
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
				detail := `brew install failed, please install manually:

1. Open terminal and run:
   brew install whisper-cpp

2. If brew errors, try updating:
   brew update && brew install whisper-cpp

3. If it still fails, compile from source:
   git clone https://github.com/ggerganov/whisper.cpp
   cd whisper.cpp && make
   sudo cp build/bin/whisper-cli /usr/local/bin/`
				return "", "", detail, fmt.Errorf("brew install whisper-cpp failed: %w", runErr)
			}
			path, lookErr := exec.LookPath(binaryName)
			if lookErr != nil {
				detail := fmt.Sprintf(
					"brew install succeeded, but %s is not in PATH.\nPlease run: brew link whisper-cpp",
					binaryName,
				)
				return "", "", detail, fmt.Errorf("brew install succeeded but %s not found in PATH", binaryName)
			}
			return path, "installed via brew", "", nil
		}
		detail := `Homebrew not found. Unable to auto-install whisper-cli.

Please select one of the following options to install:

Option 1 — Install Homebrew then install whisper-cpp:
  /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
  brew install whisper-cpp

Option 2 — Download prebuilt binary:
  Visit https://github.com/ggerganov/whisper.cpp/releases/latest
  Download the whisper-cli binary for macOS
  Place it in /usr/local/bin/ and make it executable:
    chmod +x /usr/local/bin/whisper-cli`
		return "", "", detail, fmt.Errorf("Homebrew not found")

	case "linux":
		if _, lookErr := exec.LookPath("apt-get"); lookErr == nil {
			detail := `Please run the following commands in terminal to install whisper-cpp:

  sudo apt-get update
  sudo apt-get install -y whisper-cpp

If whisper-cpp is not available in repositories, compile from source:
  git clone https://github.com/ggerganov/whisper.cpp
  cd whisper.cpp && make
  sudo cp build/bin/whisper-cli /usr/local/bin/`
			return "", "", detail, fmt.Errorf("manual installation required for whisper-cpp")
		}
		detail := `Please compile and install whisper-cpp from source:

  git clone https://github.com/ggerganov/whisper.cpp
  cd whisper.cpp && make
  sudo cp build/bin/whisper-cli /usr/local/bin/

Or download a prebuilt binary:
  Visit https://github.com/ggerganov/whisper.cpp/releases/latest
  Download the Linux binary`
		return "", "", detail, fmt.Errorf("manual installation required for whisper-cpp")

	default: // windows
		detail := `Please install whisper-cpp manually:

1. Visit https://github.com/ggerganov/whisper.cpp/releases/latest
2. Download whisper-cli.exe for Windows
3. Place it in any directory in your PATH (e.g. C:\Windows\System32)

Or using vcpkg:
  vcpkg install whisper-cpp`
		return "", "", detail, fmt.Errorf("manual installation required for whisper-cpp")
	}
}

// downloadSpeechModel downloads a GGML model file from HuggingFace.
// On success returns ("", nil). On failure returns (detailInstructions, error).
func downloadSpeechModel(model, destPath string) (string, error) {
	url := fmt.Sprintf("https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-%s.bin", model)

	resp, err := http.Get(url)
	if err != nil {
		detail := fmt.Sprintf(`Cannot connect to HuggingFace. Please download the model file manually.

1. Open in browser:
   https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-%s.bin

2. After downloading, move to:
   %s

3. Or download via mirror site:
   https://hf-mirror.com/ggerganov/whisper.cpp/resolve/main/ggml-%s.bin

Network error: %v`, model, destPath, model, err)
		return detail, fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			detail := fmt.Sprintf(`Model "ggml-%s.bin" does not exist.

Available models: tiny, base, small, medium (note: large model requires downloading large-v3).

Confirm model name and retry, or download manually:
  curl -L -o %s https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-%s.bin`, model, destPath, model)
			return detail, fmt.Errorf("model %q not found (HTTP 404)", model)
		}
		detail := fmt.Sprintf(`Failed to download model (HTTP %d).

Please download manually:
  curl -L -o %s https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-%s.bin

If network is restricted, use a mirror site:
  curl -L -o %s https://hf-mirror.com/ggerganov/whisper.cpp/resolve/main/ggml-%s.bin`, resp.StatusCode, destPath, model, destPath, model)
		return detail, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Sprintf("Cannot write file %s.\nPlease check directory permissions.", destPath), err
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		os.Remove(destPath)
		detail := fmt.Sprintf(`Download interrupted. Incomplete file deleted.

Please retry installation or download manually:
  curl -L -o %s https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-%s.bin`, destPath, model)
		return detail, fmt.Errorf("download interrupted: %w", err)
	}

	return "", nil
}
