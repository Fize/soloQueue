package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ─── Tool block rendering ─────────────────────────────────────────────────────

// parseToolPath 从 JSON args 中提取 path 参数
func parseToolPath(args string) string {
	args = strings.TrimSpace(args)
	// 尝试匹配 "path": "..."
	if idx := strings.Index(args, `"path"`); idx >= 0 {
		rest := args[idx+6:]
		rest = strings.TrimLeft(rest, ` :"`)
		if end := strings.Index(rest, `"`); end > 0 {
			return rest[:end]
		}
	}
	// 尝试匹配 "path":"..."
	if idx := strings.Index(args, `"path":"`); idx >= 0 {
		rest := args[idx+8:]
		if end := strings.Index(rest, `"`); end > 0 {
			return rest[:end]
		}
	}
	return ""
}

func (m *model) renderToolStartBlock(name, args string) {
	if !m.lastLineEmpty {
		m.addScrollLine("", lipgloss.NewStyle())
	}
	path := parseToolPath(args)
	var content string
	if path != "" {
		content = fmt.Sprintf("● %s(%s)", name, path)
	} else {
		content = fmt.Sprintf("● %s", name)
	}
	m.addScrollLine(content, styleToolName)
	m.lastLineEmpty = false
}

func (m *model) renderToolDoneBlock(info *toolExecInfo) {
	if info.err != nil {
		content := fmt.Sprintf("  ✗ %s failed: %s", info.name, truncate(info.err.Error(), 50))
		m.addScrollLine(content, styleToolError)
		m.lastLineEmpty = false
		m.addScrollLine("", lipgloss.NewStyle())
		return
	}

	// 计算结果行数
	lineCount := 0
	for _, line := range strings.Split(info.result, "\n") {
		if strings.TrimSpace(line) != "" {
			lineCount++
		}
	}

	var content string
	if lineCount > 0 {
		content = fmt.Sprintf("  ✓ %s %d lines (ctrl+o to expand)", info.name, lineCount)
	} else {
		content = fmt.Sprintf("  ✓ %s done", info.name)
	}

	// 存储完整结果用于展开
	var fullLines []string
	for _, line := range strings.Split(info.result, "\n") {
		if strings.TrimSpace(line) != "" {
			fullLines = append(fullLines, line)
		}
	}

	m.scrollback = append(m.scrollback, scrollLine{
		content:    content,
		style:      styleToolSuccess,
		expandable: len(fullLines) > 0,
		expanded:   false,
		fullLines:  fullLines,
		fullStyle:  styleToolResult,
	})
	m.lastLineEmpty = false
	m.addScrollLine("", lipgloss.NewStyle())
}
