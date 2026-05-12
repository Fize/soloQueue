package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	"github.com/muesli/termenv"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/session"
	"github.com/xiaobaitu/soloqueue/internal/skill"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/timeline"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// ─── Config ─────────────────────────────────

// SandboxInitMsg delivers the result of async sandbox + session initialization
// to the TUI. If Err is non-nil, Sess is nil and the TUI displays the error.
// If both are nil, initialization succeeded but produced no session.
type SandboxInitMsg struct {
	Sess *session.Session
	Err  error
}

// moreHistoryMsg delivers older history loaded from the timeline in response
// to a scroll-to-top event.
type moreHistoryMsg struct {
	messages []message
	cursor   *time.Time // next page cursor, nil if no more
}

type Config struct {
	Session       *session.Session
	ModelID       string
	Version       string
	RulesCreated  bool
	RulesPath     string
	Registry      *agent.Registry
	SupervisorsFn func() []*agent.Supervisor
	Skills        *skill.SkillRegistry
	SandboxInitCh <-chan SandboxInitMsg // async sandbox + session init channel
	NotifyCh      <-chan string         // background task notifications

	// HTTPServerAddr is the address of the embedded HTTP API server.
	HTTPServerAddr string

	// RuntimeMetrics is the shared metrics struct that the TUI writes
	// and the HTTP API (/api/runtime) reads. May be nil.
	RuntimeMetrics RuntimeMetricsWriter

	// ContextIdleThresholdMin is the idle timeout (minutes) for auto-clearing context.
	// Read from config at startup.
	ContextIdleThresholdMin int

	// TimelineDir is the path to timeline JSONL files for history replay.
	TimelineDir string
}

// RuntimeMetricsWriter is the interface that the TUI uses to push runtime
// metrics to the HTTP API layer. Satisfied by *server.RuntimeMetrics.
type RuntimeMetricsWriter interface {
	SetPhase(phase string)
	SetTokens(prompt, output, cacheHit, cacheMiss int64)
	SetContext(pct int)
	SetIter(iter int)
	SetContentDeltas(n int)
	SetActiveDelegations(n int)
}

// ─── Data types ──────────────────────────────

type genPhase int

