package tui

import (
	"charm.land/lipgloss/v2"

	"github.com/xiaobaitu/soloqueue/internal/agent"
)

// ─── Color palette ───────────────────────────────────────────────────────────

var (
	colorText     = lipgloss.Color("#E5E7EB")
	colorPrimary  = lipgloss.Color("#C084FC") // vivid violet for agent/UI headers
	colorAccent   = lipgloss.Color("#34D399") // green for user/success
	colorMuted    = lipgloss.Color("#A1A1AA") // readable secondary text
	colorWarning  = lipgloss.Color("#FBBF24") // amber for tool/warning states
	colorInfo     = lipgloss.Color("#38BDF8") // blue for active/generating states
	colorError    = lipgloss.Color("#F87171") // red for errors/cancel
	colorThinking = lipgloss.Color("#F0ABFC") // magenta for reasoning blocks
	colorTool     = lipgloss.Color("#A7F3D0") // mint for tool labels
	colorBorder   = lipgloss.Color("#71717A") // visible panel/divider border
	colorHint     = lipgloss.Color("#D4D4D8")
	colorStatusBg = lipgloss.Color("#18181B")
)

// ─── Pre-defined styles ──────────────────────────────────────────────────────

var (
	userStyle          lipgloss.Style
	agentStyle         lipgloss.Style
	contentStyle       lipgloss.Style
	thinkLabelStyle    string
	thinkStyle         lipgloss.Style
	foldedStyle        lipgloss.Style
	successStyle       lipgloss.Style
	toolCollapsedStyle lipgloss.Style
	errorStyle         lipgloss.Style
	dimStyle           lipgloss.Style
	clearStatusStyle   lipgloss.Style
	statusStyle        lipgloss.Style
	hintStyle          lipgloss.Style
	infoStyle          lipgloss.Style
	confirmHighlight   lipgloss.Style
	confirmNormal      lipgloss.Style
	headerStyle        lipgloss.Style
	footerStyle        lipgloss.Style
	paneTitleStyle     lipgloss.Style
	paneBorderStyle    lipgloss.Style
	composerStyle      lipgloss.Style
	copyModeStyle      lipgloss.Style
	timestampStyle     lipgloss.Style
	teamBadgeStyle     lipgloss.Style
)

func init() {
	applyTheme(true)
}

// applyTheme switches all shared TUI styles between high-contrast dark/light palettes.
func applyTheme(darkBg bool) {
	if darkBg {
		colorText = lipgloss.Color("#E5E7EB")
		colorPrimary = lipgloss.Color("#C084FC")
		colorAccent = lipgloss.Color("#34D399")
		colorMuted = lipgloss.Color("#A1A1AA")
		colorWarning = lipgloss.Color("#FBBF24")
		colorInfo = lipgloss.Color("#38BDF8")
		colorError = lipgloss.Color("#F87171")
		colorThinking = lipgloss.Color("#F0ABFC")
		colorTool = lipgloss.Color("#A7F3D0")
		colorBorder = lipgloss.Color("#71717A")
		colorHint = lipgloss.Color("#D4D4D8")
		colorStatusBg = lipgloss.Color("#18181B")
	} else {
		colorText = lipgloss.Color("#111827")
		colorPrimary = lipgloss.Color("#6D28D9")
		colorAccent = lipgloss.Color("#047857")
		colorMuted = lipgloss.Color("#4B5563")
		colorWarning = lipgloss.Color("#B45309")
		colorInfo = lipgloss.Color("#0369A1")
		colorError = lipgloss.Color("#B91C1C")
		colorThinking = lipgloss.Color("#86198F")
		colorTool = lipgloss.Color("#047857")
		colorBorder = lipgloss.Color("#6B7280")
		colorHint = lipgloss.Color("#374151")
		colorStatusBg = lipgloss.Color("#E5E7EB")
	}

	userStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	agentStyle = lipgloss.NewStyle().Foreground(colorPrimary).Bold(true)
	contentStyle = lipgloss.NewStyle().Foreground(colorText)

	thinkLabelStyle = lipgloss.NewStyle().
		Foreground(colorThinking).
		Bold(true).
		Render("▎ Thinking")

	thinkStyle = lipgloss.NewStyle().
		Foreground(colorMuted).
		PaddingLeft(2)

	foldedStyle = lipgloss.NewStyle().Foreground(colorMuted)
	successStyle = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)

	toolCollapsedStyle = lipgloss.NewStyle().
		Foreground(colorMuted).
		PaddingLeft(2)

	errorStyle = lipgloss.NewStyle().Foreground(colorError).Bold(true)
	dimStyle = lipgloss.NewStyle().Foreground(colorMuted)

	clearStatusStyle = lipgloss.NewStyle().
		Foreground(colorInfo).
		Bold(true).
		PaddingLeft(2)

	statusStyle = lipgloss.NewStyle().
		Foreground(colorText).
		Background(colorStatusBg).
		Padding(0, 1)

	hintStyle = lipgloss.NewStyle().Foreground(colorHint)
	infoStyle = lipgloss.NewStyle().Foreground(colorInfo).Bold(true)

	confirmHighlight = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	confirmNormal = lipgloss.NewStyle().Foreground(colorText)
	headerStyle = lipgloss.NewStyle().
		Foreground(colorText).
		Background(colorStatusBg).
		Padding(0, 1).
		BorderTop(true).
		BorderBottom(true).
		BorderForeground(colorBorder)
	footerStyle = lipgloss.NewStyle().Foreground(colorHint).Background(colorStatusBg).Padding(0, 1)
	paneTitleStyle = lipgloss.NewStyle().Foreground(colorPrimary).Bold(true)
	paneBorderStyle = lipgloss.NewStyle().Foreground(colorBorder)
	composerStyle = lipgloss.NewStyle().Foreground(colorText).BorderForeground(colorBorder)
	copyModeStyle = lipgloss.NewStyle().Foreground(colorWarning).Bold(true)
	timestampStyle = lipgloss.NewStyle().Foreground(colorMuted)

	teamBadgeStyle = lipgloss.NewStyle().
		Foreground(colorText).
		Bold(true)

}

// contextTokenStyle returns a style with color based on context window usage percentage.
// <50%: green, 50-69%: light blue, 70-84%: yellow, 85-100%: orange.
// No red/warning colors since compression triggers at ~90%.
func agentBadgeStyle(s agent.State) lipgloss.Style {
	return stateStyle(s).Bold(true).Padding(0, 1)
}

func contextTokenStyle(pct int) lipgloss.Style {
	switch {
	case pct < 50:
		return lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	case pct < 70:
		return lipgloss.NewStyle().Foreground(colorInfo).Bold(true)
	case pct < 85:
		return lipgloss.NewStyle().Foreground(colorWarning).Bold(true)
	default:
		return lipgloss.NewStyle().Foreground(colorError).Bold(true)
	}
}
