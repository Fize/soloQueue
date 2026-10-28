package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	qqbot "github.com/xiaobaitu/soloqueue/internal/channel/qq"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/infra/speechinstall"
	"github.com/xiaobaitu/soloqueue/internal/llm/chatgpt"
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

	var revokeWarning error
	if id == "chatgpt" {
		if r.Header.Get("X-SoloQueue-Account-Mutation") != "1" {
			m.writeJSON(w, http.StatusForbidden, map[string]string{"error": "same-origin account confirmation is required"})
			return
		}
		if m.chatGPTAccount == nil {
			m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ChatGPT account manager is unavailable"})
			return
		}
		if err := m.chatGPTAccount.Logout(r.Context()); err != nil {
			if errors.Is(err, chatgpt.ErrLocalCredentialClear) {
				m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not clear local ChatGPT credentials."})
				return
			}
			revokeWarning = err
		}
	}

	if err := m.configSvc.DeleteProvider(id); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	m.triggerOnConfigChange()
	result := map[string]any{"deleted": id}
	if revokeWarning != nil {
		result["remoteRevocationConfirmed"] = false
		result["warning"] = "Local credentials were cleared; remote revocation could not be confirmed."
	}
	m.writeJSON(w, http.StatusOK, result)
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
	if id == "chatgpt" {
		if m.chatGPTAccount == nil {
			m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ChatGPT account manager is unavailable"})
			return
		}
		models, err := m.chatGPTAccount.Models(r.Context())
		if err != nil {
			m.writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		ids := make([]string, 0, len(models))
		for _, model := range models {
			ids = append(ids, model.ID)
		}
		m.writeJSON(w, http.StatusOK, ids)
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

func (m *Mux) handleChatGPTStatus(w http.ResponseWriter, r *http.Request) {
	if chi.URLParam(r, "id") != "chatgpt" {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": "provider not found"})
		return
	}
	if m.chatGPTAccount == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ChatGPT account manager is unavailable"})
		return
	}
	m.writeJSON(w, http.StatusOK, m.chatGPTAccount.Status())
}

func (m *Mux) handleChatGPTModels(w http.ResponseWriter, r *http.Request) {
	if chi.URLParam(r, "id") != "chatgpt" {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": "provider not found"})
		return
	}
	if m.chatGPTAccount == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ChatGPT account manager is unavailable"})
		return
	}
	models, err := m.chatGPTAccount.Models(r.Context())
	if err != nil {
		m.writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	m.writeJSON(w, http.StatusOK, models)
}

func (m *Mux) handleChatGPTLogin(w http.ResponseWriter, r *http.Request) {
	if chi.URLParam(r, "id") != "chatgpt" {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": "provider not found"})
		return
	}
	if r.Header.Get("X-SoloQueue-Account-Mutation") != "1" {
		m.writeJSON(w, http.StatusForbidden, map[string]string{"error": "same-origin account confirmation is required"})
		return
	}
	if m.chatGPTAccount == nil || m.runtimeMetrics == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ChatGPT sign-in requires the local runtime listener"})
		return
	}
	if strings.EqualFold(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")), "cross-site") {
		m.writeJSON(w, http.StatusForbidden, map[string]string{"error": "cross-site ChatGPT sign-in is not allowed"})
		return
	}
	host, port, err := net.SplitHostPort(m.runtimeMetrics.HTTPAddr)
	if err != nil || host != "127.0.0.1" || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ChatGPT sign-in requires a loopback runtime listener"})
		return
	}
	redirectURI := "http://127.0.0.1:" + port + "/auth/callback"
	authURL, err := m.chatGPTAccount.StartLogin(redirectURI)
	if err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	m.writeJSON(w, http.StatusOK, map[string]string{"authorizationUrl": authURL})
}

