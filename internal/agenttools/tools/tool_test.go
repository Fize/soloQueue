package tools

import (
	"context"
	"encoding/json"
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
		t.Fatalf("required terminal tool was marked with an absolute routing prohibition: %q", wrapped[0].Description())
	}
	if _, ok := wrapped[0].(TurnTerminator); !ok {
		t.Fatal("terminal-tool semantics were not preserved")
	}
}

type plainTool struct{}

func (plainTool) Name() string                                    { return "plain" }
func (plainTool) Description() string                             { return "native description" }
func (plainTool) Parameters() json.RawMessage                     { return json.RawMessage(`{"type":"object"}`) }
func (plainTool) Execute(context.Context, string) (string, error) { return "", nil }

func TestWithFallbackPrefixLeavesDirectToolsUnchanged(t *testing.T) {
	tool := plainTool{}
	wrapped := WithFallbackPrefix([]Tool{tool})
	if len(wrapped) != 1 || wrapped[0] != tool {
		t.Fatalf("direct tool was replaced: %#v", wrapped)
	}
	if got := wrapped[0].Description(); got != "native description" {
		t.Fatalf("tool description was rewritten: %q", got)
	}
}
