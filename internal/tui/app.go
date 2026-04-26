package tui

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/xiaobaitu/soloqueue/internal/agent"
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

// timelineKind distinguishes the type of a timeline entry.
type timelineKind int

const (
	timelineThinking timelineKind = iota
	timelineTool
)

// timelineEntry represents a single item in the chronological timeline.
type timelineEntry struct {
	kind timelineKind
	text string     // for thinking entries
	tool *toolBlock // for tool entries (pointer for in-place update)
}

// message represents a single conversation turn.
type message struct {
	role     string // "user" | "agent"
	content  string
	timeline []timelineEntry
}

// toolBlock represents a tool call's lifecycle in the UI.
type toolBlock struct {
	name      string
	args      string
	callID    string
	done      bool
	duration  time.Duration
	err       error
	lineCount int
}

// toolExecInfo tracks tool execution for duration calculation.
type toolExecInfo struct {
	name   string
	args   string
	start  time.Time
	callID string
	tb     *toolBlock // back-pointer for in-place update by ToolExecDoneEvent
}

// streamState holds the state of the current streaming response.
type streamState struct {
	timeline      []timelineEntry
	content       strings.Builder
	toolExecMap   map[string]*toolExecInfo
	curToolCallID string
	curToolName   string
	curToolArgs   strings.Builder
	thinkingBuf   strings.Builder // active thinking buffer, flushed into timeline on tool start / content start
}

// confirmState holds the state of a tool confirmation dialog.
type confirmState struct {
	callID   string
	prompt   string
	options  []string
	selected int
}

// ─── Bubble Tea messages ──────────────────────────────────────────────────────

type agentEventMsg struct {
	event  agent.AgentEvent
	evCh   <-chan agent.AgentEvent
	cancel context.CancelFunc
}
type streamStartMsg struct {
	evCh   <-chan agent.AgentEvent
	cancel context.CancelFunc
	err    error
}
type streamDoneMsg struct{}
type confirmResultMsg struct {
	callID string
	choice string
}
type resetQuitMsg struct{}

// ─── Model ────────────────────────────────────────────────────────────────────

type model struct {
	cfg  Config
	sess *session.Session
	ctx  context.Context

	// UI state
	textInput    textinput.Model
	isGenerating bool
	quitCount    int
	width        int
	height       int

	// Conversation
	messages []message

	// Current stream
	current      *streamState
	streamCancel context.CancelFunc

	// History (for /history command)
	history []string

	// Tool confirmation
	confirmState *confirmState
}

// ─── Run (public entry point) ─────────────────────────────────────────────────

// Run starts the TUI application.
func Run(cfg Config) error {
	ctx := context.Background()

	m := model{
		cfg:          cfg,
		ctx:          ctx,
		messages:     []message{},
	}

	// Setup text input
	ti := textinput.New()
	ti.Prompt = "❯  "
	ti.PromptStyle = userStyle
	ti.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	ti.Cursor.Style = lipgloss.NewStyle()
	ti.Focus()
	m.textInput = ti

	// Create session
	sess, err := cfg.SessionMgr.Create(ctx, "")
	if err != nil {
		fmt.Println(errorStyle.Render("fatal: " + err.Error()))
		return err
	}
	m.sess = sess

	p := tea.NewProgram(m)

	_, err = p.Run()
	return err
}

// ─── Init ─────────────────────────────────────────────────────────────────────

func (m model) Init() tea.Cmd {
	return tea.Sequence(textinput.Blink, tea.Printf("%s", renderLogo(m.cfg.Version)))
}

