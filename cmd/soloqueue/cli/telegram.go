package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/channel"
	qqbot "github.com/xiaobaitu/soloqueue/internal/channel/qq"
	telegram "github.com/xiaobaitu/soloqueue/internal/channel/telegram"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/runtime"
	"github.com/xiaobaitu/soloqueue/internal/session"
	"path/filepath"
	"sync"
	"time"
)

type TelegramBotManager struct {
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
	gateways         []*telegram.Gateway
	clients          map[string]*telegram.Client
}

func NewTelegramBotManager(cfg *config.GlobalService, mgr *session.SessionManager, l2Store *session.L2SessionStore, rt *runtime.Stack, workDir, version string, log *logger.Logger, supervisorsFn func() []*agent.Supervisor, registry *agent.Registry) *TelegramBotManager {
	m := &TelegramBotManager{cfg: cfg, mgr: mgr, l2Store: l2Store, rt: rt, workDir: workDir, version: version, log: log, supervisorsFn: supervisorsFn, registry: registry, clients: map[string]*telegram.Client{}}
	channel.RegisterSenderFactory("telegram", func(ctx context.Context, data []byte, text string) error {
		var msg channel.Message
		if err := json.Unmarshal(data, &msg); err != nil {
			return err
		}
		m.mu.Lock()
		c := m.clients[msg.AccountID]
		m.mu.Unlock()
		if c == nil {
			return fmt.Errorf("no active telegram bot for account %q", msg.AccountID)
		}
		return c.SendText(ctx, msg, text)
	})
	channel.RegisterMediaSenderFactory("telegram", func(ctx context.Context, data []byte, media []channel.OutboundMedia) error {
		var msg channel.Message
		if err := json.Unmarshal(data, &msg); err != nil {
			return err
		}
		m.mu.Lock()
		c := m.clients[msg.AccountID]
		m.mu.Unlock()
		if c == nil {
			return fmt.Errorf("no active telegram bot for account %q", msg.AccountID)
		}
		return c.SendMedia(ctx, msg, media)
	})
	return m
}
func (m *TelegramBotManager) Reload() { m.ReloadWithSettings(m.cfg.Get()) }
func (m *TelegramBotManager) ReloadWithSettings(settings config.Settings) {
	m.reloadMu.Lock()
	defer m.reloadMu.Unlock()
	m.mu.Lock()
	for _, g := range m.gateways {
		g.Close()
	}
	m.gateways = nil
	m.clients = map[string]*telegram.Client{}
	m.mu.Unlock()
	var gateways []*telegram.Gateway
	clients := map[string]*telegram.Client{}
	var transcriber *qqbot.Transcriber
	if settings.Speech.Enabled {
		modelDir := settings.Speech.ModelDir
		if modelDir == "" {
			modelDir = filepath.Join(m.workDir, "models")
		}
		candidate := qqbot.NewTranscriber(settings.Speech.Model, modelDir)
		if candidate.AudioAvailable() {
			transcriber = candidate
		}
	}
	for _, base := range settings.TelegramBots {
		if !base.Enabled || base.BotToken == "" {
			continue
		}
		id := base.ID
		if id == "" {
			id = "telegram"
		}
		tc := telegram.Config{Enabled: true, Token: base.BotToken, BotID: base.BotID, AccountID: id}
		client := telegram.NewClient(tc, m.log)
		verifyCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if me, err := client.GetMe(verifyCtx); err != nil {
			m.log.Warn(logger.CatApp, "telegram bot token validation failed; gateway will retry", "bot_id", id, "err", err.Error())
		} else {
			tc.BotID = me.ID
			client = telegram.NewClient(tc, m.log)
		}
		cancel()
		provider := newChannelSessionProvider(channelBinding{channelID: "telegram", accountID: id, bindType: base.BindType, bindAgent: base.BindAgent}, m.mgr, m.l2Store, m.rt, m.workDir, m.log, m.supervisorsFn, m.registry)
		bridge := channel.NewTextBridge(provider, client, m.log, m.version, base.WhitelistEnabled, base.Whitelist)
		if transcriber != nil {
			bridge.SetVoiceTranscriber(transcriber.TranscribeAudio)
		}
		gw := telegram.NewGateway(tc, client, bridge, m.log)
		gateways = append(gateways, gw)
		clients[id] = client
		go func(g *telegram.Gateway) {
			if err := g.Run(context.Background()); err != nil && err != telegram.ErrClosed {
				m.log.Warn(logger.CatApp, "telegram gateway stopped", "err", err.Error())
			}
		}(gw)
	}
	m.mu.Lock()
	m.gateways = gateways
	m.clients = clients
	m.mu.Unlock()
	m.log.Info(logger.CatApp, "Telegram bots hot-reloaded", "bot_count", len(gateways))
}
func (m *TelegramBotManager) Shutdown() {
	m.reloadMu.Lock()
	defer m.reloadMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, g := range m.gateways {
		g.Close()
	}
	m.gateways = nil
	m.clients = map[string]*telegram.Client{}
}