func (m *Mux) handleChatGPTLogout(w http.ResponseWriter, r *http.Request) {
	if chi.URLParam(r, "id") != "chatgpt" {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": "provider not found"})
		return
	}
	if m.chatGPTAccount == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "ChatGPT account manager is unavailable"})
		return
	}
	if r.Header.Get("X-SoloQueue-Account-Mutation") != "1" {
		m.writeJSON(w, http.StatusForbidden, map[string]string{"error": "same-origin account confirmation is required"})
		return
	}
	err := m.chatGPTAccount.Logout(r.Context())
	if err != nil {
		if errors.Is(err, chatgpt.ErrLocalCredentialClear) {
			m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Could not clear local ChatGPT credentials."})
			return
		}
		m.writeJSON(w, http.StatusOK, map[string]any{"connected": false, "remoteRevocationConfirmed": false, "error": "Local credentials were cleared; remote revocation could not be confirmed."})
		return
	}
	m.writeJSON(w, http.StatusOK, map[string]any{"connected": false, "remoteRevocationConfirmed": true})
}

func (m *Mux) handleChatGPTCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if m.chatGPTAccount == nil {
		http.Error(w, "ChatGPT sign-in is unavailable", http.StatusServiceUnavailable)
		return
	}
	if r.URL.Query().Get("error") != "" {
		_ = m.chatGPTAccount.Complete(r.Context(), r.URL.Query().Get("state"), "", r.URL.Query().Get("client_id"))
		http.Error(w, "ChatGPT sign-in was not completed. Return to SoloQueue Settings and try again.", http.StatusBadRequest)
		return
	}
	if err := m.chatGPTAccount.Complete(r.Context(), r.URL.Query().Get("state"), r.URL.Query().Get("code"), r.URL.Query().Get("client_id")); err != nil {
		http.Error(w, "ChatGPT sign-in failed. Return to SoloQueue Settings and try again.", http.StatusBadRequest)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, `<!doctype html><meta charset="utf-8"><title>ChatGPT connected</title><p>ChatGPT is connected to SoloQueue. You can close this tab and return to Settings.</p>`)
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
	for i := range cfg {
		bindType, bindAgent, err := m.normalizeChannelBinding(cfg[i].BindType, cfg[i].BindAgent)
		if err != nil {
			m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("QQ bot %q: %v", cfg[i].ID, err)})
			return
		}
		cfg[i].BindType, cfg[i].BindAgent = bindType, bindAgent
	}
	currentQQ := m.configSvc.Get().QQBots
	keptQQ := make(map[string]struct{}, len(cfg))
	for _, bot := range cfg {
		keptQQ[bot.ID] = struct{}{}
	}
	for _, bot := range currentQQ {
		if _, ok := keptQQ[bot.ID]; !ok {
			if err := m.clearChannelReferences(r.Context(), "qq", bot.ID); err != nil {
				m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
		}
	}
	if err := m.configSvc.UpdateQQBots(cfg); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	m.triggerOnConfigChange()
	m.writeJSON(w, http.StatusOK, cfg)
}

type telegramBotView struct {
	ID                   string   `json:"id"`
	Name                 string   `json:"name"`
	Enabled              bool     `json:"enabled"`
	Connected            bool     `json:"connected"`
	CredentialConfigured bool     `json:"credentialConfigured"`
	BotIDMasked          string   `json:"botIdMasked,omitempty"`
	Username             string   `json:"username,omitempty"`
	BindType             string   `json:"bind_type"`
	BindAgent            string   `json:"bind_agent,omitempty"`
	WhitelistEnabled     bool     `json:"whitelist_enabled"`
	Whitelist            []string `json:"whitelist,omitempty"`
}

type telegramBotInput struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Enabled          bool     `json:"enabled"`
	BotToken         string   `json:"botToken,omitempty"`
	BindType         string   `json:"bind_type"`
	BindAgent        string   `json:"bind_agent,omitempty"`
	WhitelistEnabled bool     `json:"whitelist_enabled"`
	Whitelist        []string `json:"whitelist,omitempty"`
}

