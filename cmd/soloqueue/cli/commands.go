package cli

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/agenttools/mcp"
	"github.com/xiaobaitu/soloqueue/internal/agenttools/tools"
	"github.com/xiaobaitu/soloqueue/internal/assets"
	"github.com/xiaobaitu/soloqueue/internal/channel/wechat"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/cron"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/prompt"
	"github.com/xiaobaitu/soloqueue/internal/runtime"
	"github.com/xiaobaitu/soloqueue/internal/server"
	"github.com/xiaobaitu/soloqueue/internal/session"
	"github.com/xiaobaitu/soloqueue/internal/tasktype"
)

func mcpLoaderFromRT(rt *runtime.Stack) *mcp.Loader {
	if rt.MCPManager == nil {
		return nil
	}
	return rt.MCPManager.Loader()
}

func ServeCmd(version string) *cobra.Command {
	return serveCmd("serve", version, server.FrontendStatus)
}

// StartCmd runs the same runtime assembly as serve, but exposes both browser
// bundles from the one listener.
func StartCmd(version string) *cobra.Command { return serveCmd("start", version, server.FrontendStart) }

func serveCmd(use, version string, frontendMode server.FrontendMode) *cobra.Command {
	var port int
	var verbose bool
	var bypass bool
	cmd := &cobra.Command{
		Use:   use,
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
				"host", "127.0.0.1", "port", port, "version", version)

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

			rt, err := runtime.Build(workDir, cfg, log, profileSetup, bypass, assets.SkillsFS())
			if err != nil {
				return err
			}
			defer rt.Shutdown()

			factory := session.BuildFactory(rt, workDir, cfg, settings.Log.Console)
			mgr := session.NewSessionManager(factory, log)
			session.Version = version
			mgr.SetRouter(session.BuildRouterFunc(rt))
			mgr.SetMemoryHook(session.BuildMemoryHook(rt))
			mgr.SetMemoryManager(rt.MemoryManager)
			mgr.SetVisionDescriber(session.BuildVisionDescriber(cfg, log))
			mgr.SetChannelMetadataStore(rt.SharedDB)
			mgr.SetIdleReaper(30*time.Minute, 200000)
			mgr.SetPersonaStatePath(filepath.Join(workDir, "persona", "roles", "state.md"))
			mgr.SetPersonaReflection(rt.ReadLLMClient(), rt.FastModelProviderID, rt.FastModelID, func() string { return prompt.ReadSoulName(rt.PromptCfg) })

			// Initialize Scheduled Tasks (Cron & Timers) system
			cronStore := cron.NewDBStore(rt.SharedDB)
			builder := session.NewBuilder(rt, workDir, cfg, settings.Log.Console)
			// ── L2 Session Store ──
			l2Store := session.NewL2SessionStore(builder, workDir, log)
			l2Store.SetChannelMetadataStore(rt.SharedDB)
			cronScheduler := cron.NewScheduler(cronStore, cronSessionManagerWrapper{
				mgr:     mgr,
				builder: builder,
				l2Store: l2Store,
				workDir: workDir,
			}, log)
			cronScheduler.SetModelResolver(func(taskType string) (cron.ResolvedModel, error) {
				typeValue, err := tasktype.Parse(taskType)
				if err != nil {
					return cron.ResolvedModel{}, err
				}
				model, usedFallback, err := cfg.ResolveScheduledTaskModel(typeValue)
				if err != nil {
					return cron.ResolvedModel{}, err
				}
				modelID := model.APIModel
				if modelID == "" {
					modelID = model.ID
				}
				resolved := cron.ResolvedModel{
					Params: iface.ModelOverrideParams{
						ProviderID:      model.ProviderID,
						ModelID:         modelID,
						ThinkingEnabled: model.Thinking.Enabled,
						ReasoningEffort: model.Thinking.ReasoningEffort,
						ThinkingType:    model.Thinking.ThinkingType,
						TaskType:        taskType,
						ContextWindow:   model.ContextWindow,
						Vision:          model.Vision,
					},
					RequestedTaskType: taskType,
					UsedFallback:      usedFallback,
				}
				if usedFallback {
					resolved.FallbackReason = taskType + " model is not configured or enabled"
				}
				return resolved, nil
			})

			if err := cronScheduler.Start(context.Background()); err != nil {
				return fmt.Errorf("start cron scheduler: %w", err)
			}
			defer cronScheduler.Stop()

			// Wire channel notification routing into the scheduler.
			cronScheduler.SetWorkDir(workDir)
			cronStore.SetWorkDir(workDir)
			cronStore.SetLogf(func(format string, args ...any) {
				log.Warn(logger.CatApp, fmt.Sprintf(format, args...))
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
				flusher := session.NewDailyMemoryFlusher(mgr, rt.MemoryEngine, log)
				go flusher.Run(context.Background())
			}

			// ── Messaging channel integrations ──
			qqBotManager := NewQQBotManager(cfg, mgr, l2Store, rt, workDir, version, log, func() []*agent.Supervisor { return rt.Supervisors }, rt.AgentRegistry)
			wechatBotManager := NewWechatBotManager(cfg, mgr, l2Store, rt, workDir, version, log, func() []*agent.Supervisor { return rt.Supervisors }, rt.AgentRegistry)
			qqBotManager.Reload()
			wechatBotManager.Reload()
			wechatLoginManager := wechat.NewLoginManager(
				wechat.NewClient(wechat.Config{Version: version, BotAgent: "SoloQueue/" + version}),
				&wechatCredentialStore{cfg: cfg, version: version, onSaved: wechatBotManager.Reload},
			)
			defer wechatLoginManager.Close()

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
			reloadTeamCatalog := func() error {
				if err := rebuildPrompt(); err != nil {
					return err
				}
				rt.CfgMu.RLock()
				systemPrompt := rt.SystemPrompt
				rt.CfgMu.RUnlock()
				return builder.ReconcileL1TeamCatalog(mgr.Session(), systemPrompt)
			}

			// Create RuntimeMetrics (shared by Mux + Hub) for serve mode.
			listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
			if err != nil {
				return fmt.Errorf("listen 127.0.0.1:%d: %w", port, err)
			}
			actualAddr := listener.Addr().String()
			runtimeMetrics := &server.RuntimeMetrics{HTTPAddr: actualAddr}
			fmt.Println(actualAddr)
			mux := server.NewMux(workDir, log,
				server.WithFrontendMode(frontendMode),
				server.WithRegistry(rt.AgentRegistry),
				server.WithSessionManager(mgr),
				server.WithL2SessionStore(l2Store),
				server.WithSupervisors(func() []*agent.Supervisor { return rt.Supervisors }),
				server.WithConfigService(cfg),
				server.WithWechatLoginManager(wechatLoginManager),
				server.WithRuntimeMetrics(runtimeMetrics),
				server.WithTemplates(rt.AllTemplates),
				server.WithGroupsDir(filepath.Join(workDir, "groups")),
				server.WithToolsConfig(&rt.ToolsCfg),
				server.WithSkillRegistry(rt.SkillRegistry),
				server.WithSkillDirs(map[string]string{"user": filepath.Join(workDir, "skills")}),
				server.WithAgentsDir(filepath.Join(workDir, "agents")),
				server.WithPromptRebuild(rebuildPrompt),
				server.WithTeamCatalogReload(reloadTeamCatalog),
				server.WithMCPLoader(mcpLoaderFromRT(rt)),
				server.WithMCPManager(rt.MCPManager),
				server.WithTeamStore(rt.TeamStore),
				server.WithOnConfigChange(func() error {
					if err := rt.OnConfigChange(); err != nil {
						return err
					}
					qqBotManager.Reload()
					wechatBotManager.Reload()
					return nil
				}),
				server.WithSimulationEngine(rt.SimulationEngine),
				server.WithSharedDB(rt.SharedDB),
			)

			// Create and start WebSocket Hub for real-time state updates.
			wsHub := server.NewHub(mux)
			mux.SetHub(wsHub)
			go wsHub.Run()

			// Wire cron task start/complete notifications to WebSocket.
			cronScheduler.OnTaskStart = func(taskID, taskTitle string) {
				wsHub.SendNotification("cron", "info", taskTitle, "Starting execution...")
			}
			cronScheduler.OnTaskComplete = func(taskID, taskTitle string, success bool, summary string) {
				lv := "error"
				if success {
					lv = "success"
				}
				wsHub.SendNotification("cron", lv, taskTitle, summary)
			}

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

			// Periodically sync context window metrics to the WebSocket hub (10s).
			go func() {
				ticker := time.NewTicker(10 * time.Second)
				defer ticker.Stop()
				for {
					select {
					case <-rootCtx.Done():
						return
					case <-ticker.C:
						cur, maxTokens := 0, 0
						if s := mgr.Session(); s != nil && s.CW() != nil {
							cur, maxTokens, _ = s.CW().TokenUsage()
						}
						if maxTokens > 0 {
							runtimeMetrics.SetCtxwin(cur, maxTokens)
						}
					}
				}
			}()
			// Detect parent process death so the server exits when the IDE
			// (or launcher) that spawned it is terminated.
			initialPPID := os.Getppid()
			if initialPPID > 1 {
				go func() {
					ticker := time.NewTicker(2 * time.Second)
					defer ticker.Stop()
					for {
						select {
						case <-rootCtx.Done():
							return
						case <-ticker.C:
							if isParentDead(initialPPID) {
								log.Info(logger.CatApp, "Parent process terminated, initiating shutdown")
								stop()
								return
							}
						}
					}
				}()
			}

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
				// Shutdown all messaging channel gateways.
				qqBotManager.Shutdown()
				wechatBotManager.Shutdown()
				wechatLoginManager.Close()
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
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "print logs to console (stderr)")
	cmd.Flags().BoolVar(&bypass, "bypass", false, "bypass all tool confirmations for all agents")
	return cmd
}

