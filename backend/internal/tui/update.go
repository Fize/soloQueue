package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
		if msg.Type != tea.KeyCtrlC {
			m.pendingExit = false
		}

		switch msg.Type {

		case tea.KeyCtrlC:
			if m.streaming && m.streamCancel != nil {
				m.streamCancel()
				return m, nil
			}
			if m.pendingExit {
				m.cancelFn()
				return m, tea.Quit
			}
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

			if quit, cmds := m.handleBuiltin(input); quit {
				return m, tea.Quit
			} else if cmds != nil {
				return m, tea.Batch(cmds...)
			}
			if strings.HasPrefix(input, "/") {
				return m, nil
			}

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

		case tea.KeyCtrlT:
			if len(m.reasonBlocks) > 0 {
				last := &m.reasonBlocks[len(m.reasonBlocks)-1]
				last.expanded = !last.expanded
			}
			return m, nil

		case tea.KeyCtrlO:
			m.toggleLastExpandable()
			return m, nil
		}

	case agentEventMsg:
		cmds := m.handleAgentEvent(msg.ev)
		cmds = append(cmds, m.pollEvent())
		return m, tea.Batch(cmds...)

	case streamDoneMsg:
		// 流结束：flush 最后一段未完成的半行到 scrollback
		if tail := m.contentBuf.String(); tail != "" {
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
