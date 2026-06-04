package skill

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ListStoreSkills lists all available store skills from the store directory.
func ListStoreSkills(storeDir string) ([]*Skill, error) {
	if _, err := os.Stat(storeDir); os.IsNotExist(err) {
		return nil, nil
	}
	return LoadSkillsFromDir(storeDir)
}

// InstallSkill copies a skill directory from storeDir to userSkillsDir.
func InstallSkill(storeDir, userSkillsDir, id string) error {
	src := filepath.Join(storeDir, id)
	dst := filepath.Join(userSkillsDir, id)

	if _, err := os.Stat(src); os.IsNotExist(err) {
		return fmt.Errorf("skill %s not found in store", id)
	}
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("skill %s is already installed", id)
	}

	return copyDir(src, dst)
}

// InstallLocalSkill creates a symlink to a local skill or copies it if symlink fails.
func InstallLocalSkill(localPath, userSkillsDir string) error {
	absPath, err := filepath.Abs(localPath)
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(absPath, "SKILL.md")); os.IsNotExist(err) {
		return fmt.Errorf("target directory does not contain SKILL.md")
	}

	basename := filepath.Base(absPath)
	dst := filepath.Join(userSkillsDir, basename)
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("skill %s is already installed", basename)
	}

	if err := os.MkdirAll(userSkillsDir, 0o755); err != nil {
		return err
	}

	// Try symlink first, fallback to copy directory
	if err := os.Symlink(absPath, dst); err == nil {
		return nil
	}
	return copyDir(absPath, dst)
}

// InstallGithubSkill clones a git repository into userSkillsDir.
func InstallGithubSkill(ctx context.Context, repoUrl, branch, subPath, userSkillsDir string) error {
	repoUrl = strings.TrimSuffix(repoUrl, "/")
	if strings.Contains(repoUrl, "/tree/") || strings.Contains(repoUrl, "/blob/") {
		return fmt.Errorf("invalid repository URL: nested paths not allowed, url must be a repository address")
	}

	parts := strings.Split(repoUrl, "/")
	if len(parts) < 2 {
		return fmt.Errorf("invalid repository URL")
	}
	rawName := parts[len(parts)-1]
	repoName := strings.TrimSuffix(rawName, ".git")

	destName := repoName
	if subPath != "" {
		destName = filepath.Base(filepath.Clean(subPath))
	}
	dest := filepath.Join(userSkillsDir, destName)

	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("skill %s is already installed", destName)
	}

	if err := os.MkdirAll(userSkillsDir, 0o755); err != nil {
		return err
	}

	if branch == "" {
		branch = "main"
	}

	if subPath == "" {
		args := []string{"clone", "--depth", "1", "-b", branch, repoUrl, dest}
		cmd := exec.CommandContext(ctx, "git", args...)
		var stderr strings.Builder
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			argsRetry := []string{"clone", "--depth", "1", repoUrl, dest}
			cmdRetry := exec.CommandContext(ctx, "git", argsRetry...)
			var stderrRetry strings.Builder
			cmdRetry.Stderr = &stderrRetry
			if errRetry := cmdRetry.Run(); errRetry != nil {
				_ = os.RemoveAll(dest)
				return fmt.Errorf("git clone failed: %w (%s)", errRetry, strings.TrimSpace(stderrRetry.String()))
			}
		}
	} else {
		tempDir, err := os.MkdirTemp("", "soloqueue-skill-*")
		if err != nil {
			return fmt.Errorf("failed to create temp dir: %w", err)
		}
		defer os.RemoveAll(tempDir)

		args := []string{"clone", "--depth", "1", "-b", branch, repoUrl, tempDir}
		cmd := exec.CommandContext(ctx, "git", args...)
		var stderr strings.Builder
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			argsRetry := []string{"clone", "--depth", "1", repoUrl, tempDir}
			cmdRetry := exec.CommandContext(ctx, "git", argsRetry...)
			var stderrRetry strings.Builder
			cmdRetry.Stderr = &stderrRetry
			if errRetry := cmdRetry.Run(); errRetry != nil {
				return fmt.Errorf("git clone failed: %w (%s)", errRetry, strings.TrimSpace(stderrRetry.String()))
			}
		}

		srcPath := filepath.Join(tempDir, filepath.FromSlash(subPath))
		if _, err := os.Stat(srcPath); os.IsNotExist(err) {
			return fmt.Errorf("sub-directory %s not found in cloned repository", subPath)
		}

		if err := copyDir(srcPath, dest); err != nil {
			_ = os.RemoveAll(dest)
			return fmt.Errorf("failed to copy skill files: %w", err)
		}
	}

	if _, err := os.Stat(filepath.Join(dest, "SKILL.md")); os.IsNotExist(err) {
		_ = os.RemoveAll(dest)
		return fmt.Errorf("cloned repository does not contain SKILL.md")
	}

	return nil
}


