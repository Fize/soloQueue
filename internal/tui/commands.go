package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// printfOnce prints above the current View without ClearScreen.
// Use only at Init when the cellbuf is still empty — no ghost-content risk.
func printfOnce(format string, args ...any) tea.Cmd {
	return tea.Printf(format, args...)
}

// printfWithClear wraps tea.Printf with a ClearScreen to keep the v2
// cursed renderer's cellbuf in sync. Without ClearScreen after Printf,
// the renderer's diff-based incremental update produces ghost content
// because Printf's insertAbove bypasses the cellbuf.
func printfWithClear(format string, args ...any) tea.Cmd {
	return tea.Sequence(
		tea.Printf(format, args...),
		func() tea.Msg { return tea.ClearScreen() },
	)
}

// ─── Built-in commands ────────────────────────────────────────────────────────

func (m *model) handleBuiltin(input string) (bool, tea.Cmd) {
	name := strings.ToLower(strings.TrimSpace(input))

	// Flush logo to scrollback on first command
	var logoCmd tea.Cmd
	if !m.logoShown {
		m.logoShown = true
		logoCmd = printfOnce("%s", renderLogo(m.cfg.Version))
	}

	var cmd tea.Cmd
	switch name {
	case "/quit", "/exit", "/q":
		return true, nil

	case "/help", "/?":
		text := agentStyle.Render("Solo:") + "\n" + dimStyle.Render("Commands: /help /clear /version /quit") + "\n\n"
		cmd = printfWithClear("%s", text)

	case "/clear":
		// Cancel any active stream
		if m.isGenerating {
			if m.streamCancel != nil {
				m.streamCancel()
			}
			m.resetGenState()
			m.current = nil
		}
		// 清空上下文：追加 /clear 事件到 timeline，重置 ContextWindow
		if m.sess != nil {
			_ = m.sess.Clear()
		}
		m.messages = nil
		m.history = nil
		m.historyIdx = 0
		m.historyDraft = ""
		text := clearStatusStyle.Render("◆  context cleared") + "\n\n"
		cmd = printfWithClear("%s", text)

	case "/version":
		text := agentStyle.Render("Solo:") + "\n" + lipgloss.NewStyle().Bold(true).Render("SoloQueue "+m.cfg.Version) + "\n\n"
		cmd = printfWithClear("%s", text)

	default:
		if strings.HasPrefix(input, "/") {
			text := agentStyle.Render("Solo:") + "\n" + errorStyle.Render("✗ Unknown command: "+input+". Type /help") + "\n\n"
			cmd = printfWithClear("%s", text)
		}
	}

	if cmd == nil {
		return false, nil
	}
	if logoCmd != nil {
		return false, tea.Sequence(logoCmd, cmd)
	}
	return false, cmd
}
