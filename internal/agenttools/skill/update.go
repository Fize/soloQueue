package skill

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"gopkg.in/yaml.v3"
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
		installed        int
		withUpstream     int
		withAutoUpdateOn int
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

		srcPath, err := remoteSkillSourcePath(tempDir, s.SubPath)
		if err != nil {
			warn("remote skill source path is unsafe", "id", s.ID, "path", s.SubPath, "err", err.Error())
			_ = os.RemoveAll(tempDir)
			continue
		}

		skillManifest := filepath.Join(srcPath, "SKILL.md")
		manifestInfo, err := os.Lstat(skillManifest)
		if err != nil || !manifestInfo.Mode().IsRegular() {
			warn("remote repository does not contain a regular SKILL.md for skill", "id", s.ID, "path", srcPath, "err", err)
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
			if err := syncManagedFiles(srcPath, s.Dir); err != nil {
				if log != nil {
					log.LogError(ctx, logger.CatApp, "failed to copy remote skill files", err, "id", s.ID)
				}
				_ = os.RemoveAll(tempDir)
				continue
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

// compareDirectories compares remote-managed files between srcDir and dstDir.
// Local-only and ignored paths are intentionally outside the comparison: the
// remote tree is an incremental overlay, not a mirror that owns runtime state.
func compareDirectories(srcDir, dstDir string) (equal bool, modified []string, added []string, removed []string, err error) {
	type fileSignature struct {
		hash string
		perm os.FileMode
	}
	srcFiles := make(map[string]fileSignature)
	if err := requireRemoteDirectory(srcDir); err != nil {
		return false, nil, nil, nil, err
	}
	ignore, err := loadSkillIgnoreRules(srcDir, dstDir)
	if err != nil {
		return false, nil, nil, nil, err
	}

	err = filepath.Walk(srcDir, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if filePath == srcDir {
			return nil
		}
		rel, err := filepath.Rel(srcDir, filePath)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if ignore.matches(rel, info.IsDir()) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			if conflictErr := remoteDirectoryConflict(dstDir, rel); conflictErr != nil {
				return conflictErr
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("remote managed path %q is not a regular file", rel)
		}
		hash, err := fileHash(filePath)
		if err != nil {
			return err
		}
		srcFiles[rel] = fileSignature{hash: hash, perm: info.Mode().Perm()}
		return nil
	})
	if err != nil {
		return false, nil, nil, nil, err
	}

	for rel, srcSignature := range srcFiles {
		dstPath := filepath.Join(dstDir, filepath.FromSlash(rel))
		info, statErr := os.Lstat(dstPath)
		if os.IsNotExist(statErr) {
			added = append(added, rel)
			continue
		}
		if statErr != nil {
			return false, nil, nil, nil, statErr
		}
		if !info.Mode().IsRegular() {
			return false, nil, nil, nil, skillSyncConflict(rel, "remote file", info)
		}
		hash, hashErr := fileHash(dstPath)
		if hashErr != nil {
			return false, nil, nil, nil, hashErr
		}
		dstSignature := fileSignature{hash: hash, perm: info.Mode().Perm()}
		if srcSignature != dstSignature {
			modified = append(modified, rel)
		}
	}

	sort.Strings(modified)
	sort.Strings(added)
	equal = len(modified) == 0 && len(added) == 0
	return equal, modified, added, removed, nil
}

// syncManagedFiles overlays non-ignored remote files onto dstDir. It never
// removes paths merely because they are absent upstream. Type conflicts are
// reported instead of deleting local-only content to make the overlay fit.
func syncManagedFiles(srcDir, dstDir string) error {
	if err := requireRemoteDirectory(srcDir); err != nil {
		return err
	}
	ignore, err := loadSkillIgnoreRules(srcDir, dstDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return err
	}

	return filepath.Walk(srcDir, func(srcPath string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if srcPath == srcDir {
			return nil
		}
		rel, err := filepath.Rel(srcDir, srcPath)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if ignore.matches(rel, info.IsDir()) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		dstPath := filepath.Join(dstDir, filepath.FromSlash(rel))
		if info.IsDir() {
			if conflictErr := remoteDirectoryConflict(dstDir, rel); conflictErr != nil {
				return conflictErr
			}
			if err := os.MkdirAll(dstPath, info.Mode().Perm()|0o700); err != nil {
				return err
			}
			return os.Chmod(dstPath, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("remote managed path %q is not a regular file", rel)
		}

		dstInfo, statErr := os.Lstat(dstPath)
		if statErr == nil && !dstInfo.Mode().IsRegular() {
			return skillSyncConflict(rel, "remote file", dstInfo)
		} else if statErr != nil && !os.IsNotExist(statErr) {
			return statErr
		}
		if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
			return err
		}
		return copyFile(srcPath, dstPath)
	})
}

var errSkillSyncConflict = errors.New("skill sync type conflict")

func remoteSkillSourcePath(repoDir, subPath string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(subPath))
	if subPath == "" {
		clean = "."
	}
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("subpath %q escapes the cloned repository", subPath)
	}

	candidate := filepath.Join(repoDir, clean)
	rel, err := filepath.Rel(repoDir, candidate)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("subpath %q escapes the cloned repository", subPath)
	}
	current := repoDir
	if rel != "." {
		for _, component := range strings.Split(rel, string(filepath.Separator)) {
			current = filepath.Join(current, component)
			info, err := os.Lstat(current)
			if err != nil {
				return "", err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return "", fmt.Errorf("remote skill source component %q is a symlink", component)
			}
		}
	}
	if err := requireRemoteDirectory(candidate); err != nil {
		return "", err
	}
	return candidate, nil
}

