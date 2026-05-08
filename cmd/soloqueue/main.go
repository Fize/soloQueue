package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
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

			rt, err := runtime.Build(workDir, cfg, log, profileSetup)
			if err != nil {
				return err
			}
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

			// Start embedded HTTP server on a random port for the TUI sidebar API.
			var httpServerAddr string
			httpListener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				log.Warn(logger.CatApp, "failed to start HTTP server", "err", err)
			} else {
				httpServerAddr = fmt.Sprintf("http://%s", httpListener.Addr().String())
				httpMux := server.NewMux(workDir, log, rt.TodoStore)
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
				AssistantName:  prompt.ReadSoulName(rt.PromptCfg),
				Templates:      rt.AllTemplates,
				Groups:         rt.Groups,
				HTTPServerAddr: httpServerAddr,
			})
		},
	}

	root.AddCommand(cli.VersionCmd(version))
	root.AddCommand(cli.ServeCmd(version))
	root.AddCommand(cli.CleanupCmd())

	return root
}
