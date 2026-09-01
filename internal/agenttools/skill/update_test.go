package skill

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

func TestCompareDirectoriesPreservesLocalOnlyAndIgnoredPaths(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	writeSkillTestFile(t, srcDir, "SKILL.md", "same skill", 0o644)
	writeSkillTestFile(t, dstDir, "SKILL.md", "same skill", 0o644)
	writeSkillTestFile(t, dstDir, "notes/local.md", "keep me", 0o644)
	writeSkillTestFile(t, dstDir, ".venv/bin/python", "local python", 0o755)
	writeSkillTestFile(t, dstDir, "node_modules/pkg/index.js", "local module", 0o644)
	writeSkillTestFile(t, dstDir, "data/cache.json", "runtime state", 0o600)
	writeSkillTestFile(t, srcDir, ".gitignore", "generated/\n", 0o644)
	writeSkillTestFile(t, dstDir, ".gitignore", "generated/\n", 0o644)
	writeSkillTestFile(t, srcDir, "generated/remote.txt", "remote generated", 0o644)
	writeSkillTestFile(t, dstDir, "generated/local.txt", "local generated", 0o644)

	equal, modified, added, removed, err := compareDirectories(srcDir, dstDir)
	if err != nil {
		t.Fatalf("compareDirectories failed: %v", err)
	}
	if !equal {
		t.Fatalf("local-only and ignored paths must not affect comparison: modified=%v added=%v removed=%v", modified, added, removed)
	}
}

func TestGitignoreMatcherSupportsNegation(t *testing.T) {
	root := t.TempDir()
	writeSkillTestFile(t, root, ".gitignore", "generated/\n!generated/keep.txt\n", 0o644)
	matcher, err := loadGitignore(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatalf("loadGitignore failed: %v", err)
	}
	if !matcher.matches("generated/drop.txt", false) {
		t.Fatal("generated/drop.txt should be ignored")
	}
	if matcher.matches("generated/keep.txt", false) {
		t.Fatal("negated generated/keep.txt should be managed")
	}
}

func TestSyncManagedFilesOverlaysRemoteAndPreservesLocalRuntime(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	writeSkillTestFile(t, srcDir, "SKILL.md", "remote skill", 0o644)
	writeSkillTestFile(t, srcDir, "scripts/run.sh", "#!/bin/sh\necho remote\n", 0o755)
	writeSkillTestFile(t, srcDir, "new/remote.txt", "remote only", 0o640)
	writeSkillTestFile(t, srcDir, ".gitignore", "generated/\n", 0o644)
	writeSkillTestFile(t, srcDir, "generated/remote.txt", "must not copy", 0o644)
	writeSkillTestFile(t, srcDir, "data/default.json", "must not copy", 0o644)

	writeSkillTestFile(t, dstDir, "SKILL.md", "local skill", 0o600)
	writeSkillTestFile(t, dstDir, "scripts/run.sh", "#!/bin/sh\necho local\n", 0o644)
	writeSkillTestFile(t, dstDir, "notes/local.md", "local only", 0o600)
	writeSkillTestFile(t, dstDir, ".venv/bin/python", "venv", 0o755)
	writeSkillTestFile(t, dstDir, "node_modules/pkg/index.js", "module", 0o644)
	writeSkillTestFile(t, dstDir, "data/runtime.db", "ledger", 0o600)
	writeSkillTestFile(t, dstDir, "generated/local.txt", "generated", 0o600)
	writeSkillTestFile(t, dstDir, ".disabled", "", 0o644)

	if err := syncManagedFiles(srcDir, dstDir); err != nil {
		t.Fatalf("syncManagedFiles failed: %v", err)
	}

	assertSkillTestFile(t, dstDir, "SKILL.md", "remote skill", 0o644)
	assertSkillTestFile(t, dstDir, "scripts/run.sh", "#!/bin/sh\necho remote\n", 0o755)
	assertSkillTestFile(t, dstDir, "new/remote.txt", "remote only", 0o640)
	assertSkillTestFile(t, dstDir, "notes/local.md", "local only", 0o600)
	assertSkillTestFile(t, dstDir, ".venv/bin/python", "venv", 0o755)
	assertSkillTestFile(t, dstDir, "node_modules/pkg/index.js", "module", 0o644)
	assertSkillTestFile(t, dstDir, "data/runtime.db", "ledger", 0o600)
	assertSkillTestFile(t, dstDir, "generated/local.txt", "generated", 0o600)
	assertSkillTestFile(t, dstDir, ".disabled", "", 0o644)
	if _, err := os.Stat(filepath.Join(dstDir, "generated", "remote.txt")); !os.IsNotExist(err) {
		t.Fatalf("gitignored remote file must not be copied, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dstDir, "data", "default.json")); !os.IsNotExist(err) {
		t.Fatalf("remote runtime data must not be copied, stat err=%v", err)
	}
}

func TestManagedRemoteSymlinksAreRejected(t *testing.T) {
	for _, tt := range []struct {
		name string
		rel  string
		run  func(string, string) error
	}{
		{
			name: "managed file",
			rel:  "scripts/run.sh",
			run: func(srcDir, dstDir string) error {
				_, _, _, _, err := compareDirectories(srcDir, dstDir)
				return err
			},
		},
		{
			name: ".gitignore",
			rel:  ".gitignore",
			run:  syncManagedFiles,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			srcDir := t.TempDir()
			dstDir := t.TempDir()
			outside := filepath.Join(t.TempDir(), "outside")
			if err := os.WriteFile(outside, []byte("host secret"), 0o644); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(srcDir, filepath.FromSlash(tt.rel))
			if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, link); err != nil {
				t.Fatal(err)
			}

			err := tt.run(srcDir, dstDir)
			if err == nil || !strings.Contains(err.Error(), "not a regular file") {
				t.Fatalf("remote symlink must be rejected explicitly, got %v", err)
			}
		})
	}
}

