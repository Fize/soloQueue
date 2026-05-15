package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/logger"
	"gopkg.in/yaml.v3"
)

// ─── 包级 Logger ────────────────────────────────────────────────────────────

// pkgLogger 是 skill 包的可选日志实例，通过 SetPackageLogger 设置。
// 用于记录 Skill 加载过程中的非致命错误（如单个 SKILL.md 解析失败）。
var pkgLogger *logger.Logger

// SetPackageLogger 设置 skill 包的全局日志实例。
// 在程序启动时调用一次即可。
func SetPackageLogger(l *logger.Logger) {
	pkgLogger = l
}

// ─── SKILL.md 文件加载器 ──────────────────────────────────────────────────

// SkillMDConfig 是 SKILL.md 的 YAML frontmatter
//
// 对齐 Claude Code 的 Skill frontmatter 字段。
type SkillMDConfig struct {
	Name                   string `yaml:"name"`
	Description            string `yaml:"description"`
	WhenToUse              string `yaml:"when_to_use"`
	AllowedTools           string `yaml:"allowed-tools"`
	DisableModelInvocation bool   `yaml:"disable-model-invocation"`
	UserInvocable          *bool  `yaml:"user-invocable"` // 指针区分"未设置"和"false"
	Context                string `yaml:"context"`
	Agent                  string `yaml:"agent"`
}

// ParseSkillMD 解析单个 SKILL.md 文件
//
// 文件格式：
//
//	---
//	name: my-skill
//	description: What this skill does
//	allowed-tools: Bash(git:*),Read,Edit(src/**/*.ts)
//	disable-model-invocation: false
//	user-invocable: true
//	context: fork
//	agent: Explore
//	---
//	# Skill Instructions
//	The actual markdown content...
func ParseSkillMD(path string) (*Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read skill md: %w", err)
	}

	cfg, body, err := parseFrontmatter(data)
	if err != nil {
		return nil, fmt.Errorf("parse frontmatter %s: %w", path, err)
	}

	// name: frontmatter > 目录名
	name := cfg.Name
	if name == "" {
		name = skillNameFromPath(path)
	}

	// description: frontmatter > body 首段
	desc := cfg.Description
	if desc == "" {
		desc = firstParagraph(body)
	}

	// user-invocable: 默认 true，*bool 区分"未设置"和"false"
	userInvocable := true
	if cfg.UserInvocable != nil {
		userInvocable = *cfg.UserInvocable
	}

	// allowed-tools: 解析逗号分隔的模式字符串
	var allowedTools []string
	if cfg.AllowedTools != "" {
		allowedTools = ParseAllowedTools(cfg.AllowedTools)
	}

	// filePath: 绝对路径
	absPath, _ := filepath.Abs(path)

	return &Skill{
		ID:                     name,
		Description:            desc,
		WhenToUse:              cfg.WhenToUse,
		Instructions:           strings.TrimSpace(body),
		AllowedTools:           allowedTools,
		DisableModelInvocation: cfg.DisableModelInvocation,
		UserInvocable:          userInvocable,
		Context:                cfg.Context,
		Agent:                  cfg.Agent,
		Category:               SkillUser,
		FilePath:               absPath,
		Dir:                    filepath.Dir(absPath),
	}, nil
}

// LoadSkillsFromDir 从目录加载所有 SKILL.md
//
// 目录结构：
//
//	dir/
//	  <skill-name>/
//	    SKILL.md
//	  <another-skill>/
//	    SKILL.md
//
// 只扫描一级子目录中的 SKILL.md 文件。
// 目录不存在时返回 nil, nil。
func LoadSkillsFromDir(dir string) ([]*Skill, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			if pkgLogger != nil {
				pkgLogger.Debug(logger.CatApp, "skill: directory not found, skipping",
					"dir", dir)
			}
			return nil, nil
		}
		return nil, fmt.Errorf("read skills dir %s: %w", dir, err)
	}

	loaded := 0
	var skills []*Skill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillFile := filepath.Join(dir, e.Name(), "SKILL.md")
		info, err := os.Stat(skillFile)
		if err != nil || info.IsDir() {
			continue
		}

		md, err := ParseSkillMD(skillFile)
		if err != nil {
			// 单个 skill 加载失败不阻塞其他
			if pkgLogger != nil {
				pkgLogger.Warn(logger.CatApp, "skill: load failed",
					"path", skillFile, "err", err.Error())
			}
			continue
		}
		skills = append(skills, md)
		loaded++
	}

	if pkgLogger != nil && loaded > 0 {
		pkgLogger.Info(logger.CatApp, "skill: loaded from directory",
			"dir", dir, "count", loaded)
	}
	return skills, nil
}

// LoadSkillsFromDirs 按优先级从多个目录加载 skill
//
// dirs 的 key 为作用域标识（"plugin", "user", "project"），value 为目录路径。
// 低优先级先加载，高优先级后加载覆盖同名 skill。
// 优先级顺序：plugin → user → project（project 最高）。
func LoadSkillsFromDirs(dirs map[string]string) ([]*Skill, error) {
	order := []string{"plugin", "user", "project"}
	seen := make(map[string]*Skill)
	for _, scope := range order {
		dir, ok := dirs[scope]
		if !ok || dir == "" {
			continue
		}
		skills, err := LoadSkillsFromDir(dir)
		if err != nil {
			continue
		}
		for _, s := range skills {
			seen[s.ID] = s // 后加载覆盖先加载
		}
	}
	result := make([]*Skill, 0, len(seen))
	for _, s := range seen {
		result = append(result, s)
	}
	return result, nil
}

// ─── 内部辅助 ──────────────────────────────────────────────────────────────

// parseFrontmatter 从 SKILL.md 内容中解析 YAML frontmatter 和 body
func parseFrontmatter(data []byte) (SkillMDConfig, string, error) {
	var cfg SkillMDConfig

	content := string(data)

	// 检测 --- 分隔符
	if !strings.HasPrefix(content, "---") {
		// 无 frontmatter，整个内容作为 body
		return cfg, content, nil
	}

	// 找到结束 ---
	end := strings.Index(content[3:], "---")
	if end < 0 {
		return cfg, content, nil
	}

	fm := strings.TrimSpace(content[3 : end+3])
	body := strings.TrimSpace(content[end+6:])

	if err := yaml.Unmarshal([]byte(fm), &cfg); err != nil {
		return cfg, body, fmt.Errorf("yaml unmarshal: %w", err)
	}

	return cfg, body, nil
}

// skillNameFromPath 从文件路径提取 skill 名（父目录名）
//
//	/home/user/.soloqueue/skills/commit/SKILL.md → "commit"
func skillNameFromPath(path string) string {
	return filepath.Base(filepath.Dir(path))
}

// firstParagraph 提取 markdown body 的第一段作为 description
func firstParagraph(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	// 取第一个空行或标题前的内容
	lines := strings.Split(body, "\n")
	var para []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" && len(para) > 0 {
			break
		}
		// 跳过标题行
		if strings.HasPrefix(trimmed, "#") {
			if len(para) > 0 {
				break
			}
			continue
		}
		if trimmed != "" {
			para = append(para, trimmed)
		}
	}
	desc := strings.Join(para, " ")
	// 截断过长描述
	if len(desc) > 200 {
		desc = desc[:197] + "..."
	}
	return desc
}
