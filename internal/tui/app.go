package tui

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/huh"
	"github.com/xiaobaitu/soloqueue/internal/session"
)

// ─── Config ───────────────────────────────────────────────────────────────────

// Config stores TUI application configuration.
type Config struct {
	SessionMgr *session.SessionManager
	ModelID    string
	Version    string
}

// ─── Data types ───────────────────────────────────────────────────────────────

// thinkBlock holds a reasoning block.
type thinkBlock struct {
	lines []string
}

type toolExecInfo struct {
	name     string
	args     string
	start    time.Time
	duration time.Duration
	err      error
	result   string
	done     bool
	callID   string
}

// ─── App ──────────────────────────────────────────────────────────────────────

// App is the main TUI application.
type App struct {
	cfg  Config
	sess *session.Session
	ctx  context.Context

	// History (for /history command)
	history []string

	// Stream state
	streamCancel context.CancelFunc
	contentBuf  strings.Builder
	streamPhase string
	streamStart time.Time

	// Reasoning
	reasonBuf    strings.Builder
	reasonBlocks []thinkBlock
	curThinkIdx  int

	// Tool
	currentTool string
	toolArgs    strings.Builder
	toolExecMap map[string]*toolExecInfo

	lastLineEmpty bool
}

// ─── Run ──────────────────────────────────────────────────────────────────────

// Run starts the TUI application. This is the public entry point.
func Run(cfg Config) error {
	ctx := context.Background()

	app := &App{
		cfg:          cfg,
		ctx:          ctx,
		toolExecMap:  make(map[string]*toolExecInfo),
		curThinkIdx:  -1,
	}

	return app.run()
}

func (a *App) run() error {
	// Print logo
	a.printLogo()

	// Create session
	sess, err := a.cfg.SessionMgr.Create(a.ctx, "")
	if err != nil {
		fmt.Println(Styled("fatal: "+err.Error(), styleError))
		return err
	}
	a.sess = sess

	fmt.Println(Styled("session ready — type your question or /help", styleDim))
	fmt.Println()

	// Serial prompt loop
	for {
		var result string
		err := huh.NewInput().
			Prompt("> ❯ ").
			Placeholder("Ask anything...").
			Value(&result).
			Run()
		if err != nil {
			// Ctrl+C or Ctrl+D — exit
			break
		}

		input := strings.TrimSpace(result)
		if input == "" {
			continue
		}

		// Handle built-in commands
		if quit := a.handleBuiltin(input); quit {
			break
		}
		if strings.HasPrefix(input, "/") {
			continue
		}

		// Echo user input
		fmt.Println(Styled("> "+input, styleUser))
		fmt.Println()

		// Add to history
		a.addHistory(input)

		// Stream output
		a.streamOutput(input)
	}

	fmt.Println(Styled("Bye!", styleDim))
	return nil
}

// ─── Stream ───────────────────────────────────────────────────────────────────

func (a *App) streamOutput(prompt string) {
	streamCtx, cancel := context.WithCancel(a.ctx)
	defer cancel()

	evCh, err := a.sess.AskStream(streamCtx, prompt)
	if err != nil {
		fmt.Println(Styled("✗ "+err.Error(), styleError))
		return
	}

	a.streamCancel = cancel
	a.resetStreamState()

	// Consume events serially
	for ev := range evCh {
		a.handleAgentEvent(ev)
	}

	a.handleStreamDone(nil)
}

// ─── History ──────────────────────────────────────────────────────────────────

func (a *App) addHistory(line string) {
	if line == "" || (len(a.history) > 0 && a.history[len(a.history)-1] == line) {
		return
	}
	a.history = append(a.history, line)
}

// ─── String helpers ───────────────────────────────────────────────────────────

func truncate(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", "↵")
	s = strings.ReplaceAll(s, "\r", "")
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "…"
}

// ─── Logo ─────────────────────────────────────────────────────────────────────

func (a *App) printLogo() {
	if a.cfg.Version == "" {
		return
	}

	logoLines := []string{
		" ╭──╮ ╭──╮ ",
		" ╰──╮ │  │  soloqueue",
		" ╰──╯ ╰──┼  " + a.cfg.Version,
		"         ╰ ",
	}

	startR, startG, startB := uint8(0), uint8(229), uint8(255)
	endR, endG, endB := uint8(245), uint8(208), uint8(97)

	for i, line := range logoLines {
		ratio := float64(i) / float64(len(logoLines)-1)
		r := startR + uint8(float64(endR-startR)*ratio)
		g := startG + uint8(float64(endG-startG)*ratio)
		b := startB + uint8(float64(endB-startB)*ratio)
		fmt.Println(Styled(line, FgRGB(r, g, b)))
	}
	fmt.Println()
}
