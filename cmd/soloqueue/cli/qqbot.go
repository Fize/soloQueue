package cli

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/channel"
	qqbot "github.com/xiaobaitu/soloqueue/internal/channel/qq"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/cron"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/runtime"
	"github.com/xiaobaitu/soloqueue/internal/session"
)

// msgQueueCap is the buffer capacity for the rate-limiting message queue.
const msgQueueCap = 100

// msgQueueInterval is the minimum interval between active message sends.
// QQ Bot rate limit is ~1.667s per message (3 per 5s); 1.7s provides a safe margin.
const msgQueueInterval = 1700 * time.Millisecond

// StartQQBots initializes and starts the QQ Bot gateways if configured.
// It creates a dedicated logger under logs/qqbot/, sets up rate-limiting
// MessageQueues, and returns both the gateways and the queues for shutdown
// coordination. Returns empty slices if no QQ bot is enabled or configured.
func StartQQBots(cfg *config.GlobalService, mgr *session.SessionManager, l2Store *session.L2SessionStore, rt *runtime.Stack, workDir string, version string, mainLog *logger.Logger, supervisorsFn func() []*agent.Supervisor, registry *agent.Registry) ([]*qqbot.Gateway, []*qqbot.MessageQueue, []*qqbot.SessionBridge) {
	settings := cfg.Get()
	var gateways []*qqbot.Gateway
	var queues []*qqbot.MessageQueue
	var bridges []*qqbot.SessionBridge

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
			qqbot.WithVersion(version),
			qqbot.WithMessageQueue(qqQueue),
			qqbot.WithWhitelist(qqCfgBase.WhitelistEnabled, qqCfgBase.Whitelist),
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
	cronSched     *cron.Scheduler
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
func NewQQBotManager(cfg *config.GlobalService, mgr *session.SessionManager, l2Store *session.L2SessionStore, rt *runtime.Stack, cronSched *cron.Scheduler, workDir string, version string, mainLog *logger.Logger, supervisorsFn func() []*agent.Supervisor, registry *agent.Registry) *QQBotManager {
	manager := &QQBotManager{
		cfg:           cfg,
		mgr:           mgr,
		l2Store:       l2Store,
		rt:            rt,
		cronSched:     cronSched,
		workDir:       workDir,
		version:       version,
		mainLog:       mainLog,
		supervisorsFn: supervisorsFn,
		registry:      registry,
	}

	if cronSched != nil {
		cronSched.OnTaskCompleted(func(ctx context.Context, task cron.Task, reply string, mediaFiles []cron.SendFileMedia) {
			manager.mu.Lock()
			currentBridges := manager.bridges
			manager.mu.Unlock()

			msg := qqbot.QQMessage{
				Source:       qqbot.MessageSource(task.QQSource),
				OpenID:       task.QQOpenID,
				TargetOpenID: task.QQTargetOpenID,
				ChatID:       task.QQChatID,
			}
			formatted := qqbot.QQMarkdown(reply)

			for _, bridge := range currentBridges {
				// Try to send via each bridge. Since a target is specific to a bot, only the correct bot will succeed.
				err := bridge.SendActiveMessage(ctx, msg, qqbot.MsgTypeMarkdown, formatted)
				if err == nil {
					if len(mediaFiles) > 0 {
						var pm []qqbot.PendingMedia
						for _, m := range mediaFiles {
							pm = append(pm, qqbot.PendingMedia{
								FileType:   m.FileType,
								URL:        m.URL,
								Base64Data: m.Base64Data,
								FileName:   m.FileName,
							})
						}
						bridge.SendMediaList(ctx, msg, pm)
					}
					break
				}
			}
		})
	}

	return manager
}

// Reload reloads the QQBots. It stops all currently running gateways and queues, and then starts new ones based on the latest configuration.
func (m *QQBotManager) Reload() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.mainLog.Info(logger.CatApp, "hot-reloading QQBots...")

	// Stop existing gateways and queues
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

	// Start new gateways and queues
	gateways, queues, bridges := StartQQBots(m.cfg, m.mgr, m.l2Store, m.rt, m.workDir, m.version, m.mainLog, m.supervisorsFn, m.registry)
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
