package tui

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	"github.com/muesli/termenv"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/session"
)

// ─── Config ───────────────────────────────────────────────────────────────────

// Config stores TUI application configuration.
type Config struct {
	SessionMgr   *session.SessionManager
	ModelID      string
	Version      string
	RulesCreated bool   // 是否新创建了 rules.md
	RulesPath    string // rules.md 路径（用于通知用户）
}

// ─── Data types ───────────────────────────────────────────────────────────────

// genPhase tracks what the model is currently doing during generation.
type genPhase int

const (
	phaseWaiting    genPhase = iota // Before first delta arrives (waiting for model)
	phaseThinking                   // Receiving ReasoningDeltaEvent
	phaseGenerating                 // Receiving ContentDeltaEvent
	phaseToolCall                   // Receiving ToolCallDeltaEvent / executing tools
)

func (p genPhase) String() string {
	switch p {
	case phaseWaiting:
		return "waiting for model"
	case phaseThinking:
		return "thinking"
	case phaseGenerating:
		return "generating"
	case phaseToolCall:
		return "running tools"
	default:
		return ""
	}
}

// timelineKind distinguishes the type of a timeline entry.
type timelineKind int

const (
	timelineThinking timelineKind = iota
	timelineContent
	timelineTool
)

// timelineEntry represents a single item in the chronological timeline.
type timelineEntry struct {
	kind timelineKind
	text string     // for thinking and content entries
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
	flushedIdx    int             // timeline[0..flushedIdx-1] have been flushed to scrollback
	labelFlushed  bool            // true once "Solo:" has been printed to scrollback for this turn
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
type clearCancelMsg struct{}
type clearErrMsg struct{}
type clearSummaryMsg struct{}
type dotMsg struct{}
type hintRotateMsg struct{}

// ─── Model ────────────────────────────────────────────────────────────────────

type model struct {
	cfg  Config
	sess *session.Session
	ctx  context.Context

	// UI state
	textInput    textinput.Model
	isGenerating bool
	cancelReason string
	errMsg       string
	genDotOn     bool
	quitCount    int
	width        int
	height       int
	renderer     *glamour.TermRenderer
	darkBg       bool

	// Generation status tracking
	genStartTime   time.Time
	genPhase       genPhase
	promptTokens          int
	outputTokens          int
	cacheHitTokens        int
	cacheMissTokens       int
	reasoningTokens       int
	genSummary            string

	// Conversation
	messages []message

	// Current stream
	current      *streamState
	streamCancel context.CancelFunc

	// Input history navigation
	history      []string
	historyIdx   int    // 0 = not browsing; >0 = offset from end
	historyDraft string // saved input before history browsing

	// Hint rotation
	hintIndex int // current index into commandHints

	// Tool confirmation
	confirmState *confirmState

	// Logo display: true once the logo has been flushed to scrollback
	logoShown bool
}

// ─── Run (public entry point) ─────────────────────────────────────────────────

// Run starts the TUI application.
func Run(cfg Config) error {
	ctx := context.Background()

	m := model{
		cfg:      cfg,
		ctx:      ctx,
		messages: []message{},
		history:  loadHistory(),
	}

	// Setup text input
	ti := textinput.New()
	ti.Prompt = "❯ "
	styles := textinput.DefaultStyles(m.darkBg)
	styles.Focused.Prompt = userStyle
	styles.Focused.Text = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	styles.Cursor.Color = lipgloss.Color("252")
	ti.SetStyles(styles)
	focusCmd := ti.Focus()
	m.textInput = ti

	// Create session
	sess, err := cfg.SessionMgr.Create(ctx, "")
	if err != nil {
		fmt.Println(errorStyle.Render("fatal: " + err.Error()))
		return err
	}
	m.sess = sess
	_ = focusCmd

	// Detect terminal background before bubbletea takes over stdin.
	// This avoids OSC 11 query responses leaking into bubbletea's input stream.
	m.darkBg = termenv.HasDarkBackground()

	// Initialize glamour markdown renderer
	m.renderer = m.newRenderer()

	p := tea.NewProgram(m)

	_, err = p.Run()
	return err
}

// ─── Init ─────────────────────────────────────────────────────────────────────

func (m model) Init() tea.Cmd {
	cmds := []tea.Cmd{textinput.Blink, hintRotateCmd()}

	// Flush rules creation notice to scrollback before View renders
	if m.cfg.RulesCreated && m.cfg.RulesPath != "" {
		cmds = append(cmds, printfOnce("  rules.md not found — default created at %s. Feel free to edit it anytime.\n\n", m.cfg.RulesPath))
	}

	return tea.Batch(cmds...)
}

// newRenderer creates a glamour TermRenderer configured for the current
// terminal width. A 2-char safety margin is reserved to avoid native terminal
// line breaks at the edge column that can cause Bubbletea line-count drift.
//
// Uses custom JSON styles instead of built-in "dark"/"light" to work around
// a glamour bug where H2-H4 headings are not rendered with standard styles.
func (m *model) newRenderer() *glamour.TermRenderer {
	wrapWidth := m.width - 2
	if wrapWidth <= 0 {
		wrapWidth = 78
	}
	var styleJSON []byte
	if m.darkBg {
		styleJSON = []byte(darkStyleJSON)
	} else {
		styleJSON = []byte(lightStyleJSON)
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStylesFromJSONBytes(styleJSON),
		glamour.WithWordWrap(wrapWidth),
	)
	if err != nil {
		return nil
	}
	return r
}

// ─── Update ───────────────────────────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		var clearCmd tea.Cmd
		if m.width > 0 && msg.Width < m.width {
			clearCmd = func() tea.Msg { return tea.ClearScreen() }
		}
		m.width = msg.Width
		m.height = msg.Height
		m.textInput.SetWidth(m.width - lipgloss.Width(m.textInput.Prompt) - 1)
		m.renderer = m.newRenderer()
		return m, clearCmd

	case tea.KeyPressMsg:
		// Reset quit count on any non-Ctrl+C key
		if msg.String() != "ctrl+c" && m.quitCount > 0 {
			m.quitCount = 0
		}

		switch msg.String() {
		case "ctrl+c":
			if m.isGenerating {
				// Cancel current stream
				if m.streamCancel != nil {
					m.streamCancel()
				}
				m.resetGenState()
				return m, m.flushCompletedTurn()
			}
			m.quitCount++
			if m.quitCount >= 2 {
				return m, tea.Quit
			}
			return m, tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return resetQuitMsg{} })

		case "esc":
			if m.isGenerating {
				if m.streamCancel != nil {
					m.streamCancel()
				}
				m.cancelReason = "Esc"
				m.resetGenState()
				return m, tea.Sequence(m.flushCompletedTurn(), tea.Tick(3*time.Second, func(t time.Time) tea.Msg { return clearCancelMsg{} }))
			}

		case "enter":
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

			// Flush logo to scrollback on first input
			var logoPrintCmd tea.Cmd
			if !m.logoShown {
				m.logoShown = true
				logoPrintCmd = printfOnce("%s", renderLogo(m.cfg.Version))
			}

			// Print user message to terminal scrollback immediately
			userPrintCmd := printfWithClear("%s", renderUserMessage(message{role: "user", content: input}))

			// Combine logo flush + user message if needed
			if logoPrintCmd != nil {
				userPrintCmd = tea.Sequence(logoPrintCmd, userPrintCmd)
			}

			// Start streaming
			m.isGenerating = true
			m.genDotOn = true
			m.genStartTime = time.Now()
			m.genPhase = phaseWaiting
			m.promptTokens = 0
			m.outputTokens = 0
			m.cacheHitTokens = 0
			m.cacheMissTokens = 0
			m.reasoningTokens = 0
			m.genSummary = ""
			m.current = &streamState{
				toolExecMap: make(map[string]*toolExecInfo),
			}
			m.messages = append(m.messages, message{role: "agent"})

			// Launch AskStream in goroutine
			cmd = m.startStream(input)
			return m, tea.Sequence(userPrintCmd, cmd, dotCmd())

		case "up":
			if m.confirmState != nil {
				if m.confirmState.selected > 0 {
					m.confirmState.selected--
				}
				return m, nil
			}
			m.navHistory(-1)
			return m, nil

		case "down":
			if m.confirmState != nil {
				if m.confirmState.selected < len(m.confirmState.options)-1 {
					m.confirmState.selected++
				}
				return m, nil
			}
			m.navHistory(1)
			return m, nil
		}

	case streamStartMsg:
		if msg.err != nil {
			m.errMsg = summarizeError(msg.err)
			msg.cancel()
			m.resetGenState()
			return m, tea.Sequence(m.flushCompletedTurn(), tea.Tick(5*time.Second, func(t time.Time) tea.Msg { return clearErrMsg{} }))
		}
		m.streamCancel = msg.cancel
		// Start consuming events
		return m, waitForAgentEvent(msg.evCh, msg.cancel)

	case agentEventMsg:
		m.handleAgentEvent(msg.event)
		// Flush completed timeline entries to scrollback if content exceeds budget
		flushCmd := m.maybeFlushIncremental()
		if flushCmd != nil {
			return m, tea.Sequence(flushCmd, waitForAgentEvent(msg.evCh, msg.cancel))
		}
		return m, waitForAgentEvent(msg.evCh, msg.cancel)

	case streamDoneMsg:
		elapsed := formatDuration(time.Since(m.genStartTime))
		pt, ot := m.promptTokens, m.outputTokens
		ch, cm, rt := m.cacheHitTokens, m.cacheMissTokens, m.reasoningTokens
		flushCmd := m.flushCompletedTurn()
		m.resetGenState()
		var tokenParts []string
		if pt > 0 {
			tokenParts = append(tokenParts, fmt.Sprintf("↓ %s", formatTokenCount(pt)))
		}
		if ot > 0 {
			tokenParts = append(tokenParts, fmt.Sprintf("↑ %s", formatTokenCount(ot)))
		}
		if ch > 0 || cm > 0 {
			tokenParts = append(tokenParts, fmt.Sprintf("cache %s/%s", formatTokenCount(ch), formatTokenCount(cm)))
		}
		if rt > 0 {
			tokenParts = append(tokenParts, fmt.Sprintf("think %s", formatTokenCount(rt)))
		}
		if len(tokenParts) > 0 {
			m.genSummary = fmt.Sprintf("✓ %s · %s", elapsed, strings.Join(tokenParts, " · "))
		} else {
			m.genSummary = fmt.Sprintf("✓ %s", elapsed)
		}
		if flushCmd != nil {
			return m, tea.Sequence(flushCmd, tea.Tick(6*time.Second, func(t time.Time) tea.Msg { return clearSummaryMsg{} }))
		}
		return m, tea.Tick(6*time.Second, func(t time.Time) tea.Msg { return clearSummaryMsg{} })

	case dotMsg:
		if m.isGenerating {
			m.genDotOn = !m.genDotOn
			return m, dotCmd()
		}
		return m, nil

	case clearCancelMsg:
		m.cancelReason = ""
		return m, nil

	case clearErrMsg:
		m.errMsg = ""
		return m, nil

	case clearSummaryMsg:
		m.genSummary = ""
		return m, nil

	case confirmResultMsg:
		if m.confirmState != nil {
			if err := m.sess.Agent.Confirm(msg.callID, msg.choice); err != nil {
				errText := agentStyle.Render("Solo:") + "\n" + errorStyle.Render("✗ confirm error: "+summarizeError(err)) + "\n\n"
				m.confirmState = nil
				return m, printfWithClear("%s", errText)
			}
			m.confirmState = nil
		}
		return m, nil

	case resetQuitMsg:
		m.quitCount = 0
		return m, nil

	case hintRotateMsg:
		m.hintIndex = (m.hintIndex + 1) % len(commandHints)
		return m, hintRotateCmd()
	}

	// Pass through to textinput when not in confirm mode and not generating
	if m.confirmState == nil {
		m.textInput, cmd = m.textInput.Update(msg)
	}
	return m, cmd
}