// WebCmd serves the standalone Web Console without constructing a backend
// runtime. The frontend calls --backend directly; no reverse proxy is used.
func WebCmd(version string) *cobra.Command {
	var port int
	var backendURL string
	cmd := &cobra.Command{
		Use:   "web",
		Short: "Start the standalone Web Console",
		RunE: func(cmd *cobra.Command, args []string) error {
			listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
			if err != nil {
				return fmt.Errorf("listen 127.0.0.1:%d: %w", port, err)
			}
			fmt.Println(listener.Addr().String())
			srv := &http.Server{Handler: server.NewWebHandler(assets.WebFS(), backendURL)}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			go func() {
				<-ctx.Done()
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = srv.Shutdown(shutdownCtx)
			}()
			if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
				return fmt.Errorf("http serve: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().IntVarP(&port, "port", "p", 57648, "Web server port (0 = random)")
	cmd.Flags().StringVar(&backendURL, "backend", "http://127.0.0.1:57647", "Backend URL used by the browser")
	_ = version // retained for command registration symmetry and future banner use
	return cmd
}

func VersionCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(version)
		},
	}
}

type cronSessionManagerWrapper struct {
	mgr     *session.SessionManager
	builder *session.Builder
	l2Store *session.L2SessionStore
	workDir string
}

