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

	"github.com/pelletier/go-toml/v2"
	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// SkillsUpdateConfig defines which skills are allowed to be auto-updated.
// By default, if a skill is not listed in AutoUpdate or set to false, auto-update is rejected.
type SkillsUpdateConfig struct {
	AutoUpdate map[string]bool `toml:"auto_update"`
}

// LoadSkillsUpdateConfig reads the configuration file. If it doesn't exist, it creates a default one.
func LoadSkillsUpdateConfig(workDir string) (*SkillsUpdateConfig, error) {
	path := filepath.Join(workDir, "skills_update.toml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		defaultContent := `# Skills Auto-Update Configuration
# By default, all skills have auto-update disabled (false).
# Set a skill's ID to true to enable auto-update for it.

[auto_update]
# Example:
# docx = true
# agent-browser = true
`
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("failed to create config directory: %w", err)
		}
		if err := os.WriteFile(path, []byte(defaultContent), 0o644); err != nil {
			return nil, fmt.Errorf("failed to write default config file: %w", err)
		}
		return &SkillsUpdateConfig{AutoUpdate: make(map[string]bool)}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read skills update config: %w", err)
	}

	var cfg SkillsUpdateConfig
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse skills update config: %w", err)
	}
	if cfg.AutoUpdate == nil {
		cfg.AutoUpdate = make(map[string]bool)
	}
	return &cfg, nil
}

// computeCatalogSignature calculates a combined checksum of file paths, sizes, and mtimes under catalogDirs.
func computeCatalogSignature(catalogDirs []string) (string, error) {
	hasher := sha256.New()
	for _, dir := range catalogDirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil // skip errors to avoid blocking
			}
			name := info.Name()
			if strings.HasPrefix(name, ".") {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if info.IsDir() {
				return nil
			}
			relPath, err := filepath.Rel(dir, path)
			if err != nil {
				return nil
			}
			fmt.Fprintf(hasher, "%s:%d:%d\n", filepath.ToSlash(relPath), info.Size(), info.ModTime().UnixNano())
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// AutoUpdateLocalSkills compares and updates local self-created skills at startup.
func AutoUpdateLocalSkills(workDir, userSkillsDir string, catalogDirs []string) {
	if userSkillsDir == "" {
		return
	}

	// 1. Calculate catalog signature
	currentSig, err := computeCatalogSignature(catalogDirs)
	if err != nil {
		if pkgLogger != nil {
			pkgLogger.Error(logger.CatApp, "failed to compute local skills signature", "err", err.Error())
		}
		return
	}

	hashFilePath := filepath.Join(workDir, "local_skills_state.hash")
	var oldSig string
	if data, err := os.ReadFile(hashFilePath); err == nil {
		oldSig = strings.TrimSpace(string(data))
	}

	if oldSig == currentSig {
		if pkgLogger != nil {
			pkgLogger.Debug(logger.CatApp, "local skills signature unchanged, skipping update check")
		}
		return
	}

	// Load configuration permissions
	cfg, err := LoadSkillsUpdateConfig(workDir)
	if err != nil {
		if pkgLogger != nil {
			pkgLogger.Error(logger.CatApp, "failed to load skills update config", "err", err.Error())
		}
		return
	}

	// 2. Load installed user skills
	installed, err := LoadSkillsFromDir(userSkillsDir)
	if err != nil {
		if pkgLogger != nil {
			pkgLogger.Error(logger.CatApp, "failed to load installed skills", "err", err.Error())
		}
		return
	}

	updatedAny := false

	for _, s := range installed {
		// Only auto-update local self-created skills (no Upstream)
		if s.Upstream != "" {
			continue
		}

		// Check if config allows auto-update
		if !cfg.AutoUpdate[s.ID] {
			continue
		}

		// Search for source in catalogDirs
		var foundSrc string
		for _, catDir := range catalogDirs {
			srcPath := filepath.Join(catDir, s.ID)
			if info, err := os.Stat(srcPath); err == nil && info.IsDir() {
				foundSrc = srcPath
				break
			}
		}

		if foundSrc == "" {
			continue
		}

		// Compare directory contents
		equal, _, _, _, err := compareDirectories(foundSrc, s.Dir)
		if err != nil {
			if pkgLogger != nil {
				pkgLogger.Warn(logger.CatApp, "failed to compare local skill directory", "id", s.ID, "err", err.Error())
			}
			continue
		}

		if !equal {
			// Update local skill: copy everything from foundSrc to s.Dir
			disabledFile := filepath.Join(s.Dir, ".disabled")
			hasDisabled := false
			if _, err := os.Stat(disabledFile); err == nil {
				hasDisabled = true
			}

			// Clean the destination first to avoid leaving orphaned files
			_ = os.RemoveAll(s.Dir)

			if err := copyDir(foundSrc, s.Dir); err != nil {
				if pkgLogger != nil {
					pkgLogger.Error(logger.CatApp, "failed to copy local skill files on auto-update", "err", err.Error(), "id", s.ID)
				}
				continue
			}

			// Restore .disabled
			if hasDisabled {
				_ = os.WriteFile(disabledFile, []byte(""), 0o644)
			}

			updatedAny = true
			if pkgLogger != nil {
				pkgLogger.Info(logger.CatApp, "auto-updated local self-created skill", "id", s.ID, "source", foundSrc)
			}
		}
	}

	// Save the new signature
	if err := os.WriteFile(hashFilePath, []byte(currentSig), 0o644); err != nil {
		if pkgLogger != nil {
			pkgLogger.Warn(logger.CatApp, "failed to save local skills state hash", "err", err.Error())
		}
	}

	if updatedAny && pkgLogger != nil {
		pkgLogger.Info(logger.CatApp, "local skills auto-update completed")
	}
}

// SyncRemoteSkills performs remote sync for Git-linked stub skills.
// It checks both local installed skills and embedded skills for upstream information.
func SyncRemoteSkills(ctx context.Context, workDir, userSkillsDir string, reg *SkillRegistry, log *logger.Logger, embeddedFS fs.FS) error {
	if userSkillsDir == "" {
		return nil
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

		if upstream != "" && cfg.AutoUpdate[s.ID] {
			// Create a copy with the correct upstream info
			syncSkill := *s
			syncSkill.Upstream = upstream
			syncSkill.Branch = branch
			syncSkill.SubPath = subPath
			toSync = append(toSync, &syncSkill)
		}
	}

	if len(toSync) == 0 {
		return nil
	}

	log.Info(logger.CatApp, "syncing remote skills start", "count", len(toSync))
	updatedAny := false

	// Open/create separate log file
	logsDir := filepath.Join(workDir, "logs")
	_ = os.MkdirAll(logsDir, 0o755)
	logFilePath := filepath.Join(logsDir, "skill_updates.log")
	logFile, logErr := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)

	for _, s := range toSync {
		if err := ctx.Err(); err != nil {
			if logFile != nil {
				_ = logFile.Close()
			}
			return err
		}

		log.Debug(logger.CatApp, "syncing remote skill", "id", s.ID, "upstream", s.Upstream)

		tempDir, err := os.MkdirTemp("", "soloqueue-skill-sync-*")
		if err != nil {
			log.Warn(logger.CatApp, "failed to create temp dir for skill sync", "id", s.ID, "err", err.Error())
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
				log.Warn(logger.CatApp, "failed to clone remote skill repo", "id", s.ID, "upstream", s.Upstream, "err", errRetry.Error(), "stderr", strings.TrimSpace(stderrRetry.String()))
				_ = os.RemoveAll(tempDir)
				continue
			}
		}

		srcPath := tempDir
		if s.SubPath != "" {
			srcPath = filepath.Join(tempDir, filepath.FromSlash(s.SubPath))
		}

		if _, err := os.Stat(filepath.Join(srcPath, "SKILL.md")); os.IsNotExist(err) {
			log.Warn(logger.CatApp, "remote repository does not contain SKILL.md for skill", "id", s.ID, "path", srcPath)
			_ = os.RemoveAll(tempDir)
			continue
		}

		equal, modified, added, removed, compErr := compareDirectories(srcPath, s.Dir)
		if compErr != nil {
			log.Warn(logger.CatApp, "failed to compare directories during remote sync", "id", s.ID, "err", compErr.Error())
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
				log.LogError(ctx, logger.CatApp, "failed to copy remote skill files", err, "id", s.ID)
				_ = os.RemoveAll(tempDir)
				continue
			}

			if hasDisabled {
				_ = os.WriteFile(disabledFile, []byte(""), 0o644)
			}

			updatedAny = true
			log.Info(logger.CatApp, "updated skill from remote", "id", s.ID, "upstream", s.Upstream)

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

	if logFile != nil {
		_ = logFile.Close()
	}

	if updatedAny && reg != nil {
		skillDirs := map[string]string{
			"user": userSkillsDir,
		}
		if err := reg.Rebuild(skillDirs); err != nil {
			log.Warn(logger.CatApp, "failed to rebuild skill registry after remote sync", "err", err.Error())
		}
	}

	log.Info(logger.CatApp, "syncing remote skills complete")
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
	srcFiles := make(map[string]string)
	dstFiles := make(map[string]string)

	err = filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
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
		srcFiles[filepath.ToSlash(rel)] = hash
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
		dstFiles[filepath.ToSlash(rel)] = hash
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return false, nil, nil, nil, err
	}

	for rel, srcH := range srcFiles {
		dstH, exists := dstFiles[rel]
		if !exists {
			added = append(added, rel)
		} else if srcH != dstH {
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
