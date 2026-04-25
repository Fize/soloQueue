package tui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// ─── Scrollback management ────────────────────────────────────────────────────

func (m *model) addScrollLine(content string, style lipgloss.Style) {
	m.appendScrollback(scrollLine{content: content, style: style})
	m.lastLineEmpty = (content == "")
}

// appendScrollback 统一追加 scrollLine 并执行上限裁剪。
// thinking 和 tool 渲染应使用此函数，避免直接操作 m.scrollback 绕过上限。
func (m *model) appendScrollback(line scrollLine) {
	m.scrollback = append(m.scrollback, line)
	if len(m.scrollback) > m.cfg.MaxScrollbackLines {
		keep := m.cfg.MaxScrollbackLines * 9 / 10
		m.scrollback = m.scrollback[len(m.scrollback)-keep:]
	}
}

// toggleLastExpandable 切换最近一个可展开块的展开/折叠状态
func (m *model) toggleLastExpandable() {
	m.toggleExpandableByFilter(func(sl scrollLine) bool {
		return sl.expandable
	})
}

// toggleLastThinkBlock 切换最近一个 thinking 块的展开/折叠状态
func (m *model) toggleLastThinkBlock() {
	m.toggleExpandableByFilter(func(sl scrollLine) bool {
		return sl.expandable && strings.HasPrefix(sl.content, "💭")
	})
}

// toggleExpandableByFilter 根据过滤器切换最近一个可展开块的状态（通用函数）
func (m *model) toggleExpandableByFilter(filter func(scrollLine) bool) {
	for i := len(m.scrollback) - 1; i >= 0; i-- {
		if filter(m.scrollback[i]) {
			m.scrollback[i].expanded = !m.scrollback[i].expanded
			return
		}
	}
}

func (m *model) getScrollLines(maxLines int) []scrollLine {
	if len(m.scrollback) <= maxLines {
		return m.scrollback
	}
	return m.scrollback[len(m.scrollback)-maxLines:]
}

func (sl scrollLine) render(width int) string {
	if sl.content == "" {
		return ""
	}
	// Wrap content if needed
	lines := wrapLine(sl.content, width)
	if len(lines) == 0 {
		return ""
	}
	// Render all lines with style
	var sb strings.Builder
	for i, line := range lines {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(sl.style.Render(line))
	}
	return sb.String()
}