// ─── Error summarization ─────────────────────────────────────────────────────

func summarizeError(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	switch {
	case strings.Contains(s, "no such host"):
		return "Network error: cannot resolve host"
	case strings.Contains(s, "connection refused"):
		return "Network error: connection refused"
	case strings.Contains(s, "timeout") || strings.Contains(s, "deadline exceeded"):
		return "Network error: request timed out"
	case strings.Contains(s, "TLS handshake") || strings.Contains(s, "certificate"):
		return "Network error: TLS failure"
	case strings.Contains(s, "connection reset") || strings.Contains(s, "broken pipe"):
		return "Network error: connection lost"
	case strings.Contains(s, "429"):
		return "Rate limited: too many requests"
	case strings.Contains(s, "401") || strings.Contains(s, "Unauthorized"):
		return "Auth error: invalid API key"
	case strings.Contains(s, "403") || strings.Contains(s, "Forbidden"):
		return "Auth error: access denied"
	case strings.Contains(s, "500") || strings.Contains(s, "502") || strings.Contains(s, "503"):
		return "Server error: service unavailable"
	default:
		if len(s) > 80 {
			return s[:80] + "..."
		}
		return s
	}
}

// ─── Message rendering helpers ────────────────────────────────────────────────

// renderUserMessage renders a single user message as a string.
func renderUserMessage(msg message) string {
	return userStyle.Render("❯ ") + msg.content + "\n\n"
}

