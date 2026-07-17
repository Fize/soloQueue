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
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/channel"
	"github.com/xiaobaitu/soloqueue/internal/channel/wechat"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/cron"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/mcp"
	"github.com/xiaobaitu/soloqueue/internal/memoryengine"
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

			rt, err := runtime.Build(workDir, cfg, log, profileSetup, bypass, server.DistFS())
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
			mgr.SetMemoryEngine(rt.MemoryEngine)
			mgr.SetIdleReaper(30*time.Minute, 200000)

			// Initialize Scheduled Tasks (Cron & Timers) system
			cronStore := cron.NewDBStore(rt.SharedDB)
			builder := session.NewBuilder(rt, workDir, cfg, settings.Log.Console)
			cronScheduler := cron.NewScheduler(cronStore, cronSessionManagerWrapper{
				mgr:     mgr,
				builder: builder,
				workDir: workDir,
			}, log)
			cronScheduler.SetModelResolver(func(taskLevel string) (cron.ResolvedModel, error) {
				model, role, usedFallback, err := cfg.ResolveScheduledTaskModel(taskLevel)
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
						Level:           taskLevel,
						ContextWindow:   model.ContextWindow,
						Vision:          model.Vision,
					},
					RequestedRole: role,
					UsedFallback:  usedFallback,
				}
				if usedFallback {
					resolved.FallbackReason = role + " model is not configured or enabled"
				}
				return resolved, nil
			})

			// Wire memory engine into the scheduler so cron tasks can recall memories.
			if rt.MemoryEngine != nil {
				cronScheduler.SetMemoryEngine(rt.MemoryEngine, func(ctx context.Context, prompt string, memEngine interface{}, log *logger.Logger) string {
					return session.BuildRecalledContext(ctx, prompt, memEngine.(*memoryengine.Engine), log)
				})
			}

			if err := cronScheduler.Start(context.Background()); err != nil {
				return fmt.Errorf("start cron scheduler: %w", err)
			}
			defer cronScheduler.Stop()

			// ── Channel Registry for cron notification routing ──
			chanRegistry := &channel.Registry{}
			rt.ChannelRegistry = chanRegistry

			// Wire channel notification routing into the scheduler.
			cronScheduler.SetChannelRegistry(chanRegistry)
			cronScheduler.SetAgentChannelResolver(&agentChannelResolver{registry: rt.AgentRegistry, stack: rt})

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

			// ── L2 Session Store ──
			// Manages multiple L2 sessions for direct conversation (web chat + QQ bots).
			// Each L2 session has its own agent, timeline, and context window.
			l2Store := session.NewL2SessionStore(builder, workDir, log)

			// ── Daily memory flush (midnight) ──
			if rt.MemoryManager != nil {
				flusher := session.NewDailyMemoryFlusher(mgr, rt.MemoryEngine, log)
				go flusher.Run(context.Background())
			}

			// ── Messaging channel integrations ──
			qqBotManager := NewQQBotManager(cfg, mgr, l2Store, rt, cronScheduler, workDir, version, log, func() []*agent.Supervisor { return rt.Supervisors }, rt.AgentRegistry)
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

			// Create RuntimeMetrics (shared by Mux + Hub) for serve mode.
			listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", host, port))
			if err != nil {
				return fmt.Errorf("listen %s:%d: %w", host, port, err)
			}
			actualAddr := listener.Addr().String()
			runtimeMetrics := &server.RuntimeMetrics{HTTPAddr: actualAddr}
			fmt.Println(actualAddr)
			mux := server.NewMux(workDir, log,
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
				server.WithMCPLoader(MCPLoaderFromRT(rt)),
				server.WithMCPManager(rt.MCPManager),
				server.WithTeamStore(rt.TeamStore),
				server.WithAuthConfig(cfg.Get().Auth),
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
				ticker := time.NewTicker(10 * time.Second)
				defer ticker.Stop()
				for {
					select {
					case <-rootCtx.Done():
						return
					case <-ticker.C:
						cur, maxTokens := 0, 0
						if l2Active := l2Store.ActiveSession(); l2Active != nil && l2Active.CW() != nil {
							cur, maxTokens, _ = l2Active.CW().TokenUsage()
						} else if s := mgr.Session(); s != nil && s.CW() != nil {
							cur, maxTokens, _ = s.CW().TokenUsage()
						}
						if maxTokens > 0 {
							runtimeMetrics.SetCtxwin(cur, maxTokens)
						}
					}
				}
			}()
			// Background goroutine: monitor parent process death
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
	mgr     *session.SessionManager
	builder *session.Builder
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
// For "L1" (or empty): returns the existing L1 session with no-op cleanup.
// For other teams: creates a temporary L2 session via BuildL2ForCron with
// a cron-specific log directory at workDir/logs/cron/<taskID>/.
func (w cronSessionManagerWrapper) GetSession(ctx context.Context, teamID, taskID string) (cron.Session, bool, func(), error) {
	if teamID == "" || strings.EqualFold(teamID, "L1") {
		s := w.mgr.Session()
		if s == nil {
			return nil, false, nil, fmt.Errorf("L1 session not initialized")
		}
		return s, false, nil, nil
	}

	// Create a temporary L2 session for this L2 team.
	cronLogDir := filepath.Join(w.workDir, "logs", "cron", taskID)

	// Ensure the cron log directory exists.
	if err := os.MkdirAll(cronLogDir, 0755); err != nil {
		return nil, false, nil, fmt.Errorf("create cron log dir: %w", err)
	}

	l2Session, err := w.builder.BuildL2ForCron(ctx, taskID, teamID, cronLogDir)
	if err != nil {
		return nil, false, nil, fmt.Errorf("build L2 session for cron: %w", err)
	}

	cleanup := func() {
		// Stop the agent with a timeout.
		_ = l2Session.Agent.Stop(5 * time.Second)

		// Close the session (timeline writer is closed internally).
		l2Session.Close()
	}

	return l2Session, true, cleanup, nil
}

// agentChannelResolver implements cron.AgentChannelResolver by looking up
// agents in the agent registry, with a special case for L1 from Stack.
type agentChannelResolver struct {
	registry *agent.Registry
	stack    *runtime.Stack
}

func (r *agentChannelResolver) GetChannels(agentID string) (map[string]string, string, bool) {
	// L1: check registry first, then Stack channels.
	if agentID == "L1" || agentID == "l1-agent" {
		agents := r.registry.GetByTemplate(agentID)
		if len(agents) > 0 {
			def := agents[0].Def
			return def.Channels, def.NotifyChannel, len(def.Channels) > 0
		}
		if r.stack != nil && len(r.stack.L1Channels) > 0 {
			return r.stack.L1Channels, r.stack.L1NotifyChannel, true
		}
		return nil, "", false
	}

	agents := r.registry.GetByTemplate(agentID)
	if len(agents) == 0 {
		return nil, "", false
	}
	def := agents[0].Def
	return def.Channels, def.NotifyChannel, true
}
