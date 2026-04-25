package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

// ─── Tool block rendering ─────────────────────────────────────────────────────

// toolArgs 定义工具参数的通用结构
type toolArgs struct {
	Path    string `json:"path,omitempty"`
	Command string `json:"command,omitempty"`
	File    string `json:"file,omitempty"`
}

// parseToolArgs 解析 JSON 格式的工具参数
func parseToolArgs(argsJSON string) toolArgs {
	var args toolArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		// 如果解析失败，返回空结构体
		return toolArgs{}
	}
	return args
}

func (m *model) renderToolStartBlock(name, args string) {
	if !m.lastLineEmpty {
		m.addScrollLine("", lipgloss.NewStyle())
	}

	// 解析工具参数
	toolArgs := parseToolArgs(args)

	// 优先显示 path，其次 command，其次 file
	var displayArg string
	if toolArgs.Path != "" {
		displayArg = toolArgs.Path
	} else if toolArgs.Command != "" {
		displayArg = toolArgs.Command
	} else if toolArgs.File != "" {
		displayArg = toolArgs.File
	}

	var content string
	if displayArg != "" {
		content = fmt.Sprintf("● %s(%s)", name, displayArg)
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

	// 构建执行时间提示
	var durHint string
	if info.duration > 0 {
		durHint = fmt.Sprintf(" · %s", info.duration.Round(time.Millisecond))
	}

	var content string
	if lineCount > 0 {
		content = fmt.Sprintf("  ✓ %s %d lines%s (ctrl+o to expand)", info.name, lineCount, durHint)
	} else {
		content = fmt.Sprintf("  ✓ %s done%s", info.name, durHint)
	}

	// 存储完整结果用于展开
	var fullLines []string
	for _, line := range strings.Split(info.result, "\n") {
		if strings.TrimSpace(line) != "" {
			fullLines = append(fullLines, line)
		}
	}

	m.appendScrollback(scrollLine{
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