// renderContent renders agent content text using glamour for rich markdown
// rendering with syntax highlighting and word wrap. Falls back to plain
// contentStyle if the renderer is unavailable or rendering fails.
func (m *model) renderContent(text string) string {
	if m.renderer == nil {
		return contentStyle.Render(text)
	}
	rendered, err := m.renderer.Render(text)
	if err != nil {
		return contentStyle.Render(text)
	}
	return strings.Trim(rendered, "\n")
}

// renderAgentMessage renders a single agent message as a string.
// It prepends the "Solo:" label and delegates the body rendering.
func (m *model) renderAgentMessage(msg message) string {
	return agentStyle.Render("Solo:") + "\n" + m.renderAgentMessageBody(msg)
}

// renderAgentMessageBody renders the body of an agent message (timeline entries
// and remaining content) without the "Solo:" prefix. Timeline entries (thinking
// + tool calls) are rendered in chronological order. Thinking blocks are always
// expanded; tool calls are shown in compact collapsed form.
func (m *model) renderAgentMessageBody(msg message) string {
	var sb strings.Builder

	var lastKind timelineKind = -1
	for _, entry := range msg.timeline {
		// Blank line separator between different block types
		if lastKind >= 0 && lastKind != entry.kind {
			sb.WriteString("\n")
		}
		// Blank line separator between consecutive tool blocks
		if lastKind == timelineTool && entry.kind == timelineTool {
			sb.WriteString("\n")
		}

		switch entry.kind {
		case timelineThinking:
			sb.WriteString(thinkLabelStyle + "\n")
			sb.WriteString(thinkStyle.Render(entry.text) + "\n")
		case timelineContent:
			sb.WriteString(m.renderContent(entry.text) + "\n")
		case timelineTool:
			if entry.tool != nil {
				sb.WriteString(toolLabelStyle + "\n")
				sb.WriteString(toolCollapsedStyle.Render(formatToolBlock(*entry.tool)) + "\n")
			}
		}
		lastKind = entry.kind
	}

	// Remaining content not yet flushed into timeline (during active streaming)
	if msg.content != "" {
		if lastKind >= 0 && lastKind != timelineContent {
			sb.WriteString("\n")
		}
		sb.WriteString(m.renderContent(msg.content) + "\n")
	}

	return sb.String()
}

