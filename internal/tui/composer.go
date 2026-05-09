package tui

import (
	"fmt"
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

func isMultilineInput(input string) bool {
	return strings.Contains(input, "\n")
}

func isSlashCommandInput(input string) bool {
	trimmed := strings.TrimSpace(input)
	return !isMultilineInput(trimmed) && strings.HasPrefix(trimmed, "/")
}

func (m model) renderComposer(ly layout) string {
	if len(m.confirmQueue) > 0 {
		return m.renderConfirmDialog(ly)
	}
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

	return input
}

func (m model) renderConfirmDialog(ly layout) string {
	cs := m.confirmQueue[0]
	var sb strings.Builder

	// Prompt line (dimmed, with ? prefix)
	queueHint := ""
	if len(m.confirmQueue) > 1 {
		queueHint = fmt.Sprintf(" (%d/%d)", 1, len(m.confirmQueue))
	}
	prompt := dimStyle.Render("? " + cs.prompt + queueHint)
	sb.WriteString(prompt + "\n")

	// Options with selection highlight
	for i, opt := range cs.options {
		line := "  " + opt
		if i == cs.selected {
			line = "> " + confirmHighlight.Render(opt)
		}
		if lipgloss.Width(line) < ly.mainW {
			line += strings.Repeat(" ", ly.mainW-lipgloss.Width(line))
		}
		sb.WriteString(line + "\n")
	}

	result := sb.String()
	return result
}