const (
	phaseWaiting genPhase = iota
	phaseThinking
	phaseGenerating
	phaseToolCall
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

type timelineKind int

const (
	timelineThinking timelineKind = iota
	timelineContent
	timelineTool
)

type timelineEntry struct {
	kind timelineKind
	text string
	tool *toolBlock
}

type message struct {
	role      string
	content   string
	timeline  []timelineEntry
	dirty     bool      // true if content changed since last render
	rendered  string    // cached rendered output
	timestamp time.Time // when the message was created
	isHistory bool      // true if message is from replayed history (context cleared)
}

type confirmState struct {
	callID   string
	prompt   string
	options  []string
	selected int
}

type streamState struct {
	timeline      []timelineEntry
	content       strings.Builder
	toolExecMap   map[string]*toolExecInfo
	curToolCallID string
	curToolName   string
	curToolArgs   strings.Builder
	thinkingBuf   strings.Builder
}

// ─── Bubble Tea messages ──────────────────────

// systemNotifyMsg carries a background task notification to the TUI.
type systemNotifyMsg struct {
	message string
}

type postSandboxInitMsg struct {
	messages       []message
	contextCleared bool
}

type agentEventMsg struct {
	event    agent.AgentEvent
	evCh     <-chan agent.AgentEvent
	cancel   context.CancelFunc
	streamID int
}

type streamStartMsg struct {
	evCh     <-chan agent.AgentEvent
	cancel   context.CancelFunc
	err      error
	streamID int
}

type streamDoneMsg struct {
	streamID int
}

type confirmResultMsg struct {
	callID string
	choice string
}

type resetQuitMsg struct{}
type clearCancelMsg struct{}
type clearErrMsg struct{}
type clearSummaryMsg struct{}

// ─── Model ─────────────────────────────────────

type model struct {
	cfg  Config
	sess *session.Session
	ctx  context.Context

	viewport     viewport.Model
	textArea     textarea.Model
	isGenerating bool
	cancelReason string
	errMsg       string
	spinner      spinner
	quitCount    int
	width        int
	height       int
	renderer     *glamour.TermRenderer
	darkBg       bool

	genStartTime      time.Time
	genPhase          genPhase
	promptTokens      int
	outputTokens      int
	cacheHitTokens    int
	cacheMissTokens   int
	reasoningTokens   int
	genSummary        string
	activeDelegations int // current number of in-flight async delegations
	currentIter       int // current tool-use iteration (from IterationDoneEvent)
	contentDeltas     int // diagnostic: number of ContentDeltaEvents received this turn

	messages     []message
	current      *streamState
	streamCancel context.CancelFunc
	nextStreamID int

	history      []string
	historyIdx   int
	historyDraft string

	focus          focusMode
	copyMode       bool
	confirmQueue    []confirmState // FIFO queue of pending tool confirmations
	loading         bool   // true while waiting for sandbox + session init
	sandboxErr      string // sandbox init error message
	contextCleared  bool   // true if context was silently cleared on startup
	historyCursor   *time.Time // cursor for loading older history; nil = no more
	historyLoading  bool       // true while a loadMoreHistoryCmd is in flight
	runtimeMetrics RuntimeMetricsWriter // shared metrics writer (may be nil)
}

// ─── Run (public entry point) ────────────────

func Run(cfg Config) error {
	ctx := context.Background()

	m := model{
		cfg:            cfg,
		ctx:            ctx,
		messages:       []message{},
		history:        loadHistory(),
		spinner:        newSpinner(),
		focus:          focusComposer,
		runtimeMetrics: cfg.RuntimeMetrics,
	}

	m.darkBg = termenv.HasDarkBackground()
	applyTheme(m.darkBg)

	vp := viewport.New(viewport.WithWidth(80), viewport.WithHeight(20))
	vp.SoftWrap = true
	m.viewport = vp

	ta := textarea.New()
	ta.Placeholder = "Type a message..."
	ta.SetVirtualCursor(false)
	ta.Focus()
	ta.Prompt = "┃ "
	ta.CharLimit = 0
	ta.DynamicHeight = true
	ta.MinHeight = 5
	ta.MaxHeight = maxComposerLines
	ta.ShowLineNumbers = false
	// Rebind InsertNewline to shift+enter only.
	// Plain enter is intercepted by Update() and used as submit.
	ta.KeyMap.InsertNewline.SetKeys("ctrl+j")
	s := ta.Styles()
	s.Focused.CursorLine = lipgloss.NewStyle()
	ta.SetStyles(s)
	m.textArea = ta

	m.sess = cfg.Session
	m.renderer = m.newRenderer()

	// If sandbox init is deferred, start in loading state.
	if cfg.SandboxInitCh != nil {
		m.loading = true
		ta.Placeholder = "Sandbox initializing..."
	}

	p := tea.NewProgram(m)

	if cfg.NotifyCh != nil {
		go func() {
			defer func() {
				_ = recover() // prevent TUI notification relay from crashing the process
			}()
			for msg := range cfg.NotifyCh {
				p.Send(systemNotifyMsg{message: msg})
			}
		}()
	}
	_, runErr := p.Run()

	return runErr
}

// ─── Init ─────────────────────────────────────

func (m model) Init() tea.Cmd {
	cmds := []tea.Cmd{textarea.Blink}
	if m.loading && m.cfg.SandboxInitCh != nil {
		cmds = append(cmds, waitForSandboxInit(m.cfg.SandboxInitCh))
	}
	if m.cfg.RulesCreated && m.cfg.RulesPath != "" {
		notice := fmt.Sprintf("rules.md not found — default created at %s. Feel free to edit it anytime.", m.cfg.RulesPath)
		m.messages = append(m.messages, message{role: "agent", content: notice})
	}
	return tea.Batch(cmds...)
}

// ─── Layout sizing ─────────────────────────────

func (m *model) resizeViewport() {
	ly := m.computeLayout()
	// SetWidth triggers DynamicHeight recalculation, so call it first.
	m.textArea.SetWidth(ly.composerW)
	// Now read the auto-calculated height and recompute layout with it.
	composerH := m.textArea.Height() // textarea lines only, no title
	bodyH := m.height - ly.headerH - composerH - ly.footerH
	if bodyH < minBodyHeight {
		bodyH = minBodyHeight
	}
	viewportH := bodyH // no title line subtracted anymore
	if viewportH < 3 {
		viewportH = 3
	}
	m.viewport.SetHeight(viewportH)
	// Viewport sits inside paneStyle which is Width(mainW-2).Padding(0,1).
	// In lipgloss v2, Width includes padding, so the content area is
	// (mainW-2) - 2 = mainW-4. Viewport lines must fit within this
	// content area or lipgloss will wrap them, adding extra lines.
	m.viewport.SetWidth(max(ly.mainW-4, 1))
}

// ─── rebuildViewportContent ───────────────────

// rebuildViewportContent reconstructs viewport content. Historical messages
// are cached — only dirty (newly changed) messages are re-rendered.
// The live streaming message is always re-rendered since it changes every event.
func (m *model) rebuildViewportContent() {
	var sb strings.Builder

	// When streaming, the last agent message in m.messages is a placeholder
	// whose content is being built via m.current. Skip rendering it here
	// to avoid duplicate "Solo:" headers — the live block below handles it.
	skipLast := m.current != nil && len(m.messages) > 0 &&
		m.messages[len(m.messages)-1].role == "agent"

	for i := range m.messages {
		if skipLast && i == len(m.messages)-1 {
			continue
		}
		msg := &m.messages[i]
		if msg.dirty || msg.rendered == "" {
			msg.rendered = m.renderMessage(*msg)
			msg.dirty = false
		}
		sb.WriteString(msg.rendered)
	}

	if m.current != nil {
		ts := formatTimestamp("Solo", time.Now())
		sb.WriteString(timestampStyle.Render(ts) + "\n")
		var liveTimeline []timelineEntry
		liveTimeline = append(liveTimeline, m.current.timeline...)
		if m.current.thinkingBuf.Len() > 0 {
			liveTimeline = append(liveTimeline, timelineEntry{
				kind: timelineThinking,
				text: m.current.thinkingBuf.String(),
			})
		}
		liveContent := m.current.content.String()
		displayMsg := message{role: "agent", timeline: liveTimeline, content: liveContent}
		sb.WriteString(m.renderAgentMessageBody(displayMsg))
	}

	m.viewport.SetContent(sb.String())
}

// ─── Update ─────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeViewport()
		m.renderer = m.newRenderer()
		m.invalidateMessageCache()
		m.rebuildViewportContent()
		m.viewport.GotoBottom()
		return m, nil

	case SandboxInitMsg:
		m.loading = false
		if msg.Err != nil {
			m.sandboxErr = summarizeError(msg.Err)
			m.textArea.Placeholder = "Sandbox failed — /quit to exit"
			m.resizeViewport()
			m.rebuildViewportContent()
			return m, nil
		}
		m.sess = msg.Sess
		m.sandboxErr = ""
		m.textArea.Placeholder = "Type a message..."
		m.resizeViewport()
		return m, postSandboxInitCmd(m.sess, m.cfg.ContextIdleThresholdMin)

	case postSandboxInitMsg:
		m.contextCleared = msg.contextCleared
		if len(msg.messages) > 0 {
			m.messages = append(m.messages, msg.messages...)
			m.rebuildViewportContent()
			m.viewport.GotoBottom()
		}
		return m, nil

	case tea.KeyPressMsg:
		if msg.String() != "ctrl+c" && m.quitCount > 0 {
			m.quitCount = 0
		}

		switch msg.String() {
		case "ctrl+y":
			m.copyMode = true
			m.focus = focusCopy
			return m, nil

		case "ctrl+c":
			if m.cancelCurrent() {
				return m, tea.Tick(3*time.Second, func(t time.Time) tea.Msg { return clearCancelMsg{} })
			}
			m.quitCount++
			if m.quitCount >= 2 {
				return m, tea.Quit
			}
			return m, tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return resetQuitMsg{} })

		case "esc":
			if m.copyMode {
				m.copyMode = false
				m.focus = focusComposer
				return m, nil
			}
			if m.cancelCurrent() {
				return m, tea.Tick(3*time.Second, func(t time.Time) tea.Msg { return clearCancelMsg{} })
			}

		case "enter":
			if len(m.confirmQueue) > 0 {
				return m.handleConfirmEnter()
			}
			if m.loading || m.isGenerating || strings.TrimSpace(m.textArea.Value()) == "" {
				return m, nil
			}
			input := strings.TrimSpace(m.textArea.Value())
			m.textArea.Reset()

			if isSlashCommandInput(input) {
				quit, builtinCmd, handled := m.handleBuiltin(input)
				if quit {
					if builtinCmd != nil {
						return m, tea.Sequence(builtinCmd, tea.Quit)
					}
					return m, tea.Quit
				}
				if handled {
					if builtinCmd != nil {
						return m, builtinCmd
					}
					return m, nil
				}
				// Unrecognized slash command: fall through to LLM stream.
			}

			m.messages = append(m.messages, message{role: "user", content: input, timestamp: time.Now()})
			m.addHistory(input)

			m.nextStreamID++
			sid := m.nextStreamID
			m.isGenerating = true
			m.spinner = newSpinner()
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
			m.messages = append(m.messages, message{role: "agent", timestamp: time.Now()})
			m.syncRuntimeMetrics()
			m.resizeViewport()
			m.rebuildViewportContent()
			m.viewport.GotoBottom()
			cmd = m.startStream(input, sid)
			return m, tea.Sequence(cmd, spinnerCmd())

		case "up":
			if len(m.confirmQueue) > 0 {
				if m.confirmQueue[0].selected > 0 {
					m.confirmQueue[0].selected--
				}
				return m, nil
			}
			if m.textArea.Value() == "" || m.historyIdx > 0 {
				m.navHistory(-1)
				return m, nil
			}
			m.textArea, cmd = m.textArea.Update(msg)
			return m, cmd

		case "down":
			if len(m.confirmQueue) > 0 {
				if m.confirmQueue[0].selected < len(m.confirmQueue[0].options)-1 {
					m.confirmQueue[0].selected++
				}
				return m, nil
			}
			if m.textArea.Value() == "" || m.historyIdx > 0 {
				m.navHistory(1)
				return m, nil
			}
			m.textArea, cmd = m.textArea.Update(msg)
			return m, cmd
		}

	case tea.PasteMsg:
		if len(m.confirmQueue) == 0 {
			m.textArea, cmd = m.textArea.Update(msg)
			newH := visualLineCount(m.textArea)
			if newH != m.textArea.Height() {
				m.resizeViewport()
				m.rebuildViewportContent()
				m.viewport.GotoBottom()
			}
			return m, cmd
		}

	case tea.MouseWheelMsg:
		var c tea.Cmd
		m.viewport, c = m.viewport.Update(msg)
		return m, c

	case streamStartMsg:
		if msg.err != nil {
			m.errMsg = summarizeError(msg.err)
			msg.cancel()
			m.resetGenState()
			m.current = nil
			return m, tea.Tick(5*time.Second, func(t time.Time) tea.Msg { return clearErrMsg{} })
		}
		m.streamCancel = msg.cancel
		return m, waitForAgentEvent(msg.evCh, msg.cancel, msg.streamID)

	case agentEventMsg:
		m.handleAgentEvent(msg.event)
		if _, ok := msg.event.(agent.DelegationStartedEvent); ok {
			m.isGenerating = false
		}
		wasAtBottom := m.viewport.AtBottom()
		m.rebuildViewportContent()
		if wasAtBottom {
			m.viewport.GotoBottom()
		}
		return m, waitForAgentEvent(msg.evCh, msg.cancel, msg.streamID)

	case streamDoneMsg:
		if msg.streamID == m.nextStreamID || !m.isGenerating {
			elapsed := formatDuration(time.Since(m.genStartTime))
			pt, ot := m.promptTokens, m.outputTokens
			ch, cm, rt := m.cacheHitTokens, m.cacheMissTokens, m.reasoningTokens
			m.finalizeCurrentStream()
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
			m.rebuildViewportContent()
			m.resizeViewport()
			m.viewport.GotoBottom()
			return m, tea.Tick(6*time.Second, func(t time.Time) tea.Msg { return clearSummaryMsg{} })
		}
		m.finalizeCurrentStream()
		m.rebuildViewportContent()
		m.viewport.GotoBottom()
		return m, nil

	case spinnerMsg:
		if m.isGenerating || m.loading {
			if !m.copyMode {
				m.spinner.Next()
			}
			return m, spinnerCmd()
		}
		return m, nil

	case clearCancelMsg:
		m.cancelReason = ""
		m.resizeViewport()
		return m, nil

	case clearErrMsg:
		m.errMsg = ""
		m.resizeViewport()
		return m, nil

	case clearSummaryMsg:
		m.genSummary = ""
		m.resizeViewport()
		m.rebuildViewportContent()
		return m, nil

	case confirmResultMsg:
		if m.sess == nil {
			return m, nil
		}
		if err := m.sess.Agent.Confirm(msg.callID, msg.choice); err != nil {
			errText := fmt.Sprintf("✗ confirm error: %s", summarizeError(err))
			m.messages = append(m.messages, message{role: "agent", content: errText})
			m.rebuildViewportContent()
			m.viewport.GotoBottom()
			return m, nil
		}
		return m, nil

	case resetQuitMsg:
		m.quitCount = 0
		return m, nil

	case systemNotifyMsg:
		m.messages = append(m.messages, message{role: "system", content: msg.message})
		m.resizeViewport()
		m.rebuildViewportContent()
		m.viewport.GotoBottom()
		return m, nil
	}

	if len(m.confirmQueue) == 0 {
		// Any key not handled above resets history navigation state.
		if _, ok := msg.(tea.KeyPressMsg); ok && m.historyIdx > 0 {
			m.historyIdx = 0
			m.historyDraft = ""
		}
		var c tea.Cmd
		m.textArea, c = m.textArea.Update(msg)
		newH := visualLineCount(m.textArea)
		if newH != m.textArea.Height() {
			m.resizeViewport()
			m.rebuildViewportContent()
			m.viewport.GotoBottom()
		}
		return m, c
	}
	return m, cmd
}