// ─── View ─────────────────────────────────────────────────────────────────────

func (m model) View() tea.View {
	var sb strings.Builder

	// 1. Logo (shown in View until flushed to scrollback on first input)
	if !m.logoShown {
		sb.WriteString(renderLogo(m.cfg.Version))
	}

	// Dynamic zone: only render current interactive content.
	// Completed history has been flushed to scrollback via tea.Printf.

	// 2. Active agent message (only unflushed portion)
	agentMsg := m.currentMessage()
	if agentMsg != nil {
		var liveTimeline []timelineEntry
		var liveContent string

		if m.current != nil {
			// Only render timeline entries that haven't been flushed to scrollback
			if m.current.flushedIdx < len(m.current.timeline) {
				liveTimeline = m.current.timeline[m.current.flushedIdx:]
			}
			// Append in-progress thinking as a virtual entry
			if m.current.thinkingBuf.Len() > 0 {
				liveTimeline = append(liveTimeline, timelineEntry{
					kind: timelineThinking,
					text: m.current.thinkingBuf.String(),
				})
			}
			liveContent = m.current.content.String()
		} else {
			liveTimeline = agentMsg.timeline
			liveContent = agentMsg.content
		}

		if len(liveTimeline) > 0 || liveContent != "" {
			// "Solo:" label: only show if not already flushed to scrollback
			if m.current == nil || !m.current.labelFlushed {
				sb.WriteString(agentStyle.Render("Solo:") + "\n")
			}
			displayMsg := message{role: "agent", timeline: liveTimeline, content: liveContent}
			sb.WriteString(m.renderAgentMessageBody(displayMsg))
		}
	}

	// 3. Confirm dialog (if active)
	if m.confirmState != nil {
		sb.WriteString("\n")
		sb.WriteString(lipgloss.NewStyle().Foreground(colorWarning).Bold(true).Render(m.confirmState.prompt) + "\n")
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
	var statusText string
	var hasStatus bool
	if m.quitCount > 0 {
		statusText = foldedStyle.Render("✗ Confirm exit (Press Ctrl+C again)")
		hasStatus = true
	} else if m.errMsg != "" {
		statusText = foldedStyle.Render(fmt.Sprintf("✗ %s", m.errMsg))
		hasStatus = true
	} else if m.cancelReason != "" {
		statusText = foldedStyle.Render(fmt.Sprintf("✗ Cancelled (%s)", m.cancelReason))
		hasStatus = true
	} else if m.genSummary != "" {
		statusText = foldedStyle.Render(m.genSummary)
		hasStatus = true
	} else if m.isGenerating {
		dot := "●"
		if !m.genDotOn {
			dot = "○"
		}
		elapsed := formatDuration(time.Since(m.genStartTime))
		phase := m.genPhase.String()
		statusText = foldedStyle.Render(fmt.Sprintf("%s %s · %s · esc to interrupt", dot, elapsed, phase))
		hasStatus = true
	}
	if hasStatus {
		sb.WriteString(statusText + "\n")
	}

	// 5. Divider (above input)
	divider := lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Render(strings.Repeat("-", max(m.width, 0)))
	sb.WriteString(divider + "\n")

	// 6. Input box
	sb.WriteString(m.textInput.View() + "\n")

	// 7. Divider (below input)
	sb.WriteString(divider + "\n")

	// 8. Hint line: left=rotating command hints, right=context token usage
	leftHint := hintStyle.Render(commandHints[m.hintIndex])

	var pct int
	if m.sess != nil {
		current, max, _ := m.sess.ContextWindow().TokenUsage()
		if max > 0 {
			pct = current * 100 / max
		}
	}
	rightHint := contextTokenStyle(pct).Render(fmt.Sprintf("Context Tokens: %d%%", pct))

	separator := dimStyle.Render(" · ")
	availWidth := m.width - lipgloss.Width(leftHint) - lipgloss.Width(separator) - lipgloss.Width(rightHint)
	padding := availWidth
	if padding < 1 {
		padding = 1
	}
	sb.WriteString(leftHint + strings.Repeat(" ", padding) + separator + rightHint)

	return tea.NewView(sb.String())
}

// dynamicHeightBudget returns the maximum number of lines available
// for dynamic content in View(). It reserves lines for the status bar,
// input box, and hint line.
func (m model) dynamicHeightBudget() int {
	reserved := 4 // input + hint + 2 dividers
	if m.quitCount > 0 || m.errMsg != "" || m.cancelReason != "" || m.genSummary != "" || m.isGenerating {
		reserved++ // status bar is visible
	}
	if m.height <= reserved {
		return 1
	}
	return m.height - reserved
}

// maybeFlushIncremental checks if the unflushed timeline entries exceed the
// dynamic height budget. If so, it flushes the oldest entries to scrollback
// via tea.Printf, keeping only the recent portion in View().
func (m *model) maybeFlushIncremental() tea.Cmd {
	if m.current == nil || m.current.flushedIdx >= len(m.current.timeline) {
		return nil
	}

	// Measure the unflushed portion's rendered size
	unflushed := m.current.timeline[m.current.flushedIdx:]
	var liveTimeline []timelineEntry
	liveTimeline = append(liveTimeline, unflushed...)
	if m.current.thinkingBuf.Len() > 0 {
		liveTimeline = append(liveTimeline, timelineEntry{
			kind: timelineThinking,
			text: m.current.thinkingBuf.String(),
		})
	}
	displayMsg := message{role: "agent", timeline: liveTimeline, content: m.current.content.String()}
	rendered := m.renderAgentMessageBody(displayMsg)
	lineCount := strings.Count(rendered, "\n")

	budget := m.dynamicHeightBudget()
	if lineCount <= budget {
		return nil // Content fits within budget, no flush needed
	}

	// Determine how many entries to flush.
	// Strategy: flush everything up to (but not including) the last content/thinking entry,
	// so View() always shows at least the current content block.
	flushUpTo := m.current.flushedIdx
	for i := len(m.current.timeline) - 1; i >= m.current.flushedIdx; i-- {
		kind := m.current.timeline[i].kind
		if kind == timelineContent || kind == timelineThinking {
			flushUpTo = i
			break
		}
	}
	if flushUpTo <= m.current.flushedIdx {
		flushUpTo = m.current.flushedIdx + 1 // Flush at least one entry
	}

	// Render the entries being flushed
	flushMsg := message{role: "agent", timeline: m.current.timeline[m.current.flushedIdx:flushUpTo]}
	var sb strings.Builder
	if !m.current.labelFlushed {
		sb.WriteString(agentStyle.Render("Solo:") + "\n")
		m.current.labelFlushed = true
	}
	sb.WriteString(m.renderAgentMessageBody(flushMsg))

	m.current.flushedIdx = flushUpTo
	return printfWithClear("%s", sb.String())
}

// formatDuration formats elapsed time for the status bar.
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
}

