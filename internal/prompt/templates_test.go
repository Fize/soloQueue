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
	if !strings.Contains(DefaultRules, "NEVER include them in a user-facing answer") {
		t.Error("DefaultRules should keep internal identifiers out of user-facing answers")
	}
}

func TestDefaultRules_EmotionalToneAdaptation(t *testing.T) {
	// Rule 28 "Emotional Tone Adaptation" is appended after rule 27
	// "Frustration Detection" in HardcodedL1Rules — the ruleset that is
	// unconditionally injected into the L1 agent's system prompt
	// (internal/prompt/assemble.go). The plan named DefaultRules, but that
	// constant holds only orchestrator rules 1-14 and contains no rule 27;
	// placing rule 28 there would dangle its "per rule 27" references and
	// would not guarantee delivery to the main agent. See tester report.
	if !strings.Contains(HardcodedL1Rules, "Emotional Tone Adaptation") {
		t.Fatal("HardcodedL1Rules should contain rule 28 'Emotional Tone Adaptation'")
	}
	// Rule 28 must appear after rule 27 (Frustration Detection).
	idxFrustration := strings.Index(HardcodedL1Rules, "Frustration Detection")
	idxTone := strings.Index(HardcodedL1Rules, "Emotional Tone Adaptation")
	if idxFrustration < 0 {
		t.Fatal("HardcodedL1Rules should contain 'Frustration Detection'")
	}
	if idxTone <= idxFrustration {
		t.Errorf("'Emotional Tone Adaptation' should appear after 'Frustration Detection' (frustration idx=%d, tone idx=%d)", idxFrustration, idxTone)
	}
}

func TestSharedAgentRules_ThreeSkillExecutionModes(t *testing.T) {
	// The old model made delegation and skill selection mutually exclusive.
	// The new model classifies by HOW the task reaches the agent: skill
	// instance / skill step / standalone.
	absent := []string{
		"Delegation and help-seeking decisions take precedence over skill selection",
	}
	for _, phrase := range absent {
		if strings.Contains(SharedAgentRules, phrase) {
			t.Errorf("SharedAgentRules should NOT contain %q", phrase)
		}
	}

	required := []string{
		"YOU ARE THE SKILL",
		"SKILL STEP",
		"STANDALONE",
		"do not re-select",
		"If none matches, proceed with raw tools",
		"This is step N of the <skill> SOP",
	}
	for _, phrase := range required {
		if !strings.Contains(SharedAgentRules, phrase) {
			t.Errorf("SharedAgentRules should contain %q", phrase)
		}
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

func TestSharedAgentRules_DelegationCarriesDomainSignals(t *testing.T) {
	// The delegating agent cannot see the executor's skill set, so tasks must
	// carry domain signals (goal, formats, artifacts, keywords) instead of
	// skill IDs — the executing agent matches its own skills.
	required := []string{
		"task description MUST carry enough domain signals",
		"file types/formats involved",
		"Do NOT reference skill IDs",
		"executing agent decides",
	}
	for _, phrase := range required {
		if !strings.Contains(SharedAgentRules, phrase) {
			t.Errorf("SharedAgentRules should contain %q", phrase)
		}
	}
}

func TestSharedAgentRules_LowCostInvocation(t *testing.T) {
	// Breaks the cost/benefit asymmetry: invoking the Skill tool is cheap, so
	// the model should call first and evaluate rather than skip.
	required := []string{
		"Invoking the Skill tool is cheap",
		"invoke it first and evaluate",
	}
	for _, phrase := range required {
		if !strings.Contains(SharedAgentRules, phrase) {
			t.Errorf("SharedAgentRules should contain %q", phrase)
		}
	}
}
