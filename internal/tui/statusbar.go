package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// ─── Shared header/status rendering helpers ──────────────────────────────────

// renderContextBar renders the context window usage bar: "ctx 42% ███░░░░"
func renderContextBar(pct int) string {
	barWidth := 7
	filled := pct * barWidth / 100
	if filled > barWidth {
		filled = barWidth
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	label := fmt.Sprintf("ctx %d%% ", pct)
	return contextTokenStyle(pct).Render(label + bar)
}

// phaseStyle returns the style for the generation phase text.
func phaseStyle(p genPhase) lipgloss.Style {
	switch p {
	case phaseThinking:
		return lipgloss.NewStyle().Foreground(colorThinking).Bold(true)
	case phaseGenerating:
		return lipgloss.NewStyle().Foreground(colorInfo).Bold(true)
	case phaseToolCall:
		return lipgloss.NewStyle().Foreground(colorWarning).Bold(true)
	default:
		return foldedStyle
	}
}
