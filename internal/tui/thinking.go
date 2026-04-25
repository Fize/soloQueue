package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// ─── Thinking block rendering ─────────────────────────────────────────────────
//
// 使用与 tool 结果完全一致的 expandable scrollLine 模式：
//   - 推理阶段：只缓冲到 reasonBuf，不写 scrollback（状态栏已显示 "Thinking…"）
//   - 推理结束：追加一条 expandable=true 的 scrollLine 到 scrollback 末尾
//   - Ctrl+T：扫描找到 💭 开头的 expandable 行，toggle expanded 标志
//   - View()：自动根据 expanded 标志渲染折叠或展开（复用已有逻辑）
//
// 零索引追踪、零位置管理、零删除/插入操作。

func (m *model) startNewThinkBlock() {
	m.reasoning.reasonBuf.Reset()
	m.reasoning.reasonBlocks = append(m.reasoning.reasonBlocks, thinkBlock{expanded: false})
	m.reasoning.curThinkIdx = len(m.reasoning.reasonBlocks) - 1
	m.stream.streamPhase = "thinking"
}

func (m *model) appendReasoning(delta string) {
	if m.reasoning.curThinkIdx < 0 {
		m.startNewThinkBlock()
	}
	m.reasoning.reasonBuf.WriteString(delta)
}

// finalizeCurrentThink 推理结束时，将 think 块作为 expandable scrollLine 追加到 scrollback。
func (m *model) finalizeCurrentThink() {
	if m.reasoning.curThinkIdx < 0 || m.reasoning.curThinkIdx >= len(m.reasoning.reasonBlocks) {
		return
	}

	raw := m.reasoning.reasonBuf.String()
	var lines []string
	for _, l := range strings.Split(raw, "\n") {
		if trimmed := strings.TrimSpace(l); trimmed != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		m.reasoning.curThinkIdx = -1
		m.reasoning.reasonBuf.Reset()
		return
	}

	tb := &m.reasoning.reasonBlocks[m.reasoning.curThinkIdx]
	tb.lines = lines

	// 构造展开时的完整内容行
	var fullLines []string
	for _, line := range lines {
		wrapped := wrapLine(line, m.ui.width-4)
		fullLines = append(fullLines, wrapped...)
	}

	// 折叠标题
	title := fmt.Sprintf("💭 Thinking for %d lines · Ctrl+T", len(lines))

	// 作为 expandable 行追加到 scrollback（与 tool 结果 renderToolDoneBlock 完全一致的模式）
	if !m.lastLineEmpty {
		m.addScrollLine("", lipgloss.NewStyle())
	}
	m.appendScrollback(scrollLine{
		content:    title,
		style:      styleThinkTitle,
		expandable: true,
		expanded:   false,
		fullLines:  fullLines,
		fullStyle:  styleThinkText,
	})
	m.lastLineEmpty = false

	m.reasoning.curThinkIdx = -1
	m.reasoning.reasonBuf.Reset()
}
