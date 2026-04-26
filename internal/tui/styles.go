package tui

import "charm.land/lipgloss/v2"

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

	thinkLabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("183")).
			Bold(true).
			Render("▎ Thinking")

	thinkStyle = lipgloss.NewStyle().
			Foreground(colorMuted).
			Italic(true).
			PaddingLeft(2)

	foldedStyle = lipgloss.NewStyle().Foreground(colorMuted).Italic(true)

	toolLabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("150")).
			Bold(true).
			Render("▎ Tool Use")

	toolCollapsedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("246")).
				Italic(true).
				PaddingLeft(2)

	errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	dimStyle   = lipgloss.NewStyle().Foreground(colorMuted)

	statusStyle = lipgloss.NewStyle().Background(lipgloss.Color("236")).Padding(0, 1)
	hintStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("189"))

	confirmHighlight = lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Bold(true)
	confirmNormal    = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
)