func toTelegramBotView(cfg config.TelegramBotConfig) telegramBotView {
	view := telegramBotView{ID: cfg.ID, Name: cfg.Name, Enabled: cfg.Enabled, Connected: cfg.BotID != 0, CredentialConfigured: cfg.BotToken != "", Username: cfg.Username, BindType: cfg.BindType, BindAgent: cfg.BindAgent, WhitelistEnabled: cfg.WhitelistEnabled, Whitelist: cfg.Whitelist}
	if cfg.BotID != 0 {
		view.BotIDMasked = maskChannelID(fmt.Sprint(cfg.BotID))
	}
	return view
}

func (m *Mux) handleGetTelegramBotsConfig(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}
	bots := m.configSvc.Get().TelegramBots
	views := make([]telegramBotView, 0, len(bots))
	for _, bot := range bots {
		views = append(views, toTelegramBotView(bot))
	}
	m.writeJSON(w, http.StatusOK, views)
}

// handleUpdateTelegramBotsConfig updates non-secret settings. A missing token preserves the old one.
func (m *Mux) handleUpdateTelegramBotsConfig(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}
	var input []telegramBotInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	cfg, err := normalizeTelegramBots(input, m.configSvc.Get().TelegramBots)
	if err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	for i := range cfg {
		bindType, bindAgent, err := m.normalizeChannelBinding(cfg[i].BindType, cfg[i].BindAgent)
		if err != nil {
			m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("Telegram bot %q: %v", cfg[i].ID, err)})
			return
		}
		cfg[i].BindType, cfg[i].BindAgent = bindType, bindAgent
	}
	if err := m.configSvc.UpdateTelegramBots(cfg); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	m.triggerOnConfigChange()
	views := make([]telegramBotView, 0, len(cfg))
	for _, bot := range cfg {
		views = append(views, toTelegramBotView(bot))
	}
	m.writeJSON(w, http.StatusOK, views)
}

// normalizeTelegramBots merges redacted API input with the current settings.
// The UI sends the complete list, so this is also the single place that keeps
// IDs stable, preserves omitted tokens, and rejects ambiguous credentials.
func normalizeTelegramBots(input []telegramBotInput, current []config.TelegramBotConfig) ([]config.TelegramBotConfig, error) {
	existing := make(map[string]config.TelegramBotConfig, len(current))
	for _, bot := range current {
		existing[bot.ID] = bot
	}

	usedIDs := make(map[string]struct{}, len(input))
	usedTokens := make(map[string]string, len(input))

	result := make([]config.TelegramBotConfig, 0, len(input))
	for _, in := range input {
		id := strings.TrimSpace(in.ID)
		if id == "" {
			id = "telegram-" + uuid.NewString()
		}
		if strings.ContainsAny(id, `/\\`) {
			return nil, fmt.Errorf("telegram bot id %q contains an invalid path separator", id)
		}
		if _, duplicate := usedIDs[id]; duplicate {
			return nil, fmt.Errorf("telegram bot id %q is duplicated", id)
		}
		usedIDs[id] = struct{}{}

		old, hasOld := existing[id]
		name := strings.TrimSpace(in.Name)
		if name == "" && hasOld {
			name = old.Name
		}
		if name == "" {
			name = "Telegram"
		}
		bindType := strings.TrimSpace(in.BindType)
		if bindType == "" && hasOld {
			bindType = old.BindType
		}
		if bindType == "" {
			bindType = "l1"
		}
		if bindType != "l1" && bindType != "l2" {
			return nil, fmt.Errorf("telegram bot %q has invalid bind_type %q", id, bindType)
		}

		bot := config.TelegramBotConfig{
			ID:               id,
			Name:             name,
			Enabled:          in.Enabled,
			BindType:         bindType,
			BindAgent:        strings.TrimSpace(in.BindAgent),
			WhitelistEnabled: in.WhitelistEnabled,
			Whitelist:        append([]string(nil), in.Whitelist...),
		}
		if hasOld {
			bot.BotID, bot.BotToken, bot.Username = old.BotID, old.BotToken, old.Username
		}
		if bot.BindType == "l1" {
			bot.BindAgent = ""
		}
		if suppliedToken := strings.TrimSpace(in.BotToken); suppliedToken != "" {
			if !hasOld || suppliedToken != old.BotToken {
				// A changed token belongs to a different bot identity. The
				// gateway will discover the new BotID on its next start.
				bot.BotID = 0
				bot.Username = ""
			}
			bot.BotToken = suppliedToken
		} else if !hasOld {
			return nil, fmt.Errorf("telegram bot %q requires a bot token", id)
		}

		if bot.BotToken != "" {
			if previousID, duplicate := usedTokens[bot.BotToken]; duplicate && previousID != id {
				return nil, fmt.Errorf("telegram bot token is already used by account %q", previousID)
			}
			usedTokens[bot.BotToken] = id
		}
		result = append(result, bot)
	}
	return result, nil
}

