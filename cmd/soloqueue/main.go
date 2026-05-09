package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/xiaobaitu/soloqueue/cmd/soloqueue/cli"
	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/prompt"
	"github.com/xiaobaitu/soloqueue/internal/runtime"
	"github.com/xiaobaitu/soloqueue/internal/server"
	"github.com/xiaobaitu/soloqueue/internal/session"
	"github.com/xiaobaitu/soloqueue/internal/tui"
)

const version = "0.1.0"

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	var port int

	root := &cobra.Command{
		Use:   "soloqueue",
		Short: "SoloQueue — AI multi-agent collaboration tool",
		Long: `SoloQueue is an AI multi-agent collaboration tool built on the Actor model.

Run without subcommands for interactive TUI mode.
Use 'soloqueue serve' to start the local HTTP/WebSocket server.`,
		SilenceUsage:  true,
		SilenceErrors: true,
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

			// cli.PromptProfileQuestions is only needed in TUI mode,
			// so pass it as the profileSetup callback
			profileSetup := func(cfg *prompt.PromptConfig) error {
				answers := cli.PromptProfileQuestions()
				return cfg.WriteSoul(answers)
			}

			buildStart := time.Now()
			rt, err := runtime.Build(workDir, cfg, log, profileSetup)
			if err != nil {
				return err
			}
			log.Info(logger.CatApp, "runtime.Build done", "duration", time.Since(buildStart).String())

			log.Info(logger.CatApp, "soloqueue tui starting",
				"version", version, "model", rt.ReadDefaultModel().ID)

			agentFactory := session.BuildFactory(rt, workDir, cfg, false /* TUI: no console log */)
			mgr := session.NewSessionManager(agentFactory, log)
			mgr.SetRouter(session.BuildRouterFunc(rt))
			mgr.SetMemoryHook(session.BuildMemoryHook(rt))

			// Run shutdown concurrently so a slow Docker destroy or
			// agent stop doesn't block the process exit after the TUI
			// has already restored the terminal.
			defer func() {
				done := make(chan struct{})
				go func() {
					defer close(done)
					mgr.Shutdown(3 * time.Second)
					rt.Shutdown()
				}()
				select {
				case <-done:
				case <-time.After(4 * time.Second):
					log.Warn(logger.CatApp, "shutdown timed out, exiting")
				}
			}()

			// Start embedded HTTP server for the TUI sidebar API.
			var httpServerAddr string
			var httpListener net.Listener
			var listenErr error
			var runtimeMetrics *server.RuntimeMetrics
			if port > 0 {
				httpListener, listenErr = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
			} else {
				httpListener, listenErr = net.Listen("tcp", "127.0.0.1:0")
			}
			if listenErr != nil {
				log.Warn(logger.CatApp, "failed to start HTTP server", "err", listenErr)
			} else {
				httpServerAddr = fmt.Sprintf("http://%s", httpListener.Addr().String())

				// Create shared runtime metrics that the TUI writes and HTTP API reads.
				runtimeMetrics = &server.RuntimeMetrics{HTTPAddr: httpServerAddr}

			httpMux := server.NewMux(workDir, log, rt.TodoStore,
				server.WithRegistry(rt.AgentRegistry),
				server.WithSupervisors(func() []*agent.Supervisor { return rt.Supervisors }),
				server.WithConfigService(cfg),
				server.WithRuntimeMetrics(runtimeMetrics),
				server.WithTemplates(rt.AllTemplates, rt.Groups),
				server.WithToolsConfig(&rt.ToolsCfg),
				server.WithSkillRegistry(rt.SkillRegistry),
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
			wsHub := server.NewHub(httpMux)
			httpMux.SetHub(wsHub)
			go wsHub.Run()

			// Wire onChange callbacks so RuntimeMetrics and Registry
			// changes trigger WebSocket broadcasts.
			runtimeMetrics.SetOnChange(wsHub.Notify)
			rt.AgentRegistry.SetOnChange(wsHub.Notify)
				rt.HTTPServer = &http.Server{Handler: httpMux}
				rt.HTTPListener = httpListener
				go func() {
					defer func() {
						if r := recover(); r != nil {
							log.Error(logger.CatApp, "HTTP server goroutine panic recovered", fmt.Errorf("panic: %v", r))
						}
					}()
					log.Info(logger.CatApp, "HTTP API server started", "addr", httpServerAddr)
					if err := rt.HTTPServer.Serve(httpListener); err != nil && err != http.ErrServerClosed {
						log.Warn(logger.CatApp, "HTTP server error", "err", err)
					}
				}()
			}

			// Start TUI immediately; sandbox + session init run in background.
			sandboxCh := make(chan tui.SandboxInitMsg, 1)

			go func() {
				defer func() {
					if r := recover(); r != nil {
						log.Error(logger.CatApp, "sandbox init goroutine panic recovered", fmt.Errorf("panic: %v", r))
					}
				}()
				sb, executor, err := runtime.StartSandbox(context.Background(), rt.SandboxMounts, log)
				if err != nil {
					sandboxCh <- tui.SandboxInitMsg{Err: err}
					return
				}
				rt.DockerSandbox = sb
				rt.CfgMu.Lock()
				rt.ToolsCfg.Executor = executor
				rt.CfgMu.Unlock()

				sess, err := mgr.Init(context.Background(), "")
				sandboxCh <- tui.SandboxInitMsg{Sess: sess, Err: err}
			}()

			return tui.Run(tui.Config{
				Session:        nil,
				SandboxInitCh:  sandboxCh,
				ModelID:        rt.ReadDefaultModel().ID,
				Version:        version,
				RulesCreated:   rt.RulesCreated,
				RulesPath:      rt.PromptCfg.RulesPath(),
				Registry:       rt.AgentRegistry,
				SupervisorsFn:  func() []*agent.Supervisor { return rt.Supervisors },
				Skills:         rt.SkillRegistry,
				NotifyCh:       rt.PermNotifyCh,
				HTTPServerAddr: httpServerAddr,
				RuntimeMetrics: runtimeMetrics,
				ContextIdleThresholdMin: cfg.Get().Session.ContextIdleThresholdMin,
			})
		},
	}

	root.Flags().IntVarP(&port, "port", "p", 0, "HTTP server port for TUI mode (0 = random)")

	root.AddCommand(cli.VersionCmd(version))
	root.AddCommand(cli.ServeCmd(version))
	root.AddCommand(cli.CleanupCmd())

	return root
}