// formatTokenCount formats token counts for display.
// Under 1000: "386". 1000+: "1.2k", "10.5k", "100k".
func formatTokenCount(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000)
}

// resetGenState resets all generation-related state fields.
func (m *model) resetGenState() {
	m.isGenerating = false
	m.genStartTime = time.Time{}
	m.genPhase = phaseWaiting
	m.promptTokens = 0
	m.outputTokens = 0
	m.cacheHitTokens = 0
	m.cacheMissTokens = 0
	m.reasoningTokens = 0
}

// ─── Command hints ─────────────────────────────────────────────────────────────

var commandHints = []string{
	"· ctrl+c×2 quit",
	"· esc interrupt",
	"· ↑↓ history",
	"· /help commands",
	"· /clear context",
	"· /quit exit",
}

func hintRotateCmd() tea.Cmd {
	return tea.Tick(10*time.Second, func(t time.Time) tea.Msg { return hintRotateMsg{} })
}

// ─── Stream ───────────────────────────────────────────────────────────────────

func dotCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg { return dotMsg{} })
}

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
	// Flush any remaining thinking and content into timeline
	m.current.flushContent()
	m.current.flushThinking()
	msg.timeline = make([]timelineEntry, len(m.current.timeline))
	copy(msg.timeline, m.current.timeline)
	msg.content = ""

	if m.streamCancel != nil {
		m.streamCancel()
		m.streamCancel = nil
	}
	m.current = nil
}

