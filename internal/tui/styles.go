package tui

import "github.com/charmbracelet/lipgloss"

// ─── Color palette ───────────────────────────────────────────────────────────

var (
	colorPrimary = lipgloss.Color("99")  // purple
	colorAccent  = lipgloss.Color("86")  // green
	colorMuted   = lipgloss.Color("240") // gray
	colorWarning = lipgloss.Color("203") // orange-red
)

// ─── Pre-defined styles ──────────────────────────────────────────────────────

var (
	userStyle    = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	agentStyle   = lipgloss.NewStyle().Foreground(colorPrimary).Bold(true)
	contentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))

	thinkStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Italic(true).
			BorderLeft(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("238")).
			PaddingLeft(1)

	foldedStyle = lipgloss.NewStyle().Foreground(colorMuted).Italic(true)

	toolStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("170")).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("63")).
			Padding(0, 1)

	errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	dimStyle   = lipgloss.NewStyle().Foreground(colorMuted)

	statusStyle = lipgloss.NewStyle().Background(lipgloss.Color("236")).Padding(0, 1)
	hintStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("235")).Background(lipgloss.Color("240")).Padding(0, 1)

	confirmHighlight = lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Bold(true)
	confirmNormal    = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
)
