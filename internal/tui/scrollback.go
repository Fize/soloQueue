package tui

import (
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ─── Scrollback management ────────────────────────────────────────────────────

func (m *model) addScrollLine(content string, style lipgloss.Style) {
	m.scrollback = append(m.scrollback, scrollLine{content: content, style: style})
	m.lastLineEmpty = (content == "")

	if !m.useAltScreen {
		m.pendingPrintLines = append(m.pendingPrintLines, content)
	}

	if len(m.scrollback) > maxScrollbackLines {
		keep := maxScrollbackLines * 9 / 10
		m.scrollback = m.scrollback[len(m.scrollback)-keep:]
	}
	// 超过上限时截断旧数据，保留前 10% 作为上下文缓冲
}

// toggleLastExpandable 切换最近一个可展开块的展开/折叠状态
func (m *model) toggleLastExpandable() {
	for i := len(m.scrollback) - 1; i >= 0; i-- {
		if m.scrollback[i].expandable {
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

// flushPrintCmds 将 pendingPrintLines 转换为 tea.Println Cmd 并清空缓冲区。
// 在 Update 的 defer 中调用，确保 Update 内对 pendingPrintLines 的写入被安全地
// 通过 handleCommands goroutine 发送到 p.msgs，避免直接写无缓冲通道导致的死锁。
func (m *model) flushPrintCmds() tea.Cmd {
	if len(m.pendingPrintLines) == 0 {
		return nil
	}
	lines := m.pendingPrintLines
	m.pendingPrintLines = nil
	var cmds []tea.Cmd
	for _, line := range lines {
		cmds = append(cmds, tea.Println(line))
	}
	return tea.Batch(cmds...)
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
	// For simplicity, render first line with style
	return sl.style.Render(lines[0])
}
