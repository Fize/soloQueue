package tui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/xiaobaitu/soloqueue/internal/agent"
)

// ─── Update ───────────────────────────────────────────────────────────────────

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case sessionReadyMsg:
		if msg.err != nil {
			m.fatalErr = msg.err
			return m, tea.Quit
		}
		m.sess = msg.sess
		m.ready = true
		m.addScrollLine("session ready — type your question or /help", styleDim)
		return m, nil

	case spinnerTickMsg:
		if m.streaming {
			m.spinnerFrame = (m.spinnerFrame + 1) % len(m.spinnerChars)
			return m, spinnerTick()
		}
		return m, nil

	case tea.KeyMsg:
		if !(msg.Key().Code == 'c' && (msg.Key().Mod & tea.ModCtrl != 0)) {
			m.pendingExit = false
		}

		// 确认弹窗激活时劫持键盘输入
		if m.confirm.active {
			choice, ok := m.resolveConfirmChoice(msg)
			if ok {
				if err := m.sess.Agent.Confirm(m.confirm.callID, choice); err != nil {
					m.addScrollLine("✗ confirm error: "+err.Error(), styleError)
				}
				m.confirm.active = false
				// 恢复事件轮询
				return m, tea.Batch(m.pollEvent())
			}
			// 非预期按键忽略，不传给输入框
			return m, nil
		}

		switch {

		case msg.Key().Code == 'c' && (msg.Key().Mod & tea.ModCtrl != 0):
			if m.streaming && m.streamCancel != nil {
				m.streamCancel()
				return m, nil
			}
			if m.pendingExit {
				m.cancelFn()
				return m, m.quitWithHistory()
			}
			m.pendingExit = true
			return m, nil

		case msg.Key().Code == 'd' && (msg.Key().Mod & tea.ModCtrl != 0):
			if m.input.Value() == "" && !m.streaming {
				m.cancelFn()
				return m, m.quitWithHistory()
			}

		case msg.Key().Code == tea.KeyEsc:
			if m.streaming && m.streamCancel != nil {
				m.streamCancel()
			}
			return m, nil

		case msg.Key().Code == tea.KeyEnter || msg.Key().Code == tea.KeyReturn:
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
			if !m.streaming {
				m.historyNavigate(-1)
			}
			return m, nil

		case msg.Key().Code == tea.KeyDown:
			if !m.streaming {
				m.historyNavigate(+1)
			}
			return m, nil

		case msg.Key().Code == 't' && (msg.Key().Mod & tea.ModCtrl != 0):
			m.toggleLastThinkBlock()
			return m, nil

		case msg.Key().Code == 'o' && (msg.Key().Mod & tea.ModCtrl != 0):
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
		if tail := m.contentBuf.String(); tail != "" {
			m.addScrollLine(tail, styleAI)
			m.p.Println(styleAI.Render(tail))
		}
		m.finalizeCurrentThink()
		if msg.err != nil && msg.err != context.Canceled {
			m.addScrollLine("✗ "+msg.err.Error(), styleError)
		}
		if !m.lastLineEmpty {
			m.addScrollLine("", lipgloss.NewStyle())
		}
		// 重置流式状态
		m.streaming = false
		m.streamCancel = nil
		m.evCh = nil
		m.contentBuf.Reset()
		m.reasonBuf.Reset()
		m.lastLineEmpty = false
		m.currentTool = ""
		m.toolArgs.Reset()
		m.streamPhase = ""
		m.toolExecMap = make(map[string]*toolExecInfo)
		return m, nil
	}

	if !m.streaming {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
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
	}
	return "", false
}

// quitWithHistory 退出时将对话历史输出到终端（仅 alt-screen 模式）。
// Alt-screen 退出后终端清空，用户无法回看对话，
// 通过 tea.Println 将 scrollback 内容持久化到终端滚动缓冲区。
func (m *model) quitWithHistory() tea.Cmd {
	if !m.useAltScreen || len(m.scrollback) == 0 {
		return tea.Quit
	}

	var cmds []tea.Cmd
	for _, line := range m.scrollback {
		rendered := line.render(m.width)
		cmds = append(cmds, tea.Println(rendered))
	}
	cmds = append(cmds, tea.Quit)
	return tea.Sequence(cmds...)
}
