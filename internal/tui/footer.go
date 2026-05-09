package tui

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
		text = "enter send · ctrl+j newline · ^Y copy · /help"
	}
	if ly.width <= 0 {
		return ""
	}
	return footerStyle.Width(ly.width).Render(truncate(text, ly.width-1))
}
