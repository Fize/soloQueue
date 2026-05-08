package tui

import (
	"strings"
	"time"

	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
)

// ─── Message rendering ──────────────────────────────────────────────────────

// formatTimestamp returns "Name · HH:MM" or just "Name" if ts is zero.
func formatTimestamp(name string, ts time.Time) string {
	if ts.IsZero() {
		return name
	}
	return name + " · " + ts.Format("15:04")
}

// renderUserMessage renders a user message with right-aligned green text.
// If msg.isHistory is true, uses muted style (gray) instead.
func renderUserMessage(msg message, vpWidth int) string {
	// Timestamp label: "You · 14:30" — right-aligned above the message
	ts := formatTimestamp("You", msg.timestamp)
	tsLine := timestampStyle.Render(ts)
	tsW := lipgloss.Width(tsLine)
	tsPad := vpWidth - tsW
	if tsPad < 0 {
		tsPad = 0
	}

	// Right-align each line of the styled text
	var text string
	if msg.isHistory {
		text = mutedStyle.Render(msg.content)
	} else {
		text = userStyle.Render(msg.content)
	}
	lines := strings.Split(text, "\n")
	var sb strings.Builder
	for i, line := range lines {
		lineW := lipgloss.Width(line)
		pad := vpWidth - lineW
		if pad < 0 {
			pad = 0
		}
		sb.WriteString(strings.Repeat(" ", pad) + line)
		if i < len(lines)-1 {
			sb.WriteString("\n")
		}
	}
	return strings.Repeat(" ", tsPad) + tsLine + "\n" + sb.String() + "\n\n"
}

func (m *model) renderContent(text string) string {
	if m.renderer == nil {
		return contentStyle.Render(text)
	}
	// Prepend "\n" so the real content never starts at column 0, preventing
	// glamour from clipping the first CJK character in certain terminals.
	rendered, err := m.renderer.Render("\n" + text)
	if err != nil {
		return contentStyle.Render(text)
	}
	return strings.TrimLeft(rendered, "\n")
}

func (m *model) renderMessage(msg message) string {
	vpWidth := m.viewport.Width()
	switch msg.role {
	case "user":
		return renderUserMessage(msg, vpWidth)
	case "agent":
		ts := formatTimestamp("Solo", msg.timestamp)
		tsLine := timestampStyle.Render(ts)
		body := m.renderAgentMessageBody(msg)
		return tsLine + "\n" + body + "\n"
	}
	return ""
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
			text := strings.TrimSpace(entry.text)
			if text != "" {
				if msg.isHistory {
					sb.WriteString(mutedStyle.Render("▎ Thinking (history)"))
				} else {
					sb.WriteString(thinkLabelStyle + "\n")
				}
				// Collapse consecutive blank lines in thinking text for compact display
				lines := strings.Split(text, "\n")
				var compacted []string
				for _, line := range lines {
					if strings.TrimSpace(line) == "" && len(compacted) > 0 && strings.TrimSpace(compacted[len(compacted)-1]) == "" {
						continue // skip consecutive blank lines
					}
					compacted = append(compacted, line)
				}
				text = strings.TrimRight(strings.Join(compacted, "\n"), "\n")
				maxThinkW := m.viewport.Width()
				if maxThinkW > 80 {
					maxThinkW = 80
				}
				if msg.isHistory {
					sb.WriteString(mutedStyle.Width(maxThinkW).Render(text) + "\n")
				} else {
					sb.WriteString(thinkStyle.Width(maxThinkW).Render(text) + "\n")
				}
			}
		case timelineContent:
			if msg.isHistory {
				sb.WriteString(mutedStyle.Render(entry.text) + "\n")
			} else {
				sb.WriteString(m.renderContent(entry.text) + "\n")
			}
		case timelineTool:
			if entry.tool != nil {
				if msg.isHistory {
					sb.WriteString(mutedStyle.Render(entry.tool.name) + "\n")
					sb.WriteString(mutedStyle.Render(formatToolBlock(*entry.tool)) + "\n")
				} else {
					sb.WriteString(renderToolLabel(entry.tool.name) + "\n")
					sb.WriteString(toolCollapsedStyle.Render(formatToolBlock(*entry.tool)) + "\n")
				}
			}
		}
		lastKind = entry.kind
	}
	// Buffer content is the most recently arrived text — always append at the end
	if msg.content != "" {
		if lastKind >= 0 && lastKind != timelineContent {
			sb.WriteString("\n")
		}
		if msg.isHistory {
			sb.WriteString(mutedStyle.Render(msg.content) + "\n")
		} else {
			sb.WriteString(m.renderContent(msg.content) + "\n")
		}
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
	// Wrap to viewport content width (mainW - 4 for paneStyle Width+Padding).
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
