package skill

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadSkillsUpdateConfig(t *testing.T) {
	tempDir := t.TempDir()

	// 1. File does not exist, should create default file and load empty map
	cfg, err := LoadSkillsUpdateConfig(tempDir)
	if err != nil {
		t.Fatalf("LoadSkillsUpdateConfig failed: %v", err)
	}
	if cfg == nil || cfg.AutoUpdate == nil {
		t.Fatal("expected non-nil config and auto_update map")
	}

	configPath := filepath.Join(tempDir, "skills_update.toml")
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("expected config file to be created, got error: %v", err)
	}

	// 2. Write custom values and load again
	customContent := `
[auto_update]
test-skill = true
another-skill = false
`
	if err := os.WriteFile(configPath, []byte(customContent), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err = LoadSkillsUpdateConfig(tempDir)
	if err != nil {
		t.Fatalf("LoadSkillsUpdateConfig failed: %v", err)
	}

	if !cfg.AutoUpdate["test-skill"] {
		t.Error("expected test-skill to be true")
	}
	if cfg.AutoUpdate["another-skill"] {
		t.Error("expected another-skill to be false")
	}
}

func TestComputeCatalogSignature(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	now := time.Now()

	// Create identical structures in both
	for _, dir := range []string{dir1, dir2} {
		f1 := filepath.Join(dir, "file1.txt")
		f2 := filepath.Join(dir, "file2.txt")
		_ = os.WriteFile(f1, []byte("content1"), 0o644)
		_ = os.WriteFile(f2, []byte("content2"), 0o644)
		_ = os.Chtimes(f1, now, now)
		_ = os.Chtimes(f2, now, now)
	}

	sig1, err := computeCatalogSignature([]string{dir1})
	if err != nil {
		t.Fatal(err)
	}
	sig2, err := computeCatalogSignature([]string{dir2})
	if err != nil {
		t.Fatal(err)
	}

	// Signatures must be deterministic
	if sig1 != sig2 {
		t.Errorf("signatures should be identical, got %q and %q", sig1, sig2)
	}

	// Modify file1 in dir1
	_ = os.WriteFile(filepath.Join(dir1, "file1.txt"), []byte("content1 modified"), 0o644)
	sig1Modified, err := computeCatalogSignature([]string{dir1})
	if err != nil {
		t.Fatal(err)
	}

	if sig1Modified == sig1 {
		t.Error("signature should change when a file is modified")
	}

	// Add file in dir2
	_ = os.WriteFile(filepath.Join(dir2, "file3.txt"), []byte("content3"), 0o644)
	sig2Added, err := computeCatalogSignature([]string{dir2})
	if err != nil {
		t.Fatal(err)
	}

	if sig2Added == sig2 {
		t.Error("signature should change when a file is added")
	}
}

func TestAutoUpdateLocalSkills(t *testing.T) {
	workDir := t.TempDir()
	userSkillsDir := filepath.Join(workDir, "user-skills")
	catalogDir := filepath.Join(workDir, "catalog")

	_ = os.MkdirAll(userSkillsDir, 0o755)
	_ = os.MkdirAll(catalogDir, 0o755)

	// Create skill "test-local" in catalog
	skillID := "test-local"
	catalogSkillPath := filepath.Join(catalogDir, skillID)
	_ = os.MkdirAll(catalogSkillPath, 0o755)
	_ = os.WriteFile(filepath.Join(catalogSkillPath, "SKILL.md"), []byte(`---
name: "test-local"
description: "A test local skill"
---
Instruction body`), 0o644)

	// Install it to userSkillsDir
	userSkillPath := filepath.Join(userSkillsDir, skillID)
	_ = os.MkdirAll(userSkillPath, 0o755)
	_ = os.WriteFile(filepath.Join(userSkillPath, "SKILL.md"), []byte(`---
name: "test-local"
description: "A test local skill"
---
Instruction body`), 0o644)

	// Write skills_update.toml permitting update
	configPath := filepath.Join(workDir, "skills_update.toml")
	customContent := `
[auto_update]
test-local = true
`
	_ = os.WriteFile(configPath, []byte(customContent), 0o644)

	// First run to establish state hash
	AutoUpdateLocalSkills(workDir, userSkillsDir, []string{catalogDir})

	hashFile := filepath.Join(workDir, "local_skills_state.hash")
	if _, err := os.Stat(hashFile); err != nil {
		t.Fatal("expected state hash file to be created")
	}

	// 1. Modify the catalog skill file
	_ = os.WriteFile(filepath.Join(catalogSkillPath, "SKILL.md"), []byte(`---
name: "test-local"
description: "A test local skill"
---
Instruction body UPDATED`), 0o644)

	// Run update again
	AutoUpdateLocalSkills(workDir, userSkillsDir, []string{catalogDir})

	// Verify update happened
	data, err := os.ReadFile(filepath.Join(userSkillPath, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Instruction body UPDATED") {
		t.Error("expected skill to be updated, but content remains old")
	}

	// 2. Setup config to disallow auto-update
	customContentDisallowed := `
[auto_update]
test-local = false
`
	_ = os.WriteFile(configPath, []byte(customContentDisallowed), 0o644)

	// Modify catalog file again
	_ = os.WriteFile(filepath.Join(catalogSkillPath, "SKILL.md"), []byte(`---
name: "test-local"
description: "A test local skill"
---
Instruction body UPDATED TWICE`), 0o644)

	// Run update
	AutoUpdateLocalSkills(workDir, userSkillsDir, []string{catalogDir})

	// Verify update DID NOT happen
	data, err = os.ReadFile(filepath.Join(userSkillPath, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "Instruction body UPDATED TWICE") {
		t.Error("expected skill NOT to be updated since it is disabled in config")
	}
}

func TestSyncRemoteSkills_NoUpdates(t *testing.T) {
	// Simple test verifying SyncRemoteSkills doesn't crash or fail when there are no skills to sync
	workDir := t.TempDir()
	userDir := filepath.Join(workDir, "user-skills")
	_ = os.MkdirAll(userDir, 0o755)

	err := SyncRemoteSkills(context.Background(), workDir, userDir, nil, nil)
	if err != nil {
		t.Fatalf("SyncRemoteSkills with no remote skills failed: %v", err)
	}
}
