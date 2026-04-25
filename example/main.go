package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ==========================================
// 1. 样式与主题定义
// ==========================================
var (
	colorPrimary = lipgloss.Color("99")
	colorAccent  = lipgloss.Color("86")
	colorMuted   = lipgloss.Color("240")
	colorWarning = lipgloss.Color("203")

	userStyle    = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	agentStyle   = lipgloss.NewStyle().Foreground(colorPrimary).Bold(true)
	thinkStyle   = lipgloss.NewStyle().Foreground(colorMuted).Italic(true).BorderLeft(true).BorderStyle(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("238")).PaddingLeft(1)
	toolStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("170")).Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("63")).Padding(0, 1)
	contentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	foldedStyle  = lipgloss.NewStyle().Foreground(colorMuted).Italic(true)

	statusStyle = lipgloss.NewStyle().Background(lipgloss.Color("236")).Padding(0, 1)
	hintStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("235")).Background(lipgloss.Color("240")).Padding(0, 1)
)

// ==========================================
// 2. 模型与状态
// ==========================================
type Message struct {
	Role     string
	Thoughts string
	Tools    string
	Content  string
}

type model struct {
	history      []Message
	textInput    textinput.Model
	isGenerating bool
	step         int 
	currIdx      int 
	
	showThinking bool
	showTools    bool
	quitCount    int
	width        int 
}

// ✨ 重构：定义清晰的状态流转消息
type streamMsg string         // 接收单个字符
type stepDoneMsg struct{}     // 当前阶段(Thinking/Tool/Content)完成
type startNextStepMsg struct{}// 触发下一个阶段的打字
type resetQuitMsg struct{}    // 重置退出状态

func initialModel() model {
	ti := textinput.New()
	ti.Prompt = "❯  "
	ti.PromptStyle = userStyle
	ti.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	ti.Cursor.Style = lipgloss.NewStyle() 
	ti.Focus()

	return model{
		textInput:    ti,
		showThinking: true,
		showTools:    true,
		history:      []Message{},
	}
}

func (m model) Init() tea.Cmd {
	return textinput.Blink
}

// ==========================================
// 3. 更新逻辑 (严格的状态机管理)
// ==========================================
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width

	case tea.KeyMsg:
		if msg.Type != tea.KeyCtrlC && m.quitCount > 0 {
			m.quitCount = 0
		}
		switch msg.Type {
		case tea.KeyCtrlC:
			m.quitCount++
			if m.quitCount >= 2 { return m, tea.Quit }
			return m, tea.Tick(time.Second*2, func(t time.Time) tea.Msg { return resetQuitMsg{} })
		case tea.KeyCtrlT:
			m.showThinking = !m.showThinking
		case tea.KeyCtrlO:
			m.showTools = !m.showTools
		case tea.KeyEnter:
			if m.isGenerating || strings.TrimSpace(m.textInput.Value()) == "" { return m, nil }
			
			input := m.textInput.Value()
			m.history = append(m.history, Message{Role: "user", Content: input})
			m.textInput.SetValue("")
			
			m.isGenerating = true
			m.step = 0
			m.currIdx = 0
			m.history = append(m.history, Message{Role: "agent"})
			// 启动第一个字符的流式输出
			return m, m.streamNextCmd()
		}

	// 处理字符追加
	case streamMsg:
		idx := len(m.history) - 1
		switch m.step {
		case 0: m.history[idx].Thoughts += string(msg)
		case 1: m.history[idx].Tools += string(msg)
		case 2: m.history[idx].Content += string(msg)
		}
		// ⚠️ 所有的状态修改都集中在 Update 这里
		m.currIdx++
		return m, m.streamNextCmd()

	// 阶段完成，准备进入下一阶段
	case stepDoneMsg:
		if m.step < 2 {
			m.step++
			m.currIdx = 0
			// 停顿 600ms 后再开始打下一阶段的字
			return m, tea.Tick(time.Millisecond*600, func(t time.Time) tea.Msg {
				return startNextStepMsg{}
			})
		}
		// 如果已经是最后一个阶段，结束生成
		m.isGenerating = false
		return m, nil

	// 真正触发下一阶段
	case startNextStepMsg:
		return m, m.streamNextCmd()

	case resetQuitMsg:
		m.quitCount = 0
	}

	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

