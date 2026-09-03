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

func TestHardcodedL1Rules_ClawHubProgressiveLoading(t *testing.T) {
	required := []string{
		"Skill Acquisition via ClawHub",
		"explicit exception to Delegate First",
		"clawhub --help",
		"identifies and runs the current version query option shown by that help",
		"clawhub <command> --help",
		"current official ClawHub installation or upgrade guidance",
		"ask the user for explicit approval before installing or upgrading host-level CLI software",
		"maintain the standalone CLI directly",
		"never delegate that maintenance",
		"Never delegate CLI help inspection, version querying, or maintenance",
		"After maintenance, re-run clawhub --help",
		"Do not hardcode, pin, or declare a ClawHub version or version-query option",
		`--workdir "$PWD" --dir skills`,
		"do not search it speculatively",
		"Before installing, inspect the candidate",
		"Never substitute openclaw",
	}
	for _, phrase := range required {
		if !strings.Contains(HardcodedL1Rules, phrase) {
			t.Errorf("HardcodedL1Rules missing progressive ClawHub guidance %q", phrase)
		}
	}

	start := strings.Index(HardcodedL1Rules, "20. **Skill Acquisition via ClawHub")
	if start < 0 {
		t.Fatal("could not find the compact ClawHub guidance block")
	}
	end := strings.Index(HardcodedL1Rules[start:], "\n22.")
	if end < 0 {
		t.Fatal("could not isolate the compact ClawHub guidance block")
	}
	if end > 1400 {
		t.Fatalf("ClawHub guidance block grew beyond its compact progressive-loading budget: %d bytes", end)
	}
	block := HardcodedL1Rules[start : start+end]
	helpIndex := strings.Index(block, "clawhub --help")
	versionQueryIndex := strings.Index(block, "identifies and runs the current version query option shown by that help")
	commandHelpIndex := strings.Index(block, "clawhub <command> --help")
	inspectIndex := strings.Index(block, "Before installing, inspect the candidate")
	approvalIndex := strings.Index(block, "ask the user for explicit approval before installing or upgrading host-level CLI software")
	approvedMaintenanceIndex := strings.Index(block, "After approval, maintain the standalone CLI directly")
	directIndex := strings.Index(block, "perform the operation directly")
	if helpIndex < 0 || versionQueryIndex < 0 || commandHelpIndex < 0 || inspectIndex < 0 || approvalIndex < 0 || approvedMaintenanceIndex < 0 || directIndex < 0 || helpIndex >= versionQueryIndex || versionQueryIndex >= commandHelpIndex || commandHelpIndex >= inspectIndex || approvalIndex >= approvedMaintenanceIndex || approvedMaintenanceIndex >= directIndex {
		t.Fatalf("ClawHub guidance must order help, dynamic version query, command help, inspection, approval, and direct mutation: help=%d version_query=%d command_help=%d inspect=%d approval=%d approved_maintenance=%d direct=%d", helpIndex, versionQueryIndex, commandHelpIndex, inspectIndex, approvalIndex, approvedMaintenanceIndex, directIndex)
	}
	afterStart := strings.Index(block, "After maintenance,")
	if afterStart < 0 {
		t.Fatal("missing post-maintenance verification sequence")
	}
	afterMaintenance := block[afterStart:]
	helpAfter := strings.Index(afterMaintenance, "clawhub --help")
	versionAfter := strings.Index(afterMaintenance, "identify and run its current version query option from that help")
	commandHelpAfter := strings.Index(afterMaintenance, "clawhub <command> --help")
	if helpAfter < 0 || versionAfter <= helpAfter || commandHelpAfter <= versionAfter {
		t.Fatalf("post-maintenance checks must repeat help, dynamic version query, and command help in order: help=%d version_query=%d command_help=%d", helpAfter, versionAfter, commandHelpAfter)
	}
	for _, fixedOption := range []string{"clawhub --version", "--cli-version", "clawhub -V"} {
		if strings.Contains(block, fixedOption) {
			t.Errorf("ClawHub guidance must not hardcode version query option %q", fixedOption)
		}
	}
}

func TestHardcodedL1Rules_ClawHubLifecycleStaysWithL1(t *testing.T) {
	required := []string{
		"Skill lifecycle management is an L1-only responsibility",
		"Never delegate Skill search, installation, update, or removal",
		"perform the operation directly",
	}
	for _, phrase := range required {
		if !strings.Contains(HardcodedL1Rules, phrase) {
			t.Errorf("HardcodedL1Rules missing direct L1 lifecycle guidance %q", phrase)
		}
	}
}

func TestBuildSkillForkSystemPrompt_ContainsSkillLifecycleBoundary(t *testing.T) {
	got := BuildSkillForkSystemPrompt("base prompt", "skill instructions")
	for _, phrase := range []string{
		"base prompt",
		"skill instructions",
		"Do not search, install, update, or uninstall Skills with ClawHub",
		"report its Skill ID and requirement to L1",
	} {
		if !strings.Contains(got, phrase) {
			t.Errorf("Skill Fork prompt missing %q", phrase)
		}
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