// ─── Update ───────────────────────────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		// Reset quit count on any non-Ctrl+C key
		if msg.Type != tea.KeyCtrlC && m.quitCount > 0 {
			m.quitCount = 0
		}

		switch msg.Type {
		case tea.KeyCtrlC:
			if m.isGenerating {
				// Cancel current stream
				if m.streamCancel != nil {
					m.streamCancel()
				}
				m.isGenerating = false
				// Finalize current stream to messages
				m.finalizeCurrentStream()
				return m, m.flushStaticHistory()
			}
			m.quitCount++
			if m.quitCount >= 2 {
				return m, tea.Quit
			}
			return m, tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return resetQuitMsg{} })


		case tea.KeyEnter:
			// If in confirm state, confirm selection
			if m.confirmState != nil {
				return m.handleConfirmEnter()
			}
			if m.isGenerating || strings.TrimSpace(m.textInput.Value()) == "" {
				return m, nil
			}

			input := strings.TrimSpace(m.textInput.Value())
			m.textInput.SetValue("")

			// Handle built-in commands
			if quit, builtinCmd := m.handleBuiltin(input); quit {
				if builtinCmd != nil {
					return m, tea.Sequence(builtinCmd, tea.Quit)
				}
				return m, tea.Quit
			} else if builtinCmd != nil {
				return m, builtinCmd
			}
			if strings.HasPrefix(input, "/") {
				return m, nil
			}

			// Add user message
			m.messages = append(m.messages, message{role: "user", content: input})
			m.addHistory(input)

			// Start streaming
			m.isGenerating = true
			m.current = &streamState{
				toolExecMap: make(map[string]*toolExecInfo),
			}
			m.messages = append(m.messages, message{role: "agent"})

			// Launch AskStream in goroutine
			cmd = m.startStream(input)
			return m, cmd

		case tea.KeyUp:
			if m.confirmState != nil && m.confirmState.selected > 0 {
				m.confirmState.selected--
			}
			return m, nil

		case tea.KeyDown:
			if m.confirmState != nil && m.confirmState.selected < len(m.confirmState.options)-1 {
				m.confirmState.selected++
			}
			return m, nil
		}

	case streamStartMsg:
		if msg.err != nil {
			m.isGenerating = false
			m.current = nil
			msg.cancel()
			// Print error as static text
			errText := agentStyle.Render("Agent:") + "\n" + errorStyle.Render("✗ "+msg.err.Error()) + "\n\n"
			return m, tea.Sequence(tea.Printf("%s", errText), m.flushStaticHistory())
		}
		m.streamCancel = msg.cancel
		// Start consuming events
		return m, waitForAgentEvent(msg.evCh, msg.cancel)

	case agentEventMsg:
		m.handleAgentEvent(msg.event)
		// Continue consuming events from the channel
		return m, waitForAgentEvent(msg.evCh, msg.cancel)

	case streamDoneMsg:
		m.finalizeCurrentStream()
		m.isGenerating = false
		m.current = nil
		return m, m.flushStaticHistory()

	case confirmResultMsg:
		if m.confirmState != nil {
			if err := m.sess.Agent.Confirm(msg.callID, msg.choice); err != nil {
				errText := agentStyle.Render("Agent:") + "\n" + errorStyle.Render("✗ confirm error: "+err.Error()) + "\n\n"
				m.confirmState = nil
				return m, tea.Printf("%s", errText)
			}
			m.confirmState = nil
		}
		return m, nil

	case resetQuitMsg:
		m.quitCount = 0
		return m, nil
	}

	// Pass through to textinput when not in confirm mode and not generating
	if m.confirmState == nil {
		m.textInput, cmd = m.textInput.Update(msg)
	}
	return m, cmd
}

// ─── Message rendering helpers ────────────────────────────────────────────────

// renderUserMessage renders a single user message as a string.
func renderUserMessage(msg message) string {
	return userStyle.Render("You: ") + msg.content + "\n\n"
}

// renderAgentMessage renders a single agent message as a string.
// Timeline entries (thinking + tool calls) are rendered in chronological order.
// Thinking blocks are always expanded; tool calls are shown in compact collapsed form
// (name, args, status, duration) inline within the thinking flow.
func renderAgentMessage(msg message) string {
	var sb strings.Builder
	sb.WriteString(agentStyle.Render("Agent:") + "\n")

	hasTimelineEntry := false

	for _, entry := range msg.timeline {
		switch entry.kind {
		case timelineThinking:
			sb.WriteString(thinkStyle.Render(entry.text) + "\n")
			hasTimelineEntry = true
		case timelineTool:
			if entry.tool != nil {
				sb.WriteString(toolCollapsedStyle.Render(formatToolBlock(*entry.tool)) + "\n")
			}
			hasTimelineEntry = true
		}
	}

	if hasTimelineEntry {
		sb.WriteString("\n")
	}

	if msg.content != "" {
		sb.WriteString(contentStyle.Render(msg.content) + "\n\n")
	}

	return sb.String()
}

// ─── View ─────────────────────────────────────────────────────────────────────

