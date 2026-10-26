package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/channel"
	"github.com/xiaobaitu/soloqueue/internal/channel/wecom"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/runtime"
	"github.com/xiaobaitu/soloqueue/internal/session"
)

type WecomBotManager struct {
	mu               sync.Mutex
	reloadMu         sync.Mutex
	cfg              *config.GlobalService
	mgr              *session.SessionManager
	l2Store          *session.L2SessionStore
	rt               *runtime.Stack
	workDir, version string
	log              *logger.Logger
	supervisorsFn    func() []*agent.Supervisor
	registry         *agent.Registry
	clients          map[string]*wecom.Client
	accounts         map[string]config.WecomBotConfig
}

func NewWecomBotManager(cfg *config.GlobalService, mgr *session.SessionManager, l2Store *session.L2SessionStore, rt *runtime.Stack, workDir, version string, log *logger.Logger, supervisorsFn func() []*agent.Supervisor, registry *agent.Registry) *WecomBotManager {
	m := &WecomBotManager{cfg: cfg, mgr: mgr, l2Store: l2Store, rt: rt, workDir: workDir, version: version, log: log, supervisorsFn: supervisorsFn, registry: registry, clients: map[string]*wecom.Client{}, accounts: map[string]config.WecomBotConfig{}}
	channel.RegisterSenderFactory("wecom", m.sendNotification)
	return m
}
func (m *WecomBotManager) sendNotification(ctx context.Context, data []byte, text string) error {
	var msg channel.Message
	if json.Unmarshal(data, &msg) != nil || msg.Channel != "wecom" || msg.AccountID == "" || msg.UserID == "" || msg.ConversationID == "" {
		return errors.New("wecom notification route invalid; send a new message")
	}
	m.mu.Lock()
	client := m.clients[msg.AccountID]
	running := m.accounts[msg.AccountID]
	m.mu.Unlock()
	// Consult current settings as well as runtime state; reload may not yet have
	// completed when a disabled account's task finishes.
	var account *config.WecomBotConfig
	for _, bot := range m.cfg.Get().WecomBots {
		if bot.ID == msg.AccountID {
			copy := bot
			account = &copy
			break
		}
	}
	if account != nil && (msg.AccountIdentity == "" || account.BotID != msg.AccountIdentity) {
		return errors.New("wecom notification bot identity changed; send a new message")
	}
	if account == nil || !account.Enabled || account.Secret == "" || client == nil {
		return errors.New("wecom notification account unavailable")
	}
	if running.BotID != "" && account != nil && running.BotID != account.BotID {
		return errors.New("wecom notification runtime is reloading")
	}
	if binding, ok := channel.NotificationBindingFromContext(ctx); ok {
		if binding.AccountID == "" && binding.Group == "" {
			return errors.New("wecom notification has no current account binding")
		}
		if binding.AccountID != "" && binding.AccountID != account.ID {
			return errors.New("wecom notification binding mismatch")
		}
		if account.BindType == "l2" {
			if binding.Group == "" || binding.Group != account.BindAgent {
				return errors.New("wecom notification group mismatch")
			}
		} else if binding.Group != "" {
			return errors.New("wecom notification session mismatch")
		}
	}
	if account.WhitelistEnabled {
		allowed := false
		for _, user := range account.Whitelist {
			if user == msg.UserID {
				allowed = true
				break
			}
		}
		if !allowed {
			return errors.New("wecom notification recipient denied")
		}
	}
	msg.ReplyToken = ""
	return client.SendNotification(ctx, msg, text)
}
func (m *WecomBotManager) Status(id string) wecom.Status {
	m.mu.Lock()
	client := m.clients[id]
	m.mu.Unlock()
	if client == nil {
		return wecom.Status{State: "disconnected"}
	}
	return client.Status()
}
func (m *WecomBotManager) Reload() { m.ReloadWithSettings(m.cfg.Get()) }
func (m *WecomBotManager) ReloadWithSettings(settings config.Settings) {
	m.reloadMu.Lock()
	defer m.reloadMu.Unlock()
	m.mu.Lock()
	for _, client := range m.clients {
		client.Close()
	}
	m.clients = map[string]*wecom.Client{}
	m.accounts = map[string]config.WecomBotConfig{}
	for _, bot := range settings.WecomBots {
		if !bot.Enabled || bot.BotID == "" || bot.Secret == "" {
			continue
		}
		provider := newChannelSessionProvider(channelBinding{channelID: "wecom", accountID: bot.ID, bindType: bot.BindType, bindAgent: bot.BindAgent}, m.mgr, m.l2Store, m.rt, m.workDir, m.log, m.supervisorsFn, m.registry)
		client := wecom.NewClient(wecom.Config{AccountID: bot.ID, BotID: bot.BotID, Secret: bot.Secret, WhitelistEnabled: bot.WhitelistEnabled, Whitelist: bot.Whitelist}, m.log)
		client.SetHandler(channel.NewTextBridge(provider, client, m.log, m.version, bot.WhitelistEnabled, bot.Whitelist))
		m.clients[bot.ID] = client
		m.accounts[bot.ID] = bot
		go func() {
			if err := client.Run(context.Background()); err != nil && !errors.Is(err, wecom.ErrClosed) {
				m.log.Warn(logger.CatApp, "wecom gateway stopped", "account_id", bot.ID, "reason", fmt.Sprint(err))
			}
		}()
	}
	m.mu.Unlock()
}
func (m *WecomBotManager) Shutdown() {
	m.reloadMu.Lock()
	defer m.reloadMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, client := range m.clients {
		client.Close()
	}
	m.clients = map[string]*wecom.Client{}
	m.accounts = map[string]config.WecomBotConfig{}
}