// renderSpacer returns a blank line between sections.
func (m *model) renderSpacer(_ layout) string {
	return ""
}

// ─── View ───────────────────────────────────────

func (m model) View() tea.View {
	ly := m.computeLayout()
	body := m.renderWorkbenchBody(ly)
	header := m.renderHeader(ly)
	composer := m.renderComposer(ly)
	footer := m.renderFooter(ly)

	fullView := lipgloss.JoinVertical(lipgloss.Left, body, m.renderSpacer(ly), header, m.renderSpacer(ly), composer, footer)
	v := tea.NewView(fullView)
	if !m.copyMode {
		c := m.textArea.Cursor()
		if c != nil {
			// Count actual rendered lines above the textarea.
			// body + spacer + header + spacer
			c.Y += lineCount(body) + 1 + lineCount(header) + 1
		}
		v.Cursor = c
		v.MouseMode = tea.MouseModeCellMotion
	} else {
		v.MouseMode = tea.MouseModeNone
	}
	v.AltScreen = true
	return v
}

// lineCount returns the number of visual lines in a rendered string.
func lineCount(s string) int {
	if s == "" {
		return 0
	}
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return len(lines)
}

// ─── Error summarization ───────────────────────

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

// ─── Message rendering helpers (in render.go) ─────

// ─── Formatting helpers (in format.go) ─────

