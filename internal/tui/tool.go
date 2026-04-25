package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ─── Tool block rendering ─────────────────────────────────────────────────────

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
		return toolArgs{}
	}
	return args
}

func (a *App) renderToolStartBlock(name, args, callID string) {
	if !a.lastLineEmpty {
		fmt.Println()
	}

	ta := parseToolArgs(args)

	var displayArg string
	if ta.Path != "" {
		displayArg = ta.Path
	} else if ta.Command != "" {
		displayArg = ta.Command
	} else if ta.File != "" {
		displayArg = ta.File
	}

	var content string
	if displayArg != "" {
		content = fmt.Sprintf("● %s(%s)", name, displayArg)
	} else {
		content = fmt.Sprintf("● %s", name)
	}
	fmt.Println(Styled(content, styleToolName, BOLD))
	a.lastLineEmpty = false
}

func (a *App) renderToolDoneBlock(info *toolExecInfo) {
	if info.err != nil {
		content := fmt.Sprintf("  ✗ %s failed: %s", info.name, truncate(info.err.Error(), 50))
		fmt.Println(Styled(content, styleToolErr))
		a.lastLineEmpty = false
		return
	}

	// Count result lines
	lineCount := 0
	for _, line := range strings.Split(info.result, "\n") {
		if strings.TrimSpace(line) != "" {
			lineCount++
		}
	}

	var durHint string
	if info.duration > 0 {
		durHint = fmt.Sprintf(" · %s", info.duration.Round(time.Millisecond))
	}

	var content string
	if lineCount > 0 {
		content = fmt.Sprintf("  ✓ %s %d lines%s", info.name, lineCount, durHint)
	} else {
		content = fmt.Sprintf("  ✓ %s done%s", info.name, durHint)
	}

	fmt.Println(Styled(content, styleToolOK))
	a.lastLineEmpty = false
}
