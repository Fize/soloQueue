package skill

import "strings"

// ParseAllowedTools 解析 allowed-tools 配置字符串
//
// 输入格式：逗号分隔的工具模式列表
//
//	"Bash(git:*),Read,Edit(src/**/*.ts),mcp__server__tool"
//
// 输出：原始模式字符串切片，不做进一步解析。
// 匹配逻辑在 FilterTools 中执行。
func ParseAllowedTools(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
