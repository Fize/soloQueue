package skill

import (
	"context"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// ─── Fork 模式 ─────────────────────────────────────────────────────────────

// SkillForkSpawnFn 创建一个临时子 agent 用于 fork 模式执行 skill。
//
// 由 Factory 注入具体的 agent 创建逻辑，避免 skill 包循环依赖 agent 包。
// 参数：
//   - ctx: 上下文
//   - s: 被调用的 skill
//   - content: 预处理后的 skill instructions（作为子 agent system prompt）
//   - args: 用户传入的参数（作为子 agent user message）
//
// 返回子 agent 的 Locatable 接口和清理函数。
type SkillForkSpawnFn func(ctx context.Context, s *Skill, content, args string) (iface.Locatable, func(), error)

// ExecuteFork 在隔离子 agent 中执行 fork 模式的 skill
//
// 由 SkillTool 调用。spawnFn 由 Factory 注入。
// 执行流程：
//  1. 调用 spawnFn 创建子 agent
//  2. 发送 AskStream 请求
//  3. 累积 content 事件
//  4. 清理子 agent
func ExecuteFork(ctx context.Context, s *Skill, content, args string, spawnFn SkillForkSpawnFn) (string, error) {
	child, cleanup, err := spawnFn(ctx, s, content, args)
	if err != nil {
		return "", err
	}
	defer cleanup()

	// args 为空时给一个默认 prompt
	prompt := args
	if prompt == "" {
		prompt = "Execute the skill instructions above."
	}

	resultCh, err := child.AskStream(ctx, prompt)
	if err != nil {
		return "", err
	}

	var result strings.Builder
	for event := range resultCh {
		if delta, ok := event.(interface{ ContentDelta() (string, bool) }); ok {
			if d, ok2 := delta.ContentDelta(); ok2 {
				result.WriteString(d)
			}
		}
	}

	return result.String(), nil
}

// ─── allowed-tools 过滤 ────────────────────────────────────────────────────

// FilterTools 根据 allowed-tools 白名单过滤工具
//
// 支持模式：
//   - "Bash" — 匹配 Bash 工具
//   - "Bash(git:*)" — 匹配 Bash 工具（命令前缀约束暂不强制，仅匹配工具名）
//   - "Edit(src/**/*.ts)" — 匹配 Edit 工具（路径约束暂不强制，仅匹配工具名）
//   - "mcp__server" — 匹配该 MCP server 的所有工具
//   - "mcp__server__tool" — 匹配特定 MCP 工具
func FilterTools(allTools []tools.Tool, allowed []string) []tools.Tool {
	var filtered []tools.Tool
	for _, t := range allTools {
		if ToolMatchesAllowed(t.Name(), allowed) {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

// ToolMatchesAllowed 检查 toolName 是否匹配 allowed 列表中的任一模式
func ToolMatchesAllowed(toolName string, allowed []string) bool {
	for _, pattern := range allowed {
		// 提取工具名部分（括号前）
		baseName := pattern
		if idx := strings.Index(pattern, "("); idx > 0 {
			baseName = pattern[:idx]
		}

		// 精确匹配
		if toolName == baseName {
			return true
		}

		// MCP 前缀匹配：mcp__server 匹配 mcp__server__tool
		if strings.HasPrefix(baseName, "mcp__") && strings.HasPrefix(toolName, baseName+"__") {
			return true
		}

		// MCP 精确匹配
		if toolName == pattern {
			return true
		}
	}
	return false
}
