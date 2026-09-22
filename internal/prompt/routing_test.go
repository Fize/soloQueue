package prompt

import (
	"strings"
	"testing"
)

func TestBuildRoutingTable_WithLeaders(t *testing.T) {
	leaders := []LeaderInfo{
		{ID: "dev", Name: "dev", Description: "Dev leader internals", Group: "DevOps", GroupDescription: "Full-stack developer"},
		{ID: "editor-in-chief", Name: "EditorInChief", Description: "Editorial leader internals", Group: "NovelCreationTeam", GroupDescription: "Novel writing and content planning"},
	}

	result := buildRoutingTable(leaders, nil)

	if !strings.Contains(result, "Team: dev (target=dev): Full-stack developer") {
		t.Error("missing dev leader entry")
	}
	if !strings.Contains(result, "Team: EditorInChief (target=editor-in-chief): Novel writing and content planning") {
		t.Error("missing EditorInChief leader entry")
	}
	if !strings.Contains(result, "Full-stack developer") {
		t.Error("missing dev description")
	}
}

func TestBuildRoutingTable_UsesOnlyTeamDescription(t *testing.T) {
	for _, tc := range []struct {
		name, group, description string
	}{
		{"team", "Engineering", "Code reading, investigation, and implementation"},
		{"missing description", "Engineering", ""},
		{"blank description", "Engineering", " \t\n"},
		{"no group", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := buildRoutingTable([]LeaderInfo{{
				ID: " CANONICAL-LEAD ", Name: "Display Name",
				Description: "Private leader responsibilities",
				Group:       tc.group, GroupDescription: tc.description,
			}}, nil)
			if tc.description != "" && !strings.Contains(result, tc.description) {
				t.Fatal("routing table omitted the team description")
			}
			if strings.Contains(result, "Private leader responsibilities") {
				t.Fatal("routing table exposed the leader description")
			}
			if !strings.Contains(result, "Team: Display Name (target=canonical-lead)") ||
				!strings.Contains(result, `delegate(target="canonical-lead"`) {
				t.Fatal("routing table changed the display label or canonical delegation target")
			}
			if strings.Contains(result, `work_dir="..."`) != (tc.group != "") {
				t.Fatal("routing table changed group workspace semantics")
			}
		})
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

	if !strings.Contains(result, "assistant (target=assistant):") {
		t.Errorf("leader without group should retain its target, got: %q", result)
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
