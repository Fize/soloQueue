package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ─── Built-in commands ────────────────────────────────────────────────────────

func (m *model) handleBuiltin(input string) (quit bool) {
	cmd := strings.ToLower(strings.TrimSpace(input))
	switch cmd {
	case "/quit", "/exit", "/q":
		return true

	case "/help", "/?":
		m.messages = append(m.messages, message{
			role:    "agent",
			content: dimStyle.Render("Commands: /help /clear /history /version /quit"),
		})

	case "/clear":
		m.messages = nil

	case "/version":
		m.messages = append(m.messages, message{
			role:    "agent",
			content: lipgloss.NewStyle().Bold(true).Render("SoloQueue " + m.cfg.Version),
		})

	case "/history":
		m.historyCmds()

	default:
		if strings.HasPrefix(input, "/") {
			m.messages = append(m.messages, message{
				role:    "agent",
				content: errorStyle.Render("✗ Unknown command: " + input + ". Type /help"),
			})
		}
	}
	return false
}

func (m *model) historyCmds() {
	if len(m.history) == 0 {
		m.messages = append(m.messages, message{
			role:    "agent",
			content: dimStyle.Render("(no history yet)"),
		})
		return
	}
	var sb strings.Builder
	sb.WriteString(lipgloss.NewStyle().Bold(true).Render("History:"))
	start := 0
	if len(m.history) > 20 {
		start = len(m.history) - 20
	}
	for i := start; i < len(m.history); i++ {
		sb.WriteString(fmt.Sprintf("\n  %3d  %s", i+1, truncate(m.history[i], 72)))
	}
	m.messages = append(m.messages, message{
		role:    "agent",
		content: sb.String(),
	})
}
