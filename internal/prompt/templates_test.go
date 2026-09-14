package prompt

import (
	"regexp"
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
	if !strings.Contains(DefaultRules, "Task Routing") {
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

func TestL1RulesLeavePersonalityToSoul(t *testing.T) {
	rules := DefaultRules + HardcodedL1Rules
	for _, competing := range []string{"Professional Conciseness", "Context-Adaptive Tone", "Frustration Detection", "Emotional Tone Adaptation", "Proactive Reminders", "casual and warm", "baseline mood"} {
		if strings.Contains(rules, competing) {
			t.Errorf("rules contain competing persona instruction %q", competing)
		}
	}
	if regexp.MustCompile(`(?m)^\d+[a-z]?\. \*\*`).MatchString(rules) {
		t.Error("global rule numbering remains")
	}
	for _, required := range []string{"### Task Routing", "ordinary concept questions", "daily chat", "failed Team", "L1-only"} {
		if !strings.Contains(rules, required) {
			t.Errorf("missing routing contract %q", required)
		}
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

	start := strings.Index(HardcodedL1Rules, "### Skill Acquisition via ClawHub")
	if start < 0 {
		t.Fatal("could not find the compact ClawHub guidance block")
	}
	end := strings.Index(HardcodedL1Rules[start:], "\n### Task Scheduling")
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
	if !strings.Contains(SharedAgentRules, "decide the executor before selecting Skills") {
		t.Fatal("executor selection must precede Skill matching")
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
		"Do not invent Skill IDs",
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

func TestPlanStorageHonorsUserLocationAcrossLevels(t *testing.T) {
	for name, rules := range map[string]string{"L1": DefaultRules, "L2": L2EnforcedPlanSection, "L3": L3EnforcedDirectives} {
		for _, want := range []string{"explicit user location", "cloud workspace", "supplied or existing plan", "configured default", "local"} {
			if !strings.Contains(rules, want) {
				t.Errorf("%s lacks plan storage contract %q", name, want)
			}
		}
	}
}
