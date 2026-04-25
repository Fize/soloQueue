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
	"os"
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
)

// Config 存储 TUI 应用的配置常量
type Config struct {
	SessionMgr        *session.SessionManager
	ModelID           string
	Version           string
	MaxScrollbackLines int              // 滚动缓冲区最大行数
	MaxExpandLines    int              // 展开内容最大显示行数
	SpinnerInterval   time.Duration  // spinner 刷新间隔
}

// DefaultConfig 返回默认配置
func DefaultConfig() Config {
	return Config{
		MaxScrollbackLines: 10000,
		MaxExpandLines:     10,
		SpinnerInterval:    80 * time.Millisecond,
	}
}

// ─── Messages ─────────────────────────────────────────────────────────────────

type agentEventMsg struct{ ev agent.AgentEvent }
type streamDoneMsg struct{ err error }
type sessionReadyMsg struct {
	sess *session.Session
	err  error
}

type spinnerTickMsg struct{}
type logoRenderedMsg struct{}

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

// ─── Sub-structures for model ────────────────────────────────────────────────

// InputState 管理用户输入相关状态
type InputState struct {
	input        textinput.Model
	history      []string
	historyPos   int
	savedBuf     string
}

// StreamState 管理流式输出相关状态
type StreamState struct {
	streaming    bool
	streamCancel context.CancelFunc
	evCh         <-chan agent.AgentEvent
	contentBuf   strings.Builder
	streamPhase  string
	streamStart  time.Time
}

// ReasoningState 管理推理过程相关状态
type ReasoningState struct {
	reasonBuf    strings.Builder
	reasonBlocks []thinkBlock
	curThinkIdx  int
}

// ToolState 管理工具调用相关状态
type ToolState struct {
	currentTool string
	toolArgs    strings.Builder
	toolExecMap map[string]*toolExecInfo
}

// UIState 管理 UI 显示相关状态
type UIState struct {
	width        int
	height       int
	useAltScreen bool
	ready        bool
	fatalErr     error
	logoVersion  string
	logoRendered bool // alt-screen logo 是否已渲染，避免 View() 副作用
	spinnerFrame int
	spinnerChars []rune
	pendingExit  bool
}

// ─── Model ────────────────────────────────────────────────────────────────────

type model struct {
	cfg      Config
	sess     *session.Session
	ctx      context.Context
	cancelFn context.CancelFunc

	// 子结构体
	input     InputState
	stream    StreamState
	reasoning ReasoningState
	tool      ToolState
	ui        UIState

	// Scrollback buffer: all historical content
	scrollback []scrollLine
	lastLineEmpty bool

	// 确认弹窗状态
	confirm confirmState
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
	// 使用默认配置补充未设置的字段
	if cfg.MaxScrollbackLines == 0 {
		cfg.MaxScrollbackLines = DefaultConfig().MaxScrollbackLines
	}
	if cfg.MaxExpandLines == 0 {
		cfg.MaxExpandLines = DefaultConfig().MaxExpandLines
	}
	if cfg.SpinnerInterval == 0 {
		cfg.SpinnerInterval = DefaultConfig().SpinnerInterval
	}

	ctx, cancel := context.WithCancel(context.Background())

	ti := textinput.New()
	ti.Placeholder = "Ask anything… (/help for commands)"
	ti.Prompt = ""
	var inputStyles textinput.Styles
	inputStyles.Focused.Text = lipgloss.NewStyle().Background(lipgloss.Color("236"))
	inputStyles.Focused.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Background(lipgloss.Color("236"))
	// 使用 textinput 内置的 virtual cursor（通过 m.input.View() 渲染），
	// 它正确处理 blink、IME 组合文本、Unicode 等所有边界情况。
	inputStyles.Cursor = textinput.CursorStyle{
		Color: lipgloss.Color("10"),
		Shape: tea.CursorBlock,
		Blink: true,
	}
	ti.SetStyles(inputStyles)
	ti.SetVirtualCursor(true)
	ti.Focus()
	ti.CharLimit = 4096

	useAlt := shouldUseAltScreen()

	m := &model{
		cfg:      cfg,
		ctx:      ctx,
		cancelFn: cancel,
		input: InputState{
			input:      ti,
			historyPos: -1,
		},
		stream: StreamState{
			streamPhase: "",
		},
		reasoning: ReasoningState{
			curThinkIdx: -1,
		},
		tool: ToolState{
			toolExecMap: make(map[string]*toolExecInfo),
		},
		ui: UIState{
			useAltScreen: useAlt,
			spinnerChars: []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'},
		},
	}

	// 在 scrollback 顶部显示启动 logo
	m.ui.logoVersion = cfg.Version

	// Alt-screen 模式：精确控制布局，输入框固定终端底部
	// Inline 降级模式：tmux/SSH 环境，保留终端滚动历史
	opts := []tea.ProgramOption{}
	if useAlt {

	}
	p := tea.NewProgram(m, opts...)
	return p
}

func (m *model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		textinput.Blink,
		m.createSessionCmd(),
	}
	// inline 模式下，logo 通过 tea.Println (insertAbove) 输出到终端，
	// 直接写入终端滚动历史，不受 renderer 行数裁剪影响。
	// alt-screen 模式下由 View() 统一渲染。
	if !m.ui.useAltScreen && m.ui.logoVersion != "" {
		cmds = append(cmds, tea.Println(m.renderLogo()))
	}
	// 使用 tea.Sequence 确保 logoRenderedMsg 在初始命令启动后的下一轮事件循环才到达，
	// 从而保证 alt-screen 模式下 View() 至少已被调用一次、logo 已渲染。
	return tea.Sequence(
		tea.Batch(cmds...),
		tea.Tick(0, func(time.Time) tea.Msg {
			return logoRenderedMsg{}
		}),
	)
}

func (m *model) createSessionCmd() tea.Cmd {
	return func() tea.Msg {
		sess, err := m.cfg.SessionMgr.Create(m.ctx, "")
		return sessionReadyMsg{sess: sess, err: err}
	}
}

// spinnerTick 返回一个新的 spinnerTick Cmd，使用配置中的 SpinnerInterval
func (m *model) spinnerTick() tea.Cmd {
	return tea.Tick(time.Millisecond*m.cfg.SpinnerInterval, func(_ time.Time) tea.Msg {
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

// ─── Terminal environment detection ───────────────────────────────────────────

// shouldUseAltScreen 判断是否应使用 alt-screen 模式。
//
// 默认使用 inline 模式（内容直接追加到终端滚动历史，与 Claude Code 一致）。
// 如需固定输入框到底部、消除闪烁，可设置环境变量 ALT_SCREEN=1 启用全屏。
func shouldUseAltScreen() bool {
	return os.Getenv("ALT_SCREEN") != "" || os.Getenv("SOLOQUEUE_ALT_SCREEN") != ""
}

// renderLogo 返回预渲染的 ASCII logo 多行字符串（仅在 View 中使用）
func (m *model) renderLogo() string {
	if m.ui.logoVersion == "" {
		return ""
	}

	logoLines := []string{
		" ╭──╮ ╭──╮ ",
		" ╰──╮ │  │  soloqueue",
		" ╰──╯ ╰──┼  " + m.ui.logoVersion,
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
