// Package tui 提供 SoloQueue 的终端交互界面（bubbletea 框架）
//
// 设计原则（对齐 Claude Code 风格）：
//
//   - 不使用全屏 alt-screen；在普通终端滚动区内联运行
//   - 已完成的行（遇到 \n）通过 tea.Println 立即永久写入滚动区（打字机效果）
//   - contentBuf 只存当前未完成的半行（无 \n），View() 只渲染该半行
//   - View() 高度极小：最多 3 行（reasoning + 当前半行 + 工具状态 + 提示）
//   - 双击 Ctrl+C 退出（首次提示，再次确认）；streaming 时 Ctrl+C / Esc 中断流
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

// ─── Styles ───────────────────────────────────────────────────────────────────

var (
	stylePrompt  = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	styleUser    = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	styleAI      = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	styleThink   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleTool    = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
	styleSuccess = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	styleError   = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	styleDim     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleBanner  = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
)

// ─── Config ───────────────────────────────────────────────────────────────────

// Config 是 TUI 的启动配置
type Config struct {
	SessionMgr *session.SessionManager
	ModelID    string
	Version    string
}

// ─── Messages ─────────────────────────────────────────────────────────────────

type agentEventMsg struct{ ev agent.AgentEvent }
type streamDoneMsg struct{ err error }
type sessionReadyMsg struct {
	sess *session.Session
	err  error
}

// ─── Model ────────────────────────────────────────────────────────────────────

type model struct {
	cfg      Config
	sess     *session.Session
	ctx      context.Context
	cancelFn context.CancelFunc

	input textinput.Model
	width int

	// 输入历史
	history    []string
	historyPos int
	savedBuf   string

	// 流式状态
	streaming    bool
	streamCancel context.CancelFunc
	evCh         <-chan agent.AgentEvent

	// contentBuf 只存当前未完成的半行（无 \n）。
	// 遇到 \n 时立即 tea.Println 已完成行并清空。
	// View() 只渲染 contentBuf（最多 1 行）。
	inReasoning bool
	reasonLines int // reasoning 已收到的行数，用于显示进度
	contentBuf  strings.Builder
	currentTool string
	toolArgs    strings.Builder

	// 双击 Ctrl+C 退出
	pendingExit bool

	ready    bool
	fatalErr error
}

// ─── Init ─────────────────────────────────────────────────────────────────────

func New(cfg Config) *tea.Program {
	ctx, cancel := context.WithCancel(context.Background())

	ti := textinput.New()
	ti.Placeholder = "Ask anything… (/help for commands)"
	ti.Focus()
	ti.CharLimit = 4096

	m := &model{
		cfg:        cfg,
		ctx:        ctx,
		cancelFn:   cancel,
		input:      ti,
		historyPos: -1,
	}

	// 不使用 WithAltScreen — 在普通终端滚动区内联运行
	return tea.NewProgram(m)
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		tea.Sequence(m.bannerCmds()...),
		m.createSessionCmd(),
	)
}

func (m *model) createSessionCmd() tea.Cmd {
	return func() tea.Msg {
		sess, err := m.cfg.SessionMgr.Create(m.ctx, "")
		return sessionReadyMsg{sess: sess, err: err}
	}
}

