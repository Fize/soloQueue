package cli

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/cron"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/mcp"
	"github.com/xiaobaitu/soloqueue/internal/prompt"
	"github.com/xiaobaitu/soloqueue/internal/runtime"
	"github.com/xiaobaitu/soloqueue/internal/server"
	"github.com/xiaobaitu/soloqueue/internal/session"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// MCPLoaderFromRT extracts the MCP loader from the runtime stack.
// Returns nil if MCP is not configured.
func MCPLoaderFromRT(rt *runtime.Stack) *mcp.Loader {
	if rt.MCPManager == nil {
		return nil
	}
	return rt.MCPManager.Loader()
}

func ServeCmd(version string) *cobra.Command {
	var port int
	var host string
	var verbose bool
	var bypass bool
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

			log, err := runtime.InitLogger(workDir, cfg, verbose)
			if err != nil {
				return fmt.Errorf("init logger: %w", err)
			}
			defer log.Close()

			log.Info(logger.CatApp, "soloqueue serve starting",
				"host", host, "port", port, "version", version)

			if tools.IsRTKEnabled() {
				log.Info(logger.CatApp, "RTK command is available; Bash tool will compress outputs using RTK")
			} else {
				log.Info(logger.CatApp, "RTK command is not available or platform not supported; Bash tool output compression disabled")
			}

			cfg.SetLogger(log)

			settings := cfg.Get()

			// serve mode has no interactive terminal, use default profile
			profileSetup := func(cfg *prompt.PromptConfig) error {
				return cfg.WriteSoul(prompt.DefaultProfileAnswers())
			}

			rt, err := runtime.Build(workDir, cfg, log, profileSetup, bypass)
			if err != nil {
				return err
			}
			defer rt.Shutdown()

			factory := session.BuildFactory(rt, workDir, cfg, settings.Log.Console)
			mgr := session.NewSessionManager(factory, log)
			mgr.SetRouter(session.BuildRouterFunc(rt))
			mgr.SetMemoryHook(session.BuildMemoryHook(rt))
			mgr.SetMemoryManager(rt.MemoryManager)
			mgr.SetIdleReaper(30*time.Minute, 200000)

			// Initialize Scheduled Tasks (Cron & Timers) system
			cronStore := cron.NewDBStore(rt.SharedDB)
			cronScheduler := cron.NewScheduler(cronStore, cronSessionManagerWrapper{mgr: mgr}, log)
			if err := cronScheduler.Start(context.Background()); err != nil {
				return fmt.Errorf("start cron scheduler: %w", err)
			}
			defer cronScheduler.Stop()

			mgr.SetCronHandler(func(ctx context.Context, expression, instruction string) (string, time.Time, error) {
				nextRun, err := cron.NextTrigger(expression, time.Now())
				if err != nil {
					return "", time.Time{}, err
				}
				task, err := cronStore.CreateTask(ctx, expression, instruction, "L1", nextRun)
				if err != nil {
					return "", time.Time{}, err
				}
				cronScheduler.Schedule(*task)
				return task.ID, task.NextRunAt, nil
			})

			// Wire the cron store and scheduler into tools configuration
			toolsCfg := rt.ReadToolsCfg()
			toolsCfg.CronStore = cronStore
			toolsCfg.CronScheduler = cronScheduler
			rt.SetToolsCfg(toolsCfg)
			rt.AgentFactory.SetToolsConfig(toolsCfg)

			_, err = mgr.Init(context.Background(), "")
			if err != nil {
				return fmt.Errorf("init session: %w", err)
			}

			// ── Daily memory flush (midnight) ──
			if rt.MemoryManager != nil {
				flusher := session.NewDailyMemoryFlusher(mgr, rt.PermanentMemory, log)
				go flusher.Run(context.Background())
			}

				// ── QQ Bot integration ──
				qqGateway, qqQueue := StartQQBot(cfg, mgr, cronScheduler, workDir, version, log, func() []*agent.Supervisor { return rt.Supervisors }, rt.AgentRegistry)

		rootCtx, stop := signal.NotifyContext(context.Background(),
			os.Interrupt, syscall.SIGTERM)
		defer stop()

		rebuildPrompt := func() error {
			if rt.TeamStore != nil {
				if err := rt.ReloadFromTeamStore(); err != nil {
					log.Warn(logger.CatApp, "reload from teamstore failed during prompt rebuild", "err", err.Error())
				}
			}
			leaders, err := prompt.LoadLeaders(filepath.Join(workDir, "agents"), rt.Groups)
			if err != nil {
				leaders = rt.Leaders
			}
			planDir, _ := config.PlanDir()
			memoryDir := filepath.Join(workDir, "memory")
			newPrompt, err := rt.PromptCfg.BuildPrompt(leaders, rt.Groups, memoryDir, memoryDir, planDir, rt.L1MCPServers())
			if err != nil {
				return err
			}
			rt.SetSystemPrompt(newPrompt)
			return nil
		}
		rt.OnPromptRebuild(rebuildPrompt)

		// Create RuntimeMetrics (shared by Mux + Hub) for serve mode.
		listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", host, port))
		if err != nil {
			return fmt.Errorf("listen %s:%d: %w", host, port, err)
		}
		actualAddr := listener.Addr().String()
		runtimeMetrics := &server.RuntimeMetrics{HTTPAddr: actualAddr}
		fmt.Println(actualAddr)
		mux := server.NewMux(workDir, log, rt.TodoStore,
			server.WithRegistry(rt.AgentRegistry),
			server.WithSupervisors(func() []*agent.Supervisor { return rt.Supervisors }),
			server.WithConfigService(cfg),
			server.WithRuntimeMetrics(runtimeMetrics),
			server.WithTemplates(rt.AllTemplates),
			server.WithGroupsDir(filepath.Join(workDir, "groups")),
			server.WithToolsConfig(&rt.ToolsCfg),
			server.WithSkillRegistry(rt.SkillRegistry),
			server.WithSkillDirs(map[string]string{"user": filepath.Join(workDir, "skills")}),
			server.WithAgentsDir(filepath.Join(workDir, "agents")),
			server.WithPromptRebuild(rebuildPrompt),
			server.WithMCPLoader(MCPLoaderFromRT(rt)),
			server.WithTeamStore(rt.TeamStore),
		server.WithAuthConfig(cfg.Get().Auth),
		)

		// Create and start WebSocket Hub for real-time state updates.
		wsHub := server.NewHub(mux)
		mux.SetHub(wsHub)
		go wsHub.Run()

		// Wire onChange callbacks so Registry changes trigger WebSocket broadcasts.
		runtimeMetrics.SetOnChange(wsHub.Notify)
		rt.AgentRegistry.SetOnChange(wsHub.Notify)

		// Wire onStateChange so every agent state transition triggers a broadcast.
		rt.AgentRegistry.SetOnRegister(func(a *agent.Agent) {
			runtimeMetrics.StartAgentWatch(a)
			a.SetOnStateChange(func(s agent.State) { wsHub.Notify() })
		})
		rt.AgentRegistry.SetOnUnregister(runtimeMetrics.StopAgentWatch)
		for _, a := range rt.AgentRegistry.List() {
			runtimeMetrics.StartAgentWatch(a)
			a.SetOnStateChange(func(s agent.State) { wsHub.Notify() })
		}

		// Background goroutine: sync context window metrics every 3s
		go func() {
			ticker := time.NewTicker(3 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-rootCtx.Done():
					return
				case <-ticker.C:
					s := mgr.Session()
					if s == nil {
						continue
					}
					cur, maxTokens, _ := s.CW().TokenUsage()
					if maxTokens > 0 {
						runtimeMetrics.SetContext(cur * 100 / maxTokens)
					}
				}
			}
		}()

		srv := &http.Server{Handler: mux}

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
				if qqQueue != nil {
					qqQueue.Stop()
				}
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				_ = srv.Shutdown(shutdownCtx)
				mgr.Shutdown(5 * time.Second)
			}()

			if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
				return fmt.Errorf("http serve: %w", err)
			}
			log.Info(logger.CatApp, "soloqueue serve stopped")
			return nil
		},
	}

	cmd.Flags().IntVarP(&port, "port", "p", 57647, "HTTP server port (57647 = default, 0 = random)")
	cmd.Flags().StringVar(&host, "host", "127.0.0.1", "HTTP server host")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "print logs to console (stderr)")
	cmd.Flags().BoolVar(&bypass, "bypass", false, "bypass all tool confirmations for all agents")
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

type cronSessionManagerWrapper struct {
	mgr *session.SessionManager
}

func (w cronSessionManagerWrapper) Session() cron.Session {
	s := w.mgr.Session()
	if s == nil {
		return nil
	}
	return s
}


