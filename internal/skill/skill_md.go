package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ─── SKILL.md 文件加载器 ──────────────────────────────────────────────────

// SkillMDConfig 是 SKILL.md 的 YAML frontmatter
type SkillMDConfig struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	WhenToUse   string `yaml:"when_to_use"`
}

// MDSkill 是从 SKILL.md 文件加载的 Skill 实现
type MDSkill struct {
	name         string
	description  string
	whenToUse    string
	instructions string
	dir          string // SKILL.md 所在目录（用于引用支持文件）
	filePath     string // SKILL.md 绝对路径（LLM 用 file_read 读取）
}

func (s *MDSkill) ID() string             { return s.name }
func (s *MDSkill) Description() string    { return s.description }
func (s *MDSkill) Instructions() string   { return s.instructions }
func (s *MDSkill) WhenToUse() string      { return s.whenToUse }
func (s *MDSkill) Category() SkillCategory { return SkillUser }
func (s *MDSkill) FilePath() string       { return s.filePath }

// Dir 返回 SKILL.md 所在目录（用于引用支持文件）
func (s *MDSkill) Dir() string { return s.dir }

// ParseSkillMD 解析单个 SKILL.md 文件
//
// 文件格式：
//
//	---
//	name: my-skill
//	description: What this skill does
//	when_to_use: Trigger phrases
//	---
//	# Skill Instructions
//	The actual markdown content...
func ParseSkillMD(path string) (*MDSkill, error) {
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

	// filePath: 绝对路径（LLM 用 file_read 读取）
	absPath, _ := filepath.Abs(path)

	return &MDSkill{
		name:         name,
		description:  desc,
		whenToUse:    cfg.WhenToUse,
		instructions: strings.TrimSpace(body),
		dir:          filepath.Dir(absPath),
		filePath:     absPath,
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
func LoadSkillsFromDir(dir string) ([]Skill, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read skills dir %s: %w", dir, err)
	}

	var skills []Skill
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
			continue
		}
		skills = append(skills, md)
	}
	return skills, nil
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
