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
	Reasoning   bool     `yaml:"reasoning"`
	Group       string   `yaml:"group"`
	IsLeader    bool     `yaml:"is_leader"`
	Skills      []string `yaml:"skills"`
	SubAgents   []string `yaml:"sub_agents"`
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
func LoadLeaders(agentsDir string) ([]LeaderInfo, error) {
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
			leaders = append(leaders, LeaderInfo{
				Name:        af.Frontmatter.Name,
				Description: af.Frontmatter.Description,
				Group:       af.Frontmatter.Group,
			})
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