// ─── Update ───────────────────────────────────────────────────────────────────

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil

	case sessionReadyMsg:
		if msg.err != nil {
			m.fatalErr = msg.err
			return m, tea.Quit
		}
		m.sess = msg.sess
		m.ready = true
		return m, tea.Println(styleDim.Render("session ready — type your question or /help"))

	case tea.KeyMsg:
		// 非 Ctrl+C 的任意按键清除退出确认
		if msg.Type != tea.KeyCtrlC {
			m.pendingExit = false
		}

		switch msg.Type {

		case tea.KeyCtrlC:
			if m.streaming && m.streamCancel != nil {
				// 第一次：中断当前流
				m.streamCancel()
				return m, nil
			}
			if m.pendingExit {
				// 第二次：真正退出
				m.cancelFn()
				return m, tea.Quit
			}
			// 第一次（空闲状态）：提示再按一次
			m.pendingExit = true
			return m, nil

		case tea.KeyCtrlD:
			if m.input.Value() == "" && !m.streaming {
				m.cancelFn()
				return m, tea.Quit
			}

		case tea.KeyEsc:
			if m.streaming && m.streamCancel != nil {
				m.streamCancel()
			}
			return m, nil

		case tea.KeyEnter:
			if m.streaming || !m.ready {
				return m, nil
			}
			input := strings.TrimSpace(m.input.Value())
			m.input.SetValue("")
			m.historyPos = -1
			m.savedBuf = ""
			if input == "" {
				return m, nil
			}
			m.addHistory(input)

			// 内置命令
			if quit, cmds := m.handleBuiltin(input); quit {
				return m, tea.Quit
			} else if cmds != nil {
				return m, tea.Batch(cmds...)
			}
			if strings.HasPrefix(input, "/") {
				return m, nil
			}

			// 发送给 agent
			return m, m.startStream(input)

		case tea.KeyUp:
			if !m.streaming {
				m.historyNavigate(-1)
			}
			return m, nil

		case tea.KeyDown:
			if !m.streaming {
				m.historyNavigate(+1)
			}
			return m, nil
		}

	case agentEventMsg:
		cmds := m.handleAgentEvent(msg.ev)
		cmds = append(cmds, m.pollEvent())
		return m, tea.Batch(cmds...)

	case streamDoneMsg:
		var cmds []tea.Cmd
		// 流结束：flush 最后一段未完成的半行（无 \n，所以直接单行输出）
		if tail := m.contentBuf.String(); tail != "" {
			cmds = append(cmds, tea.Println(styleAI.Render(tail)))
		}
		if msg.err != nil && msg.err != context.Canceled {
			cmds = append(cmds, tea.Println(styleError.Render("✗ "+msg.err.Error())))
		}
		cmds = append(cmds, tea.Println(""))
		// 重置流式状态
		m.streaming = false
		m.streamCancel = nil
		m.evCh = nil
		m.contentBuf.Reset()
		m.inReasoning = false
		m.reasonLines = 0
		m.currentTool = ""
		m.toolArgs.Reset()
		return m, tea.Batch(cmds...)
	}

	// 把键盘事件透传给 textinput
	if !m.streaming {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}

	return m, nil
}

// ─── View ─────────────────────────────────────────────────────────────────────

// View 只渲染"当前正在发生"的内容，高度极小（最多 4 行）：
//   - 流式进行中：reasoning 指示（可选）+ 当前未完成半行（可选）+ 工具状态（可选）+ 提示
//   - 空闲：输入行（+ 退出确认提示）
func (m *model) View() string {
	if m.fatalErr != nil {
		return styleError.Render("fatal: "+m.fatalErr.Error()) + "\n"
	}

	if m.streaming {
		var sb strings.Builder
		if m.inReasoning {
			sb.WriteString(styleThink.Render(fmt.Sprintf("  ◌ thinking… (%d lines)", m.reasonLines)) + "\n")
		}
		// contentBuf 现在只含当前未完成半行（无 \n），直接渲染
		if m.contentBuf.Len() > 0 {
			sb.WriteString(styleAI.Render(m.contentBuf.String()) + "\n")
		}
		if m.currentTool != "" {
			sb.WriteString(styleTool.Render("  ⚙ "+m.currentTool) +
				styleDim.Render(" ("+truncateArgs(m.toolArgs.String(), 50)+"…") + "\n")
		}
		sb.WriteString(styleDim.Render("  streaming… (Ctrl+C / Esc to interrupt)"))
		return sb.String()
	}

	// 空闲
	prompt := stylePrompt.Render("❯") + " " + m.input.View()
	if m.pendingExit {
		return prompt + "\n" + styleDim.Render("  (Ctrl+C again to exit)")
	}
	return prompt
}

// ─── Stream ───────────────────────────────────────────────────────────────────

func (m *model) startStream(prompt string) tea.Cmd {
	askCtx, cancel := context.WithCancel(m.ctx)
	evCh, err := m.sess.AskStream(askCtx, prompt)
	if err != nil {
		cancel()
		return tea.Sequence(
			tea.Println(styleError.Render("✗ "+err.Error())),
			tea.Println(""),
		)
	}

	m.streaming = true
	m.streamCancel = cancel
	m.evCh = evCh
	m.inReasoning = false
	m.reasonLines = 0
	m.contentBuf.Reset()
	m.currentTool = ""
	m.toolArgs.Reset()

	// 先把用户输入提交到滚动区
	return tea.Sequence(
		tea.Println(styleUser.Render("You: ")+prompt),
		m.pollEvent(),
	)
}

func (m *model) pollEvent() tea.Cmd {
	ch := m.evCh
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return streamDoneMsg{}
		}
		return agentEventMsg{ev: ev}
	}
}

// flushContentDelta 将 delta 追加到 contentBuf，遇到 \n 立即 tea.Println 已完成行。
// contentBuf 始终只存当前未完成的半行（无 \n）。
func (m *model) flushContentDelta(delta string) []tea.Cmd {
	var cmds []tea.Cmd
	combined := m.contentBuf.String() + delta
	m.contentBuf.Reset()

	for {
		idx := strings.Index(combined, "\n")
		if idx < 0 {
			break
		}
		line := combined[:idx]
		combined = combined[idx+1:]
		// 空行直接输出（保留段落间距，但不着色避免多余 ANSI）
		if line == "" {
			cmds = append(cmds, tea.Println(""))
		} else {
			cmds = append(cmds, tea.Println(styleAI.Render(line)))
		}
	}

	// 剩余部分（无 \n）存回 contentBuf，在 View() 中显示为当前行
	m.contentBuf.WriteString(combined)
	return cmds
}

