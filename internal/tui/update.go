package tui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/xiaobaitu/soloqueue/internal/agent"
)

// ─── Update ───────────────────────────────────────────────────────────────────

func (m *model) Update(msg tea.Msg) (_ tea.Model, cmd tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.ui.width = msg.Width
		m.ui.height = msg.Height
		return m, nil

	case logoRenderedMsg:
		m.ui.logoRendered = true
		return m, nil

	case sessionReadyMsg:
		if msg.err != nil {
			m.ui.fatalErr = msg.err
			return m, tea.Quit
		}
		m.sess = msg.sess
		m.ui.ready = true
		m.addScrollLine("session ready — type your question or /help", styleDim)
		return m, nil

	case spinnerTickMsg:
		if m.stream.streaming {
			m.ui.spinnerFrame = (m.ui.spinnerFrame + 1) % len(m.ui.spinnerChars)
			return m, m.spinnerTick()
		}
		return m, nil

	case tea.KeyMsg:
		// 确认弹窗激活时劫持键盘输入
		if m.confirm.active {
			choice, ok := m.resolveConfirmChoice(msg)
			if ok {
				if err := m.sess.Agent.Confirm(m.confirm.callID, choice); err != nil {
					m.addScrollLine("✗ confirm error: "+err.Error(), styleError)
				}
				m.confirm.active = false
				// 重置 pendingExit 以允许正常操作
				m.ui.pendingExit = false
				// 恢复事件轮询
				return m, tea.Batch(m.pollEvent())
			}
			// 对于 Ctrl+C，即使在弹窗状态也允许强制退出
			if msg.Key().Code == 'c' && (msg.Key().Mod&tea.ModCtrl != 0) {
				if m.ui.pendingExit {
					m.cancelFn()
					m.confirm.active = false
					return m, m.quitWithHistory()
				}
				m.ui.pendingExit = true
				return m, nil
			}
			// Ctrl+D：弹窗状态下也允许空输入时退出
			if msg.Key().Code == 'd' && (msg.Key().Mod&tea.ModCtrl != 0) {
				if m.input.input.Value() == "" && !m.stream.streaming {
					m.cancelFn()
					m.confirm.active = false
					return m, m.quitWithHistory()
				}
				return m, nil
			}
			// 其他非预期按键忽略，不传给输入框
			return m, nil
		}

		// 重置 pendingExit 只在非 Ctrl+C 时处理
		if !(msg.Key().Code == 'c' && (msg.Key().Mod&tea.ModCtrl != 0)) {
			m.ui.pendingExit = false
		}

		switch {

		case msg.Key().Code == 'c' && (msg.Key().Mod&tea.ModCtrl != 0):
			if m.stream.streaming && m.stream.streamCancel != nil {
				m.stream.streamCancel()
				m.ui.pendingExit = true
				return m, nil
			}
			if m.ui.pendingExit {
				m.cancelFn()
				return m, m.quitWithHistory()
			}
			m.ui.pendingExit = true
			return m, nil

		case msg.Key().Code == 'd' && (msg.Key().Mod&tea.ModCtrl != 0):
			if m.input.input.Value() == "" && !m.stream.streaming {
				m.cancelFn()
				return m, m.quitWithHistory()
			}
			return m, nil

		case msg.Key().Code == tea.KeyEsc:
			if m.stream.streaming && m.stream.streamCancel != nil {
				m.stream.streamCancel()
			}
			return m, nil

		case msg.Key().Code == tea.KeyEnter || msg.Key().Code == tea.KeyReturn:
			if m.stream.streaming || !m.ui.ready {
				return m, nil
			}
			input := strings.TrimSpace(m.input.input.Value())
			m.input.input.SetValue("")
			m.input.historyPos = -1
			m.input.savedBuf = ""
			if input == "" {
				return m, nil
			}
			m.addHistory(input)

			if quit, cmds := m.handleBuiltin(input); quit {
				return m, tea.Quit
			} else if cmds != nil {
				return m, tea.Batch(cmds...)
			}
			if strings.HasPrefix(input, "/") {
				return m, nil
			}

			return m, m.startStream(input)

		case msg.Key().Code == tea.KeyUp:
			if !m.stream.streaming {
				m.historyNavigate(-1)
			}
			return m, nil

		case msg.Key().Code == tea.KeyDown:
			if !m.stream.streaming {
				m.historyNavigate(+1)
			}
			return m, nil

		case msg.Key().Code == 't' && (msg.Key().Mod&tea.ModCtrl != 0):
			m.toggleLastThinkBlock()
			return m, nil

		case msg.Key().Code == 'o' && (msg.Key().Mod&tea.ModCtrl != 0):
			m.toggleLastExpandable()
			return m, nil
		}

	case agentEventMsg:
		cmds := m.handleAgentEvent(msg.ev)
		// 若进入确认状态，暂停 pollEvent，等用户响应后再恢复
		if !m.confirm.active {
			cmds = append(cmds, m.pollEvent())
		}
		return m, tea.Batch(cmds...)

	case streamDoneMsg:
		// 流结束：flush 最后一段未完成的半行到 scrollback
		if tail := m.stream.contentBuf.String(); tail != "" {
			m.addScrollLine(tail, styleAI)
		}
		m.finalizeCurrentThink()
		if msg.err != nil && msg.err != context.Canceled {
			m.addScrollLine("✗ "+msg.err.Error(), styleError)
		}
		if !m.lastLineEmpty {
			m.addScrollLine("", lipgloss.NewStyle())
		}
		// 重置流式状态
		m.stream.streaming = false
		m.stream.streamCancel = nil
		m.stream.evCh = nil
		m.stream.contentBuf.Reset()
		m.reasoning.reasonBuf.Reset()
		m.lastLineEmpty = false
		m.tool.currentTool = ""
		m.tool.toolArgs.Reset()
		m.stream.streamPhase = ""
		m.tool.toolExecMap = make(map[string]*toolExecInfo)
		return m, nil
	}

	if !m.stream.streaming {
		var cmd tea.Cmd
		m.input.input, cmd = m.input.input.Update(msg)
		return m, cmd
	}

	return m, nil
}

