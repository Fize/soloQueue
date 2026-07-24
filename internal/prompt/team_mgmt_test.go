package prompt

import (
	"strings"
	"testing"
)

// The team management section should use relative "groups/" and "agents/"
// paths instead of absolute workDir paths. The tool chain resolves relative
// paths against the configured workDir.

func TestBuildTeamManagementSection_UsesRelativeGroupsPath(t *testing.T) {
	result := buildTeamManagementSection("/home/user/.soloqueue")

	if strings.Contains(result, "/home/user/.soloqueue") {
		t.Error("team management section should not contain absolute workDir path")
	}
	// GROUPS_DIR/ placeholder should be replaced with relative "groups/"
	if strings.Contains(result, "GROUPS_DIR/") {
		t.Error("team management section should replace GROUPS_DIR/ with relative path")
	}
	if !strings.Contains(result, "groups/") {
		t.Error("team management section should use relative 'groups/' path")
	}
}

func TestBuildTeamManagementSection_UsesRelativeAgentsPath(t *testing.T) {
	result := buildTeamManagementSection("/home/user/.soloqueue")

	// AGENTS_DIR/ placeholder should be replaced with relative "agents/"
	if strings.Contains(result, "AGENTS_DIR/") {
		t.Error("team management section should replace AGENTS_DIR/ with relative path")
	}
	if !strings.Contains(result, "agents/") {
		t.Error("team management section should use relative 'agents/' path")
	}
}

func TestBuildTeamManagementSection_NoWorkDirLeak(t *testing.T) {
	// Multiple different workDir values should never appear in output
	for _, dir := range []string{
		"/home/user/.soloqueue",
		"/Users/test/.soloqueue",
		"/custom/path",
	} {
		result := buildTeamManagementSection(dir)
		if strings.Contains(result, dir) {
			t.Errorf("team management section should not contain workDir %q", dir)
		}
	}
}

func TestBuildTeamManagementSection_PreservesTemplateStructure(t *testing.T) {
	result := buildTeamManagementSection("/any/dir")

	// Core structure should be preserved
	if !strings.Contains(result, "## Directory Convention") {
		t.Error("should contain Directory Convention section")
	}
	if !strings.Contains(result, "### Step 1") {
		t.Error("should contain Step 1")
	}
	if !strings.Contains(result, "### Step 2") {
		t.Error("should contain Step 2")
	}
	if !strings.Contains(result, "### Step 3") {
		t.Error("should contain Step 3")
	}
	if !strings.Contains(result, "YAML frontmatter") {
		t.Error("should mention YAML frontmatter")
	}
}

func TestBuildTeamManagementSection_RelativePathsInFileFormats(t *testing.T) {
	result := buildTeamManagementSection("/home/user/.soloqueue")

	// File format examples should use relative paths
	if strings.Contains(result, "/home/user/.soloqueue/groups/") {
		t.Error("team file path should be relative, not contain workDir")
	}
	if strings.Contains(result, "/home/user/.soloqueue/agents/") {
		t.Error("agent file path should be relative, not contain workDir")
	}
}
