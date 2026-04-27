package skill

import (
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// BuildSkills 按功能域将 tools.Build 返回的 Tool 列表分组为 Skill
//
// 替代 tools.Build(cfg) []tools.Tool，改为 BuildSkills(cfg) []Skill。
// Agent 构造时通过 WithSkills 注册到 SkillRegistry。
//
// Skill 分组：
//   - fs:     文件系统读写操作
//   - search: 内容搜索
//   - shell:  Shell 命令执行
//   - web:    网络与搜索
func BuildSkills(cfg tools.Config) []Skill {
	allTools := tools.Build(cfg)

	// 按 name 分组
	var fsTools, searchTools, shellTools, webTools []tools.Tool
	for _, t := range allTools {
		switch t.Name() {
		case "file_read", "glob", "write_file", "replace", "multi_replace", "multi_write":
			fsTools = append(fsTools, t)
		case "grep":
			searchTools = append(searchTools, t)
		case "shell_exec":
			shellTools = append(shellTools, t)
		case "http_fetch", "web_search":
			webTools = append(webTools, t)
		}
	}

	var skills []Skill
	if len(fsTools) > 0 {
		skills = append(skills, NewBuiltinSkill("fs", "File system read/write operations", fsTools...))
	}
	if len(searchTools) > 0 {
		skills = append(skills, NewBuiltinSkill("search", "Content search within files", searchTools...))
	}
	if len(shellTools) > 0 {
		skills = append(skills, NewBuiltinSkill("shell", "Execute shell commands", shellTools...))
	}
	if len(webTools) > 0 {
		skills = append(skills, NewBuiltinSkill("web", "HTTP requests and web search", webTools...))
	}

	return skills
}
