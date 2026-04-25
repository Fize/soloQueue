package tui

import (
	"fmt"
	"strings"
)

// ─── Thinking block rendering ─────────────────────────────────────────────────

func (a *App) startNewThinkBlock() {
	a.reasonBuf.Reset()
	a.reasonBlocks = append(a.reasonBlocks, thinkBlock{})
	a.curThinkIdx = len(a.reasonBlocks) - 1
	a.streamPhase = "thinking"
}

func (a *App) appendReasoning(delta string) {
	a.reasonBuf.WriteString(delta)
}

// finalizeCurrentThink prints the think block as a collapsed summary.
func (a *App) finalizeCurrentThink() {
	if a.curThinkIdx < 0 || a.curThinkIdx >= len(a.reasonBlocks) {
		return
	}

	raw := a.reasonBuf.String()
	var lines []string
	for _, l := range strings.Split(raw, "\n") {
		if trimmed := strings.TrimSpace(l); trimmed != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		a.curThinkIdx = -1
		a.reasonBuf.Reset()
		return
	}

	tb := &a.reasonBlocks[a.curThinkIdx]
	tb.lines = lines

	// Print collapsed title
	title := fmt.Sprintf("💭 Thinking for %d lines", len(lines))
	if !a.lastLineEmpty {
		fmt.Println()
	}
	fmt.Println(Styled(title, styleThinkTitle))
	a.lastLineEmpty = false

	a.curThinkIdx = -1
	a.reasonBuf.Reset()
}