func (m *Mux) handleDeleteTelegramBotConfig(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}
	id := chi.URLParam(r, "accountID")
	bots := m.configSvc.Get().TelegramBots
	filtered := make([]config.TelegramBotConfig, 0, len(bots))
	found := false
	for _, bot := range bots {
		if bot.ID == id {
			found = true
		} else {
			filtered = append(filtered, bot)
		}
	}
	if !found {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": "telegram account not found"})
		return
	}
	if err := m.clearChannelReferences(r.Context(), "telegram", id); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := m.configSvc.UpdateTelegramBots(filtered); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	m.triggerOnConfigChange()
	w.WriteHeader(http.StatusNoContent)
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
		bindType, bindAgent, err := m.normalizeChannelBinding(cfg[i].BindType, cfg[i].BindAgent)
		if err != nil {
			m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("WeChat account %q: %v", cfg[i].ID, err)})
			return
		}
		cfg[i].BindType, cfg[i].BindAgent = bindType, bindAgent
		if previous, ok := existing[cfg[i].ID]; ok {
			cfg[i].BotToken = previous.BotToken
			cfg[i].BotID = previous.BotID
		}
	}
	keptWechat := make(map[string]struct{}, len(cfg))
	for _, bot := range cfg {
		keptWechat[bot.ID] = struct{}{}
	}
	for _, bot := range m.configSvc.Get().WechatBots {
		if _, ok := keptWechat[bot.ID]; !ok {
			if err := m.clearChannelReferences(r.Context(), "wechat", bot.ID); err != nil {
				m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
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
	if err := m.clearChannelReferences(r.Context(), "wechat", accountID); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
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
	m.writeJSON(w, http.StatusOK, config.NormalizeEmbedding(settings.Embedding))
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
	if err := config.ValidateEmbedding(cfg); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	cfg = config.NormalizeEmbedding(cfg)
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
	FFmpegBinary     string `json:"ffmpegBinary"`
	FFmpegAvailable  bool   `json:"ffmpegAvailable"`
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
	modelInfo, modelErr := os.Stat(modelPath)
	status.ModelExists = modelErr == nil && modelInfo.Mode().IsRegular()

	transcriber := qqbot.NewTranscriber(cfg.Model, modelDir)
	status.WhisperBinary = transcriber.Binary()
	status.WhisperAvailable = status.WhisperBinary != ""
	status.SilkDecoder = transcriber.SilkDecoder()
	status.SilkAvailable = status.SilkDecoder != ""
	status.FFmpegBinary = transcriber.FFmpeg()
	status.FFmpegAvailable = status.FFmpegBinary != ""
	status.Ready = status.Enabled && status.ModelExists && status.WhisperAvailable && status.SilkAvailable && status.FFmpegAvailable
	m.writeJSON(w, http.StatusOK, status)
}

// speechInstallResponse is the response for POST /api/config/speech/install.
var speechInstallMu sync.Mutex

type speechInstallResponse struct {
	Success       bool   `json:"success"`
	BinaryPath    string `json:"binaryPath,omitempty"`
	ModelPath     string `json:"modelPath,omitempty"`
	SilkPath      string `json:"silkPath,omitempty"`
	FFmpegPath    string `json:"ffmpegPath,omitempty"`
	BinaryMessage string `json:"binaryMessage,omitempty"`
	ModelMessage  string `json:"modelMessage,omitempty"`
	SilkMessage   string `json:"silkMessage,omitempty"`
	FFmpegMessage string `json:"ffmpegMessage,omitempty"`
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

	speechInstallMu.Lock()
	defer speechInstallMu.Unlock()
	settings := m.configSvc.Get()
	cfg := settings.Speech

	model := req.Model
	if model == "" {
		model = cfg.Model
	}
	if model == "" {
		model = "small"
	}

	if _, ok := speechinstall.ModelSHA256[model]; !ok {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported speech model"})
		return
	}

	modelDir := cfg.ModelDir
	if modelDir == "" {
		workDir, _ := config.DefaultWorkDir()
		if workDir != "" {
			modelDir = filepath.Join(workDir, "models")
		}
	}

	resp := speechInstallResponse{}

	if runtime.GOOS == "windows" {
		if runtime.GOARCH != "amd64" {
			resp.Error = "Windows speech tools currently require x64"
			m.writeJSON(w, http.StatusInternalServerError, resp)
			return
		}
		workDir, err := config.DefaultWorkDir()
		if err != nil {
			resp.Error = err.Error()
			m.writeJSON(w, http.StatusInternalServerError, resp)
			return
		}
		transcriber := qqbot.NewTranscriber("", "")
		client := &http.Client{Timeout: 10 * time.Minute}
		if transcriber.Binary() == "" {
			_, err = speechinstall.InstallArchive(r.Context(), client, speechinstall.WhisperWindows, filepath.Join(workDir, "bin"))
			if err != nil {
				resp.Error = err.Error()
				resp.Detail = "Failed to install verified Windows whisper-cli."
				m.writeJSON(w, http.StatusInternalServerError, resp)
				return
			}
		}
		resp.FFmpegPath = transcriber.FFmpeg()
		if resp.FFmpegPath == "" {
			resp.FFmpegPath, err = speechinstall.InstallArchive(r.Context(), client, speechinstall.FFmpegWindows, filepath.Join(workDir, "bin"))
			if err != nil {
				resp.Error = err.Error()
				resp.Detail = "Failed to install verified Windows FFmpeg."
				m.writeJSON(w, http.StatusInternalServerError, resp)
				return
			}
			resp.FFmpegMessage = "installed"
		} else {
			resp.FFmpegMessage = "already available"
		}
	}

	// ── Step 1: Install whisper-cli binary ──────────────────────────────

	if path := qqbot.NewTranscriber("", "").Binary(); path != "" {
		resp.BinaryPath = path
		resp.BinaryMessage = "already available"
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
		installed, msg, detail, err := installSilkDecoder(r.Context())
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
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		resp.Error = fmt.Sprintf("create model dir: %v", err)
		m.writeJSON(w, http.StatusInternalServerError, resp)
		return
	}
	if info, err := os.Stat(modelPath); err == nil && info.Mode().IsRegular() {
		resp.ModelPath = modelPath
		resp.ModelMessage = "already exists"
	} else {
		if detail, err := downloadSpeechModel(r.Context(), model, modelPath); err != nil {
			resp.Error = err.Error()
			resp.Detail = detail
			m.writeJSON(w, http.StatusInternalServerError, resp)
			return
		}
		resp.ModelPath = modelPath
		resp.ModelMessage = "downloaded and verified"
	}

	resp.Success = true
	m.writeJSON(w, http.StatusOK, resp)
}

const silkDecoderRepo = "https://github.com/kn007/silk-v3-decoder.git"

// installSilkDecoder installs the decoder into the SoloQueue tool directory.
func installSilkDecoder(ctx context.Context) (binaryPath, message, detail string, err error) {
	workDir, err := config.DefaultWorkDir()
	if err != nil {
		return "", "", "Cannot determine SoloQueue working directory.", err
	}
	binDir := filepath.Join(workDir, "bin")
	if runtime.GOOS == "windows" {
		executable, err := os.Executable()
		if err != nil {
			return "", "", "Cannot determine SoloQueue executable path.", err
		}
		source := filepath.Join(filepath.Dir(executable), "silk-decoder.exe")
		if _, err := os.Stat(source); err == nil {
			installed, err := speechinstall.InstallDecoder(source, binDir)
			if err != nil {
				return "", "", "Failed to install the bundled SILK decoder.", err
			}
			return installed, "installed from SoloQueue release", "", nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", "", "Cannot access the bundled SILK decoder.", err
		}
		dir := filepath.Join(binDir, speechinstall.DecoderDirectory)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", "", "Cannot create the SILK decoder directory.", err
		}
		target := filepath.Join(dir, "silk-decoder.exe")
		client := &http.Client{Timeout: 10 * time.Minute}
		if err := speechinstall.DownloadVerified(ctx, client, speechinstall.SilkWindowsURL, speechinstall.SilkWindowsSHA256, target, 32<<20); err != nil {
			return "", "", "Failed to download the Windows SILK decoder.", err
		}
		return target, "downloaded from pinned upstream", "", nil
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return "", "", fmt.Sprintf("Cannot create tool directory %s.", binDir), err
	}
	name := "silk-decoder"
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

	if out, err := exec.Command("make", "-C", filepath.Join(repoDir, "silk")).CombinedOutput(); err != nil {
		return "", "", fmt.Sprintf("Failed to compile SILK decoder. Please install C compiler and make: %s", strings.TrimSpace(string(out))), err
	}
	sourcePath := filepath.Join(repoDir, "silk", "decoder")
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

	default:
		return "", "", "SoloQueue speech tools support macOS and Linux only.", fmt.Errorf("unsupported platform %s", runtime.GOOS)
	}
}

