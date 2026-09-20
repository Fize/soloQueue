package prompt

import (
	"regexp"
	"strings"
	"testing"
)

func TestDefaultSoulContent(t *testing.T) {
	want := `You are SoloQueue, the user's personal assistant and CEO-like point of contact.

Your job is to understand the user's intent, protect their private context, make sound decisions, and get useful work completed. You may coordinate a listed Team when it clearly owns the work, but you remain responsible for the outcome and execute directly when no suitable Team exists.

## Communication baseline

Warm, direct, and conversational. Lead with the answer. For simple questions, respond in 1-3 sentences without unnecessary headings or lists. Expand only when complexity requires it or the user asks. Use humor and metaphors sparingly and only when they improve understanding. Avoid performative, flattering, sales-like, or overly familiar language. Stay calm and precise on serious topics.

## Personalization

- Name: SoloQueue
- Gender: female
- Personality: playful. Uses humor and metaphors sparingly and only when they improve understanding
- Communication style: casual. Uses conversational, casual, and natural language`

	if DefaultSoul != want {
		t.Fatalf("DefaultSoul differs from the approved default:\n--- got ---\n%s\n--- want ---\n%s", DefaultSoul, want)
	}
}

func TestDefaultRules(t *testing.T) {
	if !strings.Contains(DefaultRules, "Execution Ownership") {
		t.Error("DefaultRules should contain the execution ownership contract")
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
	if !strings.Contains(DefaultRules, "Internal runtime identifiers are never needed in a response") {
		t.Error("DefaultRules should keep runtime identifiers out of agent responses")
	}
}

func TestL1RulesLeavePersonalityToSoul(t *testing.T) {
	rules := DefaultRules + HardcodedAssistantRules
	for _, competing := range []string{"Professional Conciseness", "Context-Adaptive Tone", "Frustration Detection", "Emotional Tone Adaptation", "Proactive Reminders", "casual and warm", "baseline mood"} {
		if strings.Contains(rules, competing) {
			t.Errorf("rules contain competing persona instruction %q", competing)
		}
	}
	if regexp.MustCompile(`(?m)^\d+[a-z]?\. \*\*`).MatchString(rules) {
		t.Error("global rule numbering remains")
	}
	for _, required := range []string{"### Execution Ownership", "ordinary conversation", "private-memory lookups", "a Team fails", "capability/configuration questions"} {
		if !strings.Contains(rules, required) {
			t.Errorf("missing routing contract %q", required)
		}
	}
}

func TestHardcodedAssistantRules_ClawHubProgressiveLoading(t *testing.T) {
	required := []string{
		"Skill Acquisition via ClawHub",
		"explicit exception to Delegate First",
		"clawhub --help",
		"identify and run the current version query option shown by that help",
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
		if !strings.Contains(HardcodedAssistantRules, phrase) {
			t.Errorf("HardcodedAssistantRules missing progressive ClawHub guidance %q", phrase)
		}
	}

	start := strings.Index(HardcodedAssistantRules, "### Skill Acquisition via ClawHub")
	if start < 0 {
		t.Fatal("could not find the compact ClawHub guidance block")
	}
	end := strings.Index(HardcodedAssistantRules[start:], "\n### Task Scheduling")
	if end < 0 {
		t.Fatal("could not isolate the compact ClawHub guidance block")
	}
	if end > 1400 {
		t.Fatalf("ClawHub guidance block grew beyond its compact progressive-loading budget: %d bytes", end)
	}
	block := HardcodedAssistantRules[start : start+end]
	helpIndex := strings.Index(block, "clawhub --help")
	versionQueryIndex := strings.Index(block, "identify and run the current version query option shown by that help")
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

func TestHardcodedAssistantRules_ClawHubLifecycleStaysWithAssistant(t *testing.T) {
	required := []string{
		"Skill lifecycle management is handled directly by the assistant",
		"Never delegate Skill search, installation, update, or removal",
		"perform the operation directly",
	}
	for _, phrase := range required {
		if !strings.Contains(HardcodedAssistantRules, phrase) {
			t.Errorf("HardcodedAssistantRules missing direct lifecycle guidance %q", phrase)
		}
	}
}

func TestBuildSkillForkSystemPrompt_ContainsSkillLifecycleBoundary(t *testing.T) {
	got := BuildSkillForkSystemPrompt("base prompt", "skill instructions")
	for _, phrase := range []string{
		"base prompt",
		"skill instructions",
		"Do not search, install, update, or uninstall Skills with ClawHub",
		"report its Skill ID and requirement to the caller",
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
	for name, rules := range map[string]string{"assistant": DefaultRules, "team": L2EnforcedPlanSection, "worker": L3EnforcedDirectives} {
		for _, want := range []string{"explicit user location", "cloud workspace", "supplied or existing plan", "configured default", "local"} {
			if !strings.Contains(rules, want) {
				t.Errorf("%s lacks plan storage contract %q", name, want)
			}
		}
	}
}
