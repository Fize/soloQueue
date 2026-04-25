package tui

import (
	"fmt"
	"strings"
)

// ─── Built-in commands ────────────────────────────────────────────────────────

func (a *App) handleBuiltin(input string) (quit bool) {
	cmd := strings.ToLower(strings.TrimSpace(input))
	switch cmd {
	case "/quit", "/exit", "/q":
		return true

	case "/help", "/?":
		fmt.Println(Styled("Commands: /help /clear /history /version /quit", styleDim))
		fmt.Println()

	case "/clear":
		// Just print some blank lines — no scrollback to clear
		fmt.Print("\033[2J\033[H") // clear screen + cursor home

	case "/version":
		fmt.Println(Styled("SoloQueue "+a.cfg.Version, BOLD))
		fmt.Println()

	case "/history":
		a.historyCmds()

	default:
		if strings.HasPrefix(input, "/") {
			fmt.Println(Styled("✗ Unknown command: "+input+". Type /help", styleError))
			fmt.Println()
		}
	}
	return false
}

func (a *App) historyCmds() {
	if len(a.history) == 0 {
		fmt.Println(Styled("(no history yet)", styleDim))
		fmt.Println()
		return
	}
	fmt.Println(Styled("History:", BOLD))
	start := 0
	if len(a.history) > 20 {
		start = len(a.history) - 20
	}
	for i := start; i < len(a.history); i++ {
		fmt.Println(Styled(fmt.Sprintf("  %3d  %s", i+1, truncate(a.history[i], 72)), styleDim))
	}
	fmt.Println()
}
