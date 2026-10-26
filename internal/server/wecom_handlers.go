package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/xiaobaitu/soloqueue/internal/channel/wecom"
	"github.com/xiaobaitu/soloqueue/internal/config"
)

type wecomBotInput struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	BotID            string   `json:"botId"`
	Secret           string   `json:"secret"`
	Enabled          bool     `json:"enabled"`
	BindType         string   `json:"bind_type"`
	BindAgent        string   `json:"bind_agent"`
	WhitelistEnabled bool     `json:"whitelist_enabled"`
	Whitelist        []string `json:"whitelist"`
}
type wecomBotView struct {
	config.WecomBotConfig
	CredentialConfigured bool   `json:"credentialConfigured"`
	Connected            bool   `json:"connected"`
	State                string `json:"state"`
	Error                string `json:"error,omitempty"`
}

func (m *Mux) wecomViews(bots []config.WecomBotConfig) []wecomBotView {
	views := make([]wecomBotView, 0, len(bots))
	for _, bot := range bots {
		status := wecom.Status{State: "disconnected"}
		if !bot.Enabled {
			status.State = "disabled"
		} else if m.wecomStatus != nil {
			status = m.wecomStatus(bot.ID)
		}
		views = append(views, wecomBotView{WecomBotConfig: bot, CredentialConfigured: bot.Secret != "", Connected: status.Connected, State: status.State, Error: status.Error})
	}
	return views
}
func (m *Mux) handleGetWecomBotsConfig(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, 503, map[string]string{"error": "config service not available"})
		return
	}
	m.writeJSON(w, 200, m.wecomViews(m.configSvc.Get().WecomBots))
}
func normalizeWecomBots(input []wecomBotInput, current []config.WecomBotConfig) ([]config.WecomBotConfig, error) {
	existing := map[string]config.WecomBotConfig{}
	for _, bot := range current {
		existing[bot.ID] = bot
	}
	ids := map[string]bool{}
	botIDs := map[string]bool{}
	result := make([]config.WecomBotConfig, 0, len(input))
	for _, in := range input {
		id := strings.TrimSpace(in.ID)
		if id == "" {
			id = "wecom-" + uuid.NewString()
		}
		if strings.ContainsAny(id, "/\\\x00") || id == "." || id == ".." {
			return nil, fmt.Errorf("invalid WeCom account ID")
		}
		if ids[id] {
			return nil, fmt.Errorf("duplicate WeCom account ID")
		}
		ids[id] = true
		old, exists := existing[id]
		botID := strings.TrimSpace(in.BotID)
		if botID == "" {
			botID = old.BotID
		}
		if botID == "" || strings.ContainsAny(botID, "\r\n\x00") {
			return nil, fmt.Errorf("WeCom Bot ID is required and must be a single line")
		}
		if botIDs[botID] {
			return nil, fmt.Errorf("WeCom Bot ID already belongs to another account")
		}
		botIDs[botID] = true
		secret := strings.TrimSpace(in.Secret)
		if secret == "" {
			if exists && botID != old.BotID {
				return nil, fmt.Errorf("changing Bot ID requires a new Secret")
			}
			secret = old.Secret
		}
		if secret == "" {
			return nil, fmt.Errorf("WeCom Secret is required")
		}
		bindType := strings.TrimSpace(in.BindType)
		if bindType == "" {
			bindType = old.BindType
		}
		if bindType == "" {
			bindType = "l1"
		}
		if bindType != "l1" && bindType != "l2" {
			return nil, fmt.Errorf("invalid WeCom bind_type")
		}
		name := strings.TrimSpace(in.Name)
		if name == "" {
			name = "WeCom"
		}
		bot := config.WecomBotConfig{ID: id, Name: name, BotID: botID, Secret: secret, Enabled: in.Enabled, BindType: bindType, BindAgent: strings.TrimSpace(in.BindAgent), WhitelistEnabled: in.WhitelistEnabled, Whitelist: append([]string(nil), in.Whitelist...)}
		if bindType == "l1" {
			bot.BindAgent = ""
		}
		result = append(result, bot)
	}
	return result, nil
}
func (m *Mux) handleUpdateWecomBotsConfig(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, 503, map[string]string{"error": "config service not available"})
		return
	}
	var input []wecomBotInput
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input) != nil {
		m.writeJSON(w, 400, map[string]string{"error": "invalid WeCom configuration"})
		return
	}
	current := m.configSvc.Get().WecomBots
	bots, err := normalizeWecomBots(input, current)
	if err != nil {
		m.writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	// Account deletion uses DELETE so agent references cannot be silently orphaned.
	for _, old := range current {
		found := false
		for _, bot := range bots {
			if bot.ID == old.ID {
				found = true
				break
			}
		}
		if !found {
			m.writeJSON(w, 400, map[string]string{"error": "use DELETE to remove a WeCom account"})
			return
		}
	}
	for i := range bots {
		bt, ba, err := m.normalizeChannelBinding(bots[i].BindType, bots[i].BindAgent)
		if err != nil {
			m.writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		bots[i].BindType, bots[i].BindAgent = bt, ba
		for _, old := range current {
			if old.ID == bots[i].ID && (old.BindType != bt || old.BindAgent != ba) {
				m.writeJSON(w, 400, map[string]string{"error": "change WeCom bindings from L1 or team agent settings"})
				return
			}
		}
	}
	if err := m.configSvc.UpdateWecomBots(bots); err != nil {
		m.writeJSON(w, 500, map[string]string{"error": "failed to save WeCom configuration"})
		return
	}
	m.triggerOnConfigChange()
	m.writeJSON(w, 200, m.wecomViews(bots))
}
func (m *Mux) handleDeleteWecomBotConfig(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, 503, map[string]string{"error": "config service not available"})
		return
	}
	id := chi.URLParam(r, "accountID")
	bots := m.configSvc.Get().WecomBots
	remaining := make([]config.WecomBotConfig, 0, len(bots))
	found := false
	for _, bot := range bots {
		if bot.ID == id {
			found = true
		} else {
			remaining = append(remaining, bot)
		}
	}
	if !found {
		m.writeJSON(w, 404, map[string]string{"error": "WeCom account not found"})
		return
	}
	if err := m.clearChannelReferences(r.Context(), "wecom", id); err != nil {
		m.writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if err := m.configSvc.UpdateWecomBots(remaining); err != nil {
		m.writeJSON(w, 500, map[string]string{"error": "failed to remove WeCom account"})
		return
	}
	m.triggerOnConfigChange()
	w.WriteHeader(http.StatusNoContent)
}
