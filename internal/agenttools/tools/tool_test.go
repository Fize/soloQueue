package tools

import (
	"strings"
	"testing"
)

func TestWithFallbackPrefixPreservesRequiredTerminalTool(t *testing.T) {
	submit := newSubmitCronResultTool()
	wrapped := WithFallbackPrefix([]Tool{submit})
	if len(wrapped) != 1 || wrapped[0] != submit {
		t.Fatalf("required terminal tool was wrapped: %#v", wrapped)
	}
	if strings.Contains(wrapped[0].Description(), "DO NOT USE") {
		t.Fatalf("required terminal tool was marked fallback-only: %q", wrapped[0].Description())
	}
	if _, ok := wrapped[0].(TurnTerminator); !ok {
		t.Fatal("terminal-tool semantics were not preserved")
	}
}
