package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

type focusMode int

const (
	focusComposer focusMode = iota
	focusTranscript
	focusAgents
	focusCopy
)

func composerLineCountForValue(value string, width int, maxLines int) int {
	if width <= 0 {
		width = 1
	}
	if maxLines <= 0 {
		maxLines = 1
	}
	lines := strings.Split(value, "\n")
	count := 0
	for _, line := range lines {
		lineWidth := lipgloss.Width(line)
		if lineWidth == 0 {
			count++
		} else {
			count += (lineWidth + width - 1) / width
		}
	}
	if count < 1 {
		count = 1
	}
	if count > maxLines {
		count = maxLines
	}
	return count
}

func composerLineCount(value string, width int, maxLines int) int {
	return composerLineCountForValue(value, width, maxLines)
}

func isMultilineInput(input string) bool {
	return strings.Contains(input, "\n")
}

func isSlashCommandInput(input string) bool {
	trimmed := strings.TrimSpace(input)
	return !isMultilineInput(trimmed) && strings.HasPrefix(trimmed, "/")
}

func (m model) renderComposer(ly layout) string {
	line := lipgloss.NewStyle().Foreground(colorBorder).Render(strings.Repeat("─", max(ly.mainW, 0)))
	title := paneTitleStyle.Render(" Composer ")

	// Ensure the textarea uses exact width by trimming/padding as needed
	// (BubbleTea's textarea doesn't reliably fill space on its own)
	input := m.textArea.View()
	lines := strings.Split(input, "\n")
	for i, l := range lines {
		if lipgloss.Width(l) < ly.mainW {
			lines[i] = l + strings.Repeat(" ", ly.mainW-lipgloss.Width(l))
		}
	}
	input = strings.Join(lines, "\n")

	composerBlock := lipgloss.JoinVertical(lipgloss.Left, line, title+" "+dimStyle.Render("enter send · shift+enter newline"), input)

	if ly.mode == layoutTwoPane && m.showAgents {
		// Composer matches the main Workspace, left is filled with spaces to align with sidebar + separator
		var padding strings.Builder
		padding.WriteString(strings.Repeat(" ", ly.leftW))
		padding.WriteString(paneBorderStyle.Render("│"))

		lines = strings.Split(composerBlock, "\n")
		for i, l := range lines {
			// For the top border line, continue the separator upwards?
			// No, just keep the standard separator structure.
			lines[i] = padding.String() + l
		}
		return strings.Join(lines, "\n")
	}
	return composerBlock
}
