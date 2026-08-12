package skill

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

// SkillsUpdateConfig defines which skills are allowed to be auto-updated.
// By default, if a skill is not listed in AutoUpdate or set to false, auto-update is rejected.
type SkillsUpdateConfig struct {
	AutoUpdate map[string]bool `yaml:"auto_update"`
}

const skillsUpdateFileName = "skills_update.yaml"

// LoadSkillsUpdateConfig reads the YAML configuration file. If the file does
// not exist, it creates a default one.
func LoadSkillsUpdateConfig(workDir string) (*SkillsUpdateConfig, error) {
	yamlPath := filepath.Join(workDir, skillsUpdateFileName)

	if _, err := os.Stat(yamlPath); os.IsNotExist(err) {
		defaultContent := `# Skills Auto-Update Configuration
# By default, all skills have auto-update disabled.
# Set a skill's ID to true to enable auto-update for it.
auto_update:
  # color-system: true
  # agent-browser: true
`
		if err := os.MkdirAll(filepath.Dir(yamlPath), 0o755); err != nil {
			return nil, fmt.Errorf("failed to create config directory: %w", err)
		}
		if err := os.WriteFile(yamlPath, []byte(defaultContent), 0o644); err != nil {
			return nil, fmt.Errorf("failed to write default config file: %w", err)
		}
		return &SkillsUpdateConfig{AutoUpdate: make(map[string]bool)}, nil
	}

	data, err := os.ReadFile(yamlPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read skills update config: %w", err)
	}

	var cfg SkillsUpdateConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse skills update config: %w", err)
	}
	if cfg.AutoUpdate == nil {
		cfg.AutoUpdate = make(map[string]bool)
	}
	return &cfg, nil
}

