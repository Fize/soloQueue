package tui

import "fmt"

// ─── ANSI escape constants ────────────────────────────────────────────────────

const (
	RESET     = "\033[0m"
	BOLD      = "\033[1m"
	DIM       = "\033[2m"
	UNDERLINE = "\033[4m"
)

// ─── StyleAttr ────────────────────────────────────────────────────────────────

// StyleAttr represents an ANSI style attribute.
type StyleAttr string

// 256-color foreground: Fg(10) → \033[38;5;10m
func Fg(n uint8) StyleAttr { return StyleAttr(fmt.Sprintf("\033[38;5;%dm", n)) }

// 256-color background: Bg(236) → \033[48;5;236m
func Bg(n uint8) StyleAttr { return StyleAttr(fmt.Sprintf("\033[48;5;%dm", n)) }

// 24-bit foreground: FgRGB(r,g,b) → \033[38;2;r;g;bm
func FgRGB(r, g, b uint8) StyleAttr {
	return StyleAttr(fmt.Sprintf("\033[38;2;%d;%d;%dm", r, g, b))
}

// 24-bit background: BgRGB(r,g,b) → \033[48;2;r;g;bm
func BgRGB(r, g, b uint8) StyleAttr {
	return StyleAttr(fmt.Sprintf("\033[48;2;%d;%d;%dm", r, g, b))
}

// Styled applies ANSI style attributes to text and resets at the end.
func Styled(text string, attrs ...StyleAttr) string {
	var prefix string
	for _, a := range attrs {
		prefix += string(a)
	}
	if prefix == "" {
		return text
	}
	return prefix + text + RESET
}

// ─── Pre-defined styles ──────────────────────────────────────────────────────

var (
	styleUser       = Fg(10)  // green
	styleAI         = Fg(252) // light gray
	styleDim        = Fg(245) // dim gray
	styleThinkTitle = Fg(8)
	styleToolName   = Fg(7)   // gray
	styleToolOK     = Fg(2)   // green
	styleToolErr    = Fg(9)   // red
	styleToolResult = Fg(8)   // gray
	styleError      = Fg(9)   // red
)