// flushContentBuf 将 contentBuf 中的半行立即提交到滚动区（工具调用 / 流结束前使用）
func (m *model) flushContentBuf() []tea.Cmd {
	if m.contentBuf.Len() == 0 {
		return nil
	}
	line := m.contentBuf.String()
	m.contentBuf.Reset()
	return []tea.Cmd{tea.Println(styleAI.Render(line))}
}

// handleAgentEvent 处理单个 agent 事件，返回需要立即提交到滚动区的 tea.Cmd 列表
func (m *model) handleAgentEvent(ev agent.AgentEvent) []tea.Cmd {
	var cmds []tea.Cmd

	switch e := ev.(type) {

	case agent.ReasoningDeltaEvent:
		m.inReasoning = true
		// 统计行数用于 View() 显示进度
		m.reasonLines += strings.Count(e.Delta, "\n")

	case agent.ContentDeltaEvent:
		if m.inReasoning {
			m.inReasoning = false
			// reasoning 结束：把"thinking"记录提交到滚动区
			cmds = append(cmds, tea.Println(
				styleThink.Render(fmt.Sprintf("  ◌ thought for %d lines", m.reasonLines)),
			))
		}
		// 逐行流式输出：遇到 \n 立即提交完成行，半行留在 contentBuf 供 View() 渲染
		cmds = append(cmds, m.flushContentDelta(e.Delta)...)

	case agent.ToolCallDeltaEvent:
		if m.inReasoning {
			m.inReasoning = false
			cmds = append(cmds, tea.Println(
				styleThink.Render(fmt.Sprintf("  ◌ thought for %d lines", m.reasonLines)),
			))
		}
		// 切换到新工具时先把当前内容半行提交
		if e.Name != "" && e.Name != m.currentTool {
			cmds = append(cmds, m.flushContentBuf()...)
			if m.currentTool != "" {
				// 上一个 tool call 定型
				cmds = append(cmds, tea.Println(
					styleTool.Render("  ⚙ "+m.currentTool)+
						styleDim.Render(" ("+truncateArgs(m.toolArgs.String(), 80)+")"),
				))
			}
			m.currentTool = e.Name
			m.toolArgs.Reset()
		}
		if e.ArgsDelta != "" {
			m.toolArgs.WriteString(e.ArgsDelta)
		}

	case agent.ToolExecStartEvent:
		// 工具即将执行，先把当前内容半行提交
		cmds = append(cmds, m.flushContentBuf()...)
		// 定型 tool call 行
		cmds = append(cmds, tea.Println(
			styleTool.Render("  ⚙ "+e.Name)+
				styleDim.Render(" ("+truncateArgs(e.Args, 80)+")"),
		))
		m.currentTool = ""
		m.toolArgs.Reset()

	case agent.ToolExecDoneEvent:
		dur := e.Duration.Round(time.Millisecond)
		if e.Err != nil {
			cmds = append(cmds, tea.Println(fmt.Sprintf("  %s %s",
				styleError.Render("✗"),
				styleDim.Render(e.Name+" ("+dur.String()+"): "+e.Err.Error()),
			)))
		} else {
			preview := truncate(e.Result, 60)
			cmds = append(cmds, tea.Println(fmt.Sprintf("  %s %s",
				styleSuccess.Render("✓"),
				styleDim.Render(e.Name+" → "+preview+" ("+dur.String()+")"),
			)))
		}

	case agent.IterationDoneEvent:
		// no-op

	case agent.DoneEvent:
		// 内容由 streamDoneMsg 统一提交

	case agent.ErrorEvent:
		cmds = append(cmds, tea.Println(styleError.Render("✗ "+e.Err.Error())))
	}

	return cmds
}

// ─── Built-in commands ────────────────────────────────────────────────────────

