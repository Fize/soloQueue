package prompt

import (
	"strings"
	"testing"
)

func TestAssembleWithXML_Full(t *testing.T) {
	result := assembleWithXML(
		"profile content",
		"user context",
		"",
		"",
		"routing table",
		"team management",
		"rules content",
		"/home/user/.soloqueue/plan",
	)

	if !strings.Contains(result, "<identity>\nprofile content\n</identity>") {
		t.Error("missing or incorrect identity section")
	}
	if !strings.Contains(result, "<user_context>\nuser context\n</user_context>") {
		t.Error("missing or incorrect user_context section")
	}
	if !strings.Contains(result, "<available_teams>\nrouting table\n</available_teams>") {
		t.Error("missing or incorrect available_teams section")
	}
	if !strings.Contains(result, "<team_management>\nteam management\n</team_management>") {
		t.Error("missing or incorrect team_management section")
	}
	if !strings.Contains(result, "<rules>\nrules content\n</rules>") {
		t.Error("missing or incorrect rules section")
	}
	if !strings.Contains(result, "<plan_before_action>") {
		t.Error("missing plan_before_action section when planDir is provided")
	}
	if !strings.Contains(result, "/home/user/.soloqueue/plan") {
		t.Error("missing plan directory path in plan_before_action section")
	}
}

func TestAssembleWithXML_NoUserCtx(t *testing.T) {
	result := assembleWithXML(
		"profile content",
		"",
		"",
		"",
		"routing table",
		"team management",
		"rules content",
		"/home/user/.soloqueue/plan",
	)

	if strings.Contains(result, "<user_context>") {
		t.Error("user_context section should be omitted when empty")
	}
}

func TestAssembleWithXML_EmptyPlanDir(t *testing.T) {
	result := assembleWithXML(
		"profile content",
		"user context",
		"",
		"",
		"routing table",
		"team management",
		"rules content",
		"",
	)

	if strings.Contains(result, "<plan_before_action>") {
		t.Error("plan_before_action section should be omitted when planDir is empty")
	}
}
