package tui

import (
	"strings"

	"charm.land/glamour/v2"
)

// ─── Message rendering ──────────────────────────────────────────────────────

func renderUserMessage(msg message) string {
	return userStyle.Render("❯ "+msg.content) + "\n\n"
}

func (m *model) renderContent(text string) string {
	if m.renderer == nil {
		return contentStyle.Render(text)
	}
	rendered, err := m.renderer.Render(text)
	if err != nil {
		return contentStyle.Render(text)
	}
	return strings.Trim(rendered, "\n")
}

func (m *model) renderMessage(msg message) string {
	var sb strings.Builder
	switch msg.role {
	case "user":
		sb.WriteString(renderUserMessage(msg))
	case "agent":
		sb.WriteString(agentStyle.Render("Solo:") + "\n")
		sb.WriteString(m.renderAgentMessageBody(msg))
		sb.WriteString("\n")
	}
	return sb.String()
}

func (m *model) renderAgentMessage(msg message) string {
	return agentStyle.Render("Solo:") + "\n" + m.renderAgentMessageBody(msg)
}

func (m *model) renderAgentMessageBody(msg message) string {
	var sb strings.Builder
	var lastKind timelineKind = -1
	for _, entry := range msg.timeline {
		if lastKind >= 0 && lastKind != entry.kind {
			sb.WriteString("\n")
		}
		if lastKind == timelineTool && entry.kind == timelineTool {
			sb.WriteString("\n")
		}
		switch entry.kind {
		case timelineThinking:
			sb.WriteString(thinkLabelStyle + "\n")
			sb.WriteString(thinkStyle.Render(entry.text) + "\n")
		case timelineContent:
			sb.WriteString(m.renderContent(entry.text) + "\n")
		case timelineTool:
			if entry.tool != nil {
				sb.WriteString(toolLabelStyle + "\n")
				sb.WriteString(toolCollapsedStyle.Render(formatToolBlock(*entry.tool)) + "\n")
			}
		}
		lastKind = entry.kind
	}
	if msg.content != "" {
		if lastKind >= 0 && lastKind != timelineContent {
			sb.WriteString("\n")
		}
		sb.WriteString(m.renderContent(msg.content) + "\n")
	}
	return sb.String()
}

// invalidateMessageCache marks all historical messages as needing re-render.
// Called when the renderer changes (e.g. window resize alters wrap width).
func (m *model) invalidateMessageCache() {
	for i := range m.messages {
		m.messages[i].dirty = true
	}
}

// ─── Renderer ───────────────────────────────────────────────────────────────

func (m *model) newRenderer() *glamour.TermRenderer {
	// Wrap to viewport content width (mainW - 2 for paneStyle padding).
	wrapWidth := m.viewport.Width()
	if wrapWidth <= 0 {
		wrapWidth = 78
	}
	var styleJSON []byte
	if m.darkBg {
		styleJSON = []byte(darkStyleJSON)
	} else {
		styleJSON = []byte(lightStyleJSON)
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStylesFromJSONBytes(styleJSON),
		glamour.WithWordWrap(wrapWidth),
	)
	if err != nil {
		return nil
	}
	return r
}