// 指令生成器：只负责读取当前状态，绝对不修改状态！
func (m model) streamNextCmd() tea.Cmd {
	var targetStr string
	switch m.step {
	case 0: targetStr = "正在分析 SoloQueue 队列压力... 发现节点 bj-v4 响应延迟过高。"
	case 1: targetStr = "soloqueue_inspect --node=bj-v4"
	case 2: targetStr = "诊断完成。节点由于 OOM 重启，已自动调整资源配额。"
	}

	target := []rune(targetStr)

	// 如果还没打完，继续发下一个字符
	if m.currIdx < len(target) {
		return tea.Tick(time.Millisecond*30, func(t time.Time) tea.Msg {
			return streamMsg(string(target[m.currIdx]))
		})
	}

	// 如果打完了，发出当前阶段完成的信号
	return func() tea.Msg { return stepDoneMsg{} }
}

// ==========================================
// 4. 视图布局
// ==========================================
func (m model) View() string {
	var sb strings.Builder

	for _, msg := range m.history {
		if msg.Role == "user" {
			sb.WriteString(userStyle.Render("You: ") + msg.Content + "\n\n")
		} else {
			sb.WriteString(agentStyle.Render("Agent:") + "\n")
			if msg.Thoughts != "" {
				if m.showThinking {
					sb.WriteString(lipgloss.NewStyle().Foreground(colorMuted).Render("▾ Thinking") + "\n")
					sb.WriteString(thinkStyle.Render(msg.Thoughts) + "\n\n")
				} else {
					sb.WriteString(foldedStyle.Render("▸ Thinking (Folded)") + "\n\n")
				}
			}
			if msg.Tools != "" {
				if m.showTools {
					sb.WriteString(lipgloss.NewStyle().Foreground(colorMuted).Render("▾ Tools") + "\n")
					sb.WriteString(toolStyle.Render("⚙ "+msg.Tools) + "\n\n")
				} else {
					sb.WriteString(foldedStyle.Render("▸ Tools (Folded)") + "\n\n")
				}
			}
			if msg.Content != "" {
				sb.WriteString(contentStyle.Render(msg.Content) + "\n\n")
			}
		}
	}

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

	sb.WriteString(m.textInput.View() + "\n")

	hint := "Ctrl+T Thinking | Ctrl+O Tools | Ctrl+C Quit"
	renderedHint := hintStyle.Render(hint)
	padding := m.width - lipgloss.Width(renderedHint)
	if padding > 0 {
		sb.WriteString(strings.Repeat(" ", padding))
	}
	sb.WriteString(renderedHint)

	return sb.String()
}

func printLogo() {
	logo := `
   _____       __      ____                       
  / ___/____  / /___  / __ \__  _____  __  _____  
  \__ \/ __ \/ / __ \/ / / / / / / _ \/ / / / _ \ 
 ___/ / /_/ / / /_/ / /_/ / /_/ /  __/ /_/ /  __/ 
/____/\____/_/\____/\___\_\__,_/\___/\__,_/\___/  
	`
	styledLogo := lipgloss.NewStyle().
		Foreground(colorPrimary).
		Bold(true).
		Border(lipgloss.DoubleBorder(), true, false, true, false).
		BorderForeground(lipgloss.Color("63")).
		MarginBottom(1).
		Render(strings.TrimRight(logo, "\n"))

	fmt.Println(styledLogo)
}

func main() {
	printLogo() 
	
	p := tea.NewProgram(initialModel())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v", err)
	}
}