package tui

import "charm.land/bubbletea/v2"

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
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

func (m *model) View() tea.View {
	if m.fatalErr != nil {
		return tea.NewView(styleError.Render("fatal: "+m.fatalErr.Error()) + "\n")
	}

	if m.width == 0 || m.height == 0 {
		return tea.NewView("")
	}

	var sb strings.Builder
	inputLineY := 0

	// Logo：alt-screen 模式由 View() 渲染，inline 模式已通过 tea.Println (insertAbove) 输出
	if m.useAltScreen && m.logoVersion != "" {
		sb.WriteString(m.renderLogo())
		inputLineY += 5 // logo 4行 + 1空行（renderLogo 末尾多一个 \n）
		m.logoVersion = ""
	}

	// 固定底部区域占用的行数
	fixedLines := 2 // 空行 + 输入框
	if m.streaming {
		fixedLines = 4 // 空行 + 状态行 + 空行 + 输入框
	}

	// 滚动区：取最后 maxScroll 行，自然渲染
	maxScroll := m.height - fixedLines
	maxScroll = max(maxScroll, 2)

	scrollLines := m.getScrollLines(maxScroll)
	for _, line := range scrollLines {
		rendered := line.render(m.width)
		sb.WriteString(rendered)
		sb.WriteString("\n")
		inputLineY++
		// 展开内容
		if line.expanded && len(line.fullLines) > 0 {
			maxExpandLines := 10
			displayLines := line.fullLines
			if len(displayLines) > maxExpandLines {
				displayLines = append([]string{}, displayLines[:5]...)
				displayLines = append(displayLines, fmt.Sprintf("  ... (%d lines hidden, ctrl+o to collapse)", len(line.fullLines)-maxExpandLines))
				displayLines = append(displayLines, line.fullLines[len(line.fullLines)-5:]...)
			}
			for _, fl := range displayLines {
				wrapped := wrapLine(fl, m.width-2)
				for _, wl := range wrapped {
					sb.WriteString(line.fullStyle.Render("  " + wl))
					sb.WriteString("\n")
					inputLineY++
				}
			}
		}
	}

	// 空行分隔（输出区与下方固定区之间）
	sb.WriteString("\n")
	inputLineY++

	// 状态行（仅 streaming 时显示）
	if m.streaming {
		sb.WriteString(m.renderStatusBar())
		sb.WriteString("\n")
		inputLineY++

		sb.WriteString("\n")
		inputLineY++
	}

	// 输入框 — 自行渲染而非使用 m.input.View()，
	// 避免 inline 模式下 renderer 裁剪 View 内容时与 tea.Println 输出的 logo 冲突。
	//
	// 光标处理策略（关键设计决策）：
	//   不使用 v.Cursor / m.input.Cursor() 坐标体系。
	//   原因：inline 模式下 renderer 会裁剪顶部行（cursed_renderer.go:315），
	//         导致基于完整内容计算的 inputLineY 与裁剪后的 cellbuf 坐标系错位，
	//         且 inline 模式的 SetRelativeCursor(true) 使光标移动使用相对序列，
	//         手动设置的 Cursor 坐标很难与 renderer 内部状态对齐。
	//   方案：仿照 bubbles/cursor 虚拟光标的做法，将光标作为文本内容的一部分渲染 ——
	//         在光标位置插入一个反色样式的字符块，视觉上等同于终端真实光标。
	val := m.input.Value()
	pos := m.input.Position()
	var inputContent string

	// 样式定义（val 和 placeholder 分开，避免混用）
	textStyle := lipgloss.NewStyle().Background(lipgloss.Color("236"))
	cursorStyle := lipgloss.NewStyle().Background(lipgloss.Color("10")).Foreground(lipgloss.Color("236"))
	placeholderTextStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Background(lipgloss.Color("236"))
	placeholderCursorStyle := lipgloss.NewStyle().Background(lipgloss.Color("10")).Foreground(lipgloss.Color("8"))

	if val != "" {
		// 用 []rune 切片确保中文等多字节字符的正确处理
		runes := []rune(val)
		// pos 边界保护：IME 组合或快速输入时可能出现短暂不一致
		clampedPos := max(0, min(pos, len(runes)))
		before := string(runes[:clampedPos])
		var cursorChar string
		var after string
		if clampedPos < len(runes) {
			cursorChar = string(runes[clampedPos])
			after = string(runes[clampedPos+1:])
		} else {
			cursorChar = " "
			after = ""
		}
		inputContent = textStyle.Render(before) + cursorStyle.Render(cursorChar) + textStyle.Render(after)
	} else {
		// val 为空时才显示 placeholder，两者严格互斥
		placeholder := m.input.Placeholder
		pRunes := []rune(placeholder)
		pPos := max(0, min(pos, len(pRunes)))
		before := string(pRunes[:pPos])
		var cursorChar string
		var after string
		if pPos < len(pRunes) {
			cursorChar = string(pRunes[pPos])
			after = string(pRunes[pPos+1:])
		} else {
			cursorChar = " "
			after = ""
		}
		inputContent = placeholderTextStyle.Render(before) + placeholderCursorStyle.Render(cursorChar) + placeholderTextStyle.Render(after)
	}

	// 构建完整输入行：prompt + 内容，并用背景色填满整行宽度。
	// .Width(m.width) 确保 lipgloss 用背景色空格填充剩余区域，
	// 清除上一帧可能残留的旧内容（特别是 IME 直接写入终端的文本）。
	prompt := stylePromptBg.Render("> ") + inputContent
	inputLine := styleInputLine.Width(m.width).Render(prompt)
	sb.WriteString(inputLine)

	v := tea.NewView(sb.String())
	v.AltScreen = m.useAltScreen
	// 光标已作为文本内容内联渲染（见上方输入框渲染注释），
	// 不需要设置 v.Cursor。这避免了 inline 模式下 renderer 裁剪 + 相对光标
	// 导致的坐标系错位问题。
	return v
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
