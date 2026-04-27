package prompt

import (
	"strings"
	"testing"
)

func TestBuildRoutingTable_WithLeaders(t *testing.T) {
	leaders := []LeaderInfo{
		{Name: "dev", Description: "全栈开发工程师", Group: "DevOps"},
		{Name: "EditorInChief", Description: "总编辑，负责内容策划", Group: "NovelCreationTeam"},
	}

	result := buildRoutingTable(leaders)

	if !strings.Contains(result, "dev (DevOps)") {
		t.Error("missing dev leader entry")
	}
	if !strings.Contains(result, "EditorInChief (NovelCreationTeam)") {
		t.Error("missing EditorInChief leader entry")
	}
	if !strings.Contains(result, "全栈开发工程师") {
		t.Error("missing dev description")
	}
}

func TestBuildRoutingTable_Empty(t *testing.T) {
	result := buildRoutingTable(nil)

	if !strings.Contains(result, "No Team Leaders") {
		t.Errorf("empty leaders should show fallback message, got: %q", result)
	}
}

func TestBuildRoutingTable_NoGroup(t *testing.T) {
	leaders := []LeaderInfo{
		{Name: "assistant", Description: "通用助手", Group: ""},
	}

	result := buildRoutingTable(leaders)

	if !strings.Contains(result, "assistant: 通用助手") {
		t.Errorf("leader without group should not show parentheses, got: %q", result)
	}
}

func TestBuildRoutingTable_SortedByGroup(t *testing.T) {
	leaders := []LeaderInfo{
		{Name: "z_leader", Description: "Z leader", Group: "ZGroup"},
		{Name: "a_leader", Description: "A leader", Group: "AGroup"},
	}

	result := buildRoutingTable(leaders)

	aIdx := strings.Index(result, "a_leader")
	zIdx := strings.Index(result, "z_leader")
	if aIdx >= zIdx {
		t.Error("leaders should be sorted by group name")
	}
}
