package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ─── Thinking block rendering ─────────────────────────────────────────────────

func (m *model) startNewThinkBlock() {
	m.reasonBuf.Reset()
	m.reasonBlocks = append(m.reasonBlocks, thinkBlock{expanded: false})
	m.curThinkIdx = len(m.reasonBlocks) - 1
	m.streamPhase = "thinking"
}

func (m *model) appendReasoning(delta string) {
	if m.curThinkIdx < 0 {
		m.startNewThinkBlock()
	}
	m.reasonBuf.WriteString(delta)
}

func (m *model) finalizeCurrentThink() {
	if m.curThinkIdx < 0 || m.curThinkIdx >= len(m.reasonBlocks) {
		return
	}

	raw := m.reasonBuf.String()
	var lines []string
	for _, l := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(l)
		if trimmed != "" {
			lines = append(lines, l)
		}
	}
	m.reasonBlocks[m.curThinkIdx].lines = lines

	m.renderThinkBlock(m.curThinkIdx)

	m.curThinkIdx = -1
	m.reasonBuf.Reset()
}

func (m *model) renderThinkBlock(idx int) {
	if idx < 0 || idx >= len(m.reasonBlocks) {
		return
	}
	tb := &m.reasonBlocks[idx]
	lineCount := len(tb.lines)

	if lineCount == 0 {
		return
	}

	// 如果是重新渲染，先移除旧内容（使用之前记录的 scrollCount）
	if tb.scrollStart >= 0 && tb.scrollCount > 0 && tb.scrollStart+tb.scrollCount <= len(m.scrollback) {
		m.scrollback = append(m.scrollback[:tb.scrollStart], m.scrollback[tb.scrollStart+tb.scrollCount:]...)
	}

	// 记录当前 scrollback 起始位置
	tb.scrollStart = len(m.scrollback)
	startLen := len(m.scrollback) // 记录渲染前的长度，用于计算 scrollCount

	if !m.lastLineEmpty {
		m.addScrollLine("", lipgloss.NewStyle())
	}

	if tb.expanded {
		m.addScrollLine("💭 Thinking", styleThinkIcon)
		for _, line := range tb.lines {
			wrapped := wrapLine(line, m.width-4)
			for _, wl := range wrapped {
				m.addScrollLine("  "+wl, styleThinkText)
			}
		}
	} else {
		m.addScrollLine(
			"💭"+fmt.Sprintf(" Thinking for %d lines", lineCount)+" · Ctrl+T",
			styleThinkTitle,
		)
	}

	// 记录本次渲染占用的行数
	tb.scrollCount = len(m.scrollback) - startLen

	m.lastLineEmpty = false
}
