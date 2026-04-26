package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ─── Built-in commands ────────────────────────────────────────────────────────

func (m *model) handleBuiltin(input string) (bool, tea.Cmd) {
	name := strings.ToLower(strings.TrimSpace(input))
	switch name {
	case "/quit", "/exit", "/q":
		return true, nil

	case "/help", "/?":
		text := agentStyle.Render("Agent:") + "\n" + dimStyle.Render("Commands: /help /clear /history /version /quit") + "\n\n"
		return false, tea.Printf("%s", text)

	case "/clear":
		// Cancel any active stream
		if m.isGenerating {
			if m.streamCancel != nil {
				m.streamCancel()
			}
			m.isGenerating = false
			m.current = nil
		}
		m.messages = nil
		m.history = nil
		// Clear scrollback + screen + cursor home
		return false, tea.Printf("\x1b[3J\x1b[2J\x1b[H")

	case "/version":
		text := agentStyle.Render("Agent:") + "\n" + lipgloss.NewStyle().Bold(true).Render("SoloQueue "+m.cfg.Version) + "\n\n"
		return false, tea.Printf("%s", text)

	case "/history":
		return false, m.historyPrintf()

	default:
		if strings.HasPrefix(input, "/") {
			text := agentStyle.Render("Agent:") + "\n" + errorStyle.Render("✗ Unknown command: "+input+". Type /help") + "\n\n"
			return false, tea.Printf("%s", text)
		}
	}
	return false, nil
}

func (m *model) historyPrintf() tea.Cmd {
	var sb strings.Builder
	sb.WriteString(agentStyle.Render("Agent:") + "\n")
	if len(m.history) == 0 {
		sb.WriteString(dimStyle.Render("(no history yet)") + "\n\n")
		return tea.Printf("%s", sb.String())
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
	return tea.Printf("%s", sb.String())
}
