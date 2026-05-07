package tui

import "strings"

func (m model) renderFooter(ly layout) string {
	var text string
	switch {
	case m.copyMode:
		text = "COPY MODE · select text in terminal · esc return"
	case len(m.confirmQueue) > 0:
		text = "↑/↓ choose · enter confirm · esc back"
	case m.isGenerating:
		text = "esc interrupt · ^C cancel · ^Y copy"
	default:
		if ly.mode == layoutCompact {
			text = "enter send · ctrl+j newline · ^A agents · ^Y copy · /help"
		} else {
			text = "enter send · ctrl+j newline · ^A agents · ^Y copy · /help"
		}
	}
	if ly.width <= 0 {
		return ""
	}
	rendered := footerStyle.Width(ly.width).Render(truncate(text, ly.width-1))

	if ly.mode == layoutTwoPane && m.showAgents {
		pad := strings.Repeat(" ", ly.leftW) + paneBorderStyle.Render("│")
		lines := strings.Split(rendered, "\n")
		for i := range lines {
			lines[i] = pad + lines[i]
		}
		return strings.Join(lines, "\n")
	}
	return rendered
}
