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

func renderToolLabel(name string) string {
	return lipgloss.NewStyle().
		Foreground(colorTool).
		Bold(true).
		Render("▎ " + name)
}

// ─── Tool argument display ──────────────────────────────────────────────────

// toolDisplay extracts a human-readable summary of tool arguments
// based on the tool name. Returns empty string if nothing meaningful
// can be extracted.
func toolDisplay(name, argsJSON string) string {
	switch name {
	case "Bash":
		var a struct {
			Command string `json:"command"`
		}
		if json.Unmarshal([]byte(argsJSON), &a) == nil && a.Command != "" {
			return a.Command
		}
	case "Read":
		var a struct {
			Path string `json:"path"`
		}
		if json.Unmarshal([]byte(argsJSON), &a) == nil && a.Path != "" {
			return a.Path
		}
	case "Write":
		var a struct {
			Path string `json:"path"`
		}
		if json.Unmarshal([]byte(argsJSON), &a) == nil && a.Path != "" {
			return a.Path
		}
	case "Edit":
		var a struct {
			Path string `json:"path"`
		}
		if json.Unmarshal([]byte(argsJSON), &a) == nil && a.Path != "" {
			return a.Path
		}
	case "MultiEdit":
		var a struct {
			Path string `json:"path"`
		}
		if json.Unmarshal([]byte(argsJSON), &a) == nil && a.Path != "" {
			return a.Path
		}
	case "MultiWrite":
		var a struct {
			Files []struct {
				Path string `json:"path"`
			} `json:"files"`
		}
		if json.Unmarshal([]byte(argsJSON), &a) == nil && len(a.Files) > 0 {
			var paths []string
			for _, f := range a.Files {
				paths = append(paths, f.Path)
			}
			return strings.Join(paths, ", ")
		}
	case "Glob":
		var a struct {
			Pattern string `json:"pattern"`
			Dir     string `json:"dir"`
		}
		if json.Unmarshal([]byte(argsJSON), &a) == nil && a.Pattern != "" {
			if a.Dir != "" {
				return a.Pattern + "  in " + a.Dir
			}
			return a.Pattern
		}
	case "Grep":
		var a struct {
			Pattern string `json:"pattern"`
		}
		if json.Unmarshal([]byte(argsJSON), &a) == nil && a.Pattern != "" {
			return a.Pattern
		}
	case "WebFetch":
		var a struct {
			URL string `json:"url"`
		}
		if json.Unmarshal([]byte(argsJSON), &a) == nil && a.URL != "" {
			return a.URL
		}
	case "WebSearch":
		var a struct {
			Query string `json:"query"`
		}
		if json.Unmarshal([]byte(argsJSON), &a) == nil && a.Query != "" {
			return a.Query
		}
	case "Remember":
		var a struct {
			Content string `json:"content"`
		}
		if json.Unmarshal([]byte(argsJSON), &a) == nil && a.Content != "" {
			return truncate(a.Content, 60)
		}
	case "RecallMemory":
		var a struct {
			Query string `json:"query"`
		}
		if json.Unmarshal([]byte(argsJSON), &a) == nil && a.Query != "" {
			return a.Query
		}
	default:
		if strings.HasPrefix(name, "delegate_") {
			var a struct {
				Task string `json:"task"`
			}
			if json.Unmarshal([]byte(argsJSON), &a) == nil && a.Task != "" {
				return truncate(a.Task, 80)
			}
		}
	}
	// Fallback: extract common fields
	return parseToolDisplay(argsJSON)
}

// parseToolDisplay is the generic fallback that tries common field names.
func parseToolDisplay(argsJSON string) string {
	var m map[string]any
	if json.Unmarshal([]byte(argsJSON), &m) != nil {
		return ""
	}
	// Try common fields in priority order
	for _, key := range []string{"path", "command", "query", "url", "pattern", "task", "content", "file"} {
		if v, ok := m[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// ─── Tool block formatting ──────────────────────────────────────────────────

func formatToolBlock(tb toolBlock) string {
	display := toolDisplay(tb.name, tb.args)
	display = truncate(display, 60)

	if !tb.done {
		if display != "" {
			return "⚙ " + display
		}
		return "⚙"
	}

	var durStr string
	if tb.duration > 0 {
		durStr = " · " + tb.duration.Round(time.Millisecond).String()
	}

	if tb.err != nil {
		errMsg := truncate(tb.err.Error(), 40)
		if display != "" {
			return "✗ " + display + " — " + errMsg
		}
		return "✗ " + errMsg
	}

	var detail string
	if tb.lineCount > 0 {
		detail = fmt.Sprintf("%d行", tb.lineCount)
	}
	if display != "" {
		if detail != "" {
			return "✓ " + display + " · " + detail + durStr
		}
		return "✓ " + display + durStr
	}
	if detail != "" {
		return "✓ " + detail + durStr
	}
	return "✓" + durStr
}