// SyncRemoteSkills performs remote sync for Git-linked stub skills.
// It checks both local installed skills and embedded skills for upstream information.
func SyncRemoteSkills(ctx context.Context, workDir, userSkillsDir string, reg *SkillRegistry, log *logger.Logger, embeddedFS fs.FS) error {
	if userSkillsDir == "" {
		return nil
	}

	// log / info / debug / warn are no-op when log is nil so this function is
	// safe to call from tests without a real logger.
	info := func(msg string, kv ...any) {
		if log != nil {
			log.Info(logger.CatApp, msg, kv...)
		}
	}
	debug := func(msg string, kv ...any) {
		if log != nil {
			log.Debug(logger.CatApp, msg, kv...)
		}
	}
	warn := func(msg string, kv ...any) {
		if log != nil {
			log.Warn(logger.CatApp, msg, kv...)
		}
	}

	cfg, err := LoadSkillsUpdateConfig(workDir)
	if err != nil {
		return fmt.Errorf("failed to load skills update config: %w", err)
	}

	installed, err := LoadSkillsFromDir(userSkillsDir)
	if err != nil {
		return fmt.Errorf("failed to load installed skills: %w", err)
	}

	// Load embedded skills to get upstream information
	embeddedSkills := make(map[string]*Skill)
	if embeddedFS != nil {
		if embedded, err := LoadSkillsFromFS(embeddedFS, "skills"); err == nil {
			for _, s := range embedded {
				embeddedSkills[s.ID] = s
			}
		}
	}

	var toSync []*Skill
	stats := struct {
		installed         int
		withUpstream      int
		withAutoUpdateOn  int
	}{installed: len(installed)}
	for _, s := range installed {
		// Check if local skill has upstream, or if embedded version has upstream
		upstream := s.Upstream
		branch := s.Branch
		subPath := s.SubPath

		if upstream == "" {
			// Try to get upstream from embedded skill
			if embedded, ok := embeddedSkills[s.ID]; ok && embedded.Upstream != "" {
				upstream = embedded.Upstream
				branch = embedded.Branch
				subPath = embedded.SubPath
			}
		}

		if upstream == "" {
			debug("skipping skill: no upstream", "id", s.ID)
			continue
		}
		stats.withUpstream++

		if !cfg.AutoUpdate[s.ID] {
			debug("skipping skill: auto_update disabled", "id", s.ID)
			continue
		}
		stats.withAutoUpdateOn++

		// Create a copy with the correct upstream info
		syncSkill := *s
		syncSkill.Upstream = upstream
		syncSkill.Branch = branch
		syncSkill.SubPath = subPath
		toSync = append(toSync, &syncSkill)
	}

	// Open log file up front so we can write the no-op summary too.
	logsDir := filepath.Join(workDir, "logs")
	_ = os.MkdirAll(logsDir, 0o755)
	logFilePath := filepath.Join(logsDir, "skill_updates.log")
	logFile, logErr := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)

	writeSummary := func(checked, updated int) {
		timeStr := time.Now().Format("2006-01-02 15:04:05")
		var entry string
		if updated > 0 {
			entry = fmt.Sprintf("[%s] Remote skill sync run: checked=%d updated=%d\n\n", timeStr, checked, updated)
		} else {
			entry = fmt.Sprintf("[%s] Remote skill sync run: checked=%d updated=0 (no changes)\n\n", timeStr, checked)
		}
		if logErr == nil && logFile != nil {
			_, _ = logFile.WriteString(entry)
		}
	}

	if len(toSync) == 0 {
		info("remote skill sync: nothing to check",
			"installed", stats.installed,
			"with_upstream", stats.withUpstream,
			"with_auto_update", stats.withAutoUpdateOn)
		writeSummary(0, 0)
		if logFile != nil {
			_ = logFile.Close()
		}
		return nil
	}

	info("syncing remote skills start", "count", len(toSync))
	updatedAny := false
	updatedCount := 0

	for _, s := range toSync {
		if err := ctx.Err(); err != nil {
			if logFile != nil {
				_ = logFile.Close()
			}
			return err
		}

		debug("syncing remote skill", "id", s.ID, "upstream", s.Upstream)

		tempDir, err := os.MkdirTemp("", "soloqueue-skill-sync-*")
		if err != nil {
			warn("failed to create temp dir for skill sync", "id", s.ID, "err", err.Error())
			continue
		}

		branch := s.Branch
		if branch == "" {
			branch = "main"
		}

		args := []string{"clone", "--depth", "1", "-b", branch, s.Upstream, tempDir}
		cmd := exec.CommandContext(ctx, "git", args...)
		var stderr strings.Builder
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			argsRetry := []string{"clone", "--depth", "1", s.Upstream, tempDir}
			cmdRetry := exec.CommandContext(ctx, "git", argsRetry...)
			var stderrRetry strings.Builder
			cmdRetry.Stderr = &stderrRetry
			if errRetry := cmdRetry.Run(); errRetry != nil {
				warn("failed to clone remote skill repo", "id", s.ID, "upstream", s.Upstream, "err", errRetry.Error(), "stderr", strings.TrimSpace(stderrRetry.String()))
				_ = os.RemoveAll(tempDir)
				continue
			}
		}

		srcPath := tempDir
		if s.SubPath != "" {
			srcPath = filepath.Join(tempDir, filepath.FromSlash(s.SubPath))
		}

		if _, err := os.Stat(filepath.Join(srcPath, "SKILL.md")); os.IsNotExist(err) {
			warn("remote repository does not contain SKILL.md for skill", "id", s.ID, "path", srcPath)
			_ = os.RemoveAll(tempDir)
			continue
		}

		equal, modified, added, removed, compErr := compareDirectories(srcPath, s.Dir)
		if compErr != nil {
			warn("failed to compare directories during remote sync", "id", s.ID, "err", compErr.Error())
			_ = os.RemoveAll(tempDir)
			continue
		}

		if !equal {
			disabledFile := filepath.Join(s.Dir, ".disabled")
			hasDisabled := false
			if _, err := os.Stat(disabledFile); err == nil {
				hasDisabled = true
			}

			_ = os.RemoveAll(s.Dir)
			if err := copyDir(srcPath, s.Dir); err != nil {
				if log != nil {
					log.LogError(ctx, logger.CatApp, "failed to copy remote skill files", err, "id", s.ID)
				}
				_ = os.RemoveAll(tempDir)
				continue
			}

			if hasDisabled {
				_ = os.WriteFile(disabledFile, []byte(""), 0o644)
			}

			updatedAny = true
			updatedCount++
			info("updated skill from remote", "id", s.ID, "upstream", s.Upstream)

			if logErr == nil && logFile != nil {
				timeStr := time.Now().Format("2006-01-02 15:04:05")
				logEntry := fmt.Sprintf("[%s] Skill %q updated from remote %q (branch: %q)\n", timeStr, s.ID, s.Upstream, branch)
				if len(modified) > 0 {
					logEntry += fmt.Sprintf("  Modified: %s\n", strings.Join(modified, ", "))
				}
				if len(added) > 0 {
					logEntry += fmt.Sprintf("  Added:    %s\n", strings.Join(added, ", "))
				}
				if len(removed) > 0 {
					logEntry += fmt.Sprintf("  Removed:  %s\n", strings.Join(removed, ", "))
				}
				logEntry += "\n"
				_, _ = logFile.WriteString(logEntry)
			}
		}

		_ = os.RemoveAll(tempDir)
	}

	writeSummary(len(toSync), updatedCount)

	if logFile != nil {
		_ = logFile.Close()
	}

	if updatedAny && reg != nil {
		skillDirs := map[string]string{
			"user": userSkillsDir,
		}
		if err := reg.Rebuild(skillDirs); err != nil {
			warn("failed to rebuild skill registry after remote sync", "err", err.Error())
		}
	}

	info("remote skill sync done", "checked", len(toSync), "updated_any", updatedAny)
	return nil
}