// flushCompletedTurn renders any unflushed portion of the current (or just-completed)
// agent message to scrollback. User messages are already printed on entry.
// It calls finalizeCurrentStream internally.
func (m *model) flushCompletedTurn() tea.Cmd {
	// Save flush state before finalizeCurrentStream clears m.current
	savedFlushedIdx := 0
	labelAlreadyFlushed := false
	if m.current != nil {
		savedFlushedIdx = m.current.flushedIdx
		labelAlreadyFlushed = m.current.labelFlushed
	}

	m.finalizeCurrentStream()

	agentMsg := m.currentMessage()
	if agentMsg == nil {
		return nil
	}

	// Skip if agent message is empty (e.g. cancelled before any output)
	if len(agentMsg.timeline) == 0 && agentMsg.content == "" {
		m.messages = nil
		return nil
	}

	var sb strings.Builder

	if savedFlushedIdx < len(agentMsg.timeline) {
		// There are unflushed timeline entries
		if !labelAlreadyFlushed {
			sb.WriteString(agentStyle.Render("Solo:") + "\n")
		}
		remainingMsg := message{role: "agent", timeline: agentMsg.timeline[savedFlushedIdx:]}
		sb.WriteString(m.renderAgentMessageBody(remainingMsg))
	} else if !labelAlreadyFlushed {
		// Nothing was incrementally flushed — render the complete message
		sb.WriteString(m.renderAgentMessage(*agentMsg))
	}

	m.messages = nil

	if sb.Len() == 0 {
		return nil
	}
	return printfWithClear("%s", sb.String())
}

