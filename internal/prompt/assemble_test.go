package prompt

import (
	"strings"
	"testing"
)

func TestAssembleWithXML_Full(t *testing.T) {
	result := assembleWithXML(
		"profile content",
		"user context",
		"routing table",
		"rules content",
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
	if !strings.Contains(result, "<rules>\nrules content\n</rules>") {
		t.Error("missing or incorrect rules section")
	}
}

func TestAssembleWithXML_NoUserCtx(t *testing.T) {
	result := assembleWithXML(
		"profile content",
		"",
		"routing table",
		"rules content",
	)

	if strings.Contains(result, "<user_context>") {
		t.Error("user_context section should be omitted when empty")
	}
}
