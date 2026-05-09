package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

func (m model) renderWorkbenchBody(ly layout) string {
	return m.renderConversationPane(ly.mainW, ly.bodyH)
}

func (m model) renderConversationPane(width, height int) string {
	if height <= 0 {
		return ""
	}
	content := fitLines(m.viewport.View(), height)
	return paneStyle(width).Render(content)
}

func paneStyle(width int) lipgloss.Style {
	return lipgloss.NewStyle().Width(max(width-2, 1)).Padding(0, 1)
}

func fitLines(s string, maxLines int) string {
	if maxLines <= 0 {
		return ""
	}
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > maxLines {
		lines = lines[:maxLines]
	}
	for len(lines) < maxLines {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}