// ─── Generation state ────────────────────────

func (m *model) resetGenState() {
	m.isGenerating = false
	m.genStartTime = time.Time{}
	m.genPhase = phaseWaiting
	m.promptTokens = 0
	m.outputTokens = 0
	m.cacheHitTokens = 0
	m.cacheMissTokens = 0
	m.reasoningTokens = 0
	m.activeDelegations = 0
	m.currentIter = 0
	m.contentDeltas = 0
	m.confirmQueue = nil
	m.syncRuntimeMetrics()
}

// cancelCurrent cancels the most relevant running task in two stages:
// Stage 1: Cancel A1 (session agent) if it's generating.
// Stage 2: Cancel A2/A3 supervisors' running children.
// Returns true if something was cancelled.
func (m *model) cancelCurrent() bool {
	// Stage 1: Cancel A1 if it's generating.
	if m.isGenerating {
		if m.streamCancel != nil {
			m.streamCancel()
		}
		m.cancelReason = "Esc"
		m.resetGenState()
		m.current = nil
		m.resizeViewport()
		m.rebuildViewportContent()
		m.viewport.GotoBottom()
		return true
	}

	// Stage 2: Cancel A2/A3 supervisors with running children.
	if m.cancelAllSupervisors() {
		m.cancelReason = "Esc"
		m.resizeViewport()
		m.rebuildViewportContent()
		return true
	}

	return false
}