func (w cronSessionManagerWrapper) Session() cron.Session {
	s := w.mgr.Session()
	if s == nil {
		return nil
	}
	return s
}

// GetSession returns a session for the given teamID.
// For L1: returns the existing L1 session.
// L2 Cron runs always use an isolated temporary session.
func (w cronSessionManagerWrapper) GetSession(ctx context.Context, teamID, taskID string) (cron.Session, bool, func(), error) {
	if teamID == "" || strings.EqualFold(teamID, "L1") {
		s := w.mgr.Session()
		if s == nil {
			return nil, false, nil, fmt.Errorf("L1 session not initialized")
		}
		return s, false, nil, nil
	}

	cronLogDir := filepath.Join(w.workDir, "logs", "cron", taskID)

	// Ensure the cron log directory exists.
	if err := os.MkdirAll(cronLogDir, 0755); err != nil {
		return nil, false, nil, fmt.Errorf("create cron log dir: %w", err)
	}

	l2Session, err := w.builder.BuildL2ForCron(ctx, taskID, teamID, cronLogDir)
	if err != nil {
		return nil, false, nil, fmt.Errorf("build L2 session for cron: %w", err)
	}
	if w.l2Store != nil {
		w.l2Store.ApplyChannelSendersTo(teamID, l2Session)
	}

	cleanup := newCronSessionCleanup(l2Session, w.builder.RT.AgentRegistry)

	return l2Session, true, cleanup, nil
}

func newCronSessionCleanup(l2Session *session.Session, registry *agent.Registry) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			if l2Session.Agent != nil {
				_ = l2Session.Agent.Stop(5 * time.Second)
			}
			if l2Session.Supervisor != nil {
				_ = l2Session.Supervisor.ReapAll(5 * time.Second)
			}
			if registry != nil && l2Session.Agent != nil {
				registry.Unregister(l2Session.Agent.InstanceID)
			}
			// Close the session after all Agents have stopped so timeline and
			// logger handles remain available to lifecycle logging.
			l2Session.Close()
		})
	}
}
