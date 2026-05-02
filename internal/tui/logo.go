package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
)

// ─── Logo rendering ─────────────────────────────────────────────────────────

// sidebarLogoLines is the height of the sidebar logo block (ASCII art + separator).
const sidebarLogoLines = 5

// renderSidebarLogo renders the ASCII art logo for the top of the sidebar pane.
// Fits within the given width. Returns exactly sidebarLogoLines lines.
func renderSidebarLogo(width int, version string) string {
	logoLines := []string{
		" ╭──╮ ╭──╮ ",
		" ╰──╮ │  │  soloqueue",
		" ╰──╯ ╰──┼  " + version,
		"         ╰ ",
	}
	startR, startG, startB := 192, 132, 252
	endR, endG, endB := 244, 114, 182
	var sb strings.Builder
	for i, line := range logoLines {
		ratio := float64(i) / float64(len(logoLines)-1)
		r := startR + int(float64(endR-startR)*ratio)
		g := startG + int(float64(endG-startG)*ratio)
		b := startB + int(float64(endB-startB)*ratio)
		hex := fmt.Sprintf("#%02X%02X%02X", r, g, b)
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(hex)).Bold(true).Render(line) + "\n")
	}
	// Separator line
	sep := lipgloss.NewStyle().Foreground(colorBorder).Render(strings.Repeat("─", max(width, 1)))
	sb.WriteString(sep)

	return fitLines(sb.String(), sidebarLogoLines)
}

// renderLogo renders the full-width logo for the conversation viewport.
func renderLogo(version string) string {
	if version == "" {
		return ""
	}
	logoLines := []string{
		" ╭──╮ ╭──╮ ",
		" ╰──╮ │  │  soloqueue",
		" ╰──╯ ╰──┼  " + version,
		"         ╰ ",
	}
	startR, startG, startB := 192, 132, 252
	endR, endG, endB := 244, 114, 182
	var sb strings.Builder
	for i, line := range logoLines {
		ratio := float64(i) / float64(len(logoLines)-1)
		r := startR + int(float64(endR-startR)*ratio)
		g := startG + int(float64(endG-startG)*ratio)
		b := startB + int(float64(endB-startB)*ratio)
		hex := fmt.Sprintf("#%02X%02X%02X", r, g, b)
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(hex)).Bold(true).Render(line) + "\n")
	}
	sb.WriteString(dimStyle.Render("session ready — type your question or /help") + "\n\n")
	return sb.String()
}
