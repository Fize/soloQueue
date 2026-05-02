package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

func (m model) renderWorkbenchBody(ly layout) string {
	switch ly.mode {
	case layoutTwoPane:
		main := m.renderConversationPane(ly.mainW, ly.bodyH)
		if m.showAgents {
			left := m.renderLeftPane(ly.leftW, ly.bodyH)
			return lipgloss.JoinHorizontal(lipgloss.Top, left, paneSeparator(ly.bodyH), main)
		}
		return main
	default:
		var parts []string
		parts = append(parts, m.renderConversationPane(ly.mainW, ly.bodyH-1))
		if m.showAgents {
			parts = append(parts, m.sidebar.AgentSummary(ly.width))
		}
		return fitLines(strings.Join(parts, "\n"), ly.bodyH)
	}
}

// renderLeftPane renders the left sidebar with logo on top and AgentInspector below.
func (m model) renderLeftPane(width, height int) string {
	logo := renderSidebarLogo(width, m.cfg.Version)
	inspectorH := height - sidebarLogoLines
	if inspectorH < 4 {
		inspectorH = 4
	}
	inspector := m.sidebar.AgentInspector(width, inspectorH, m, true)
	return lipgloss.JoinVertical(lipgloss.Left, logo, inspector)
}

func (m model) renderConversationPane(width, height int) string {
	if height <= 0 {
		return ""
	}
	content := fitLines(m.viewport.View(), height)
	return paneStyle(width).Render(content)
}

func paneSeparator(height int) string {
	if height <= 0 {
		return ""
	}
	line := paneBorderStyle.Render("│")
	lines := make([]string, height)
	for i := range lines {
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}
