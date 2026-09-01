package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/channel"
	qqbot "github.com/xiaobaitu/soloqueue/internal/channel/qq"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/runtime"
	"github.com/xiaobaitu/soloqueue/internal/session"
)

// msgQueueCap is the buffer capacity for the rate-limiting message queue.
const msgQueueCap = 100

// msgQueueInterval is the minimum interval between active message sends.
// QQ Bot rate limit is ~1.667s per message (3 per 5s); 1.7s provides a safe margin.
const msgQueueInterval = 1700 * time.Millisecond

// startQQBots creates QQ Bot gateways with dedicated loggers and rate-limiting
// queues. Returns empty slices when no QQ bot is enabled.
func startQQBots(settings config.Settings, mgr *session.SessionManager, l2Store *session.L2SessionStore, rt *runtime.Stack, workDir string, version string, mainLog *logger.Logger, supervisorsFn func() []*agent.Supervisor, registry *agent.Registry) ([]*qqbot.Gateway, []*qqbot.MessageQueue, []*qqbot.SessionBridge) {
	var gateways []*qqbot.Gateway
	var queues []*qqbot.MessageQueue
	var bridges []*qqbot.SessionBridge

	// Initialize speech-to-text transcriber (shared across all QQ bots).
	var transcriber *qqbot.Transcriber
	if settings.Speech.Enabled {
		modelDir := settings.Speech.ModelDir
		if modelDir == "" {
			modelDir = filepath.Join(workDir, "models")
		}
		transcriber = qqbot.NewTranscriber(settings.Speech.Model, modelDir)
		if transcriber.Available() {
			mainLog.Info(logger.CatApp, "speech transcriber initialized",
				"model", transcriber.Model(),
				"model_path", transcriber.ModelPath(),
				"binary", transcriber.Binary(),
				"silk_decoder", transcriber.SilkDecoder(),
			)
		} else {
			mainLog.Warn(logger.CatApp, "speech enabled but required decoder, whisper-cli, or model file not found",
				"model", settings.Speech.Model,
				"model_dir", modelDir,
			)
		}
	}

	for _, qqCfgBase := range settings.QQBots {
		qqCfg := qqCfgBase.ToQQBotConfig()
		if !qqCfg.Enabled {
			continue
		}
		if qqCfg.AppID == "" || qqCfg.AppSecret == "" {
			mainLog.Warn(logger.CatApp, "qqbot enabled but appId/appSecret not configured, skipping", "bot_id", qqCfgBase.ID)
			continue
		}

		// Create dedicated QQ bot logger under logs/qqbot-<id>/
		botSubdir := "qqbot"
		if qqCfgBase.ID != "" {
			botSubdir = "qqbot-" + qqCfgBase.ID
		}
		qqLog, err := logger.New(workDir,
			logger.WithLevel(logger.ParseLogLevel(settings.Log.Level)),
			logger.WithConsole(settings.Log.Console),
			logger.WithFile(settings.Log.File),
			logger.WithLogSubdir(botSubdir),
		)
		if err != nil {
			mainLog.Warn(logger.CatApp, "failed to create qqbot logger, using main logger", "bot_id", qqCfgBase.ID, "err", err)
			qqLog = mainLog
		}

		qqQueue := qqbot.NewMessageQueue(msgQueueInterval, msgQueueCap)
		qqAPI := qqbot.NewAPIClient(qqCfg, qqLog)

		var sessionProvider channel.SessionProvider = newChannelSessionProvider(channelBinding{
			channelID: "qqbot",
			accountID: qqCfg.AppID,
			bindType:  qqCfgBase.BindType,
			bindAgent: qqCfgBase.BindAgent,
		}, mgr, l2Store, rt, workDir, qqLog, supervisorsFn, registry)

		qqBridge := qqbot.NewSessionBridge(sessionProvider, qqAPI, qqLog,
			qqbot.WithAccountID(qqCfg.AppID),
			qqbot.WithVersion(version),
			qqbot.WithMessageQueue(qqQueue),
			qqbot.WithWhitelist(qqCfgBase.WhitelistEnabled, qqCfgBase.Whitelist),
			qqbot.WithTranscriber(transcriber),
		)

		gateway := qqbot.NewGateway(qqCfg, qqBridge, qqAPI, qqLog)

		go func(g *qqbot.Gateway, log *logger.Logger, appID string, sandbox bool) {
			defer func() {
				if r := recover(); r != nil {
					mainLog.Error(logger.CatApp, "qqbot gateway goroutine panic recovered",
						"panic", fmt.Sprintf("%v", r))
				}
			}()
			log.Info(logger.CatApp, "qqbot gateway starting",
				"app_id", appID, "sandbox", sandbox)
			if err := g.Run(context.Background()); err != nil {
				log.Warn(logger.CatApp, "qqbot gateway stopped", "err", err.Error())
			}
		}(gateway, qqLog, qqCfg.AppID, qqCfg.Sandbox)

		gateways = append(gateways, gateway)
		queues = append(queues, qqQueue)
		bridges = append(bridges, qqBridge)
	}

	return gateways, queues, bridges
}

