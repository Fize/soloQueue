package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/logger"
)

const version = "0.1.0"

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "soloqueue",
		Short: "SoloQueue — AI multi-agent collaboration tool",
		Long: `SoloQueue is an AI multi-agent collaboration tool built on the Actor model.

Run without subcommands for interactive TUI mode.
Use 'soloqueue serve' to start the local HTTP/WebSocket server.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// TODO: 启动 TUI 交互模式（claude code 风格）
			return cmd.Help()
		},
	}

	root.AddCommand(versionCmd())
	root.AddCommand(serveCmd())

	return root
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			workDir, err := defaultWorkDir()
			if err != nil {
				return err
			}

			// 初始化 config
			cfg, err := initConfig(workDir)
			if err != nil {
				return fmt.Errorf("init config: %w", err)
			}
			defer cfg.Close()

			// 初始化 logger
			log, err := initLogger(workDir, cfg)
			if err != nil {
				return fmt.Errorf("init logger: %w", err)
			}
			defer log.Close()

			log.Info(logger.CatApp, "soloqueue starting", "version", version)

			fmt.Printf("soloqueue version %s\n", version)
			fmt.Printf("work dir: %s\n", workDir)

			settings := cfg.Get()
			fmt.Printf("log level: %s\n", settings.Log.Level)

			p := cfg.DefaultProvider()
			if p != nil {
				fmt.Printf("default provider: %s (%s)\n", p.Name, p.ID)
			}

			m := cfg.DefaultModel("")
			if m != nil {
				fmt.Printf("default model: %s (%s)\n", m.Name, m.ID)
			}

			log.Info(logger.CatApp, "version command completed")
			return nil
		},
	}
}

func serveCmd() *cobra.Command {
	var port int
	var host string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the local HTTP/WebSocket server",
		RunE: func(cmd *cobra.Command, args []string) error {
			workDir, err := defaultWorkDir()
			if err != nil {
				return err
			}

			cfg, err := initConfig(workDir)
			if err != nil {
				return fmt.Errorf("init config: %w", err)
			}
			defer cfg.Close()

			log, err := initLogger(workDir, cfg)
			if err != nil {
				return fmt.Errorf("init logger: %w", err)
			}
			defer log.Close()

			log.Info(logger.CatApp, "soloqueue serve starting",
				"host", host,
				"port", port,
				"version", version,
			)

			// TODO: 启动 Fastify/HTTP 服务器（后续 Phase 实现）
			fmt.Printf("soloqueue serve listening on %s:%d\n", host, port)
			fmt.Println("(server not yet implemented)")

			return nil
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", 8765, "HTTP server port")
	cmd.Flags().StringVar(&host, "host", "127.0.0.1", "HTTP server host")

	return cmd
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// defaultWorkDir 返回 ~/.soloqueue
func defaultWorkDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	dir := filepath.Join(home, ".soloqueue")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create work dir %s: %w", dir, err)
	}
	return dir, nil
}

// initConfig 加载并启动热加载
func initConfig(workDir string) (*config.GlobalService, error) {
	cfg, err := config.New(workDir)
	if err != nil {
		return nil, err
	}

	if err := cfg.Load(); err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	if err := cfg.Watch(); err != nil {
		// 热加载失败不阻断启动
		fmt.Fprintf(os.Stderr, "warn: config watch failed: %v\n", err)
	}

	// 若 settings.json 不存在，写入默认值
	settingsPath := filepath.Join(workDir, "settings.json")
	if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
		if err := cfg.Save(); err != nil {
			fmt.Fprintf(os.Stderr, "warn: failed to write default settings: %v\n", err)
		}
	}

	return cfg, nil
}

// initLogger 根据当前配置创建 system 层 Logger
func initLogger(workDir string, cfg *config.GlobalService) (*logger.Logger, error) {
	settings := cfg.Get()

	level := parseLogLevel(settings.Log.Level)
	log, err := logger.System(workDir,
		logger.WithLevel(level),
		logger.WithConsole(settings.Log.Console),
		logger.WithFile(settings.Log.File),
	)
	if err != nil {
		return nil, err
	}

	// 热加载时自动更新日志级别（注：slog.Logger 不支持动态修改级别，重建 logger）
	// 此处仅演示 OnChange 用法
	cfg.OnChange(func(old, new config.Settings) {
		if old.Log.Level != new.Log.Level {
			fmt.Fprintf(os.Stderr, "info: log level changed: %s → %s\n", old.Log.Level, new.Log.Level)
		}
	})

	// fsnotify watcher 的 error 路由到 logger（之前被静默吞掉）
	cfg.SetErrorHandler(func(err error) {
		log.Error(logger.CatConfig, "config watcher error", "err", err)
	})

	return log, nil
}

// parseLogLevel 将字符串日志级别转为 slog.Level
func parseLogLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
