package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/llm/deepseek"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/server"
	"github.com/xiaobaitu/soloqueue/internal/session"
	"github.com/xiaobaitu/soloqueue/internal/tools"
	"github.com/xiaobaitu/soloqueue/internal/tui"
)

// resolveAPIKey 读取 provider.APIKeyEnv 指定的环境变量
func resolveAPIKey(primary string) string {
	return os.Getenv(primary)
}

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
Use 'soloqueue serve' to start the local HTTP/WebSocket server.

Environment:
  ALT_SCREEN=1    Enable fullscreen TUI with fixed bottom input (default: inline mode)`,
		SilenceUsage: true,
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

			log, err := initLogger(workDir, cfg, false)
			if err != nil {
				return fmt.Errorf("init logger: %w", err)
			}
			defer log.Close()

			settings := cfg.Get()
			provider := cfg.DefaultProvider()
			if provider == nil {
				return errors.New("no default provider configured")
			}
			defaultModel := cfg.DefaultModel("")
			if defaultModel == nil {
				return errors.New("no default model configured")
			}

			apiKey := resolveAPIKey(provider.APIKeyEnv)
			if apiKey == "" {
				log.Warn(logger.CatApp, "LLM API key not set", "env", provider.APIKeyEnv)
			}

			baseURL := provider.BaseURL
			if v := os.Getenv("DEEPSEEK_BASE_URL"); v != "" && baseURL == "" {
				baseURL = v
			}

			llmClient, err := deepseek.NewClient(deepseek.Config{
				BaseURL:   baseURL,
				APIKey:    apiKey,
				Headers:   provider.Headers,
				TimeoutMs: provider.TimeoutMs,
				Retry: llm.RetryPolicy{
					MaxRetries:   provider.Retry.MaxRetries,
					InitialDelay: time.Duration(provider.Retry.InitialDelayMs) * time.Millisecond,
					MaxDelay:     time.Duration(provider.Retry.MaxDelayMs) * time.Millisecond,
					Multiplier:   provider.Retry.BackoffMultiplier,
				},
				Log: log,
			})
			if err != nil {
				return fmt.Errorf("build llm client: %w", err)
			}

			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("get working directory: %w", err)
			}
			allowedDirs := append([]string{workDir, cwd}, settings.Tools.AllowedDirs...)
			toolsCfg := toolsConfigFromSettings(settings.Tools, allowedDirs)
			toolList := tools.Build(toolsCfg)

			// 共享 Tokenizer（所有 session 复用同一个编码实例）
			tokenizer := ctxwin.NewTokenizer()

			factory := func(ctx context.Context, teamID string) (*agent.Agent, *ctxwin.ContextWindow, error) {
				agentID := newAgentID()
				// APIModel 优先，空则 fallback 到 ID
				effectiveModelID := defaultModel.APIModel
				if effectiveModelID == "" {
					effectiveModelID = defaultModel.ID
				}
				def := agent.Definition{
					ID:              agentID,
					TeamID:          teamID,
					Kind:            agent.KindChat,
					ModelID:         effectiveModelID,
					Temperature:     defaultModel.Generation.Temperature,
					MaxTokens:       defaultModel.Generation.MaxTokens,
					ReasoningEffort: defaultModel.Thinking.ReasoningEffort,
					MaxIterations:   10,
					ContextWindow:   defaultModel.ContextWindow,
				}
				effectiveTeam := teamID
				if effectiveTeam == "" {
					effectiveTeam = "default"
				}
				sessLog, err := logger.Session(workDir, effectiveTeam, agentID,
					logger.WithLevel(parseLogLevel(settings.Log.Level)),
					logger.WithConsole(false), // TUI 模式不在 stderr 输出日志
					logger.WithFile(settings.Log.File),
					)
				if err != nil {
					return nil, nil, fmt.Errorf("build session logger: %w", err)
				}
				a := agent.NewAgent(def, llmClient, sessLog,
					agent.WithTools(toolList...),
					agent.WithParallelTools(true),
					agent.WithToolTimeout("shell_exec", 30*time.Second),
					agent.WithToolTimeout("http_fetch", 10*time.Second),
					agent.WithToolTimeout("web_search", 15*time.Second),
				)

				// 创建 ContextWindow 并 push system prompt
				cw := ctxwin.NewContextWindow(defaultModel.ContextWindow, defaultModel.ContextWindow/10, tokenizer)
				if def.SystemPrompt != "" {
					cw.Push(ctxwin.RoleSystem, def.SystemPrompt)
				}

				if err := a.Start(context.Background()); err != nil {
					return nil, nil, err
				}
				return a, cw, nil
			}

			mgr := session.NewSessionManager(factory, 30*time.Minute)

			rootCtx, stop := signal.NotifyContext(context.Background(),
				os.Interrupt, syscall.SIGTERM)
			defer stop()
			go mgr.ReapLoop(rootCtx, time.Minute, 5*time.Second)
			defer mgr.Shutdown(5 * time.Second)

			log.Info(logger.CatApp, "soloqueue tui starting",
				"version", version, "model", defaultModel.ID)

			return tui.Run(tui.Config{
				SessionMgr: mgr,
				ModelID:    defaultModel.ID,
				Version:    version,
			})
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

			// 初始化 logger（version 命令为非交互模式，console 默认关闭）
			log, err := initLogger(workDir, cfg, false)
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
	var verbose bool

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

			log, err := initLogger(workDir, cfg, verbose)
			if err != nil {
				return fmt.Errorf("init logger: %w", err)
			}
			defer log.Close()

			log.Info(logger.CatApp, "soloqueue serve starting",
				"host", host,
				"port", port,
				"version", version,
			)

			settings := cfg.Get()

			// ── Tools：沙箱根目录默认落在 workDir + CWD；用户在 settings.json
			// 通过 tools.allowedDirs 追加 ────────────────────────────────────
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("get working directory: %w", err)
			}
			allowedDirs := append([]string{workDir, cwd}, settings.Tools.AllowedDirs...)
			toolsCfg := toolsConfigFromSettings(settings.Tools, allowedDirs)
			toolList := tools.Build(toolsCfg)

			// ── LLM 工厂：从 default provider + default model 构造 DeepSeek
			//    客户端（目前仅 DeepSeek；未来可按 Provider.ID 分派）─────────
			provider := cfg.DefaultProvider()
			if provider == nil {
				return errors.New("no default provider configured")
			}
			defaultModel := cfg.DefaultModel("")
			if defaultModel == nil {
				return errors.New("no default model configured")
			}
			apiKey := resolveAPIKey(provider.APIKeyEnv)
			if apiKey == "" {
				log.Warn(logger.CatApp, "LLM API key not set in env",
					"env", provider.APIKeyEnv,
				)
			}
			baseURL := provider.BaseURL
			if v := os.Getenv("DEEPSEEK_BASE_URL"); v != "" && baseURL == "" {
				baseURL = v
			}

			llmClient, err := deepseek.NewClient(deepseek.Config{
				BaseURL:   baseURL,
				APIKey:    apiKey,
				Headers:   provider.Headers,
				TimeoutMs: provider.TimeoutMs,
				Retry: llm.RetryPolicy{
					MaxRetries:   provider.Retry.MaxRetries,
					InitialDelay: time.Duration(provider.Retry.InitialDelayMs) * time.Millisecond,
					MaxDelay:     time.Duration(provider.Retry.MaxDelayMs) * time.Millisecond,
					Multiplier:   provider.Retry.BackoffMultiplier,
				},
				Log: log,
			})
			if err != nil {
				return fmt.Errorf("build deepseek client: %w", err)
			}

			// ── Agent factory ───────────────────────────────────────────────
			// 共享 Tokenizer
			tokenizer := ctxwin.NewTokenizer()

			factory := func(ctx context.Context, teamID string) (*agent.Agent, *ctxwin.ContextWindow, error) {
				agentID := newAgentID()
				// APIModel 优先，空则 fallback 到 ID
				effectiveModelID := defaultModel.APIModel
				if effectiveModelID == "" {
					effectiveModelID = defaultModel.ID
				}
				def := agent.Definition{
					ID:              agentID,
					TeamID:          teamID,
					Kind:            agent.KindChat,
					ModelID:         effectiveModelID,
					Temperature:     defaultModel.Generation.Temperature,
					MaxTokens:       defaultModel.Generation.MaxTokens,
					ReasoningEffort: defaultModel.Thinking.ReasoningEffort,
					MaxIterations:   10,
					ContextWindow:   defaultModel.ContextWindow,
				}

				// agent 使用 session-layer logger（CatActor / CatLLM / CatTool）
				// teamID 可为空（demo 场景）；Session 层要求非空 id，退化到 "default"
				effectiveTeam := teamID
				if effectiveTeam == "" {
					effectiveTeam = "default"
				}
				sessLog, err := logger.Session(workDir, effectiveTeam, agentID,
					logger.WithLevel(parseLogLevel(settings.Log.Level)),
					logger.WithConsole(settings.Log.Console),
					logger.WithFile(settings.Log.File),
			)
				if err != nil {
					return nil, nil, fmt.Errorf("build session logger: %w", err)
				}

				a := agent.NewAgent(def, llmClient, sessLog,
					agent.WithTools(toolList...),
					agent.WithParallelTools(true),
					agent.WithToolTimeout("shell_exec", 30*time.Second),
					agent.WithToolTimeout("http_fetch", 10*time.Second),
					agent.WithToolTimeout("web_search", 15*time.Second),
				)

				// 创建 ContextWindow 并 push system prompt
				cw := ctxwin.NewContextWindow(defaultModel.ContextWindow, defaultModel.ContextWindow/10, tokenizer)
				if def.SystemPrompt != "" {
					cw.Push(ctxwin.RoleSystem, def.SystemPrompt)
				}

				// agent 生命周期由 SessionManager 管理，parent 用 Background
				if err := a.Start(context.Background()); err != nil {
					return nil, nil, err
				}
				return a, cw, nil
			}

			// ── Session manager + reap loop ─────────────────────────────────
			mgr := session.NewSessionManager(factory, 30*time.Minute)

			// 根 ctx + SIGINT/SIGTERM 处理
			rootCtx, stop := signal.NotifyContext(context.Background(),
				os.Interrupt, syscall.SIGTERM)
			defer stop()

			go mgr.ReapLoop(rootCtx, time.Minute, 5*time.Second)

			// ── HTTP server ─────────────────────────────────────────────────
			mux := server.NewMux(mgr, log)
			srv := &http.Server{
				Addr:    fmt.Sprintf("%s:%d", host, port),
				Handler: mux,
			}

			// Graceful shutdown
			go func() {
				<-rootCtx.Done()
				log.Info(logger.CatApp, "shutdown signal received")
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				_ = srv.Shutdown(shutdownCtx)
				mgr.Shutdown(5 * time.Second)
			}()

			log.Info(logger.CatApp, "server listening",
				"addr", srv.Addr,
				"tools", len(toolList),
			)
			fmt.Printf("soloqueue serve listening on %s:%d\n", host, port)

			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				return fmt.Errorf("http listen: %w", err)
			}
			log.Info(logger.CatApp, "soloqueue serve stopped")
			return nil
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", 8765, "HTTP server port")
	cmd.Flags().StringVar(&host, "host", "127.0.0.1", "HTTP server host")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "print logs to console (stderr)")

	return cmd
}

// toolsConfigFromSettings 把 settings.ToolsConfig 转换为 tools.Config
//
// 关键转换：
//   - allowedDirs 由 caller 拼接（workDir + cwd + settings）后传入
//   - *Ms 字段 → time.Duration
//   - TavilyAPIKeyEnv → os.Getenv（为空时 Build 跳过 web_search）
func toolsConfigFromSettings(s config.ToolsConfig, allowedDirs []string) tools.Config {
	tavilyKey := ""
	if s.TavilyAPIKeyEnv != "" {
		tavilyKey = os.Getenv(s.TavilyAPIKeyEnv)
	}
	return tools.Config{
		AllowedDirs:        allowedDirs,
		MaxFileSize:        defaultInt64(s.MaxFileSize, 1<<20),
		MaxMatches:         defaultInt(s.MaxMatches, 100),
		MaxLineLen:         defaultInt(s.MaxLineLen, 500),
		MaxGlobItems:       defaultInt(s.MaxGlobItems, 1000),
		MaxWriteSize:       defaultInt64(s.MaxWriteSize, 1<<20),
		MaxMultiWriteBytes: defaultInt64(s.MaxMultiWriteBytes, 10<<20),
		MaxMultiWriteFiles: defaultInt(s.MaxMultiWriteFiles, 50),
		MaxReplaceEdits:    defaultInt(s.MaxReplaceEdits, 50),

		HTTPAllowedHosts: s.HTTPAllowedHosts,
		HTTPMaxBody:      defaultInt64(s.HTTPMaxBody, 5<<20),
		HTTPTimeout:      msToDuration(s.HTTPTimeoutMs, 10*time.Second),
		HTTPBlockPrivate: s.HTTPBlockPrivate,

		ShellBlockRegexes:   s.ShellBlockRegexes,
		ShellConfirmRegexes: s.ShellConfirmRegexes,
		ShellTimeout:        msToDuration(s.ShellTimeoutMs, 30*time.Second),
		ShellMaxOutput:      defaultInt64(s.ShellMaxOutput, 256<<10),

		TavilyAPIKey:   tavilyKey,
		TavilyEndpoint: defaultString(s.TavilyEndpoint, "https://api.tavily.com/search"),
		TavilyTimeout:  msToDuration(s.TavilyTimeoutMs, 15*time.Second),
	}
}

func defaultInt(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}
func defaultInt64(v, def int64) int64 {
	if v <= 0 {
		return def
	}
	return v
}
func defaultString(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
func msToDuration(ms int, def time.Duration) time.Duration {
	if ms <= 0 {
		return def
	}
	return time.Duration(ms) * time.Millisecond
}

// newAgentID returns a short random ID for an agent instance
func newAgentID() string {
	// timestamp + short random suffix (good enough for local dev)
	return fmt.Sprintf("agent-%d", time.Now().UnixNano())
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
		// Non-fatal: config changes will require restart.
		// Don't use slog.Error here — logger hasn't been initialized yet
		// and we don't want to pollute the terminal in TUI mode.
	}

	// 若 settings.json 不存在，写入默认值
	settingsPath := filepath.Join(workDir, "settings.json")
	if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
		if err := cfg.Save(); err != nil {
			// Non-fatal: don't pollute terminal before logger is ready.
		}
	}

	return cfg, nil
}

// initLogger 根据当前配置创建 system 层 Logger。
// console 参数明确控制是否向 stderr 输出日志；调用方通过 --verbose 标志传入，
// 不再依赖 settings.Log.Console（默认 false，确保 TUI 模式下日志不污染终端）。
func initLogger(workDir string, cfg *config.GlobalService, console bool) (*logger.Logger, error) {
	settings := cfg.Get()

	level := parseLogLevel(settings.Log.Level)
	log, err := logger.System(workDir,
		logger.WithLevel(level),
		logger.WithConsole(console),
		logger.WithFile(settings.Log.File),
	)
	if err != nil {
		return nil, err
	}

	// 热加载时自动更新日志级别（注：slog.Logger 不支持动态修改级别，重建 logger）
	// 此处仅演示 OnChange 用法
	cfg.OnChange(func(old, new config.Settings) {
		_ = old
		_ = new
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
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		// Unknown level: default to info without printing to terminal
		return slog.LevelInfo
	}
}
