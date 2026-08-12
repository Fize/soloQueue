package skill

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ─── LoadSkillsUpdateConfig ──────────────────────────────────────────────────

func TestLoadSkillsUpdateConfig(t *testing.T) {
	t.Run("creates_default_when_missing", func(t *testing.T) {
		tempDir := t.TempDir()
		cfg, err := LoadSkillsUpdateConfig(tempDir)
		if err != nil {
			t.Fatalf("LoadSkillsUpdateConfig failed: %v", err)
		}
		if cfg == nil || cfg.AutoUpdate == nil {
			t.Fatal("expected non-nil config and auto_update map")
		}
		if len(cfg.AutoUpdate) != 0 {
			t.Errorf("default config should have empty auto_update map, got %v", cfg.AutoUpdate)
		}
		path := filepath.Join(tempDir, "skills_update.yaml")
		if _, err := os.Stat(path); err != nil {
			t.Errorf("default config file should be created, got: %v", err)
		}
	})

	t.Run("reads_existing_yaml", func(t *testing.T) {
		tempDir := t.TempDir()
		path := filepath.Join(tempDir, "skills_update.yaml")
		content := `
auto_update:
  test-skill: true
  another-skill: false
`
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := LoadSkillsUpdateConfig(tempDir)
		if err != nil {
			t.Fatalf("LoadSkillsUpdateConfig failed: %v", err)
		}
		if !cfg.AutoUpdate["test-skill"] {
			t.Error("expected test-skill to be true")
		}
		if cfg.AutoUpdate["another-skill"] {
			t.Error("expected another-skill to be false")
		}
	})

	t.Run("auto_update_map_initialized_when_absent", func(t *testing.T) {
		// A user may hand-write `auto_update:` with no children; the loader
		// must still return a non-nil map so callers don't nil-deref.
		tempDir := t.TempDir()
		path := filepath.Join(tempDir, "skills_update.yaml")
		if err := os.WriteFile(path, []byte("auto_update: {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := LoadSkillsUpdateConfig(tempDir)
		if err != nil {
			t.Fatalf("LoadSkillsUpdateConfig failed: %v", err)
		}
		if cfg.AutoUpdate == nil {
			t.Fatal("expected non-nil AutoUpdate map for empty auto_update block")
		}
	})
}

// ─── SyncRemoteSkills ────────────────────────────────────────────────────────

// TestSyncRemoteSkills_NoInstalledSkills exercises the empty-installed-skills
// fast path. The function must be nil-safe (no nil log deref) and write a
// summary line to skill_updates.log so operators can confirm the periodic
// loop is actually running.
func TestSyncRemoteSkills_NoInstalledSkills(t *testing.T) {
	workDir := t.TempDir()
	userDir := filepath.Join(workDir, "user-skills")
	_ = os.MkdirAll(userDir, 0o755)

	if err := SyncRemoteSkills(context.Background(), workDir, userDir, nil, nil, nil); err != nil {
		t.Fatalf("SyncRemoteSkills with no skills failed: %v", err)
	}

	logBytes, err := os.ReadFile(filepath.Join(workDir, "logs", "skill_updates.log"))
	if err != nil {
		t.Fatalf("expected skill_updates.log to be created: %v", err)
	}
	if !strings.Contains(string(logBytes), "Remote skill sync run") {
		t.Errorf("expected summary line in skill_updates.log, got:\n%s", logBytes)
	}
	if !strings.Contains(string(logBytes), "checked=0 updated=0") {
		t.Errorf("expected 'checked=0 updated=0' in summary, got:\n%s", logBytes)
	}
}

// TestSyncRemoteSkills_SkipsSkillsWithoutUpstream documents the existing
// behaviour: a skill with no upstream field (and no embedded upstream match)
// is silently skipped from the sync candidate set. This is by design — the
// local catalog path handles self-created skills — and the test pins it so
// future refactors don't change it without intent.
func TestSyncRemoteSkills_SkipsSkillsWithoutUpstream(t *testing.T) {
	workDir := t.TempDir()
	userDir := filepath.Join(workDir, "user-skills")
	_ = os.MkdirAll(userDir, 0o755)

	// Install a skill with no upstream in frontmatter.
	skillID := "self-created"
	skillDir := filepath.Join(userDir, skillID)
	_ = os.MkdirAll(skillDir, 0o755)
	_ = os.WriteFile(
		filepath.Join(skillDir, "SKILL.md"),
		[]byte("---\nname: \""+skillID+"\"\ndescription: \"test\"\n---\nno upstream"),
		0o644,
	)

	// Permit it in the config — without an upstream, the sync loop still
	// must skip it.
	permit(t, workDir, skillID)

	if err := SyncRemoteSkills(context.Background(), workDir, userDir, nil, nil, nil); err != nil {
		t.Fatalf("SyncRemoteSkills failed: %v", err)
	}

	// No git clone should have happened. The strongest signal we can check
	// without mocking exec is the summary line: checked=0 since this skill
	// doesn't enter toSync.
	logBytes, err := os.ReadFile(filepath.Join(workDir, "logs", "skill_updates.log"))
	if err != nil {
		t.Fatalf("expected skill_updates.log: %v", err)
	}
	if !strings.Contains(string(logBytes), "checked=0 updated=0") {
		t.Errorf("expected skill without upstream to be excluded from toSync, got:\n%s", logBytes)
	}
}

func TestCompareDirectoriesIgnoresGitMetadata(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	for _, dir := range []string{srcDir, dstDir} {
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("same skill"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(dir, ".git", "logs"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(srcDir, ".git", "index"), []byte("source metadata"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dstDir, ".git", "index"), []byte("destination metadata"), 0o644); err != nil {
		t.Fatal(err)
	}

	equal, modified, added, removed, err := compareDirectories(srcDir, dstDir)
	if err != nil {
		t.Fatalf("compareDirectories failed: %v", err)
	}
	if !equal {
		t.Fatalf("git metadata must not affect skill comparison: modified=%v added=%v removed=%v", modified, added, removed)
	}
}

func TestCompareDirectoriesDetectsPermissionChanges(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose Unix executable permission bits")
	}
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	for _, dir := range []string{srcDir, dstDir} {
		if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "scripts", "build.sh"), []byte("#!/bin/sh\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(filepath.Join(srcDir, "scripts", "build.sh"), 0o755); err != nil {
		t.Fatal(err)
	}

	equal, modified, added, removed, err := compareDirectories(srcDir, dstDir)
	if err != nil {
		t.Fatalf("compareDirectories failed: %v", err)
	}
	if equal || len(modified) != 1 || modified[0] != "scripts/build.sh" || len(added) != 0 || len(removed) != 0 {
		t.Fatalf("permission drift must be reported as a modification: equal=%v modified=%v added=%v removed=%v", equal, modified, added, removed)
	}
}

func permit(t *testing.T, workDir, skillID string) {
	t.Helper()
	path := filepath.Join(workDir, "skills_update.yaml")
	if err := os.WriteFile(path, []byte("auto_update:\n  "+skillID+": true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
