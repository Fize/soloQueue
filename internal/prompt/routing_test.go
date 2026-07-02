package prompt

import (
	"strings"
	"testing"
)

func TestBuildRoutingTable_WithLeaders(t *testing.T) {
	leaders := []LeaderInfo{
		{Name: "dev", Description: "Full-stack developer", Group: "DevOps"},
		{Name: "EditorInChief", Description: "Editor-in-chief, responsible for content planning", Group: "NovelCreationTeam"},
	}

	result := buildRoutingTable(leaders, nil)

	if !strings.Contains(result, "dev (DevOps)") {
		t.Error("missing dev leader entry")
	}
	if !strings.Contains(result, "EditorInChief (NovelCreationTeam)") {
		t.Error("missing EditorInChief leader entry")
	}
	if !strings.Contains(result, "Full-stack developer") {
		t.Error("missing dev description")
	}
}

func TestBuildRoutingTable_Empty(t *testing.T) {
	result := buildRoutingTable(nil, nil)

	if !strings.Contains(result, "No Team Leaders") {
		t.Errorf("empty leaders should show fallback message, got: %q", result)
	}
}

func TestBuildRoutingTable_NoGroup(t *testing.T) {
	leaders := []LeaderInfo{
		{Name: "assistant", Description: "General assistant", Group: ""},
	}

	result := buildRoutingTable(leaders, nil)

	if !strings.Contains(result, "assistant: General assistant") {
		t.Errorf("leader without group should not show parentheses, got: %q", result)
	}
}

func TestBuildRoutingTable_SortedByGroup(t *testing.T) {
	leaders := []LeaderInfo{
		{Name: "z_leader", Description: "Z leader", Group: "ZGroup"},
		{Name: "a_leader", Description: "A leader", Group: "AGroup"},
	}

	result := buildRoutingTable(leaders, nil)

	aIdx := strings.Index(result, "a_leader")
	zIdx := strings.Index(result, "z_leader")
	if aIdx >= zIdx {
		t.Error("leaders should be sorted by group name")
	}
}