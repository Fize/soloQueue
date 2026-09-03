package skill

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"gopkg.in/yaml.v3"
)

// --- Package-level Logger ---------------------------------------------------

// pkgLogger is an optional logger instance for the skill package, set via SetPackageLogger.
// Used to log non-fatal errors during Skill loading (e.g., failure to parse a single SKILL.md).
var pkgLogger *logger.Logger

// SetPackageLogger sets the global logger instance for the skill package.
// Should be called once at program startup.
func SetPackageLogger(l *logger.Logger) {
	pkgLogger = l
}

// --- SKILL.md File Loader ---------------------------------------------------

// SkillMDConfig is the YAML frontmatter for SKILL.md
//
// Aligns with Claude Code's Skill frontmatter fields.
type SkillMDConfig struct {
	Name                   string         `yaml:"name"`
	Description            string         `yaml:"description"`
	WhenToUse              string         `yaml:"when_to_use"`
	AllowedTools           string         `yaml:"allowed-tools"`
	DisableModelInvocation bool           `yaml:"disable-model-invocation"`
	UserInvocable          *bool          `yaml:"user-invocable"` // Pointer distinguishes between "unset" and "false"
	Context                string         `yaml:"context"`
	Agent                  string         `yaml:"agent"`
	Triggers               []string       `yaml:"triggers"`
	Metadata               map[string]any `yaml:"metadata"`
	RequiredEnv            []string       `yaml:"required_env"`
	RequiredEnvDash        []string       `yaml:"required-env"`
}

// ParseSkillMD parses a single SKILL.md file.
//
// File format:
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
	data, err := readSkillMarkdown(path)
	if err != nil {
		return nil, fmt.Errorf("read skill md: %w", err)
	}

	cfg, body, err := parseFrontmatter(data)
	if err != nil {
		return nil, fmt.Errorf("parse frontmatter %s: %w", path, err)
	}

	// name: frontmatter > directory name
	name := cfg.Name
	if name == "" {
		name = skillNameFromPath(path)
	}

	// description: frontmatter > first paragraph of body
	desc := cfg.Description
	if desc == "" {
		desc = firstParagraph(body)
	}

	// user-invocable: defaults to true, *bool distinguishes between "unset" and "false"
	userInvocable := true
	if cfg.UserInvocable != nil {
		userInvocable = *cfg.UserInvocable
	}

	// allowed-tools: parses comma-separated pattern string
	var allowedTools []string
	if cfg.AllowedTools != "" {
		allowedTools = ParseAllowedTools(cfg.AllowedTools)
	}

	// filePath: absolute path
	absPath, _ := filepath.Abs(path)

	return &Skill{
		ID:                     name,
		Name:                   name,
		Description:            desc,
		WhenToUse:              cfg.WhenToUse,
		Instructions:           strings.TrimSpace(body),
		AllowedTools:           allowedTools,
		DisableModelInvocation: cfg.DisableModelInvocation,
		UserInvocable:          userInvocable,
		Context:                cfg.Context,
		Agent:                  cfg.Agent,
		FilePath:               absPath,
		Dir:                    filepath.Dir(absPath),
		Triggers:               cfg.Triggers,
		RequiredEnv:            getRequiredEnv(cfg),
	}, nil
}

