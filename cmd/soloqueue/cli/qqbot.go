package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/cron"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/qqbot"
	"github.com/xiaobaitu/soloqueue/internal/session"
)

// msgQueueCap is the buffer capacity for the rate-limiting message queue.
const msgQueueCap = 100

// msgQueueInterval is the minimum interval between active message sends.
// QQ Bot rate limit is ~1.667s per message (3 per 5s); 1.7s provides a safe margin.
const msgQueueInterval = 1700 * time.Millisecond

// StartQQBot initializes and starts the QQ Bot gateway if configured.
// It creates a dedicated logger under logs/qqbot/, sets up a rate-limiting
// MessageQueue, and returns both the gateway and the queue for shutdown
// coordination. Returns (nil, nil) if QQ bot is not enabled or not configured.
// supervisorsFn provides access to L2 supervisors for child agent reaping on /cancel.
// registry is used to unregister the L1 agent on /cancel (restore initial state).
func StartQQBot(cfg *config.GlobalService, mgr *session.SessionManager, cronSched *cron.Scheduler, workDir string, version string, mainLog *logger.Logger, supervisorsFn func() []*agent.Supervisor, registry *agent.Registry) (*qqbot.Gateway, *qqbot.MessageQueue) {
	settings := cfg.Get()
	qqCfg := settings.QQBot.ToQQBotConfig()

	if !qqCfg.Enabled {
		return nil, nil
	}
	if qqCfg.AppID == "" || qqCfg.AppSecret == "" {
		mainLog.Warn(logger.CatApp, "qqbot enabled but appId/appSecret not configured, skipping")
		return nil, nil
	}

	// Create dedicated QQ bot logger under logs/qqbot/
	qqLog, err := logger.New(workDir,
		logger.WithLevel(logger.ParseLogLevel(settings.Log.Level)),
		logger.WithConsole(settings.Log.Console),
		logger.WithFile(settings.Log.File),
		logger.WithLogSubdir("qqbot"),
	)
	if err != nil {
		mainLog.Warn(logger.CatApp, "failed to create qqbot logger, using main logger", "err", err)
		qqLog = mainLog
	}

	qqQueue := qqbot.NewMessageQueue(msgQueueInterval, msgQueueCap)
	qqAPI := qqbot.NewAPIClient(qqCfg, qqLog)
	qqAdapter := session.NewQQBotAdapter(mgr, qqLog)
	qqAdapter.SetSupervisorsFn(supervisorsFn)
	qqAdapter.SetRegistry(registry)
	qqBridge := qqbot.NewSessionBridge(qqAdapter, qqAPI, qqLog,
		qqbot.WithVersion(version),
		qqbot.WithMessageQueue(qqQueue),
	)

	if cronSched != nil {
		cronSched.OnTaskCompleted(func(ctx context.Context, task cron.Task, reply string) {
			msg := qqbot.QQMessage{
				Source:       qqbot.MessageSource(task.QQSource),
				OpenID:       task.QQOpenID,
				TargetOpenID: task.QQTargetOpenID,
				ChatID:       task.QQChatID,
			}
			formatted := qqbot.QQMarkdown(reply)
			if err := qqBridge.SendActiveMessage(ctx, msg, qqbot.MsgTypeMarkdown, formatted); err != nil {
				qqLog.Error(logger.CatApp, "qqbot cron: failed to send active message reply", "task_id", task.ID, "err", err)
			} else {
				qqLog.Info(logger.CatApp, "qqbot cron: active message reply sent successfully", "task_id", task.ID)
			}
		})
	}

	gateway := qqbot.NewGateway(qqCfg, qqBridge, qqAPI, qqLog)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				mainLog.Error(logger.CatApp, "qqbot gateway goroutine panic recovered",
					"panic", fmt.Sprintf("%v", r))
			}
		}()
		qqLog.Info(logger.CatApp, "qqbot gateway starting",
			"app_id", qqCfg.AppID, "sandbox", qqCfg.Sandbox)
		if err := gateway.Run(context.Background()); err != nil {
			qqLog.Warn(logger.CatApp, "qqbot gateway stopped", "err", err.Error())
		}
	}()

	return gateway, qqQueue
}
