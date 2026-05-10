package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/prompt"
	"github.com/xiaobaitu/soloqueue/internal/qqbot"
	"github.com/xiaobaitu/soloqueue/internal/runtime"
	"github.com/xiaobaitu/soloqueue/internal/sandbox"
	"github.com/xiaobaitu/soloqueue/internal/server"
	"github.com/xiaobaitu/soloqueue/internal/session"
)

func ServeCmd(version string) *cobra.Command {
	var port int
	var host string
	var verbose bool

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the local HTTP/WebSocket server",
		RunE: func(cmd *cobra.Command, args []string) error {
			workDir, err := config.DefaultWorkDir()
			if err != nil {
				return err
			}

			cfg, err := config.Init(workDir)
			if err != nil {
				return fmt.Errorf("init config: %w", err)
			}
			defer cfg.Close()

			log, err := runtime.InitLogger(workDir, cfg, verbose)
			if err != nil {
				return fmt.Errorf("init logger: %w", err)
			}
			defer log.Close()

			log.Info(logger.CatApp, "soloqueue serve starting",
				"host", host, "port", port, "version", version)

			cfg.SetLogger(log)

			settings := cfg.Get()

			// serve mode has no interactive terminal, use default profile
			profileSetup := func(cfg *prompt.PromptConfig) error {
				return cfg.WriteSoul(prompt.DefaultProfileAnswers())
			}

			rt, err := runtime.Build(workDir, cfg, log, profileSetup)
			if err != nil {
				return err
			}
			defer rt.Shutdown()

			// serve mode: start sandbox synchronously before session init
			sb, executor, err := runtime.StartSandbox(context.Background(), rt.SandboxMounts, log)
			if err != nil {
				return err
			}
			rt.DockerSandbox = sb
			rt.CfgMu.Lock()
			rt.ToolsCfg.Executor = executor
			rt.CfgMu.Unlock()

			factory := session.BuildFactory(rt, workDir, cfg, settings.Log.Console)
			mgr := session.NewSessionManager(factory, log)
			mgr.SetRouter(session.BuildRouterFunc(rt))
			mgr.SetMemoryHook(session.BuildMemoryHook(rt))

			_, err = mgr.Init(context.Background(), "")
			if err != nil {
				return fmt.Errorf("init session: %w", err)
			}

			// ── QQ Bot integration ──
			var qqGateway *qqbot.Gateway
			qqCfg := settings.QQBot.ToQQBotConfig()
			if qqCfg.Enabled && qqCfg.AppID != "" && qqCfg.AppSecret != "" {
				qqAPI := qqbot.NewAPIClient(qqCfg, log)
				qqAdapter := session.NewQQBotAdapter(mgr)
				qqBridge := qqbot.NewSessionBridge(qqAdapter, qqAPI, log)
				qqGateway = qqbot.NewGateway(qqCfg, qqBridge, log)

				go func() {
					defer func() {
						if r := recover(); r != nil {
							log.Error(logger.CatApp, "qqbot gateway goroutine panic recovered",
								"panic", fmt.Sprintf("%v", r))
						}
					}()
					log.Info(logger.CatApp, "qqbot gateway starting",
						"app_id", qqCfg.AppID, "sandbox", qqCfg.Sandbox)
					if err := qqGateway.Run(context.Background()); err != nil {
						log.Warn(logger.CatApp, "qqbot gateway stopped", "err", err.Error())
					}
				}()
			} else if qqCfg.Enabled {
				log.Warn(logger.CatApp, "qqbot enabled but appId/appSecret not configured, skipping")
			}

			rootCtx, stop := signal.NotifyContext(context.Background(),
				os.Interrupt, syscall.SIGTERM)
			defer stop()

		mux := server.NewMux(workDir, log, rt.TodoStore,
			server.WithRegistry(rt.AgentRegistry),
			server.WithSupervisors(func() []*agent.Supervisor { return rt.Supervisors }),
			server.WithConfigService(cfg),
			server.WithTemplates(rt.AllTemplates, rt.Groups),
			server.WithToolsConfig(&rt.ToolsCfg),
			server.WithSkillRegistry(rt.SkillRegistry),
			server.WithAgentsDir(filepath.Join(workDir, "agents")),
			server.WithPromptRebuild(func() error {
				leaders, err := prompt.LoadLeaders(filepath.Join(workDir, "agents"), rt.Groups)
				if err != nil {
					leaders = rt.Leaders
				}
				planDir, _ := config.PlanDir()
				memoryDir := filepath.Join(workDir, "memory")
				newPrompt, err := rt.PromptCfg.BuildPrompt(leaders, memoryDir, memoryDir, planDir)
				if err != nil {
					return err
				}
				rt.SetSystemPrompt(newPrompt)
				return nil
			}),
		)

		// Create and start WebSocket Hub for real-time state updates.
		wsHub := server.NewHub(mux)
		mux.SetHub(wsHub)
		go wsHub.Run()

		// Wire onChange callbacks so Registry changes trigger WebSocket broadcasts.
		rt.AgentRegistry.SetOnChange(wsHub.Notify)
			srv := &http.Server{
				Addr:    fmt.Sprintf("%s:%d", host, port),
				Handler: mux,
			}

			go func() {
				defer func() {
					if r := recover(); r != nil {
						log.Error(logger.CatApp, "shutdown goroutine panic recovered",
							"panic", fmt.Sprintf("%v", r))
					}
				}()
				<-rootCtx.Done()
				log.Info(logger.CatApp, "shutdown signal received")
				if qqGateway != nil {
					qqGateway.Close()
				}
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				_ = srv.Shutdown(shutdownCtx)
				mgr.Shutdown(5 * time.Second)
			}()

			log.Info(logger.CatApp, "server listening", "addr", srv.Addr)

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

func VersionCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			workDir, err := config.DefaultWorkDir()
			if err != nil {
				return err
			}

			cfg, err := config.Init(workDir)
			if err != nil {
				return fmt.Errorf("init config: %w", err)
			}
			defer cfg.Close()

			log, err := runtime.InitLogger(workDir, cfg, false)
			if err != nil {
				return fmt.Errorf("init logger: %w", err)
			}
			defer log.Close()

			cfg.SetLogger(log)

			settings := cfg.Get()

			log.Info(logger.CatApp, "soloqueue version info",
				"version", version,
				"work_dir", workDir,
				"log_level", settings.Log.Level,
			)

			p := cfg.DefaultProvider()
			if p != nil {
				log.Info(logger.CatApp, "default provider", "name", p.Name, "id", p.ID)
			}

			m := cfg.DefaultModelByRole("fast")
			if m != nil {
				log.Info(logger.CatApp, "default model", "name", m.Name, "id", m.ID)
			}
			return nil
		},
	}
}

func CleanupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cleanup",
		Short: "Remove all soloqueue sandbox containers",
		RunE: func(cmd *cobra.Command, args []string) error {
			sb, err := sandbox.NewDockerSandbox(nil)
			if err != nil {
				return fmt.Errorf("docker client init failed: is Docker running? %w", err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := sb.Cleanup(ctx); err != nil {
				return err
			}
			fmt.Println("cleanup done")
			return nil
		},
	}
}