// resolveConfirmChoice 将键盘消息解析为确认选择值。
// 返回 (choice, true) 表示有效选择；否则返回 ("", false)。
func (m *model) resolveConfirmChoice(msg tea.KeyMsg) (string, bool) {
	// 多选项模式：数字键选择
	if len(m.confirm.options) > 0 {
		switch {
		default:
			if len(msg.Key().Text) == 1 {
				r := []rune(msg.Key().Text)[0]
				if r >= '1' && r <= '9' {
					idx := int(r - '1')
					if idx < len(m.confirm.options) {
						return m.confirm.options[idx], true
					}
				}
			}
		}
		return "", false
	}

	// 二元确认模式 + allow-in-session
	switch {
	default:
		if len(msg.Key().Text) == 1 {
			switch []rune(msg.Key().Text)[0] {
			case 'y', 'Y':
				return string(agent.ChoiceApprove), true
			case 'n', 'N':
				return string(agent.ChoiceDeny), true
			case 'a', 'A':
				if m.confirm.allowInSession {
					return string(agent.ChoiceAllowInSession), true
				}
			}
		}
		// 允许 Enter 键在二元确认模式下直接确认
		if msg.Key().Code == tea.KeyEnter || msg.Key().Code == tea.KeyReturn {
			if m.confirm.allowInSession {
				return string(agent.ChoiceAllowInSession), true
			}
			return string(agent.ChoiceApprove), true
		}
	}
	return "", false
}

// quitWithHistory 退出时将对话历史输出到终端（仅 alt-screen 模式）。
// Alt-screen 退出后终端清空，用户无法回看对话，
// 通过 tea.Println 将 scrollback 内容持久化到终端滚动缓冲区。
func (m *model) quitWithHistory() tea.Cmd {
	if !m.ui.useAltScreen || len(m.scrollback) == 0 {
		return tea.Quit
	}

	var cmds []tea.Cmd
	for _, line := range m.scrollback {
		rendered := line.render(m.ui.width)
		cmds = append(cmds, tea.Println(rendered))
	}
	cmds = append(cmds, tea.Quit)
	return tea.Sequence(cmds...)
}
