package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ─── Built-in commands ────────────────────────────────────────────────────────

func (m *model) handleBuiltin(input string) (quit bool, cmds []tea.Cmd) {
	cmd := strings.ToLower(strings.TrimSpace(input))
	switch cmd {
	case "/quit", "/exit", "/q":
		return true, nil

	case "/help", "/?":
		m.addScrollLine("Commands: /help /clear /history /version /quit", styleDim)
		m.addScrollLine("Shortcuts: Ctrl+C (exit) · Ctrl+T (toggle think) · Ctrl+O (expand)", styleDim)
		m.addScrollLine("", lipgloss.NewStyle())
		if !m.ui.useAltScreen {
			m.addScrollLine("Mode: inline (scrollback preserved)", styleDim)
		} else {
			m.addScrollLine("Mode: fullscreen (alt-screen)", styleDim)
		}
		m.addScrollLine("  Toggle: ALT_SCREEN=1 go run main.go", styleDim)
		m.addScrollLine("", lipgloss.NewStyle())

	case "/clear":
		m.scrollback = nil
		cmds = append(cmds, func() tea.Msg { return nil })

	case "/version":
		m.addScrollLine("SoloQueue "+m.cfg.Version, lipgloss.NewStyle().Bold(true))
		m.addScrollLine("", lipgloss.NewStyle())

	case "/history":
		m.historyCmds()

	default:
		if strings.HasPrefix(input, "/") {
			m.addScrollLine("✗ Unknown command: "+input+". Type /help", styleError)
			m.addScrollLine("", lipgloss.NewStyle())
		}
	}
	return false, cmds
}

func (m *model) historyCmds() {
	if len(m.input.history) == 0 {
		m.addScrollLine("(no history yet)", styleDim)
		m.addScrollLine("", lipgloss.NewStyle())
		return
	}
	m.addScrollLine("History:", lipgloss.NewStyle().Bold(true))
	start := 0
	if len(m.input.history) > 20 {
		start = len(m.input.history) - 20
	}
	for i := start; i < len(m.input.history); i++ {
		m.addScrollLine(fmt.Sprintf("  %3d  %s", i+1, truncate(m.input.history[i], 72)), styleDim)
	}
	m.addScrollLine("", lipgloss.NewStyle())
}

// ─── History ──────────────────────────────────────────────────────────────────

func (m *model) addHistory(line string) {
	if line == "" {
		return
	}
	if len(m.input.history) > 0 && m.input.history[len(m.input.history)-1] == line {
		return
	}
	m.input.history = append(m.input.history, line)
}

func (m *model) historyNavigate(dir int) {
	if len(m.input.history) == 0 {
		return
	}
	if m.input.historyPos == -1 {
		m.input.savedBuf = m.input.input.Value()
		m.input.historyPos = len(m.input.history)
	}
	newPos := m.input.historyPos + dir
	if newPos < 0 {
		return
	}
	if newPos >= len(m.input.history) {
		m.input.historyPos = -1
		m.input.input.SetValue(m.input.savedBuf)
		m.input.input.CursorEnd()
		return
	}
	m.input.historyPos = newPos
	m.input.input.SetValue(m.input.history[m.input.historyPos])
	m.input.input.CursorEnd()
}