// ─── History ──────────────────────────────────────────────────────────────────

func (m *model) addHistory(line string) {
	if line == "" || (len(m.history) > 0 && m.history[len(m.history)-1] == line) {
		return
	}
	m.history = append(m.history, line)
	m.historyIdx = 0
	m.historyDraft = ""
	appendHistory(line)
}

// navHistory navigates the input history. dir=-1 = older (up), dir=1 = newer (down).
func (m *model) navHistory(dir int) {
	if len(m.history) == 0 || m.isGenerating || m.confirmState != nil {
		return
	}

	// Save current input as draft when starting history browsing
	if m.historyIdx == 0 && dir < 0 {
		m.historyDraft = m.textInput.Value()
	}

	newIdx := m.historyIdx - dir // up(-1) increases idx, down(1) decreases
	if newIdx < 0 {
		newIdx = 0
	}
	if newIdx > len(m.history) {
		newIdx = len(m.history)
	}

	// Already at boundary
	if newIdx == m.historyIdx {
		return
	}

	m.historyIdx = newIdx

	if m.historyIdx == 0 {
		// Back to present — restore draft
		m.textInput.SetValue(m.historyDraft)
	} else {
		// Show historical entry (from end)
		m.textInput.SetValue(m.history[len(m.history)-m.historyIdx])
	}
	m.textInput.CursorEnd()
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

	// Gradient: violet → pink
	startR, startG, startB := uint8(167), uint8(139), uint8(250)
	endR, endG, endB := uint8(244), uint8(114), uint8(182)

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

// ─── History persistence ─────────────────────────────────────────────────────

const maxHistory = 20

// historyFile returns the path to the history file (~/.soloqueue/history).
func historyFile() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = "/tmp"
	}
	return filepath.Join(home, ".soloqueue", "history")
}

// loadHistory reads history from ~/.soloqueue/history and returns a slice of
// historical input strings. Silently ignores read errors (file not found, etc.)
func loadHistory() []string {
	path := historyFile()
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var history []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\n\r")
		if line != "" {
			history = append(history, line)
		}
	}
	return history
}

// appendHistory appends a new entry to the history file. If the last entry is
// identical, it is not appended. The file is truncated to maxHistory entries.
func appendHistory(entry string) {
	if entry == "" {
		return
	}

	// Ensure directory exists
	dir := filepath.Dir(historyFile())
	if err := os.MkdirAll(dir, 0755); err != nil {
		return
	}

	// Read existing entries
	var history []string
	f, err := os.OpenFile(historyFile(), os.O_RDONLY, 0644)
	if err == nil {
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimRight(scanner.Text(), "\n\r")
			if line != "" {
				history = append(history, line)
			}
		}
		f.Close()
	}

	// Deduplicate adjacent identical entries
	if len(history) > 0 && history[len(history)-1] == entry {
		return
	}

	// Append new entry
	history = append(history, entry)

	// Truncate to maxHistory
	if len(history) > maxHistory {
		history = history[len(history)-maxHistory:]
	}

	// Write back
	file, err := os.OpenFile(historyFile(), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return
	}
	defer file.Close()
	writer := bufio.NewWriter(file)
	for _, h := range history {
		fmt.Fprintln(writer, h)
	}
	writer.Flush()
}
