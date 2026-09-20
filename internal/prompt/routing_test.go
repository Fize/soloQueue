package prompt

import (
	"strings"
	"testing"
)

func TestBuildRoutingTable_WithLeaders(t *testing.T) {
	leaders := []LeaderInfo{
		{ID: "dev", Name: "dev", Description: "Full-stack developer", Group: "DevOps"},
		{ID: "editor-in-chief", Name: "EditorInChief", Description: "Editor-in-chief, responsible for content planning", Group: "NovelCreationTeam"},
	}

	result := buildRoutingTable(leaders, nil)

	if !strings.Contains(result, "Team: dev (target=dev): Full-stack developer") {
		t.Error("missing dev leader entry")
	}
	if !strings.Contains(result, "Team: EditorInChief (target=editor-in-chief): Editor-in-chief, responsible for content planning") {
		t.Error("missing EditorInChief leader entry")
	}
	if !strings.Contains(result, "Full-stack developer") {
		t.Error("missing dev description")
	}
}

func TestBuildRoutingTable_DoesNotInjectGroupBody(t *testing.T) {
	result := buildRoutingTable([]LeaderInfo{{
		ID:               "dev",
		Name:             "Dev",
		Description:      "Code implementation",
		Group:            "Engineering",
		GroupDescription: "Long team handbook that belongs only in the L2 team context.",
	}}, map[string]GroupFile{
		"Engineering": {Body: "Long team handbook that belongs only in the L2 team context."},
	})
	if !strings.Contains(result, "Code implementation") {
		t.Fatal("routing table omitted the leader description")
	}
	if strings.Contains(result, "Long team handbook") {
		t.Fatal("routing table injected the group body into L1")
	}
}

func TestBuildRoutingTable_Empty(t *testing.T) {
	result := buildRoutingTable(nil, nil)

	if !strings.Contains(result, "No matching teams") {
		t.Errorf("empty leaders should show fallback message, got: %q", result)
	}
}

func TestBuildRoutingTable_NoGroup(t *testing.T) {
	leaders := []LeaderInfo{
		{Name: "assistant", Description: "General assistant", Group: ""},
	}

	result := buildRoutingTable(leaders, nil)

	if !strings.Contains(result, "assistant (target=assistant): General assistant") {
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