// UninstallSkill deletes a user skill directory.
func UninstallSkill(userSkillsDir, id string) error {
	target := filepath.Join(userSkillsDir, id)
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		return fmt.Errorf("skill %s not found in user directory", id)
	}
	if err != nil {
		return err
	}

	// If it's a symlink, delete the symlink only
	if info.Mode()&os.ModeSymlink != 0 {
		return os.Remove(target)
	}

	return os.RemoveAll(target)
}

// ImportUserSkill writes a new SKILL.md inside userSkillsDir.
func ImportUserSkill(userSkillsDir, name, description, body string, triggers []string) error {
	slug := slugifySkillName(name)
	if slug == "" {
		return fmt.Errorf("invalid skill name")
	}
	dstDir := filepath.Join(userSkillsDir, slug)
	if _, err := os.Stat(dstDir); err == nil {
		return fmt.Errorf("skill %s already exists", slug)
	}

	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return err
	}

	mdContent := buildSkillMarkdown(name, description, body, triggers)
	return os.WriteFile(filepath.Join(dstDir, "SKILL.md"), []byte(mdContent), 0o644)
}

// UpdateUserSkill updates the SKILL.md file for a user skill.
func UpdateUserSkill(userSkillsDir, id, description, body string, triggers []string) error {
	dstDir := filepath.Join(userSkillsDir, id)
	if _, err := os.Stat(dstDir); os.IsNotExist(err) {
		// It might be a built-in skill override. Create the shadow directory.
		if err := os.MkdirAll(dstDir, 0o755); err != nil {
			return err
		}
	}

	mdContent := buildSkillMarkdown(id, description, body, triggers)
	return os.WriteFile(filepath.Join(dstDir, "SKILL.md"), []byte(mdContent), 0o644)
}

// SkillFileEntry represents a single file or directory inside a skill.
type SkillFileEntry struct {
	Path string `json:"path"`
	Kind string `json:"kind"` // "file" or "directory"
	Size *int64 `json:"size,omitempty"`
}

// ListSkillFiles recursively lists all files inside the skill directory.
func ListSkillFiles(skillDir string) ([]SkillFileEntry, error) {
	var out []SkillFileEntry
	seen := make(map[string]bool)
	maxEntries := 500
	maxDepth := 6

	var walk func(dir string, depth int) error
	walk = func(dir string, depth int) error {
		if depth > maxDepth {
			return nil
		}
		if len(out) >= maxEntries {
			return nil
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if len(out) >= maxEntries {
				return nil
			}
			if strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			abs := filepath.Join(dir, entry.Name())
			rel, err := filepath.Rel(skillDir, abs)
			if err != nil {
				continue
			}
			rel = filepath.ToSlash(rel)
			if seen[rel] {
				continue
			}
			seen[rel] = true

			if entry.IsDir() {
				out = append(out, SkillFileEntry{
					Path: rel,
					Kind: "directory",
				})
				if err := walk(abs, depth+1); err != nil {
					return err
				}
			} else {
				var size *int64
				if info, err := entry.Info(); err == nil {
					sz := info.Size()
					size = &sz
				}
				out = append(out, SkillFileEntry{
					Path: rel,
					Kind: "file",
					Size: size,
				})
			}
		}
		return nil
	}

	err := walk(skillDir, 0)
	return out, err
}

// ─── Internal Helper Functions ──────────────────────────────────────────────

