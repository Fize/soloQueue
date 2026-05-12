package cli

import (
	"context"
	"fmt"

	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/qqbot"
	"github.com/xiaobaitu/soloqueue/internal/session"
)

// StartQQBot initializes and starts the QQ Bot gateway if configured.
// It creates a dedicated logger under logs/qqbot/ and returns the gateway
// for shutdown coordination. Returns nil if QQ bot is not enabled or not
// configured.
func StartQQBot(cfg *config.GlobalService, mgr *session.SessionManager, workDir string, version string, mainLog *logger.Logger) *qqbot.Gateway {
	settings := cfg.Get()
	qqCfg := settings.QQBot.ToQQBotConfig()

	if !qqCfg.Enabled {
		return nil
	}
	if qqCfg.AppID == "" || qqCfg.AppSecret == "" {
		mainLog.Warn(logger.CatApp, "qqbot enabled but appId/appSecret not configured, skipping")
		return nil
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

	qqAPI := qqbot.NewAPIClient(qqCfg, qqLog)
	qqAdapter := session.NewQQBotAdapter(mgr, qqLog)
	qqBridge := qqbot.NewSessionBridge(qqAdapter, qqAPI, qqLog, qqbot.WithVersion(version))
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

	return gateway
}
