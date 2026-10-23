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
	if !strings.Contains(DefaultRules, "Internal runtime identifiers are never needed in a response") {
		t.Error("DefaultRules should keep runtime identifiers out of agent responses")
	}
}

func TestDefaultRules_ConciseClarificationHandling(t *testing.T) {
	for _, required := range []string{
		"first resolve the questions using available context or tools",
		"only unresolved blocking questions",
		"in concise natural language, together in one turn",
		"include the task goal, answers, and only the context needed",
		"omit conversation history and redundant summaries",
		"No fixed response schema is required",
	} {
		if !strings.Contains(DefaultRules, required) {
			t.Errorf("clarification handling missing %q", required)
		}
	}
	if strings.Contains(DefaultRules, "need_clarification") {
		t.Error("clarification handling must not depend on the retired JSON status")
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

func TestSharedAgentRules_SkillUseContract(t *testing.T) {
	cases := []struct {
		name     string
		required []string
	}{
		{"matching before requested work", []string{
			"Follow the executor-selection policy first",
			"including for independently delegated tasks",
			"match the available Skill descriptions to its goal, scope, and required method",
			"You may first perform the read-only exploration needed to understand the task",
			"before proceeding with execution or producing the requested analysis, advice, design, or other content",
			"invoke it and read its instructions before the work it governs",
			"do not batch the Skill load with that work",
			"load it directly without unnecessary exploration",
			"permission to skip planning does not waive this check",
			"Keyword overlap alone is not a match",
			"If no Skill applies, proceed directly",
		}},
		{"selection reuse and changes", []string{
			"Use the Skill whose stated purpose best matches the current goal and required method",
			"Reuse instructions already available when they cover the work",
			"Reassess when the goal, scope, or required method changes materially",
			"load only missing applicable instructions or required dependencies",
			"Routine tool calls, retries, and follow-up turns for the same work do not require another selection or load",
			"An additional Skill is needed only for an applicable explicit dependency or a requirement not covered",
			"Do not load overlapping workflows merely to be thorough",
		}},
		{"assigned steps", []string{
			"For an assigned step of an existing workflow, follow its supplied instructions",
			"without restarting or replacing the workflow",
			"load supporting guidance only when needed for that step",
		}},
		{"authorized scope", []string{
			"relevant requirements and permitted adaptations within the user's authorized scope",
			"does not authorize extra deliverables, implementation, delegation, or publication",
			"Do not silently omit applicable mandatory requirements",
		}},
		{"no separate process", []string{
			"No separate selection report, checklist, or approval step is required",
		}},
		{"behavior and evidence scope", []string{
			"For fixes and fix suggestions, preserve existing behavior outside the requested correction",
			"even within the same function",
			"Names or conventions alone do not authorize semantic changes",
			"Distinguish observed facts from hypotheses",
			"do not present an unverified cause as established",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, phrase := range tc.required {
				if !strings.Contains(SharedAgentRules, phrase) {
					t.Errorf("shared Skill Use contract missing %q", phrase)
				}
			}
		})
	}
	skillUse := strings.Split(strings.SplitN(SharedAgentRules, "# Skill Use\n", 2)[1], "# Strict Scope Adherence")[0]
	for _, phrase := range []string{
		"Reuse instructions already available when they cover the work",
		"Reassess when the goal, scope, or required method changes materially",
		"Routine tool calls, retries, and follow-up turns for the same work",
	} {
		if strings.Count(skillUse, phrase) != 1 {
			t.Errorf("Skill Use must state %q exactly once", phrase)
		}
	}
	for _, example := range []string{"qa-suite", "scenario-standards", "workflow-orchestrator", "cart.go", "release.tar.gz", "release verification"} {
		if strings.Contains(skillUse, example) {
			t.Errorf("generic Skill Use must not specialize routing for %q", example)
		}
	}

}

func TestL2SkillExecutionContract_PreservesHandoffScope(t *testing.T) {
	for _, phrase := range []string{
		"# Skill Handoff",
		"For an independent delegated task",
		"requested outcome, scope, relevant work context, and any explicit user-required Skill",
		"Let the executor apply the shared Skill Use rules",
		"identify the Skill and step",
		"provide the relevant instructions and expected result",
		"preserve these constraints through further delegation",
		"Do not restart the parent workflow or expand the assigned step",
	} {
		if !strings.Contains(L2SkillExecutionContract, phrase) {
			t.Errorf("L2SkillExecutionContract should contain %q", phrase)
		}
	}
}

func TestSharedAgentRules_ExplicitSkillRequest(t *testing.T) {
	required := []string{
		"When the user explicitly asks you to use a Skill, use it within the requested scope",
		"Preserve that requirement when delegating",
		"Do not invent Skill IDs or substitute a different Skill without justification",
	}
	for _, phrase := range required {
		if !strings.Contains(SharedAgentRules, phrase) {
			t.Errorf("SharedAgentRules should contain %q", phrase)
		}
	}
}

func TestSharedAgentRules_UncertainSkillApplicability(t *testing.T) {
	required := []string{
		"When applicability is uncertain, use the least inspection needed to decide",
		"You may load an inline Skill to inspect its instructions when the description is insufficient",
		"Invoking a [fork] Skill starts execution, so do not use it merely to preview instructions",
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

func TestPlanPromptsUseOnlyStandardGFMTaskMarkers(t *testing.T) {
	for name, prompt := range map[string]string{
		"PlanDocumentFormat":    PlanDocumentFormat,
		"L2EnforcedPlanSection": L2EnforcedPlanSection,
		"L3EnforcedDirectives":  L3EnforcedDirectives,
	} {
		if strings.Contains(prompt, "[/]") {
			t.Errorf("%s contains non-GFM slash task marker guidance", name)
		}
		for _, marker := range []string{"- [ ]", "- [x]"} {
			if !strings.Contains(prompt, marker) {
				t.Errorf("%s missing standard GFM task marker %q", name, marker)
			}
		}
	}
}