// StartRemoteSkillsSyncLoop starts a background loop to sync remote skills periodically.
func StartRemoteSkillsSyncLoop(ctx context.Context, workDir, userSkillsDir string, reg *SkillRegistry, log *logger.Logger, interval time.Duration, embeddedFS fs.FS) {
	// Sync immediately on start
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}

		if err := SyncRemoteSkills(ctx, workDir, userSkillsDir, reg, log, embeddedFS); err != nil {
			log.Warn(logger.CatApp, "initial remote skill sync failed", "err", err.Error())
		}
	}()

	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := SyncRemoteSkills(ctx, workDir, userSkillsDir, reg, log, embeddedFS); err != nil {
					log.Warn(logger.CatApp, "remote skill sync failed", "err", err.Error())
				}
			}
		}
	}()
}

// compareDirectories compares files between srcDir and dstDir recursively.
func compareDirectories(srcDir, dstDir string) (equal bool, modified []string, added []string, removed []string, err error) {
	type fileSignature struct {
		hash string
		perm os.FileMode
	}
	srcFiles := make(map[string]fileSignature)
	dstFiles := make(map[string]fileSignature)

	err = filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if path != srcDir && info.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		name := info.Name()
		if strings.HasPrefix(name, ".") {
			return nil
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		hash, err := fileHash(path)
		if err != nil {
			return err
		}
		srcFiles[filepath.ToSlash(rel)] = fileSignature{hash: hash, perm: info.Mode().Perm()}
		return nil
	})
	if err != nil {
		return false, nil, nil, nil, err
	}

	err = filepath.Walk(dstDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if path != dstDir && info.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		name := info.Name()
		if strings.HasPrefix(name, ".") {
			return nil
		}
		rel, err := filepath.Rel(dstDir, path)
		if err != nil {
			return err
		}
		hash, err := fileHash(path)
		if err != nil {
			return err
		}
		dstFiles[filepath.ToSlash(rel)] = fileSignature{hash: hash, perm: info.Mode().Perm()}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return false, nil, nil, nil, err
	}

	for rel, srcSignature := range srcFiles {
		dstSignature, exists := dstFiles[rel]
		if !exists {
			added = append(added, rel)
		} else if srcSignature != dstSignature {
			modified = append(modified, rel)
		}
	}

	for rel := range dstFiles {
		if _, exists := srcFiles[rel]; !exists {
			removed = append(removed, rel)
		}
	}

	equal = len(modified) == 0 && len(added) == 0 && len(removed) == 0
	return equal, modified, added, removed, nil
}

func fileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
