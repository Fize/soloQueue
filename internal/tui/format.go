package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/lipgloss/v2"
)

// ─── Formatting utilities ───────────────────────────────────────────────────

func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
}

func formatTokenCount(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000)
}

func truncate(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", "↵")
	s = strings.ReplaceAll(s, "\r", "")
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "…"
}

// ─── Tool block types ───────────────────────────────────────────────────────

type toolBlock struct {
	name      string
	args      string
	callID    string
	done      bool
	duration  time.Duration
	err       error
	lineCount int
}

type toolExecInfo struct {
	name   string
	args   string
	start  time.Time
	callID string
	tb     *toolBlock
}

// toolArgs defines the common structure for tool arguments.
type toolArgs struct {
	Path    string `json:"path,omitempty"`
	Command string `json:"command,omitempty"`
	File    string `json:"file,omitempty"`
}

// parseToolArgs parses JSON-formatted tool arguments.
func parseToolArgs(argsJSON string) toolArgs {
	var args toolArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return toolArgs{Path: "[parse error]", Command: "[parse error]", File: "[parse error]"}
	}
	return args
}

func renderToolLabel(name string) string {
	return lipgloss.NewStyle().
		Foreground(colorTool).
		Bold(true).
		Render("▎ " + name)
}

func formatToolBlock(tb toolBlock) string {
	ta := parseToolArgs(tb.args)
	var displayArg string
	if ta.Path != "" {
		displayArg = ta.Path
	} else if ta.Command != "" {
		displayArg = ta.Command
	} else if ta.File != "" {
		displayArg = ta.File
	}
	displayArg = truncate(displayArg, 30)
	if !tb.done {
		if displayArg != "" {
			return "⚙ " + displayArg
		}
		return "⚙"
	}
	var durHint string
	if tb.duration > 0 {
		durHint = " " + tb.duration.Round(time.Millisecond).String()
	}
	if tb.err != nil {
		return "✗ " + truncate(tb.err.Error(), 40)
	}
	if tb.lineCount > 0 {
		return "✓ " + fmt.Sprintf("%d 行", tb.lineCount) + durHint
	}
	if displayArg != "" {
		return "✓ " + displayArg + durHint
	}
	return "✓" + durHint
}
