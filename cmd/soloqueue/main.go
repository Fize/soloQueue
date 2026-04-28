package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/llm/deepseek"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/prompt"
	"github.com/xiaobaitu/soloqueue/internal/server"
	"github.com/xiaobaitu/soloqueue/internal/session"
	"github.com/xiaobaitu/soloqueue/internal/skill"
	"github.com/xiaobaitu/soloqueue/internal/timeline"
	"github.com/xiaobaitu/soloqueue/internal/tools"
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
Use 'soloqueue serve' to start the local HTTP/WebSocket server.

Environment:
  ALT_SCREEN=1    Enable fullscreen TUI with fixed bottom input (default: inline mode)`,
		SilenceUsage: true,
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

			log, err := initLogger(workDir, cfg, false)
			if err != nil {
				return fmt.Errorf("init logger: %w", err)
			}
			defer log.Close()

			settings := cfg.Get()

			// promptProfileQuestions は TUI モードのみ必要なので、
			// profileSetup コールバックとして渡す
			profileSetup := func(cfg *prompt.PromptConfig) error {
				answers := promptProfileQuestions()
				return cfg.WriteProfile(answers)
			}

			rt, err := buildRuntimeStack(workDir, cfg, log, profileSetup)
			if err != nil {
				return err
			}
			defer rt.shutdown()

			log.Info(logger.CatApp, "soloqueue tui starting",
				"version", version, "model", rt.defaultModel.ID)

			agentFactory := buildSessionFactory(rt, workDir, settings, false /* TUI: no console log */)
			mgr := session.NewSessionManager(agentFactory, 30*time.Minute)

			rootCtx, stop := signal.NotifyContext(context.Background(),
				os.Interrupt, syscall.SIGTERM)
			defer stop()
			go mgr.ReapLoop(rootCtx, time.Minute, 5*time.Second)
			defer mgr.Shutdown(5 * time.Second)

			return tui.Run(tui.Config{
				SessionMgr:   mgr,
				ModelID:      rt.defaultModel.ID,
				Version:      version,
				RulesCreated: rt.rulesCreated,
				RulesPath:    rt.promptCfg.RulesPath(),
				Registry:     rt.agentRegistry,
				Supervisors:  rt.supervisors,
			})
		},
	}

	root.AddCommand(versionCmd())
	root.AddCommand(serveCmd())

	return root
}

// ─── runtimeStack ─────────────────────────────────────────────────────────────

// runtimeStack 保存两种模式（TUI / serve）共享的运行时依赖，
// 由 buildRuntimeStack 统一初始化，避免重复代码。
type runtimeStack struct {
	llmClient     agent.LLMClient
	agentRegistry *agent.Registry
	agentFactory  *agent.DefaultFactory
	supervisors   []*agent.Supervisor
	leaders       []prompt.LeaderInfo
	allTemplates  []agent.AgentTemplate
	systemPrompt  string
	promptCfg     *prompt.PromptConfig
	defaultModel  *config.LLMModel
	tokenizer     *ctxwin.Tokenizer
	toolsCfg      tools.Config
	rulesCreated  bool
}

// shutdown 优雅回收所有 L2 Supervisor 管理的子 Agent
func (rt *runtimeStack) shutdown() {
	for _, sv := range rt.supervisors {
		_ = sv.ReapAll(5 * time.Second)
	}
}

// profileSetupFn 在首次启动时写入用户 profile（TUI 用交互式问卷，serve 用默认值）
type profileSetupFn func(cfg *prompt.PromptConfig) error

// buildRuntimeStack 初始化两种模式共用的运行时栈：
//
//  1. LLM 客户端（DeepSeek）
//  2. Prompt 系统（EnsureFiles + BuildPrompt）
//  3. Agent Registry + DefaultFactory
//  4. L2 Supervisor 列表（IsLeader 模板各一个）
func buildRuntimeStack(
	workDir string,
	cfg *config.GlobalService,
	log *logger.Logger,
	profileSetup profileSetupFn,
) (*runtimeStack, error) {
	settings := cfg.Get()
	provider := cfg.DefaultProvider()
	if provider == nil {
		return nil, errors.New("no default provider configured")
	}
	defaultModel := cfg.DefaultModel("")
	if defaultModel == nil {
		return nil, errors.New("no default model configured")
	}

	// ── LLM 客户端 ───────────────────────────────────────────────────────────
	apiKey := provider.ResolveAPIKey()
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
		return nil, fmt.Errorf("build llm client: %w", err)
	}

	// ── Tools 配置 ────────────────────────────────────────────────────────────
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get working directory: %w", err)
	}
	allowedDirs := append([]string{workDir, cwd}, settings.Tools.AllowedDirs...)
	toolsCfg := settings.Tools.ToToolsConfig(allowedDirs)

	// ── Prompt 系统 ───────────────────────────────────────────────────────────
	promptCfg := &prompt.PromptConfig{
		RoleID:  "main_assistant",
		BaseDir: filepath.Join(workDir, "prompts"),
	}
	rulesCreated, err := promptCfg.EnsureFiles()
	if err != nil {
		var profileErr *prompt.ProfileNeededError
		if errors.As(err, &profileErr) {
			if writeErr := profileSetup(promptCfg); writeErr != nil {
				return nil, fmt.Errorf("write profile: %w", writeErr)
			}
			rulesCreated, err = promptCfg.EnsureFiles()
			if err != nil {
				return nil, fmt.Errorf("ensure prompt files: %w", err)
			}
		} else {
			return nil, fmt.Errorf("ensure prompt files: %w", err)
		}
	}

	// ── Leaders + Agent 模板 ──────────────────────────────────────────────────
	leaders, err := prompt.LoadLeaders(filepath.Join(workDir, "agents"))
	if err != nil {
		log.Warn(logger.CatApp, "failed to load leaders", "err", err)
		leaders = nil
	}
	allTemplates, err := agent.LoadAgentTemplates(filepath.Join(workDir, "agents"))
	if err != nil {
		log.Warn(logger.CatApp, "failed to load agent templates", "err", err)
		allTemplates = nil
	}

	systemPrompt, err := promptCfg.BuildPrompt(leaders)
	if err != nil {
		return nil, fmt.Errorf("build system prompt: %w", err)
	}

	// ── Agent Registry + Factory ──────────────────────────────────────────────
	agentRegistry := agent.NewRegistry(log)
	agentFactory := agent.NewDefaultFactory(
		agentRegistry, llmClient, toolsCfg,
		filepath.Join(workDir, "skills"), log,
	)

	// ── L2 Supervisors ────────────────────────────────────────────────────────
	// 为每个 IsLeader 模板创建 L2 Agent 并用 Supervisor 管理其 L3 子 Agent 生命周期。
	// Supervisor 在 runtimeStack.shutdown() 时负责回收所有 L3 子 Agent。
	var supervisors []*agent.Supervisor
	for _, tmpl := range allTemplates {
		if !tmpl.IsLeader {
			continue
		}
		l2Agent, _, err := agentFactory.Create(context.Background(), tmpl)
		if err != nil {
			log.Warn(logger.CatApp, "failed to create L2 agent", "name", tmpl.Name, "err", err)
			continue
		}
		sv := agent.NewSupervisor(l2Agent, agentFactory, log)
		supervisors = append(supervisors, sv)
	}

	return &runtimeStack{
		llmClient:     llmClient,
		agentRegistry: agentRegistry,
		agentFactory:  agentFactory,
		supervisors:   supervisors,
		leaders:       leaders,
		allTemplates:  allTemplates,
		systemPrompt:  systemPrompt,
		promptCfg:     promptCfg,
		defaultModel:  defaultModel,
		tokenizer:     ctxwin.NewTokenizer(),
		toolsCfg:      toolsCfg,
		rulesCreated:  rulesCreated,
	}, nil
}

// --- Session factory ---

// sessionBuilder encapsulates session creation logic, replacing the
// 140-line closure in buildSessionFactory with a testable struct.
type sessionBuilder struct {
	rt         *runtimeStack
	workDir    string
	settings   config.Settings
	consoleLog bool
	tlMaxBytes int64
	tlMaxFiles int
	replaySegs int
}

func newSessionBuilder(
	rt *runtimeStack,
	workDir string,
	settings config.Settings,
	consoleLog bool,
) *sessionBuilder {
	return &sessionBuilder{
		rt:         rt,
		workDir:    workDir,
		settings:   settings,
		consoleLog: consoleLog,
		tlMaxBytes: int64(config.DefaultInt(settings.Session.TimelineMaxFileMB, 50)) * 1024 * 1024,
		tlMaxFiles: config.DefaultInt(settings.Session.TimelineMaxFiles, 5),
		replaySegs: config.DefaultInt(settings.Session.ReplaySegments, 3),
	}
}

// Build creates a new session with its own agent, context window, and
// timeline writer. Each call produces an independent session.
func (sb *sessionBuilder) Build(ctx context.Context, teamID string) (*agent.Agent, *ctxwin.ContextWindow, *timeline.Writer, error) {
	agentID := newAgentID()

	effectiveModelID := sb.rt.defaultModel.APIModel
	if effectiveModelID == "" {
		effectiveModelID = sb.rt.defaultModel.ID
	}
	def := agent.Definition{
		ID:              agentID,
		TeamID:          teamID,
		Kind:            agent.KindChat,
		ModelID:         effectiveModelID,
		Temperature:     sb.rt.defaultModel.Generation.Temperature,
		MaxTokens:       sb.rt.defaultModel.Generation.MaxTokens,
		ReasoningEffort: sb.rt.defaultModel.Thinking.ReasoningEffort,
		ThinkingEnabled: sb.rt.defaultModel.Thinking.Enabled,
		ThinkingType:    sb.rt.defaultModel.Thinking.Type,
		MaxIterations:   10,
		ContextWindow:   sb.rt.defaultModel.ContextWindow,
		SystemPrompt:    sb.rt.systemPrompt,
	}

	effectiveTeam := teamID
	if effectiveTeam == "" {
		effectiveTeam = "default"
	}
	sessLog, err := logger.Session(sb.workDir, effectiveTeam, agentID,
		logger.WithLevel(logger.ParseLogLevel(sb.settings.Log.Level)),
		logger.WithConsole(sb.consoleLog),
		logger.WithFile(sb.settings.Log.File),
	)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build session logger: %w", err)
	}

	// Tools: built-in tools + DelegateTool (async mode: L1 -> L2)
	allTools := tools.Build(sb.rt.toolsCfg)
	for _, l := range sb.rt.leaders {
		leader := l // capture loop variable
		dt := &tools.DelegateTool{
			LeaderID: leader.Name,
			Desc:     leader.Description,
			SpawnFn: func(ctx context.Context, task string) (iface.Locatable, error) {
				a, ok := sb.rt.agentRegistry.Get(leader.Name)
				if !ok {
					return nil, fmt.Errorf("leader %q not found", leader.Name)
				}
				return &agent.LocatableAdapter{Agent: a}, nil
			},
			Timeout: 5 * time.Minute,
		}
		allTools = append(allTools, dt)
	}

	// Skills: user SKILL.md files
	var skillList []skill.Skill
	if userSkills, err := skill.LoadSkillsFromDir(filepath.Join(sb.workDir, "skills")); err == nil {
		skillList = append(skillList, userSkills...)
	}

	a := agent.NewAgent(def, sb.rt.llmClient, sessLog,
		agent.WithTools(allTools...),
		agent.WithSkills(skillList...),
		agent.WithParallelTools(true),
		agent.WithPriorityMailbox(),
		agent.WithToolTimeout("shell_exec", 30*time.Second),
		agent.WithToolTimeout("http_fetch", 10*time.Second),
		agent.WithToolTimeout("web_search", 15*time.Second),
	)
	sb.rt.agentRegistry.Register(a)

	// Timeline writer + push hook
	tlDir := filepath.Join(sb.workDir, "logs", "timelines", effectiveTeam)
	tl, err := timeline.NewWriter(tlDir, "timeline", sb.tlMaxBytes, sb.tlMaxFiles)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build timeline writer: %w", err)
	}
	pushHook := func(msg ctxwin.Message) {
		var toolCalls []timeline.ToolCallRec
		for _, tc := range msg.ToolCalls {
			toolCalls = append(toolCalls, timeline.ToolCallRec{
				ID:        tc.ID,
				Type:      tc.Type,
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			})
		}
		if err := tl.AppendMessage(&timeline.MessagePayload{
			Role:             string(msg.Role),
			Content:          msg.Content,
			ReasoningContent: msg.ReasoningContent,
			Name:             msg.Name,
			ToolCallID:       msg.ToolCallID,
			ToolCalls:        toolCalls,
			IsEphemeral:      msg.IsEphemeral,
			AgentID:          agentID,
		}); err != nil {
			sessLog.Error(logger.CatActor, "timeline append failed",
				"err", err, "role", string(msg.Role), "agent_id", agentID)
		}
	}

	// ContextWindow + system prompt
	cw := ctxwin.NewContextWindow(
		sb.rt.defaultModel.ContextWindow,
		sb.rt.defaultModel.ContextWindow/10,
		sb.rt.tokenizer,
		ctxwin.WithPushHook(pushHook),
	)
	if def.SystemPrompt != "" {
		cw.Push(ctxwin.RoleSystem, def.SystemPrompt)
	}
	if cat := a.SkillCatalog(); cat != "" {
		cw.Push(ctxwin.RoleSystem, cat)
	}

	// Replay history segments
	if sb.replaySegs > 0 {
		segments, err := timeline.ReadLastSegments(tlDir, "timeline", sb.replaySegs)
		if err == nil && len(segments) > 0 {
			cw.SetReplayMode(true)
			timeline.ReplayInto(cw, segments)
			cw.SetReplayMode(false)
		}
	}

	if err := a.Start(context.Background()); err != nil {
		tl.Close()
		return nil, nil, nil, err
	}
	return a, cw, tl, nil
}

// buildSessionFactory constructs the factory function used by SessionManager.
//
// consoleLog controls whether the session logger outputs to stderr
// (TUI=false, serve=settings.Log.Console).
func buildSessionFactory(
	rt *runtimeStack,
	workDir string,
	settings config.Settings,
	consoleLog bool,
) session.AgentFactory {
	sb := newSessionBuilder(rt, workDir, settings, consoleLog)
	return sb.Build
}

// ─── Commands ──────────────────────────────────────────────────────────────────

func versionCmd() *cobra.Command {
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
			workDir, err := config.DefaultWorkDir()
			if err != nil {
				return err
			}

			cfg, err := config.Init(workDir)
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
				"host", host, "port", port, "version", version)

			settings := cfg.Get()

			// serve 模式无交互终端，使用默认 profile
			profileSetup := func(cfg *prompt.PromptConfig) error {
				return cfg.WriteProfile(prompt.DefaultProfileAnswers())
			}

			rt, err := buildRuntimeStack(workDir, cfg, log, profileSetup)
			if err != nil {
				return err
			}
			defer rt.shutdown()

			factory := buildSessionFactory(rt, workDir, settings, settings.Log.Console)
			mgr := session.NewSessionManager(factory, 30*time.Minute)

			rootCtx, stop := signal.NotifyContext(context.Background(),
				os.Interrupt, syscall.SIGTERM)
			defer stop()

			go mgr.ReapLoop(rootCtx, time.Minute, 5*time.Second)

			mux := server.NewMux(mgr, log)
			srv := &http.Server{
				Addr:    fmt.Sprintf("%s:%d", host, port),
				Handler: mux,
			}

			go func() {
				<-rootCtx.Done()
				log.Info(logger.CatApp, "shutdown signal received")
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				_ = srv.Shutdown(shutdownCtx)
				mgr.Shutdown(5 * time.Second)
			}()

			log.Info(logger.CatApp, "server listening", "addr", srv.Addr)
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

// newAgentID returns a short random ID for an agent instance
func newAgentID() string {
	return fmt.Sprintf("agent-%d", time.Now().UnixNano())
}

// promptProfileQuestions 在 TUI 启动前执行交互式问卷，收集用户个性化设定。
func promptProfileQuestions() prompt.ProfileAnswers {
	answers := prompt.DefaultProfileAnswers()

	fmt.Println(prompt.ProfilePromptText())
	fmt.Println()

	answers.Name = readLineWithDefault("1. What should we call your assistant?", answers.Name)
	answers.Gender = readLineWithDefault("2. Assistant gender (male/female)?", answers.Gender)
	answers.Personality = readLineWithDefault("3. Personality (strict/playful/gentle/direct/custom)?", answers.Personality)
	answers.CommStyle = readLineWithDefault("4. Communication style (brief/detailed/casual/formal)?", answers.CommStyle)

	return answers
}

// readLineWithDefault 读取一行输入，空行则返回默认值。
func readLineWithDefault(prompt, def string) string {
	fmt.Printf("%s [%s] ", prompt, def)
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		input := strings.TrimSpace(scanner.Text())
		if input != "" {
			return input
		}
	}
	return def
}

// initLogger 根据当前配置创建 system 层 Logger。
func initLogger(workDir string, cfg *config.GlobalService, console bool) (*logger.Logger, error) {
	settings := cfg.Get()

	level := logger.ParseLogLevel(settings.Log.Level)
	log, err := logger.System(workDir,
		logger.WithLevel(level),
		logger.WithConsole(console),
		logger.WithFile(settings.Log.File),
	)
	if err != nil {
		return nil, err
	}

	cfg.OnChange(func(old, new config.Settings) {
		_ = old
		_ = new
	})

	cfg.SetErrorHandler(func(err error) {
		log.Error(logger.CatConfig, "config watcher error", "err", err)
	})

	return log, nil
}
