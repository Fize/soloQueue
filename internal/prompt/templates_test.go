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
	for _, required := range []string{
		"### Execution Ownership and Delegation",
		"stable task_name",
		"self-contained task",
		"inspect_delegation",
		"cancel_delegation",
		"continue directly when possible",
		"available Teams catalog",
		"Use only listed Team targets at this layer",
	} {
		if !strings.Contains(DefaultRules, required) {
			t.Errorf("DefaultRules should contain the consolidated delegation contract %q", required)
		}
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

func TestRoleSpecificExecutionOwnershipPolicies(t *testing.T) {
	for _, required := range []string{
		"available Teams catalog",
		"Use only listed Team targets at this layer",
	} {
		if !strings.Contains(L1ExecutionOwnershipPolicy, required) {
			t.Errorf("L1 policy should contain %q", required)
		}
	}
	for _, forbidden := range []string{"dynamic Worker", "peer Team", "visible Worker"} {
		if strings.Contains(L1ExecutionOwnershipPolicy, forbidden) {
			t.Errorf("L1 policy must not contain supervisor target class %q", forbidden)
		}
	}
	for _, required := range []string{
		"visible Worker from your own Team",
		"visible peer Team",
		"dynamic Worker only when no suitable Worker or peer Team exists",
		"at least two independent sub-tasks",
	} {
		if !strings.Contains(L2ExecutionOwnershipPolicy, required) {
			t.Errorf("L2 policy should contain %q", required)
		}
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
	for _, required := range []string{"### Execution Ownership", "ordinary conversation", "a Team fails", "capability/configuration questions"} {
		if !strings.Contains(rules, required) {
			t.Errorf("missing routing contract %q", required)
		}
	}
}

func TestHardcodedAssistantRules_SkillAcquisitionAndScheduling(t *testing.T) {
	required := []string{
		"### Skill Acquisition",
		"Use ClawHub directly when a required Skill is missing or incompatible",
		"do not delegate Skill search, installation, update, or removal",
		"Before mutation, run current CLI help and inspect the candidate",
		"installation, update, and removal require explicit user intent",
		`--workdir "$PWD" --dir skills`,
		"Request approval before host-level CLI installation or upgrade",
		"### Task Scheduling",
		"Use create_cron_job for scheduled tasks",
		"Use list_cron_jobs for unknown IDs",
	}
	for _, phrase := range required {
		if !strings.Contains(HardcodedAssistantRules, phrase) {
			t.Errorf("HardcodedAssistantRules missing progressive ClawHub guidance %q", phrase)
		}
	}

	for _, explanation := range []string{
		"default scheduling mechanism",
		"提醒和未来任务",
		"Skill Lifecycle Boundary",
		"handle its lifecycle",
	} {
		if strings.Contains(HardcodedAssistantRules, explanation) {
			t.Errorf("HardcodedAssistantRules must not expose explanatory Skill/scheduling text %q", explanation)
		}
	}
}

func TestBuildSkillForkSystemPrompt_ContainsSkillManagementRules(t *testing.T) {
	got := BuildSkillForkSystemPrompt("base prompt", "skill instructions")
	for _, phrase := range []string{
		"base prompt",
		"skill instructions",
		"# Skill Management",
		"Use ClawHub directly for Skill search, installation, update, or removal",
		"report its Skill ID and requirement to the caller",
	} {
		if !strings.Contains(got, phrase) {
			t.Errorf("Skill Fork prompt missing %q", phrase)
		}
	}
}

func TestSharedAgentRules_L1SkillSelection(t *testing.T) {
	if !strings.Contains(SharedAgentRules, "decide the executor before selecting Skills") {
		t.Fatal("executor selection must precede Skill matching")
	}

	required := []string{
		"# Skill Selection",
		"If this system prompt already contains a Skill's execution logic",
		"If a Skill matches, invoke it",
		"if none matches, use raw tools",
	}
	for _, phrase := range required {
		if !strings.Contains(SharedAgentRules, phrase) {
			t.Errorf("SharedAgentRules should contain %q", phrase)
		}
	}
	for _, forbidden := range []string{"SKILL STEP", "upstream step", "This is step N of the <skill> SOP"} {
		if strings.Contains(SharedAgentRules, forbidden) {
			t.Errorf("SharedAgentRules must not contain supervisor-only Skill handoff %q", forbidden)
		}
	}
}

func TestL2SkillExecutionContract_ContainsUpstreamStepHandling(t *testing.T) {
	for _, phrase := range []string{
		"SKILL INSTANCE",
		"SKILL STEP",
		"STANDALONE",
		"This is step N of the <skill> SOP",
		"preserve the exact step marker",
	} {
		if !strings.Contains(L2SkillExecutionContract, phrase) {
			t.Errorf("L2SkillExecutionContract should contain %q", phrase)
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
	required := []string{
		"When delegating a standalone task",
		"file types/formats",
		"artifact shape",
		"Do not invent Skill IDs",
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
