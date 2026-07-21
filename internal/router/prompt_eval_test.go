package router

import (
	"strings"
	"testing"
)

func TestClassifierPromptSeparatesScopeFromReasoningDepth(t *testing.T) {
	if strings.Contains(llmClassifierSystemPrompt, "raise level by 1") {
		t.Fatal("reasoning depth must not automatically increase task scope")
	}
	for _, required := range []string{"answer quality, not task scope", "wider impact"} {
		if !strings.Contains(llmClassifierSystemPrompt, required) {
			t.Fatalf("classifier prompt missing %q", required)
		}
	}
}

func TestClassifierPromptKeepsStableOutputContract(t *testing.T) {
	const schema = `{"intent":"chat|action","level":0,"reason":"..."}`
	if !strings.Contains(llmClassifierSystemPrompt, schema) {
		t.Fatalf("classifier output schema changed; want %s", schema)
	}
	for _, forbidden := range []string{`"reasoning_effort"`, `"confidence"`} {
		if strings.Contains(llmClassifierSystemPrompt, forbidden) {
			t.Fatalf("classifier output contract contains unsupported field %s", forbidden)
		}
	}
}