func (m model) View() string {
	var sb strings.Builder

	// Dynamic zone: only render current interactive content.
	// Completed history has been flushed to scrollback via tea.Printf.

	// 1. Last user message (only while generating, for context)
	if m.isGenerating {
		for i := len(m.messages) - 1; i >= 0; i-- {
			if m.messages[i].role == "user" {
				sb.WriteString(renderUserMessage(m.messages[i]))
				break
			}
		}
	}

	// 2. Active agent message (streaming or last agent in buffer)
	agentMsg := m.currentMessage()
	if agentMsg != nil {
		// Build a temporary message with live stream state if generating
		displayMsg := *agentMsg
		if m.current != nil {
			displayMsg.timeline = make([]timelineEntry, len(m.current.timeline))
			copy(displayMsg.timeline, m.current.timeline)
			// Append in-progress thinking as a virtual entry (no side effects in View)
			if m.current.thinkingBuf.Len() > 0 {
				displayMsg.timeline = append(displayMsg.timeline, timelineEntry{
					kind: timelineThinking,
					text: m.current.thinkingBuf.String(),
				})
			}
			displayMsg.content = m.current.content.String()
		}

		// Truncate content if it exceeds the dynamic height budget
		if m.height > 0 {
			budget := m.dynamicHeightBudget()
			rendered := renderAgentMessage(displayMsg)
			lineCount := strings.Count(rendered, "\n")
			if lineCount > budget {
				// Keep only the last portion of content that fits
				lines := strings.Split(rendered, "\n")
				if budget > 0 {
					rendered = strings.Join(lines[len(lines)-budget:], "\n")
				} else {
					rendered = ""
				}
			}
			sb.WriteString(rendered)
		} else {
			sb.WriteString(renderAgentMessage(displayMsg))
		}
	}

	// 3. Confirm dialog (if active)
	if m.confirmState != nil {
		sb.WriteString("\n")
		sb.WriteString(lipgloss.NewStyle().Foreground(colorWarning).Bold(true).Render("⚠ "+m.confirmState.prompt) + "\n")
		for i, opt := range m.confirmState.options {
			if i == m.confirmState.selected {
				sb.WriteString(confirmHighlight.Render("  ❯ " + opt))
			} else {
				sb.WriteString(confirmNormal.Render("    " + opt))
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	// 4. Status bar
	statusText := " READY "
	sStyle := statusStyle.Foreground(lipgloss.Color("250"))
	if m.quitCount > 0 {
		statusText = " ⚠️  CONFIRM EXIT (Press Ctrl+C again) "
		sStyle = statusStyle.Background(colorWarning).Foreground(lipgloss.Color("255"))
	} else if m.isGenerating {
		statusText = " ⏳ GENERATING... "
		sStyle = statusStyle.Background(colorPrimary).Foreground(lipgloss.Color("255"))
	}
	sb.WriteString(sStyle.Render(statusText) + "\n")

	// 5. Input box
	sb.WriteString(m.textInput.View() + "\n")

	// 6. Hint line (right-aligned)
	hint := "Ctrl+C Quit"
	renderedHint := hintStyle.Render(hint)
	padding := m.width - lipgloss.Width(renderedHint)
	if padding > 0 {
		sb.WriteString(strings.Repeat(" ", padding))
	}
	sb.WriteString(renderedHint)

	return sb.String()
}

// dynamicHeightBudget returns the maximum number of lines available
// for dynamic content in View(). It reserves lines for the status bar,
// input box, and hint line.
func (m model) dynamicHeightBudget() int {
	reserved := 3 // status bar + input + hint
	if m.height <= reserved {
		return 1
	}
	return m.height - reserved
}

// ─── Stream ───────────────────────────────────────────────────────────────────

func (m model) startStream(prompt string) tea.Cmd {
	return func() tea.Msg {
		streamCtx, cancel := context.WithCancel(m.ctx)
		evCh, err := m.sess.AskStream(streamCtx, prompt)
		return streamStartMsg{evCh: evCh, cancel: cancel, err: err}
	}
}

// waitForAgentEvent returns a tea.Cmd that blocks until an event is available
// on evCh, then returns the event as a tea.Msg. After each event, the Update
// handler should call waitForAgentEvent again to continue consuming.
func waitForAgentEvent(evCh <-chan agent.AgentEvent, cancel context.CancelFunc) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-evCh
		if !ok {
			cancel()
			return streamDoneMsg{}
		}
		return agentEventMsg{event: ev, evCh: evCh, cancel: cancel}
	}
}

// currentMessage returns a pointer to the last agent message (for live updates).
func (m *model) currentMessage() *message {
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].role == "agent" {
			return &m.messages[i]
		}
	}
	return nil
}