func slugifySkillName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	var sb strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			sb.WriteRune(r)
		} else {
			sb.WriteRune('-')
		}
	}
	res := sb.String()
	for strings.Contains(res, "--") {
		res = strings.ReplaceAll(res, "--", "-")
	}
	res = strings.Trim(res, "-")
	if len(res) > 64 {
		res = res[:64]
	}
	return res
}

func buildSkillMarkdown(name, description, body string, triggers []string) string {
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("name: \"%s\"\n", strings.ReplaceAll(name, "\"", "\\\"")))
	if description != "" {
		sb.WriteString("description: |\n")
		for _, ln := range strings.Split(description, "\n") {
			sb.WriteString(fmt.Sprintf("  %s\n", ln))
		}
	}
	if len(triggers) > 0 {
		sb.WriteString("triggers:\n")
		for _, t := range triggers {
			trimmed := strings.TrimSpace(t)
			if trimmed != "" {
				sb.WriteString(fmt.Sprintf("  - \"%s\"\n", strings.ReplaceAll(trimmed, "\"", "\\\"")))
			}
		}
	}
	sb.WriteString("---\n\n")
	sb.WriteString(strings.TrimSpace(body))
	sb.WriteString("\n")
	return sb.String()
}

func copyDir(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, info.Mode()); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err = io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

// InstallSkillFromFS copies a skill directory from a virtual filesystem to a physical directory.
func InstallSkillFromFS(fsys fs.FS, srcDir, destDir, id string) error {
	src := filepath.ToSlash(filepath.Join(srcDir, id))
	dst := filepath.Join(destDir, id)

	if _, err := fs.Stat(fsys, src); err != nil {
		return fmt.Errorf("skill %s not found in store FS", id)
	}
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("skill %s is already installed", id)
	}

	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}

	entries, err := fs.ReadDir(fsys, src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		subSrc := filepath.ToSlash(filepath.Join(src, entry.Name()))
		subDst := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := copyFSDirToDisk(fsys, subSrc, subDst); err != nil {
				return err
			}
		} else {
			data, err := fs.ReadFile(fsys, subSrc)
			if err != nil {
				return err
			}
			if err := os.WriteFile(subDst, data, 0644); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyFSDirToDisk(fsys fs.FS, src, dst string) error {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	entries, err := fs.ReadDir(fsys, src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		subSrc := filepath.ToSlash(filepath.Join(src, entry.Name()))
		subDst := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := copyFSDirToDisk(fsys, subSrc, subDst); err != nil {
				return err
			}
		} else {
			data, err := fs.ReadFile(fsys, subSrc)
			if err != nil {
				return err
			}
			if err := os.WriteFile(subDst, data, 0644); err != nil {
				return err
			}
		}
	}
	return nil
}

// ListSkillFilesFromFS recursively lists all files inside a virtual skill directory.
func ListSkillFilesFromFS(fsys fs.FS, skillDir string) ([]SkillFileEntry, error) {
	var out []SkillFileEntry
	seen := make(map[string]bool)
	maxEntries := 500
	maxDepth := 6

	skillDir = filepath.ToSlash(skillDir)

	var walk func(dir string, depth int) error
	walk = func(dir string, depth int) error {
		if depth > maxDepth {
			return nil
		}
		if len(out) >= maxEntries {
			return nil
		}
		entries, err := fs.ReadDir(fsys, dir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if len(out) >= maxEntries {
				return nil
			}
			if strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			abs := filepath.ToSlash(filepath.Join(dir, entry.Name()))
			rel, err := filepath.Rel(skillDir, abs)
			if err != nil {
				continue
			}
			rel = filepath.ToSlash(rel)
			if seen[rel] {
				continue
			}
			seen[rel] = true

			if entry.IsDir() {
				out = append(out, SkillFileEntry{
					Path: rel,
					Kind: "directory",
				})
				if err := walk(abs, depth+1); err != nil {
					return err
				}
			} else {
				var size *int64
				if info, err := entry.Info(); err == nil {
					sz := info.Size()
					size = &sz
				}
				out = append(out, SkillFileEntry{
					Path: rel,
					Kind: "file",
					Size: size,
				})
			}
		}
		return nil
	}

	err := walk(skillDir, 0)
	return out, err
}


