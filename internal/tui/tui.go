// Package tui 提供 SoloQueue 的终端交互界面（bubbletea 框架）
//
// UI 布局（默认 inline 模式，内容与终端滚动历史共存）：
//
//   [滚动区 —— 历史内容自然追加]
//     > 用户输入                              ← 用户消息（绿色加粗）
//     The user is asking about...             ← LLM 输出（白色）
//     ... 37 more lines (press Ctrl+O)        ← 折叠提示
//
//     💭 Thinking for 12 lines · Ctrl+T       ← Thinking 块（折叠态）
//
//     ▸ search(...)                           ← 工具开始（灰色加粗）
//       ✓ search 4 lines (ctrl+o to expand)   ← 工具结果（柔和绿色）
//
//   [空行分隔]
//   [状态行] * Generating... (2s) · esc to interrupt   ← 活跃时显示
//   [空行分隔]
//   [输入框] > │                              ← 输入区
//
// 关键设计：
//   - 默认 inline 模式：内容追加到主终端，退出后可滚动回看（与 Claude Code 一致）
//   - Alt-screen 可选模式：设置 ALT_SCREEN=1 启用，输入框固定底部、无闪烁
//   - 滚动区：所有历史内容统一滚动（用户输入、LLM 输出、工具、代码、表格）
//   - Scrollback 上限保留：防止超长对话内存增长
//   - 状态行：独立固定，活跃时显示在空行分隔之间
//   - 输入框：独立固定在底部（alt-screen 下）或跟随内容（inline 下）
//   - 工具块：前后自动插入空行，与输出内容隔离
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/session"
)

// ─── Styles ───────────────────────────────────────────────────────────────────

var (
	// Core text styles
	styleUser   = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	styleAI     = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	styleDim    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	// Input line background — 整行深色背景（Codex 风格）
	// 在 View() 中通过 .Width(m.width) 动态设置整行宽度，
	// 使 lipgloss 自动用带背景色的空格填满整行。
	styleInputLine = lipgloss.NewStyle().Background(lipgloss.Color("236"))
	// Prompt " >" 的样式 — 与输入区同色背景
	stylePromptBg = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true).Background(lipgloss.Color("236"))

	// Thinking block
	styleThinkIcon  = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	styleThinkText  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleThinkTitle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	// Tool block — 使用中性色阶，清晰区分层级
	styleToolName    = lipgloss.NewStyle().Foreground(lipgloss.Color("7")).Bold(true)
	styleToolSuccess = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styleToolError   = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	styleToolResult  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	// Status bar (fixed, above input)
	styleSpinner    = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	styleStatusText = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	styleError = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))

	// Scrollback buffer 上限：超过此行数时截断旧数据
	maxScrollbackLines = 10000
)

// ─── Config ───────────────────────────────────────────────────────────────────

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

type spinnerTickMsg struct{}

// ─── Scroll buffer ────────────────────────────────────────────────────────────

// scrollLine represents one line in the scrollback buffer
type scrollLine struct {
	content    string
	style      lipgloss.Style // optional style for the whole line
	expandable bool           // 是否可展开（工具结果等）
	expanded   bool           // 当前是否已展开
	fullLines  []string       // 展开后显示的完整内容行
	fullStyle  lipgloss.Style // 展开内容的样式
}

// ─── Thinking block ───────────────────────────────────────────────────────────

type thinkBlock struct {
	lines    []string
	expanded bool
}

// ─── Model ────────────────────────────────────────────────────────────────────

type model struct {
	cfg      Config
	sess     *session.Session
	ctx      context.Context
	cancelFn context.CancelFunc

	input  textinput.Model
	width  int
	height int

	// 输入历史
	history    []string
	historyPos int
	savedBuf   string

	// 流式状态
	streaming    bool
	streamCancel context.CancelFunc
	evCh         <-chan agent.AgentEvent

	// Scrollback buffer: all historical content
	scrollback []scrollLine

	// contentBuf 存当前未完成的半行（无 \n）
	contentBuf    strings.Builder
	lastLineEmpty bool

	// Reasoning state
	reasonBuf    strings.Builder
	reasonBlocks []thinkBlock
	curThinkIdx  int

	// Tool call state
	currentTool string
	toolArgs    strings.Builder

	// Tool exec tracking
	toolExecMap map[string]*toolExecInfo

	// 确认弹窗状态
	confirm confirmState

	// Logo 版本号（启动时设置，View 中渲染）
	logoVersion string

	// Spinner state
	spinnerFrame int
	spinnerChars []rune

	// Stream phase for status display
	streamPhase string

	// Timing
	streamStart time.Time

	// 双击 Ctrl+C 退出
	pendingExit bool

	// TUI 模式
	useAltScreen bool // 是否使用 alt-screen 全屏模式

	ready    bool
	fatalErr error
p *tea.Program
}