// finalizeCurrentStream copies stream state into the message history.
func (m *model) finalizeCurrentStream() {
	if m.current == nil {
		return
	}
	msg := m.currentMessage()
	if msg == nil {
		return
	}
	// Flush any remaining thinking
	m.current.flushThinking()
	msg.timeline = make([]timelineEntry, len(m.current.timeline))
	copy(msg.timeline, m.current.timeline)
	msg.content = m.current.content.String()

	if m.streamCancel != nil {
		m.streamCancel()
		m.streamCancel = nil
	}
	m.current = nil
}

// flushStaticHistory renders all messages in the buffer as static text
// via tea.Printf and clears the messages slice. This moves completed
// conversation turns into the terminal scrollback buffer, outside
// Bubble Tea's redraw jurisdiction.
func (m *model) flushStaticHistory() tea.Cmd {
	if len(m.messages) == 0 {
		return nil
	}

	var sb strings.Builder
	for _, msg := range m.messages {
		if msg.role == "user" {
			sb.WriteString(renderUserMessage(msg))
		} else {
			sb.WriteString(renderAgentMessage(msg))
		}
	}
	m.messages = nil

	text := sb.String()
	return tea.Printf("%s", text)
}

// ─── History ──────────────────────────────────────────────────────────────────

func (m *model) addHistory(line string) {
	if line == "" || (len(m.history) > 0 && m.history[len(m.history)-1] == line) {
		return
	}
	m.history = append(m.history, line)
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

// formatToolBlock renders a toolBlock for display.
func formatToolBlock(tb toolBlock) string {
	ta := parseToolArgs(tb.args)
	var displayArg string
	if ta.Path != "" {
		displayArg = ta.Path
	} else if ta.Command != "" {
		displayArg = ta.Command
	} else if ta.File != "" {
		displayArg = ta.File
	}

	if tb.done {
		var durHint string
		if tb.duration > 0 {
			durHint = fmt.Sprintf(" · %s", tb.duration.Round(time.Millisecond))
		}
		if tb.err != nil {
			return fmt.Sprintf("✗ %s(%s) failed: %s", tb.name, displayArg, truncate(tb.err.Error(), 50))
		}
		if tb.lineCount > 0 {
			return fmt.Sprintf("✓ %s(%s) %d lines%s", tb.name, displayArg, tb.lineCount, durHint)
		}
		if displayArg != "" {
			return fmt.Sprintf("✓ %s(%s)%s", tb.name, displayArg, durHint)
		}
		return fmt.Sprintf("✓ %s%s", tb.name, durHint)
	}

	if displayArg != "" {
		return fmt.Sprintf("⚙ %s(%s)", tb.name, displayArg)
	}
	return fmt.Sprintf("⚙ %s", tb.name)
}

// ─── Logo ─────────────────────────────────────────────────────────────────────

func renderLogo(version string) string {
	if version == "" {
		return ""
	}

	logoLines := []string{
		" ╭──╮ ╭──╮ ",
		" ╰──╮ │  │  soloqueue",
		" ╰──╯ ╰──┼  " + version,
		"         ╰ ",
	}

	// Gradient: cyan → gold
	startR, startG, startB := uint8(0), uint8(229), uint8(255)
	endR, endG, endB := uint8(245), uint8(208), uint8(97)

	var sb strings.Builder
	for i, line := range logoLines {
		ratio := float64(i) / float64(len(logoLines)-1)
		r := startR + uint8(float64(endR-startR)*ratio)
		g := startG + uint8(float64(endG-startG)*ratio)
		b := startB + uint8(float64(endB-startB)*ratio)
		hex := fmt.Sprintf("#%02X%02X%02X", r, g, b)
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(hex)).Render(line) + "\n")
	}
	sb.WriteString(dimStyle.Render("session ready — type your question or /help") + "\n\n")
	return sb.String()
}

// ─── Confirm handling ─────────────────────────────────────────────────────────

func (m model) handleConfirmEnter() (tea.Model, tea.Cmd) {
	if m.confirmState == nil {
		return m, nil
	}
	cs := m.confirmState
	choice := cs.options[cs.selected]

	// Map display text back to agent choice
	var agentChoice string
	switch {
	case strings.HasPrefix(choice, "[y]"):
		agentChoice = string(agent.ChoiceApprove)
	case strings.HasPrefix(choice, "[n]"):
		agentChoice = string(agent.ChoiceDeny)
	case strings.HasPrefix(choice, "[a]"):
		agentChoice = string(agent.ChoiceAllowInSession)
	default:
		agentChoice = choice
	}

	return m, func() tea.Msg {
		return confirmResultMsg{callID: cs.callID, choice: agentChoice}
	}
}
