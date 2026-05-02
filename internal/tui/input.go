package tui

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/lipgloss/v2"
)

// ─── Input / textarea utilities ─────────────────────────────────────────────

// visualLineCount computes the number of visual lines a textarea will occupy
// based on its content and width.
func visualLineCount(ta textarea.Model) int {
	lines := strings.Split(ta.Value(), "\n")
	contentWidth := ta.Width()
	if contentWidth <= 0 {
		contentWidth = 1
	}
	count := 0
	for _, line := range lines {
		lineWidth := lipgloss.Width(line)
		if lineWidth == 0 {
			count++
		} else {
			count += (lineWidth + contentWidth - 1) / contentWidth
		}
	}
	return max(1, count)
}