func requireRemoteDirectory(srcDir string) error {
	info, err := os.Lstat(srcDir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("remote skill source %s is not a regular directory", srcDir)
	}
	return nil
}

func remoteDirectoryConflict(dstDir, rel string) error {
	dstPath := filepath.Join(dstDir, filepath.FromSlash(rel))
	dstInfo, err := os.Lstat(dstPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if dstInfo.IsDir() {
		return nil
	}
	return skillSyncConflict(rel, "remote directory", dstInfo)
}

func skillSyncConflict(rel, remoteType string, localInfo os.FileInfo) error {
	localType := "non-regular path"
	switch {
	case localInfo.IsDir():
		localType = "directory"
	case localInfo.Mode().IsRegular():
		localType = "file"
	case localInfo.Mode()&os.ModeSymlink != 0:
		localType = "symlink"
	}
	return fmt.Errorf("%w at %q: %s conflicts with local %s; local content was preserved", errSkillSyncConflict, rel, remoteType, localType)
}

type skillIgnoreRules struct {
	remote gitignoreMatcher
	local  gitignoreMatcher
}

func loadSkillIgnoreRules(srcDir, dstDir string) (skillIgnoreRules, error) {
	remote, err := loadGitignore(filepath.Join(srcDir, ".gitignore"))
	if err != nil {
		return skillIgnoreRules{}, err
	}
	local, err := loadGitignore(filepath.Join(dstDir, ".gitignore"))
	if err != nil {
		return skillIgnoreRules{}, err
	}
	return skillIgnoreRules{remote: remote, local: local}, nil
}

func (r skillIgnoreRules) matches(rel string, isDir bool) bool {
	return isBuiltinSkillIgnored(rel) || r.remote.matches(rel, isDir) || r.local.matches(rel, isDir)
}

var builtinSkillIgnoredDirs = map[string]struct{}{
	".cache": {}, ".git": {}, ".mypy_cache": {}, ".next": {}, ".nox": {},
	".npm": {}, ".nuxt": {}, ".parcel-cache": {}, ".pnpm-store": {},
	".pytest_cache": {}, ".ruff_cache": {}, ".tox": {}, ".turbo": {},
	".venv": {}, ".yarn": {}, "__pycache__": {}, "bower_components": {},
	"__pypackages__": {}, "coverage": {}, "data": {}, "env": {}, "logs": {}, "node_modules": {},
	"site-packages": {}, "temp": {}, "tmp": {}, "venv": {},
}

func isBuiltinSkillIgnored(rel string) bool {
	rel = strings.Trim(filepath.ToSlash(rel), "/")
	if rel == "" {
		return false
	}
	parts := strings.Split(rel, "/")
	for _, part := range parts {
		if _, ok := builtinSkillIgnoredDirs[part]; ok {
			return true
		}
	}
	base := parts[len(parts)-1]
	if base == ".disabled" || base == ".DS_Store" || base == ".env" || strings.HasPrefix(base, ".env.") {
		return true
	}
	lower := strings.ToLower(base)
	for _, suffix := range []string{".pyc", ".pyo", ".db", ".db-shm", ".db-wal", ".sqlite", ".sqlite3", ".log"} {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return strings.HasSuffix(lower, ".egg-info")
}

type gitignorePattern struct {
	pattern  string
	negated  bool
	dirOnly  bool
	rooted   bool
	baseOnly bool
}

type gitignoreMatcher []gitignorePattern

func loadGitignore(ignorePath string) (gitignoreMatcher, error) {
	info, err := os.Lstat(ignorePath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect %s: %w", ignorePath, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("ignore file %s is not a regular file", ignorePath)
	}
	data, err := os.ReadFile(ignorePath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", ignorePath, err)
	}

	var matcher gitignoreMatcher
	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSuffix(rawLine, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, `\#`) || strings.HasPrefix(line, `\!`) {
			line = line[1:]
		}
		negated := strings.HasPrefix(line, "!")
		if negated {
			line = strings.TrimPrefix(line, "!")
		}
		dirOnly := strings.HasSuffix(line, "/")
		line = strings.TrimSuffix(line, "/")
		rooted := strings.HasPrefix(line, "/")
		line = strings.TrimPrefix(line, "/")
		line = path.Clean(filepath.ToSlash(line))
		if line == "." || line == "" {
			continue
		}
		matcher = append(matcher, gitignorePattern{
			pattern:  line,
			negated:  negated,
			dirOnly:  dirOnly,
			rooted:   rooted,
			baseOnly: !strings.Contains(line, "/"),
		})
	}
	return matcher, nil
}

func (m gitignoreMatcher) matches(rel string, isDir bool) bool {
	rel = strings.Trim(filepath.ToSlash(rel), "/")
	ignored := false
	for _, rule := range m {
		if rule.matches(rel, isDir) {
			ignored = !rule.negated
		}
	}
	return ignored
}

func (p gitignorePattern) matches(rel string, isDir bool) bool {
	if rel == "" {
		return false
	}
	if p.baseOnly && !p.rooted {
		parts := strings.Split(rel, "/")
		for i, part := range parts {
			matched, _ := doublestar.Match(p.pattern, part)
			if matched && (!p.dirOnly || i < len(parts)-1 || isDir) {
				return true
			}
		}
		return false
	}

	parts := strings.Split(rel, "/")
	for i := len(parts); i > 0; i-- {
		candidate := strings.Join(parts[:i], "/")
		candidateIsDir := i < len(parts) || isDir
		matched, _ := doublestar.Match(p.pattern, candidate)
		if matched && (!p.dirOnly || candidateIsDir) {
			return true
		}
	}
	return false
}

func fileHash(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("refusing to hash non-regular file %s", path)
	}
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