// QQBotManager manages the lifecycle of all QQBots, coordinating hot reloads on configuration changes.
type QQBotManager struct {
	mu            sync.Mutex
	cfg           *config.GlobalService
	mgr           *session.SessionManager
	l2Store       *session.L2SessionStore
	rt            *runtime.Stack
	workDir       string
	version       string
	mainLog       *logger.Logger
	supervisorsFn func() []*agent.Supervisor
	registry      *agent.Registry

	gateways []*qqbot.Gateway
	queues   []*qqbot.MessageQueue
	bridges  []*qqbot.SessionBridge
}

// NewQQBotManager creates a new QQBotManager.
func NewQQBotManager(cfg *config.GlobalService, mgr *session.SessionManager, l2Store *session.L2SessionStore, rt *runtime.Stack, workDir string, version string, mainLog *logger.Logger, supervisorsFn func() []*agent.Supervisor, registry *agent.Registry) *QQBotManager {
	m := &QQBotManager{
		cfg:           cfg,
		mgr:           mgr,
		l2Store:       l2Store,
		rt:            rt,
		workDir:       workDir,
		version:       version,
		mainLog:       mainLog,
		supervisorsFn: supervisorsFn,
		registry:      registry,
	}

	channel.RegisterSenderFactory("qq", func(ctx context.Context, data []byte, text string) error {
		var msg qqbot.QQMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return err
		}
		formatted := qqbot.QQMarkdown(text)

		m.mu.Lock()
		bridges := make([]*qqbot.SessionBridge, len(m.bridges))
		copy(bridges, m.bridges)
		m.mu.Unlock()

		if len(bridges) == 0 {
			return fmt.Errorf("no active qq bridge available")
		}
		if msg.AccountID == "" && len(bridges) > 1 {
			return fmt.Errorf("qq route metadata has no account id and multiple bridges are active")
		}

		var lastErr error
		for _, b := range bridges {
			if msg.AccountID != "" && b.AccountID() != msg.AccountID {
				continue
			}
			err := b.SendActiveMessage(ctx, msg, qqbot.MsgTypeMarkdown, formatted)
			if err == nil {
				return nil
			}
			lastErr = err
		}
		if lastErr == nil {
			return fmt.Errorf("no active qq bridge for account %q", msg.AccountID)
		}
		return fmt.Errorf("qq bridge failed to send: %w", lastErr)
	})
	channel.RegisterMediaSenderFactory("qq", func(ctx context.Context, data []byte, media []channel.OutboundMedia) error {
		var msg qqbot.QQMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return err
		}
		m.mu.Lock()
		bridges := append([]*qqbot.SessionBridge(nil), m.bridges...)
		m.mu.Unlock()
		if msg.AccountID == "" && len(bridges) > 1 {
			return fmt.Errorf("qq route metadata has no account id and multiple bridges are active")
		}
		for _, bridge := range bridges {
			if msg.AccountID != "" && bridge.AccountID() != msg.AccountID {
				continue
			}
			return bridge.SendMediaForMessage(ctx, msg, media)
		}
		return fmt.Errorf("no active qq bridge for account %q", msg.AccountID)
	})

	return m
}

// Reload stops all running gateways and starts fresh ones from current config.
func (m *QQBotManager) Reload() {
	m.ReloadWithSettings(m.cfg.Get())
}

// ReloadWithSettings rebuilds gateways from an accepted settings snapshot.
func (m *QQBotManager) ReloadWithSettings(settings config.Settings) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.mainLog.Info(logger.CatApp, "hot-reloading QQBots...")

	for _, gw := range m.gateways {
		if gw != nil {
			gw.Close()
		}
	}
	for _, q := range m.queues {
		if q != nil {
			q.Stop()
		}
	}

	gateways, queues, bridges := startQQBots(settings, m.mgr, m.l2Store, m.rt, m.workDir, m.version, m.mainLog, m.supervisorsFn, m.registry)
	m.gateways = gateways
	m.queues = queues
	m.bridges = bridges

	m.mainLog.Info(logger.CatApp, "QQBots hot-reloaded successfully", "bot_count", len(m.gateways))
}

// Shutdown gracefully shuts down all QQBots.
func (m *QQBotManager) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.mainLog.Info(logger.CatApp, "shutting down QQBots...")
	for _, gw := range m.gateways {
		if gw != nil {
			gw.Close()
		}
	}
	for _, q := range m.queues {
		if q != nil {
			q.Stop()
		}
	}
	m.gateways = nil
	m.queues = nil
	m.bridges = nil
}
