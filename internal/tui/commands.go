package tui

import (
	"fmt"
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
	switch name {
	case "/quit", "/exit", "/q":
		return true, nil

	case "/help", "/?":
		text := agentStyle.Render("Solo:") + "\n" + dimStyle.Render("Commands: /help /clear /history /version /quit") + "\n\n"
		return false, printfWithClear("%s", text)

	case "/clear":
		// Cancel any active stream
		if m.isGenerating {
			if m.streamCancel != nil {
				m.streamCancel()
			}
			m.resetGenState()
			m.current = nil
		}
		m.messages = nil
		m.history = nil
		// Clear scrollback + screen + cursor home
		return false, tea.Printf("\x1b[3J\x1b[2J\x1b[H")

	case "/version":
		text := agentStyle.Render("Solo:") + "\n" + lipgloss.NewStyle().Bold(true).Render("SoloQueue "+m.cfg.Version) + "\n\n"
		return false, printfWithClear("%s", text)

	case "/history":
		return false, m.historyPrintf()

	default:
		if strings.HasPrefix(input, "/") {
			text := agentStyle.Render("Solo:") + "\n" + errorStyle.Render("✗ Unknown command: "+input+". Type /help") + "\n\n"
			return false, printfWithClear("%s", text)
		}
	}
	return false, nil
}

func (m *model) historyPrintf() tea.Cmd {
	var sb strings.Builder
	sb.WriteString(agentStyle.Render("Solo:") + "\n")
	if len(m.history) == 0 {
		sb.WriteString(dimStyle.Render("(no history yet)") + "\n\n")
		return printfWithClear("%s", sb.String())
	}
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render("History:"))
	start := 0
	if len(m.history) > 20 {
		start = len(m.history) - 20
	}
	for i := start; i < len(m.history); i++ {
		sb.WriteString(fmt.Sprintf("\n  %3d  %s", i+1, truncate(m.history[i], 72)))
	}
	sb.WriteString("\n\n")
	return printfWithClear("%s", sb.String())
}
