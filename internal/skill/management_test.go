package skill

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSlugifySkillName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Commit Skill", "commit-skill"},
		{"  Hello_World! 123 ", "hello_world-123"},
		{"--test--slug--", "test-slug"},
		{"A very long name that exceeds sixty-four characters and should be truncated properly", "a-very-long-name-that-exceeds-sixty-four-characters-and-should-b"},
	}

	for _, tt := range tests {
		got := slugifySkillName(tt.input)
		if got != tt.want {
			t.Errorf("slugifySkillName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestListSkillFiles(t *testing.T) {
	dir := t.TempDir()

	// Create a nested file structure
	files := []string{
		"SKILL.md",
		"assets/logo.png",
		"assets/styles.css",
		"references/specs.md",
		"examples/sample.html",
	}

	for _, f := range files {
		path := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("test"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	entries, err := ListSkillFiles(dir)
	if err != nil {
		t.Fatalf("ListSkillFiles failed: %v", err)
	}

	expectedPaths := map[string]string{
		"SKILL.md":             "file",
		"assets":               "directory",
		"assets/logo.png":      "file",
		"assets/styles.css":    "file",
		"references":           "directory",
		"references/specs.md":  "file",
		"examples":             "directory",
		"examples/sample.html": "file",
	}

	if len(entries) != len(expectedPaths) {
		t.Errorf("expected %d entries, got %d", len(expectedPaths), len(entries))
	}

	for _, entry := range entries {
		kind, ok := expectedPaths[entry.Path]
		if !ok {
			t.Errorf("unexpected path found: %q", entry.Path)
			continue
		}
		if entry.Kind != kind {
			t.Errorf("path %q: kind = %q, want %q", entry.Path, entry.Kind, kind)
		}
	}
}

func TestInstallAndUninstallSkill(t *testing.T) {
	storeDir := t.TempDir()
	userDir := t.TempDir()

	// Create a skill in the store
	skillID := "test-store-skill"
	skillPath := filepath.Join(storeDir, skillID)
	if err := os.MkdirAll(filepath.Join(skillPath, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillPath, "SKILL.md"), []byte("---name: test\n---\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillPath, "assets/img.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 1. Install
	err := InstallSkill(storeDir, userDir, skillID)
	if err != nil {
		t.Fatalf("InstallSkill failed: %v", err)
	}

	installedMD := filepath.Join(userDir, skillID, "SKILL.md")
	if _, err := os.Stat(installedMD); err != nil {
		t.Errorf("SKILL.md not found in installed dir: %v", err)
	}

	// 2. Double install should fail
	err = InstallSkill(storeDir, userDir, skillID)
	if err == nil {
		t.Errorf("expected duplicate install to fail")
	}

	// 3. Uninstall
	err = UninstallSkill(userDir, skillID)
	if err != nil {
		t.Fatalf("UninstallSkill failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(userDir, skillID)); !os.IsNotExist(err) {
		t.Errorf("installed skill folder was not removed")
	}
}

func TestInstallLocalSkill(t *testing.T) {
	localPath := t.TempDir()
	userDir := t.TempDir()

	// Create SKILL.md in local path
	if err := os.WriteFile(filepath.Join(localPath, "SKILL.md"), []byte("SKILL"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := InstallLocalSkill(localPath, userDir)
	if err != nil {
		t.Fatalf("InstallLocalSkill failed: %v", err)
	}

	basename := filepath.Base(localPath)
	installedMD := filepath.Join(userDir, basename, "SKILL.md")
	if _, err := os.Stat(installedMD); err != nil {
		t.Errorf("SKILL.md not found in installed folder: %v", err)
	}
}

func TestImportAndUpdateUserSkill(t *testing.T) {
	userDir := t.TempDir()

	// 1. Import
	name := "My Skill"
	desc := "A custom user skill"
	body := "Do something cool."
	triggers := []string{"cool", "do"}

	err := ImportUserSkill(userDir, name, desc, body, triggers)
	if err != nil {
		t.Fatalf("ImportUserSkill failed: %v", err)
	}

	slug := "my-skill"
	mdPath := filepath.Join(userDir, slug, "SKILL.md")
	data, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("failed to read imported SKILL.md: %v", err)
	}

	content := string(data)
	if !strings.Contains(content, `name: "My Skill"`) {
		t.Errorf("expected name in YAML frontmatter, got: %s", content)
	}
	if !strings.Contains(content, "A custom user skill") {
		t.Errorf("expected description in YAML frontmatter, got: %s", content)
	}
	if !strings.Contains(content, "Do something cool.") {
		t.Errorf("expected body in file, got: %s", content)
	}

	// 2. Update
	newBody := "Do something even cooler."
	err = UpdateUserSkill(userDir, slug, desc, newBody, triggers)
	if err != nil {
		t.Fatalf("UpdateUserSkill failed: %v", err)
	}

	data, err = os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("failed to read updated SKILL.md: %v", err)
	}

	content = string(data)
	if !strings.Contains(content, "Do something even cooler.") {
		t.Errorf("expected updated body, got: %s", content)
	}
}

func TestInstallGithubSkill_InvalidUrl(t *testing.T) {
	userDir := t.TempDir()
	err := InstallGithubSkill(context.Background(), "invalid-url", userDir)
	if err == nil {
		t.Error("expected invalid github url to fail")
	}
}

func TestParseGithubUrl(t *testing.T) {
	cases := []struct {
		url          string
		wantRepo     string
		wantSubPath  string
		wantBranch   string
		wantErr      bool
	}{
		{
			url:          "https://github.com/anthropics/skills/tree/main/docx",
			wantRepo:     "https://github.com/anthropics/skills",
			wantSubPath:  "docx",
			wantBranch:   "main",
			wantErr:      false,
		},
		{
			url:          "https://github.com/vercel-labs/agent-browser/blob/main/skills/agent-browser/SKILL.md",
			wantRepo:     "https://github.com/vercel-labs/agent-browser",
			wantSubPath:  "skills/agent-browser",
			wantBranch:   "main",
			wantErr:      false,
		},
		{
			url:          "https://github.com/vercel-labs/agent-browser/tree/main/skills/agent-browser",
			wantRepo:     "https://github.com/vercel-labs/agent-browser",
			wantSubPath:  "skills/agent-browser",
			wantBranch:   "main",
			wantErr:      false,
		},
		{
			url:          "https://github.com/Fize/soloQueue/tree/main/skills/pua",
			wantRepo:     "https://github.com/Fize/soloQueue",
			wantSubPath:  "skills/pua",
			wantBranch:   "main",
			wantErr:      false,
		},
		{
			url:          "https://github.com/remotion-dev/remotion",
			wantRepo:     "https://github.com/remotion-dev/remotion",
			wantSubPath:  "",
			wantBranch:   "",
			wantErr:      false,
		},
	}

	for _, tc := range cases {
		repo, sub, branch, err := parseGithubUrl(tc.url)
		if tc.wantErr {
			if err == nil {
				t.Errorf("url %q: expected error but got nil", tc.url)
			}
			continue
		}
		if err != nil {
			t.Errorf("url %q: unexpected error: %v", tc.url, err)
			continue
		}
		if repo != tc.wantRepo {
			t.Errorf("url %q: got repo = %q, want %q", tc.url, repo, tc.wantRepo)
		}
		if sub != tc.wantSubPath {
			t.Errorf("url %q: got sub = %q, want %q", tc.url, sub, tc.wantSubPath)
		}
		if branch != tc.wantBranch {
			t.Errorf("url %q: got branch = %q, want %q", tc.url, branch, tc.wantBranch)
		}
	}
}

func TestInstallGithubSkill_RealIntegration(t *testing.T) {
	if os.Getenv("SOLOQUEUE_RUN_GITHUB_INTEGRATION") == "" {
		t.Skip("Set SOLOQUEUE_RUN_GITHUB_INTEGRATION=1 to run real github download tests")
	}

	userDir := t.TempDir()
	
	// 1. Test pulling docx
	err := InstallGithubSkill(context.Background(), "https://github.com/anthropics/skills/tree/main/skills/docx", userDir)
	if err != nil {
		t.Fatalf("failed to install docx from monorepo: %v", err)
	}
	installedMD := filepath.Join(userDir, "docx", "SKILL.md")
	if _, err := os.Stat(installedMD); err != nil {
		t.Fatalf("SKILL.md not found in installed folder: %v", err)
	}

	// 2. Test pulling agent-browser
	err = InstallGithubSkill(context.Background(), "https://github.com/vercel-labs/agent-browser/tree/main/skills/agent-browser", userDir)
	if err != nil {
		t.Fatalf("failed to install agent-browser from monorepo: %v", err)
	}
	installedAgentBrowserMD := filepath.Join(userDir, "agent-browser", "SKILL.md")
	if _, err := os.Stat(installedAgentBrowserMD); err != nil {
		t.Fatalf("agent-browser SKILL.md not found in installed folder: %v", err)
	}
}


