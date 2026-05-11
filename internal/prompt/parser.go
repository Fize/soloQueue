package prompt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// AgentFrontmatter 对应 ~/.soloqueue/agents/*.md 的 YAML frontmatter。
type AgentFrontmatter struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Model       string   `yaml:"model"`
	Group       string   `yaml:"group"`
	IsLeader    bool     `yaml:"is_leader"`
	Permission  bool     `yaml:"permission"`
	MCPServers  []string `yaml:"mcp_servers"`
	Skills      []string `yaml:"skills"`
}

// GroupFrontmatter 对应 ~/.soloqueue/groups/*.md 的 YAML frontmatter。
type GroupFrontmatter struct {
	Name       string      `yaml:"name"`
	Workspaces []Workspace `yaml:"workspaces"`
}

// Workspace 描述团队关联的工作空间。
type Workspace struct {
	Name     string         `yaml:"name"`
	Path     string         `yaml:"path"`
	AutoWork AutoWorkConfig `yaml:"autoWork"`
}

// AutoWorkConfig 描述自动工作配置。
type AutoWorkConfig struct {
	Enabled                 bool `yaml:"enabled"`
	InitialCooldownMinutes  int  `yaml:"initialCooldownMinutes"`
	PostTaskCooldownMinutes int  `yaml:"postTaskCooldownMinutes"`
	MaxIntervalsPerDay      int  `yaml:"maxIntervalsPerDay"`
}

// GroupFile 解析结果：frontmatter + markdown body（团队描述）。
type GroupFile struct {
	Frontmatter GroupFrontmatter
	Body        string
}

// AgentFile 解析结果：frontmatter + markdown body。
type AgentFile struct {
	Frontmatter AgentFrontmatter
	Body        string
}

// ParseAgentFile 解析单个 agent markdown 文件（YAML frontmatter + body）。
func ParseAgentFile(path string) (*AgentFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read agent file %s: %w", path, err)
	}

	content := string(data)

	// 提取 --- 之间的 frontmatter
	if !strings.HasPrefix(content, "---") {
		return nil, fmt.Errorf("agent file %s: missing frontmatter delimiter", path)
	}

	end := strings.Index(content[3:], "---")
	if end < 0 {
		return nil, fmt.Errorf("agent file %s: unclosed frontmatter", path)
	}

	fmContent := strings.TrimSpace(content[3 : end+3])
	body := strings.TrimSpace(content[end+6:])

	var fm AgentFrontmatter
	if err := yaml.Unmarshal([]byte(fmContent), &fm); err != nil {
		return nil, fmt.Errorf("parse frontmatter %s: %w", path, err)
	}

	return &AgentFile{Frontmatter: fm, Body: body}, nil
}

// LoadLeaders 扫描 agents 目录，返回所有 is_leader=true 的 agent。
// 仅提取 Name/Description/Group，不提取 Skills（主 Agent 不需要知道工具细节）。
// 如果传入 groups，会填充 GroupDescription。
func LoadLeaders(agentsDir string, groups map[string]GroupFile) ([]LeaderInfo, error) {
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return nil, fmt.Errorf("read agents dir %s: %w", agentsDir, err)
	}

	var leaders []LeaderInfo
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		path := filepath.Join(agentsDir, entry.Name())
		af, err := ParseAgentFile(path)
		if err != nil {
			continue // 跳过解析失败的文件
		}

		if af.Frontmatter.IsLeader {
			li := LeaderInfo{
				Name:        af.Frontmatter.Name,
				Description: af.Frontmatter.Description,
				Group:       af.Frontmatter.Group,
			}

			// 填充 group 信息
			if gf, ok := groups[af.Frontmatter.Group]; ok {
				li.GroupDescription = gf.Body
			}

			leaders = append(leaders, li)
		}
	}

	return leaders, nil
}

// LoadAgentFiles 扫描 agents 目录，返回所有解析后的 AgentFile
//
// 不过滤 IsLeader，返回所有 .md 文件。解析失败的文件被跳过（不打断流程）。
func LoadAgentFiles(agentsDir string) ([]AgentFile, error) {
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return nil, fmt.Errorf("read agents dir %s: %w", agentsDir, err)
	}

	var files []AgentFile
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		path := filepath.Join(agentsDir, entry.Name())
		af, err := ParseAgentFile(path)
		if err != nil {
			continue // 跳过解析失败的文件
		}

		files = append(files, *af)
	}

	return files, nil
}

// ParseGroupFile 解析单个 group markdown 文件（YAML frontmatter + body）。
func ParseGroupFile(path string) (*GroupFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read group file %s: %w", path, err)
	}

	content := string(data)

	if !strings.HasPrefix(content, "---") {
		return nil, fmt.Errorf("group file %s: missing frontmatter delimiter", path)
	}

	end := strings.Index(content[3:], "---")
	if end < 0 {
		return nil, fmt.Errorf("group file %s: unclosed frontmatter", path)
	}

	fmContent := strings.TrimSpace(content[3 : end+3])
	body := strings.TrimSpace(content[end+6:])

	var fm GroupFrontmatter
	if err := yaml.Unmarshal([]byte(fmContent), &fm); err != nil {
		return nil, fmt.Errorf("parse group frontmatter %s: %w", path, err)
	}

	return &GroupFile{Frontmatter: fm, Body: body}, nil
}

// LoadGroups 扫描 groups 目录，返回 name -> GroupFile 的映射。
// 如果目录不存在，返回空 map 而非错误（向后兼容）。
func LoadGroups(groupsDir string) (map[string]GroupFile, error) {
	entries, err := os.ReadDir(groupsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]GroupFile), nil
		}
		return nil, fmt.Errorf("read groups dir %s: %w", groupsDir, err)
	}

	groups := make(map[string]GroupFile)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		path := filepath.Join(groupsDir, entry.Name())
		gf, err := ParseGroupFile(path)
		if err != nil {
			continue // 跳过解析失败的文件
		}

		name := gf.Frontmatter.Name
		if name == "" {
			// 用文件名（去掉 .md）作为 fallback
			name = strings.TrimSuffix(entry.Name(), ".md")
		}
		groups[name] = *gf
	}

	return groups, nil
}
