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
	// 光标位置由 m.input.Cursor() 提供（SetVirtualCursor(true)）。
	var inputContent string
	if val := m.input.Value(); val != "" {
		inputContent = lipgloss.NewStyle().Background(lipgloss.Color("236")).Render(val)
	} else {
		inputContent = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Background(lipgloss.Color("236")).Render(m.input.Placeholder)
	}
	prompt := stylePromptBg.Render("> ") + inputContent
	inputLine := styleInputLine.Width(m.width).Render(prompt)
	sb.WriteString(inputLine)

	v := tea.NewView(sb.String())
	v.AltScreen = m.useAltScreen
	if cur := m.input.Cursor(); cur != nil {
		cur.X += 2 // "> " prompt width
		cur.Y = inputLineY
		// inline 模式下 renderer 会裁剪顶部行（只保留最后 s.height 行），
		// 但 inputLineY 是按完整内容计算的，需要映射到裁剪后的 frame 坐标系。
		if !m.useAltScreen && cur.Y >= m.height {
			cur.Y = m.height - 1
		}
		v.Cursor = cur
	}
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