// cancelAllSupervisors stops all running children across all supervisors.
// Returns true if at least one child was stopped.
func (m *model) cancelAllSupervisors() bool {
	if m.cfg.SupervisorsFn == nil {
		return false
	}
	supervisors := m.cfg.SupervisorsFn()
	if supervisors == nil {
		return false
	}
	stopped := false
	for _, sv := range supervisors {
		if sv == nil {
			continue
		}
		for _, child := range sv.Children() {
			if child.State() == agent.StateProcessing {
				if err := sv.ReapChild(child.InstanceID, 10*time.Second); err == nil {
					stopped = true
				}
			}
		}
	}
	return stopped
}

// ─── Stream helpers ────────────────────────

func (m model) startStream(prompt string, streamID int) tea.Cmd {
	return func() tea.Msg {
		streamCtx, cancel := context.WithCancel(m.ctx)
		evCh, err := m.sess.AskStream(streamCtx, prompt)
		return streamStartMsg{evCh: evCh, cancel: cancel, err: err, streamID: streamID}
	}
}

func waitForAgentEvent(evCh <-chan agent.AgentEvent, cancel context.CancelFunc, streamID int) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-evCh
		if !ok {
			cancel()
			return streamDoneMsg{streamID: streamID}
		}
		return agentEventMsg{event: ev, evCh: evCh, cancel: cancel, streamID: streamID}
	}
}