func TestRemoteSkillSourcePathRejectsSymlinkAndTraversal(t *testing.T) {
	repoDir := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(repoDir, "linked-skill")); err != nil {
		t.Fatal(err)
	}
	if _, err := remoteSkillSourcePath(repoDir, "linked-skill"); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink subpath must be rejected, got %v", err)
	}
	if _, err := remoteSkillSourcePath(repoDir, "../outside"); err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("traversal subpath must be rejected, got %v", err)
	}
}

func TestSyncManagedFilesTypeConflictsPreserveLocalContent(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, srcDir, dstDir string)
		keep  string
		want  string
	}{
		{
			name: "remote file conflicts with local directory",
			setup: func(t *testing.T, srcDir, dstDir string) {
				writeSkillTestFile(t, srcDir, "managed", "remote file", 0o644)
				writeSkillTestFile(t, dstDir, "managed/local-only.txt", "preserve descendant", 0o600)
			},
			keep: "managed/local-only.txt",
			want: "preserve descendant",
		},
		{
			name: "remote directory conflicts with local file",
			setup: func(t *testing.T, srcDir, dstDir string) {
				writeSkillTestFile(t, srcDir, "managed/remote.txt", "remote descendant", 0o644)
				writeSkillTestFile(t, dstDir, "managed", "preserve local file", 0o600)
			},
			keep: "managed",
			want: "preserve local file",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srcDir := t.TempDir()
			dstDir := t.TempDir()
			tt.setup(t, srcDir, dstDir)

			_, _, _, _, compareErr := compareDirectories(srcDir, dstDir)
			if !errors.Is(compareErr, errSkillSyncConflict) {
				t.Fatalf("comparison must report an explicit type conflict, got %v", compareErr)
			}
			if err := syncManagedFiles(srcDir, dstDir); !errors.Is(err, errSkillSyncConflict) {
				t.Fatalf("overlay must report an explicit type conflict, got %v", err)
			}
			got, err := os.ReadFile(filepath.Join(dstDir, filepath.FromSlash(tt.keep)))
			if err != nil {
				t.Fatalf("read preserved local content: %v", err)
			}
			if string(got) != tt.want {
				t.Fatalf("local content = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSyncRemoteSkillsIncrementalOverlay(t *testing.T) {
	workDir := t.TempDir()
	userDir := filepath.Join(workDir, "user-skills")
	remoteDir := filepath.Join(workDir, "remote")
	skillID := "incremental-skill"

	remoteSkill := fmt.Sprintf("---\nname: %s\ndescription: remote\nupstream: %s\nbranch: main\n---\nremote body\n", skillID, remoteDir)
	writeSkillTestFile(t, remoteDir, "SKILL.md", remoteSkill, 0o644)
	writeSkillTestFile(t, remoteDir, "scripts/run.sh", "#!/bin/sh\necho remote\n", 0o755)
	runGitTestCommand(t, remoteDir, "init", "-b", "main")
	runGitTestCommand(t, remoteDir, "config", "user.email", "test@example.com")
	runGitTestCommand(t, remoteDir, "config", "user.name", "SoloQueue Test")
	runGitTestCommand(t, remoteDir, "add", ".")
	runGitTestCommand(t, remoteDir, "commit", "-m", "initial")

	localSkill := fmt.Sprintf("---\nname: %s\ndescription: local\nupstream: %s\nbranch: main\n---\nlocal body\n", skillID, remoteDir)
	skillDir := filepath.Join(userDir, skillID)
	writeSkillTestFile(t, skillDir, "SKILL.md", localSkill, 0o600)
	writeSkillTestFile(t, skillDir, "scripts/run.sh", "#!/bin/sh\necho local\n", 0o644)
	writeSkillTestFile(t, skillDir, "local-only.txt", "preserve", 0o600)
	writeSkillTestFile(t, skillDir, ".venv/bin/python", "preserve venv", 0o755)
	writeSkillTestFile(t, skillDir, ".disabled", "", 0o644)
	permit(t, workDir, skillID)

	if err := SyncRemoteSkills(context.Background(), workDir, userDir, nil, nil, nil); err != nil {
		t.Fatalf("SyncRemoteSkills failed: %v", err)
	}

	assertSkillTestFile(t, skillDir, "SKILL.md", remoteSkill, 0o644)
	assertSkillTestFile(t, skillDir, "scripts/run.sh", "#!/bin/sh\necho remote\n", 0o755)
	assertSkillTestFile(t, skillDir, "local-only.txt", "preserve", 0o600)
	assertSkillTestFile(t, skillDir, ".venv/bin/python", "preserve venv", 0o755)
	assertSkillTestFile(t, skillDir, ".disabled", "", 0o644)
}

func writeSkillTestFile(t *testing.T, root, rel, content string, perm os.FileMode) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), perm); err != nil {
		t.Fatal(err)
	}
}

func assertSkillTestFile(t *testing.T, root, rel, want string, wantPerm os.FileMode) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	if string(got) != want {
		t.Fatalf("%s content = %q, want %q", rel, got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", rel, err)
	}
	if info.Mode().Perm() != wantPerm {
		t.Fatalf("%s permissions = %o, want %o", rel, info.Mode().Perm(), wantPerm)
	}
}

func runGitTestCommand(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func permit(t *testing.T, workDir, skillID string) {
	t.Helper()
	path := filepath.Join(workDir, "skills_update.yaml")
	if err := os.WriteFile(path, []byte("auto_update:\n  "+skillID+": true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