// handleBuiltin 返回 (quit, cmds)；cmds 用 tea.Println 输出到滚动区
func (m *model) handleBuiltin(input string) (quit bool, cmds []tea.Cmd) {
	cmd := strings.ToLower(strings.TrimSpace(input))
	switch cmd {
	case "/quit", "/exit", "/q":
		return true, nil

	case "/help", "/?":
		cmds = helpCmds()

	case "/clear":
		// 打印足够多换行把内容推离屏幕，再重新打印 banner
		cmds = append(cmds, tea.Println(strings.Repeat("\n", 40)))
		cmds = append(cmds, m.bannerCmds()...)

	case "/version":
		cmds = append(cmds,
			tea.Println(lipgloss.NewStyle().Bold(true).Render("SoloQueue")+" "+m.cfg.Version),
			tea.Println(""),
		)

	case "/history":
		cmds = m.historyCmds()

	default:
		if strings.HasPrefix(input, "/") {
			cmds = append(cmds,
				tea.Println(styleError.Render("✗ Unknown command: "+input+". Type /help")),
				tea.Println(""),
			)
		}
	}
	return false, cmds
}

func helpCmds() []tea.Cmd {
	lines := []string{
		lipgloss.NewStyle().Bold(true).Render("Commands:"),
		fmt.Sprintf("  %-12s %s", styleTool.Render("/help"), styleDim.Render("Show this help")),
		fmt.Sprintf("  %-12s %s", styleTool.Render("/clear"), styleDim.Render("Clear the screen")),
		fmt.Sprintf("  %-12s %s", styleTool.Render("/history"), styleDim.Render("Show input history")),
		fmt.Sprintf("  %-12s %s", styleTool.Render("/version"), styleDim.Render("Show version")),
		fmt.Sprintf("  %-12s %s", styleTool.Render("/quit"), styleDim.Render("Exit SoloQueue")),
		"",
		lipgloss.NewStyle().Bold(true).Render("Shortcuts:"),
		fmt.Sprintf("  %-12s %s", styleTool.Render("Ctrl+C"), styleDim.Render("Interrupt stream / exit (press twice)")),
		fmt.Sprintf("  %-12s %s", styleTool.Render("Esc"), styleDim.Render("Interrupt stream")),
		fmt.Sprintf("  %-12s %s", styleTool.Render("Ctrl+D"), styleDim.Render("Exit (on empty line)")),
		fmt.Sprintf("  %-12s %s", styleTool.Render("↑ / ↓"), styleDim.Render("History navigation")),
		"",
	}
	cmds := make([]tea.Cmd, len(lines))
	for i, l := range lines {
		cmds[i] = tea.Println(l)
	}
	return cmds
}

func (m *model) bannerCmds() []tea.Cmd {
	lines := []string{
		styleBanner.Render("SoloQueue") + "  " + styleDim.Render("v"+m.cfg.Version+" — AI multi-agent collaboration"),
		styleDim.Render("model: " + m.cfg.ModelID),
		styleDim.Render("Ctrl+C×2 to quit · /help for commands · ↑↓ history"),
		"",
	}
	cmds := make([]tea.Cmd, len(lines))
	for i, l := range lines {
		cmds[i] = tea.Println(l)
	}
	return cmds
}

func (m *model) historyCmds() []tea.Cmd {
	if len(m.history) == 0 {
		return []tea.Cmd{
			tea.Println(styleDim.Render("(no history yet)")),
			tea.Println(""),
		}
	}
	var cmds []tea.Cmd
	cmds = append(cmds, tea.Println(lipgloss.NewStyle().Bold(true).Render("History:")))
	start := 0
	if len(m.history) > 20 {
		start = len(m.history) - 20
	}
	for i := start; i < len(m.history); i++ {
		cmds = append(cmds, tea.Println(fmt.Sprintf("  %s  %s",
			styleDim.Render(fmt.Sprintf("%3d", i+1)),
			truncate(m.history[i], 72),
		)))
	}
	cmds = append(cmds, tea.Println(""))
	return cmds
}

// ─── History ──────────────────────────────────────────────────────────────────

func (m *model) addHistory(line string) {
	if line == "" {
		return
	}
	if len(m.history) > 0 && m.history[len(m.history)-1] == line {
		return
	}
	m.history = append(m.history, line)
}

func (m *model) historyNavigate(dir int) {
	if len(m.history) == 0 {
		return
	}
	if m.historyPos == -1 {
		m.savedBuf = m.input.Value()
		m.historyPos = len(m.history)
	}
	newPos := m.historyPos + dir
	if newPos < 0 {
		return
	}
	if newPos >= len(m.history) {
		m.historyPos = -1
		m.input.SetValue(m.savedBuf)
		m.input.CursorEnd()
		return
	}
	m.historyPos = newPos
	m.input.SetValue(m.history[m.historyPos])
	m.input.CursorEnd()
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

func truncateArgs(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "…"
}

// ─── Run ──────────────────────────────────────────────────────────────────────

// Run 构造 Program 并运行，阻塞到退出
func Run(cfg Config) error {
	p := New(cfg)
	_, err := p.Run()
	if err != nil && err.Error() == "program was interrupted" {
		return nil
	}
	return err
}
