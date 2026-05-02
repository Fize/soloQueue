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
		Name:        "小Q",
		Gender:      "female",
		Personality: "playful",
		CommStyle:   "detailed",
	}
	result := BuildProfile(answers)

	if !strings.Contains(result, "You are 小Q") {
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
		Personality: "像一个老朋友一样交流",
		CommStyle:   "casual",
	}
	result := BuildProfile(answers)

	if !strings.Contains(result, "像一个老朋友一样交流") {
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

func TestProfilePromptText(t *testing.T) {
	text := ProfilePromptText()
	if !strings.Contains(text, "SoloQueue") {
		t.Error("prompt text should contain default name option")
	}
	if !strings.Contains(text, "personalize your assistant") {
		t.Error("prompt text should contain setup instructions")
	}
}

func TestPresetSelectionPrompt(t *testing.T) {
	text := PresetSelectionPrompt()
	if !strings.Contains(text, "Welcome") {
		t.Error("preset selection should contain welcome message")
	}
	if !strings.Contains(text, "韩立") {
		t.Error("preset selection should contain 韩立")
	}
	if !strings.Contains(text, "极阴老祖") {
		t.Error("preset selection should contain 极阴老祖")
	}
	if !strings.Contains(text, "Custom") {
		t.Error("preset selection should contain Custom option")
	}
}

func TestBuildProfile_Preset(t *testing.T) {
	answers := ProfileAnswers{
		Name:   "韩立",
		Gender: "male",
		Preset: "韩立",
	}
	result := BuildProfile(answers)
	if !strings.Contains(result, "韩立 (Han Li)") {
		t.Error("preset profile should contain character name")
	}
	if !strings.Contains(result, "cultivator") {
		t.Error("preset profile should contain character identity description")
	}
	if !strings.Contains(result, "Expression Style") {
		t.Error("preset profile should contain Expression Style section")
	}
	if !strings.Contains(result, "Mental Model") {
		t.Error("preset profile should contain Mental Model section")
	}
}

func TestPresetProfiles(t *testing.T) {
	presets := PresetProfiles()
	if len(presets) != 6 {
		t.Errorf("expected 6 presets, got %d", len(presets))
	}
	names := make(map[string]bool)
	for _, p := range presets {
		names[p.Name] = true
		if p.Description == "" {
			t.Errorf("preset %s should have a description", p.Name)
		}
		if p.Gender == "" {
			t.Errorf("preset %s should have a gender", p.Name)
		}
	}
	expected := []string{"韩立", "极阴老祖", "南宫婉", "玄骨上人", "元瑶", "紫灵"}
	for _, name := range expected {
		if !names[name] {
			t.Errorf("expected preset %s not found", name)
		}
	}
}