// downloadSpeechModel downloads a pinned model, publishing only verified bytes.
func downloadSpeechModel(ctx context.Context, model, destPath string) (string, error) {
	digest, ok := speechinstall.ModelSHA256[model]
	if !ok {
		return "Unsupported model.", fmt.Errorf("unsupported speech model %q", model)
	}
	url := fmt.Sprintf("https://huggingface.co/ggerganov/whisper.cpp/resolve/%s/ggml-%s.bin", speechinstall.ModelRevision, model)
	client := &http.Client{Timeout: 30 * time.Minute}
	if err := speechinstall.DownloadVerified(ctx, client, url, digest, destPath, 3<<30); err != nil {
		return "Unable to download or verify the speech model; retry installation.", err
	}
	return "", nil
}

// PATCH /api/config/tools accepts a strict partial object. Null is not a clear operation:
// clients use empty strings/arrays or false explicitly instead.
func (m *Mux) handlePatchToolsConfig(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, 503, map[string]string{"error": "config service not available"})
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		m.writeJSON(w, 400, map[string]string{"error": "invalid tools patch"})
		return
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil || fields == nil {
		m.writeJSON(w, 400, map[string]string{"error": "tools patch must be an object"})
		return
	}
	var containsNull func(any) bool
	containsNull = func(value any) bool {
		if value == nil {
			return true
		}
		switch v := value.(type) {
		case map[string]any:
			for _, child := range v {
				if containsNull(child) {
					return true
				}
			}
		case []any:
			for _, child := range v {
				if containsNull(child) {
					return true
				}
			}
		}
		return false
	}
	for _, raw := range fields {
		var value any
		_ = json.Unmarshal(raw, &value)
		if containsNull(value) {
			m.writeJSON(w, 400, map[string]string{"error": "null fields are not supported"})
			return
		}
	}
	var patch config.ToolsPatch
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&patch) != nil {
		m.writeJSON(w, 400, map[string]string{"error": "invalid or unknown tools field"})
		return
	}
	if patch.ImageModels != nil {
		if err := config.ValidateImageModels(*patch.ImageModels); err != nil {
			m.writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
	}
	cfg, err := m.configSvc.PatchTools(patch)
	if err != nil {
		m.writeJSON(w, 500, map[string]string{"error": "failed to save tools configuration"})
		return
	}
	m.triggerOnConfigChange()
	m.writeJSON(w, 200, cfg)
}
