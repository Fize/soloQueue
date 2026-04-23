package tui

import (
	"fmt"
	"strings"
	"time"
)

// ─── View ─────────────────────────────────────────────────────────────────────

// View 渲染当前界面帧。
//
// 布局从上到下：
//   1. 滚动区（最后 maxScroll 行，自然追加不填充）
//   2. 空行（输出区与下方固定区的分隔）
//   3. 状态行（streaming 时显示 spinner + 阶段信息）
//   4. 空行（状态行与输入框的分隔）
//   5. 输入框（固定底部）
//
// bubbletea 非 alt-screen 模式下自己管理光标覆写，
// 我们只需要：不填充空行、不手动干预光标。
func (m *model) View() string {
	if m.fatalErr != nil {
		return styleError.Render("fatal: "+m.fatalErr.Error()) + "\n"
	}

	if m.width == 0 || m.height == 0 {
		return ""
	}

	var sb strings.Builder

	// 固定底部区域占用的行数（始终包含输入框及上下空行）
	fixedLines := 2 // 空行 + 输入框
	if m.streaming {
		fixedLines = 4 // 空行 + 状态行 + 空行 + 输入框
	}

	// 滚动区最多显示的行数（留出空间给固定区）
	maxScroll := m.height - fixedLines
	if maxScroll < 2 {
		maxScroll = 2
	}

	// 1. 滚动区：取最后 maxScroll 行，自然渲染（不做填充/清屏）
	scrollLines := m.getScrollLines(maxScroll)
	for _, line := range scrollLines {
		rendered := line.render(m.width)
		sb.WriteString(rendered)
		sb.WriteString("\n")
		// 展开内容
		if line.expanded && len(line.fullLines) > 0 {
			for _, fl := range line.fullLines {
				wrapped := wrapLine(fl, m.width-2)
				for _, wl := range wrapped {
					sb.WriteString(line.fullStyle.Render("  " + wl))
					sb.WriteString("\n")
				}
			}
		}
	}

	// 2. 空行分隔（输出区与下方固定区之间）
	sb.WriteString("\n")

	// 3. 状态行（仅 streaming 时显示）
	if m.streaming {
		sb.WriteString(m.renderStatusBar())
		sb.WriteString("\n")

		// 4. 空行分隔（状态行与输入框之间）
		sb.WriteString("\n")
	}

	// 5. 输入框（始终固定在底部）
	// 用 styleInputLine.Width(m.width) 渲染整行，lipgloss 会自动
	// 用带背景色的空格填充右侧空白区域，实现 Codex 风格的整行背景。
	// textinput.TextStyle 已设置 Background("236")，确保文字本身也有背景色。
	prompt := stylePromptBg.Render("> ") + m.input.View()
	inputLine := styleInputLine.Width(m.width).Render(prompt)
	sb.WriteString(inputLine)

	return sb.String()
}

// renderStatusBar 渲染固定状态栏
func (m *model) renderStatusBar() string {
	spinner := string(m.spinnerChars[m.spinnerFrame])
	phaseText := m.streamPhaseText()

	var parts []string
	parts = append(parts, styleSpinner.Render("* "+spinner)+" "+styleStatusText.Render(phaseText))

	// 添加时间信息
	if !m.streamStart.IsZero() {
		elapsed := time.Since(m.streamStart).Round(time.Second)
		parts = append(parts, styleDim.Render("("+elapsed.String()+")"))
	}

	// 添加 token 计数提示（如果有）
	if m.contentBuf.Len() > 0 {
		parts = append(parts, styleDim.Render(fmt.Sprintf("↑ %d chars", m.contentBuf.Len())))
	}

	// 添加中断提示
	parts = append(parts, styleDim.Render("esc to interrupt"))

	return strings.Join(parts, " · ")
}

// streamPhaseText returns a human-readable status for the current streaming phase
func (m *model) streamPhaseText() string {
	switch m.streamPhase {
	case "thinking":
		if m.curThinkIdx >= 0 && m.curThinkIdx < len(m.reasonBlocks) {
			lines := len(m.reasonBlocks[m.curThinkIdx].lines)
			return fmt.Sprintf("Thinking… (%d lines)", lines)
		}
		return "Thinking…"
	case "generating":
		if m.currentTool != "" {
			return fmt.Sprintf("Calling %s…", m.currentTool)
		}
		return "Generating…"
	case "tool_exec":
		if m.currentTool != "" {
			return fmt.Sprintf("Running %s…", m.currentTool)
		}
		return "Running tools…"
	default:
		return "Working…"
	}
}
