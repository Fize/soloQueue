package prompt

import (
	"strings"
	"testing"
)

// The team management section uses absolute paths for groups/ and agents/
// directories. These are global configuration directories (under workDir),
// determined at runtime — not dependent on the agent's current working context.

func TestBuildTeamManagementSection_UsesAbsoluteGroupsPath(t *testing.T) {
	result := buildTeamManagementSection("/home/user/.soloqueue")

	// GROUPS_DIR/ placeholder should be replaced with the absolute path
	if strings.Contains(result, "GROUPS_DIR/") {
		t.Error("team management section should replace GROUPS_DIR/ placeholder")
	}
	if !strings.Contains(result, "/home/user/.soloqueue/groups/") {
		t.Error("team management section should use absolute groups path")
	}
}

func TestBuildTeamManagementSection_UsesAbsoluteAgentsPath(t *testing.T) {
	result := buildTeamManagementSection("/home/user/.soloqueue")

	// AGENTS_DIR/ placeholder should be replaced with the absolute path
	if strings.Contains(result, "AGENTS_DIR/") {
		t.Error("team management section should replace AGENTS_DIR/ placeholder")
	}
	if !strings.Contains(result, "/home/user/.soloqueue/agents/") {
		t.Error("team management section should use absolute agents path")
	}
}

func TestBuildTeamManagementSection_RespectsRuntimeWorkDir(t *testing.T) {
	// Different workDir values should produce different output paths
	result := buildTeamManagementSection("/custom/base")

	if !strings.Contains(result, "/custom/base/groups/") {
		t.Error("team management section should reflect the runtime workDir in groups path")
	}
	if !strings.Contains(result, "/custom/base/agents/") {
		t.Error("team management section should reflect the runtime workDir in agents path")
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

func TestBuildTeamManagementSection_AbsolutePathsInFileFormats(t *testing.T) {
	result := buildTeamManagementSection("/home/user/.soloqueue")

	// File format examples should use the absolute path
	if !strings.Contains(result, "/home/user/.soloqueue/groups/") {
		t.Error("team file format should use absolute groups path")
	}
	if !strings.Contains(result, "/home/user/.soloqueue/agents/") {
		t.Error("agent file format should use absolute agents path")
	}
}
