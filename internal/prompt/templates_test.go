package prompt

import (
	"strings"
	"testing"
)

func TestBuildProfile_Defaults(t *testing.T) {
	answers := DefaultProfileAnswers()
	result := BuildProfile(answers)

	if !strings.Contains(result, "You are SoloQueue") {
		t.Error("should contain default name in English")
	}
	if !strings.Contains(result, "personal assistant") {
		t.Error("should contain 'personal assistant'")
	}
	if !strings.Contains(result, "female") {
		t.Error("should contain default gender")
	}
	if !strings.Contains(result, "playful") {
		t.Error("should contain default personality")
	}
	if !strings.Contains(result, "vivid language") {
		t.Error("should contain 'playful' personality description in English")
	}
	if !strings.Contains(result, "casual") {
		t.Error("should contain default comm style")
	}
	if !strings.Contains(result, "conversational") {
		t.Error("should contain 'casual' comm style description in English")
	}
}

func TestBuildProfile_Custom(t *testing.T) {
	answers := ProfileAnswers{
		Name:        "Small Q",
		Gender:      "female",
		Personality: "playful",
		CommStyle:   "detailed",
	}
	result := BuildProfile(answers)

	if !strings.Contains(result, "You are Small Q") {
		t.Error("should contain custom name")
	}
	if !strings.Contains(result, "vivid language") {
		t.Error("should contain 'playful' personality description in English")
	}
	if !strings.Contains(result, "full background") {
		t.Error("should contain 'detailed' comm style description in English")
	}
}

func TestBuildProfile_CustomPersonality(t *testing.T) {
	answers := ProfileAnswers{
		Name:        "SoloQueue",
		Gender:      "female",
		Personality: "Communicate like an old friend",
		CommStyle:   "casual",
	}
	result := BuildProfile(answers)

	if !strings.Contains(result, "Communicate like an old friend") {
		t.Error("custom personality should be used as-is for description")
	}
	if !strings.Contains(result, "conversational") {
		t.Error("should contain 'casual' comm style description in English")
	}
}

func TestDefaultRules(t *testing.T) {
	if !strings.Contains(DefaultRules, "Delegate First") {
		t.Error("DefaultRules should contain Delegate First")
	}
	if !strings.Contains(DefaultRules, "Task Distribution") {
		t.Error("DefaultRules should contain Task Distribution")
	}
	if !strings.Contains(DefaultRules, "Result Aggregation") {
		t.Error("DefaultRules should contain Result Aggregation")
	}
	if !strings.Contains(DefaultRules, "Failure Fallback") {
		t.Error("DefaultRules should contain Failure Fallback")
	}
	if !strings.Contains(DefaultRules, "Clarification Handling") {
		t.Error("DefaultRules should contain Clarification Handling")
	}
	if !strings.Contains(DefaultRules, "need_clarification") {
		t.Error("DefaultRules should reference need_clarification status")
	}
}

func TestSharedAgentRules_SkillPriorityAppliesAfterDelegationDecision(t *testing.T) {
	required := []string{
		"Delegation and help-seeking decisions take precedence over skill selection",
		"After deciding to execute all or part of the task yourself",
		"inspect the available skills before using raw tools",
	}
	for _, phrase := range required {
		if !strings.Contains(SharedAgentRules, phrase) {
			t.Errorf("SharedAgentRules should contain %q", phrase)
		}
	}

	if strings.Contains(SharedAgentRules, "Before executing, scan ALL available tools") {
		t.Error("SharedAgentRules should not require skill selection before delegation decisions")
	}
}

func TestSharedAgentRules_ExplicitSkillRequest(t *testing.T) {
	required := []string{
		"If the user explicitly requests a skill",
		"invoke that skill directly",
		"Do NOT search for related skills first",
		"preserve the explicit skill requirement in the delegated task or help request",
	}
	for _, phrase := range required {
		if !strings.Contains(SharedAgentRules, phrase) {
			t.Errorf("SharedAgentRules should contain %q", phrase)
		}
	}
}