// LoadSkillsFromDir loads all SKILL.md files from a directory.
//
// Directory structure:
//
//	dir/
//	  <skill-name>/
//	    SKILL.md
//	  <another-skill>/
//	    SKILL.md
//
// Only scans supported skill entrypoint files in immediate subdirectories.
// Returns nil, nil if the directory does not exist.
func LoadSkillsFromDir(dir string) ([]*Skill, error) {
	rootInfo, err := os.Lstat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			if pkgLogger != nil {
				pkgLogger.Debug(logger.CatApp, "skill: directory not found, skipping",
					"dir", dir)
			}
			return nil, nil
		}
		return nil, fmt.Errorf("stat skills dir %s: %w", dir, err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return nil, fmt.Errorf("skills path %s is not a regular directory", dir)
	}

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
		path := filepath.Join(dir, e.Name())
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			continue
		}
		skillFile, err := findSkillMarkdownFile(path)
		if err != nil {
			continue
		}

		md, err := ParseSkillMD(skillFile)
		if err != nil {
			// Failure to load a single skill does not block others.
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

// findSkillMarkdownFile accepts the filenames used by ClawHub and older
// Claude/OpenClaw skill packages. ClawHub installs packages as directories;
// SoloQueue only needs to discover their entrypoint and never manages them.
func findSkillMarkdownFile(dir string) (string, error) {
	for _, name := range []string{"SKILL.md", "skill.md", "skills.md"} {
		path := filepath.Join(dir, name)
		info, err := os.Lstat(path)
		if err == nil && info.Mode()&os.ModeSymlink == 0 && info.Mode().IsRegular() {
			return path, nil
		}
	}
	return "", os.ErrNotExist
}

// readSkillMarkdown rejects symlink entrypoints both before and during the
// open. The no-follow open keeps the check and read on the same filesystem
// object when a package is being replaced concurrently.
func readSkillMarkdown(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("skill markdown is not a regular file")
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

// LoadSkillsFromDirs loads skills from multiple directories by priority.
//
// The keys of 'dirs' are scope identifiers ("plugin", "user", "project"), and values are directory paths.
// Lower priority skills are loaded first, higher priority skills loaded later will overwrite skills with the same name.
// Priority order: plugin → user → project (project has the highest priority).
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
			return nil, fmt.Errorf("load %s skills: %w", scope, err)
		}
		for _, s := range skills {
			seen[s.ID] = s // Later loads overwrite earlier ones
		}
	}
	result := make([]*Skill, 0, len(seen))
	for _, s := range seen {
		result = append(result, s)
	}
	return result, nil
}

// --- Internal Helpers -------------------------------------------------------

// parseFrontmatter parses the YAML frontmatter and body from SKILL.md content.
func parseFrontmatter(data []byte) (SkillMDConfig, string, error) {
	var cfg SkillMDConfig

	content := string(data)

	// Detect --- separator
	if !strings.HasPrefix(content, "---") {
		// No frontmatter, treat entire content as body
		return cfg, content, nil
	}

	// Split by line, find the second "---" on a line by itself
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], "\r") != "---" {
		return cfg, content, nil
	}

	endLineIdx := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], "\r") == "---" {
			endLineIdx = i
			break
		}
	}

	if endLineIdx < 0 {
		// Closing --- not found
		return cfg, content, nil
	}

	// Join frontmatter lines
	fmLines := lines[1:endLineIdx]
	fm := strings.Join(fmLines, "\n")

	// Join body lines
	bodyLines := lines[endLineIdx+1:]
	body := strings.Join(bodyLines, "\n")

	if err := yaml.Unmarshal([]byte(fm), &cfg); err != nil {
		return cfg, body, fmt.Errorf("yaml unmarshal: %w", err)
	}

	return cfg, body, nil
}

// skillNameFromPath extracts the skill name (parent directory name) from the file path.
//
//	/home/user/.soloqueue/skills/commit/SKILL.md → "commit"
func skillNameFromPath(path string) string {
	return filepath.Base(filepath.Dir(path))
}

// firstParagraph extracts the first paragraph of the markdown body as the description.
func firstParagraph(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	// Take content before the first empty line or heading
	lines := strings.Split(body, "\n")
	var para []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" && len(para) > 0 {
			break
		}
		// Skip heading lines
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
	// Truncate overly long descriptions
	if len(desc) > 200 {
		desc = desc[:197] + "..."
	}
	return desc
}

func getRequiredEnv(cfg SkillMDConfig) []string {
	var envs []string
	if len(cfg.RequiredEnv) > 0 {
		envs = cfg.RequiredEnv
	} else if len(cfg.RequiredEnvDash) > 0 {
		envs = cfg.RequiredEnvDash
	}

	// ClawHub packages commonly use one of these metadata namespaces. Accept
	// aliases so package metadata remains portable across compatible clients.
	if cfg.Metadata != nil {
		for _, namespace := range []string{"openclaw", "clawdbot", "clawdis"} {
			metadata, ok := cfg.Metadata[namespace].(map[string]any)
			if !ok {
				continue
			}
			requires, ok := metadata["requires"].(map[string]any)
			if !ok {
				continue
			}
			switch envList := requires["env"].(type) {
			case []any:
				for _, item := range envList {
					if str, ok := item.(string); ok {
						envs = append(envs, str)
					}
				}
			case []string:
				envs = append(envs, envList...)
			}
		}
	}

	// Remove duplicates and empty strings
	seen := make(map[string]bool)
	var uniqueEnvs []string
	for _, env := range envs {
		env = strings.TrimSpace(env)
		if env != "" && !seen[env] {
			seen[env] = true
			uniqueEnvs = append(uniqueEnvs, env)
		}
	}
	return uniqueEnvs
}
