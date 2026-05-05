package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

func (m model) renderHeader(ly layout) string {
	var contextPct int
	if m.sess != nil {
		current, maxTokens, _ := m.sess.CW().TokenUsage()
		if maxTokens > 0 {
			contextPct = current * 100 / maxTokens
		}
	}

	leftStyle := lipgloss.NewStyle().Foreground(colorText).Bold(true)
	parts := []string{leftStyle.Render("SoloQueue")}

	switch {
	case m.quitCount > 0:
		parts = append(parts, dimStyle.Render("◇ press Ctrl+C again to exit"))
	case m.sandboxErr != "":
		parts = append(parts, errorStyle.Render("✗ "+m.sandboxErr))
	case m.errMsg != "":
		parts = append(parts, errorStyle.Render("✗ "+m.errMsg))
	case m.cancelReason != "":
		parts = append(parts, errorStyle.Render(fmt.Sprintf("✗ cancelled (%s)", m.cancelReason)))
	case m.genSummary != "":
		parts = append(parts, successStyle.Render(m.genSummary))
	case m.isGenerating:
		phase := phaseStyle(m.genPhase).Render(m.spinner.Current() + " " + m.genPhase.String())
		parts = append(parts, phase, formatDuration(time.Since(m.genStartTime)))
		if m.promptTokens > 0 {
			parts = append(parts, fmt.Sprintf("↓%s", formatTokenCount(m.promptTokens)))
		}
		if m.outputTokens > 0 {
			parts = append(parts, fmt.Sprintf("↑%s", formatTokenCount(m.outputTokens)))
		}
		if m.cacheHitTokens > 0 || m.cacheMissTokens > 0 {
			parts = append(parts, fmt.Sprintf("cache %s/%s", formatTokenCount(m.cacheHitTokens), formatTokenCount(m.cacheMissTokens)))
		}
		if m.reasoningTokens > 0 {
			parts = append(parts, fmt.Sprintf("think %s", formatTokenCount(m.reasoningTokens)))
		}
	case m.loading:
		parts = append(parts, infoStyle.Render(m.spinner.Current()+" initializing sandbox..."))
	default:
		parts = append(parts, successStyle.Render("◇ ready"))
		if m.cfg.ModelID != "" {
			parts = append(parts, dimStyle.Render(m.cfg.ModelID))
		}
	}

	left := strings.Join(parts, dimStyle.Render(" · "))
	right := renderContextBar(contextPct)

	targetWidth := ly.width
	if ly.mode == layoutTwoPane && m.showAgents {
		targetWidth = ly.mainW
	}
	space := targetWidth - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if space < 1 {
		space = 1
	}
	line := left + strings.Repeat(" ", space) + right
	rendered := headerStyle.Width(targetWidth).Render(line)

	if ly.mode == layoutTwoPane && m.showAgents {
		pad := strings.Repeat(" ", ly.leftW) + paneBorderStyle.Render("│")
		lines := strings.Split(rendered, "\n")
		for i := range lines {
			lines[i] = pad + lines[i]
		}
		return strings.Join(lines, "\n")
	}
	return rendered
}