func waitForSandboxInit(ch <-chan SandboxInitMsg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return SandboxInitMsg{Err: fmt.Errorf("sandbox init channel closed unexpectedly")}
		}
		return msg
	}
}

func postSandboxInitCmd(sess *session.Session, thresholdMin int) tea.Cmd {
	return func() tea.Msg {

		if sess == nil {
			return postSandboxInitMsg{}
		}

		history := sess.History()
		msgs := loadMessagesFromHistory(history, false)
		return postSandboxInitMsg{messages: msgs, contextCleared: false}
	}
}

func (m *model) currentMessage() *message {
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].role == "agent" {
			return &m.messages[i]
		}
	}
	return nil
}

func (m *model) finalizeCurrentStream() {
	if m.current == nil {
		return
	}
	msg := m.currentMessage()
	if msg == nil {
		return
	}
	m.current.flushContent()
	m.current.flushThinking()
	msg.timeline = make([]timelineEntry, len(m.current.timeline))
	copy(msg.timeline, m.current.timeline)
	msg.content = ""
	msg.dirty = true
	if m.streamCancel != nil {
		m.streamCancel()
		m.streamCancel = nil
	}
	m.current = nil
}

// ─── History (in history.go) ─────

// ─── Confirm handling ──────────────────────

func (m model) handleConfirmEnter() (tea.Model, tea.Cmd) {
	if len(m.confirmQueue) == 0 {
		return m, nil
	}
	cs := m.confirmQueue[0]
	choice := cs.options[cs.selected]
	var agentChoice string
	switch {
	case strings.HasPrefix(choice, "[y]"):
		agentChoice = string(tools.ChoiceApprove)
	case strings.HasPrefix(choice, "[n]"):
		agentChoice = string(tools.ChoiceDeny)
	case strings.HasPrefix(choice, "[a]"):
		agentChoice = string(tools.ChoiceAllowInSession)
	default:
		agentChoice = choice
	}
	// Pop head of queue
	m.confirmQueue = m.confirmQueue[1:]
	return m, func() tea.Msg {
		return confirmResultMsg{callID: cs.callID, choice: agentChoice}
	}
}

// checkScrollTop returns a command to load more history if the viewport
// is at the top, a cursor is available, and no load is in progress.
func (m *model) checkScrollTop() tea.Cmd {
	if m.historyCursor == nil || m.historyLoading {
		return nil
	}
	if !m.viewport.AtTop() {
		return nil
	}
	m.historyLoading = true
	return loadMoreHistoryCmd(m.cfg.TimelineDir, "timeline", 10, *m.historyCursor)
}

// loadMoreHistoryCmd reads older conversation turns from the timeline.
func loadMoreHistoryCmd(dir, baseName string, maxTurns int, before time.Time) tea.Cmd {
	return func() tea.Msg {
		segs, cursor, _ := timeline.ReadTailBefore(dir, baseName, maxTurns, before)
		if len(segs) == 0 {
			return moreHistoryMsg{}
		}
		history := make([]agent.LLMMessage, 0, len(segs[0].Messages))
		for _, m := range segs[0].Messages {
			history = append(history, agent.LLMMessage{
				Role:             m.Role,
				Content:          m.Content,
				ReasoningContent: m.ReasoningContent,
				Name:             m.Name,
				ToolCallID:       m.ToolCallID,
				ToolCalls:        convertToolCalls(m.ToolCalls),
			})
		}
		msgs := loadMessagesFromHistory(history, true)
		return moreHistoryMsg{messages: msgs, cursor: cursor}
	}
}

// convertToolCalls converts timeline ToolCallRec to llm ToolCall.
func convertToolCalls(recs []timeline.ToolCallRec) []llm.ToolCall {
	if len(recs) == 0 {
		return nil
	}
	out := make([]llm.ToolCall, len(recs))
	for i, tc := range recs {
		out[i] = llm.ToolCall{
			ID:   tc.ID,
			Type: tc.Type,
			Function: llm.FunctionCall{
				Name:      tc.Name,
				Arguments: tc.Arguments,
			},
		}
	}
	return out
}
