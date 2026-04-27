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
	if !strings.Contains(result, "女") {
		t.Error("should contain default gender")
	}
	if !strings.Contains(result, "活泼") {
		t.Error("should contain default personality")
	}
	if !strings.Contains(result, "vivid language") {
		t.Error("should contain '活泼' personality description in English")
	}
	if !strings.Contains(result, "随意") {
		t.Error("should contain default comm style")
	}
	if !strings.Contains(result, "conversational") {
		t.Error("should contain '随意' comm style description in English")
	}
}

func TestBuildProfile_Custom(t *testing.T) {
	answers := ProfileAnswers{
		Name:        "小Q",
		Gender:      "女",
		Personality: "活泼",
		CommStyle:   "详细",
	}
	result := BuildProfile(answers)

	if !strings.Contains(result, "You are 小Q") {
		t.Error("should contain custom name")
	}
	if !strings.Contains(result, "vivid language") {
		t.Error("should contain '活泼' personality description in English")
	}
	if !strings.Contains(result, "full background") {
		t.Error("should contain '详细' comm style description in English")
	}
}

func TestBuildProfile_CustomPersonality(t *testing.T) {
	answers := ProfileAnswers{
		Name:        "SoloQueue",
		Gender:      "女",
		Personality: "像一个老朋友一样交流",
		CommStyle:   "随意",
	}
	result := BuildProfile(answers)

	if !strings.Contains(result, "像一个老朋友一样交流") {
		t.Error("custom personality should be used as-is for description")
	}
	if !strings.Contains(result, "conversational") {
		t.Error("should contain '随意' comm style description in English")
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
}

func TestProfilePromptText(t *testing.T) {
	text := ProfilePromptText()
	if !strings.Contains(text, "欢迎") {
		t.Error("prompt text should contain welcome message")
	}
	if !strings.Contains(text, "SoloQueue") {
		t.Error("prompt text should contain default name option")
	}
}