// confirmState 管理工具确认弹窗状态
type confirmState struct {
	active         bool
	callID         string
	prompt         string
	options        []string
	allowInSession bool
}

type toolExecInfo struct {
	name     string
	args     string
	start    time.Time
	duration time.Duration
	err      error
	result   string
	done     bool
}

// ─── Init ─────────────────────────────────────────────────────────────────────

func New(cfg Config) *tea.Program {
	ctx, cancel := context.WithCancel(context.Background())

	ti := textinput.New()
	ti.Placeholder = "Ask anything… (/help for commands)"
	ti.Prompt = ""
	var inputStyles textinput.Styles
	inputStyles.Focused.Text = lipgloss.NewStyle().Background(lipgloss.Color("236"))
	inputStyles.Focused.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Background(lipgloss.Color("236"))
	ti.SetStyles(inputStyles) // 自己在外部用 stylePrompt 渲染
	ti.Focus()
	ti.CharLimit = 4096

	useAlt := shouldUseAltScreen()

	m := &model{
		cfg:          cfg,
		ctx:          ctx,
		cancelFn:     cancel,
		input:        ti,
		historyPos:   -1,
		curThinkIdx:  -1,
		toolExecMap:  make(map[string]*toolExecInfo),
		spinnerChars: []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'},
		streamPhase:  "",
		useAltScreen: useAlt,
	}

	// 在 scrollback 顶部显示启动 logo
	m.logoVersion = cfg.Version

	// Alt-screen 模式：精确控制布局，输入框固定终端底部
	// Inline 降级模式：tmux/SSH 环境，保留终端滚动历史
	opts := []tea.ProgramOption{}
	if useAlt {
		
	}
	p := tea.NewProgram(m, opts...)
	m.p = p
	return p
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		m.createSessionCmd(),
	)
}

func (m *model) createSessionCmd() tea.Cmd {
	return func() tea.Msg {
		sess, err := m.cfg.SessionMgr.Create(m.ctx, "")
		return sessionReadyMsg{sess: sess, err: err}
	}
}

func spinnerTick() tea.Cmd {
	return tea.Tick(time.Millisecond*80, func(_ time.Time) tea.Msg {
		return spinnerTickMsg{}
	})
}

// ─── Run ──────────────────────────────────────────────────────────────────────

func Run(cfg Config) error {
	p := New(cfg)
	_, err := p.Run()
	if err != nil && err.Error() == "program was interrupted" {
		return nil
	}
	return err
}

// ─── String / formatting helpers ──────────────────────────────────────────────

func truncate(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", "↵")
	s = strings.ReplaceAll(s, "\r", "")
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "…"
}

func wrapLine(line string, width int) []string {
	if width <= 0 {
		return []string{line}
	}
	if utf8.RuneCountInString(line) <= width {
		return []string{line}
	}

	var result []string
	runes := []rune(line)
	for len(runes) > 0 {
		if len(runes) <= width {
			result = append(result, string(runes))
			break
		}
		breakAt := width
		for i := width - 1; i > width/2; i-- {
			if runes[i] == ' ' {
				breakAt = i
				break
			}
		}
		result = append(result, string(runes[:breakAt]))
		runes = runes[breakAt:]
		if len(runes) > 0 && runes[0] == ' ' {
			runes = runes[1:]
		}
	}
	return result
}

// renderLogo 返回预渲染的 ASCII logo 多行字符串（仅在 View 中使用）
func (m *model) renderLogo() string {
	if m.logoVersion == "" {
		return ""
	}

	logoLines := []string{
		" ╭──╮ ╭──╮ ",
		" ╰──╮ │  │  soloqueue",
		" ╰──╯ ╰──┼  " + m.logoVersion,
		"         ╰ ",
	}

	// 渐变色：#00E5FF → #F5D061
	startR, startG, startB := uint8(0), uint8(229), uint8(255)
	endR, endG, endB := uint8(245), uint8(208), uint8(97)

	var sb strings.Builder
	for i, line := range logoLines {
		ratio := float64(i) / float64(len(logoLines)-1)
		r := startR + uint8(float64(endR-startR)*ratio)
		g := startG + uint8(float64(endG-startG)*ratio)
		b := startB + uint8(float64(endB-startB)*ratio)

		style := lipgloss.NewStyle().Foreground(lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", r, g, b)))
		sb.WriteString(style.Render(line))
		if i < len(logoLines)-1 || true {
			sb.WriteString("\n")
		}
	}
	sb.WriteString("\n")
	return sb.String()
}
