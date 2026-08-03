package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent/agenttest"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// ─── Test fixtures ───────────────────────────────────────────────────────────

// fakeConfirmableTool is a test utility that implements the Confirmable interface
type fakeConfirmableTool struct {
	fakeTool
	needsConfirm bool
	prompt       string
}

func newFakeConfirmableTool(name string, needsConfirm bool, prompt string) *fakeConfirmableTool {
	return &fakeConfirmableTool{
		fakeTool: fakeTool{
			name:        name,
			description: "fake confirmable tool " + name,
			parameters:  json.RawMessage(`{"type":"object"}`),
		},
		needsConfirm: needsConfirm,
		prompt:       prompt,
	}
}

func (f *fakeConfirmableTool) CheckConfirmation(args string) (bool, string) {
	var m map[string]any
	if err := json.Unmarshal([]byte(args), &m); err == nil {
		if confirmed, _ := m["confirmed"].(bool); confirmed {
			return false, ""
		}
	}
	return f.needsConfirm, f.prompt
}

func (*fakeConfirmableTool) ConfirmationOptions(_ string) []string { return nil }

func (f *fakeConfirmableTool) ConfirmArgs(original string, choice tools.ConfirmChoice) string {
	if choice != tools.ChoiceApprove {
		return original
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(original), &m); err != nil {
		return original
	}
	m["confirmed"] = true
	b, _ := json.Marshal(m)
	return string(b)
}

func (*fakeConfirmableTool) SupportsSessionWhitelist() bool { return true }

// ─── Single Confirmable tool: continues execution after user confirmation ──

func TestAgent_Confirmable_Approved(t *testing.T) {
	confirmTool := newFakeConfirmableTool("danger", true, "are you sure?")
	confirmTool.result = `{"ok":true}`

	fake := &agenttest.FakeLLM{
		ToolCallsByTurn: [][]llm.ToolCall{{
			{ID: "call_1", Type: "function", Function: llm.FunctionCall{Name: "danger", Arguments: `{"cmd":"rm -rf /"}`}},
		}},
		Responses: []string{"done"},
	}

	a := startedAgentWithTools(t, fake, confirmTool)

	events, err := a.AskStream(context.Background(), "do it")
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}

	var (
		foundConfirm bool
		finalContent string
	)

	for ev := range events {
		switch e := ev.(type) {
		case ToolNeedsConfirmEvent:
			foundConfirm = true
			if e.Name != "danger" {
				t.Errorf("name = %q, want danger", e.Name)
			}
			if e.Prompt == "" {
				t.Error("prompt should not be empty")
			}
			if err := a.Confirm(e.CallID, "yes"); err != nil {
				t.Errorf("Confirm: %v", err)
			}
		case DoneEvent:
			finalContent = e.Content
		case ErrorEvent:
			t.Fatalf("unexpected error: %v", e.Err)
		}
	}

	if !foundConfirm {
		t.Error("expected ToolNeedsConfirmEvent")
	}
	if finalContent != "done" {
		t.Errorf("final = %q, want done", finalContent)
	}
	if confirmTool.CallCount() != 1 {
		t.Errorf("tool called %d times, want 1", confirmTool.CallCount())
	}
}

// ─── Single Confirmable tool: user rejects ─────────────────────────────────

func TestAgent_Confirmable_Denied(t *testing.T) {
	confirmTool := newFakeConfirmableTool("danger", true, "are you sure?")
	confirmTool.result = `{"ok":true}`

	fake := &agenttest.FakeLLM{
		ToolCallsByTurn: [][]llm.ToolCall{{
			{ID: "call_1", Type: "function", Function: llm.FunctionCall{Name: "danger", Arguments: `{"cmd":"rm -rf /"}`}},
		}},
		Responses: []string{"aborted"},
	}

	a := startedAgentWithTools(t, fake, confirmTool)

	events, err := a.AskStream(context.Background(), "do it")
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}

	var (
		foundConfirm bool
		foundDone    bool
	)

	for ev := range events {
		switch e := ev.(type) {
		case ToolNeedsConfirmEvent:
			foundConfirm = true
			if err := a.Confirm(e.CallID, ""); err != nil {
				t.Errorf("Confirm: %v", err)
			}
		case ToolExecDoneEvent:
			if e.Err == nil {
				t.Error("expected error for denied tool")
			}
		case DoneEvent:
			foundDone = true
		case ErrorEvent:
			t.Fatalf("unexpected error: %v", e.Err)
		}
	}

	if !foundConfirm {
		t.Error("expected ToolNeedsConfirmEvent")
	}
	if !foundDone {
		t.Error("expected DoneEvent")
	}
	if confirmTool.CallCount() != 0 {
		t.Errorf("tool called %d times, want 0 (denied)", confirmTool.CallCount())
	}
}

// ─── Non-Confirmable tools are unaffected ──────────────────────────────────

func TestAgent_NonConfirmable_NoEvent(t *testing.T) {
	regularTool := newFakeTool("echo")
	regularTool.result = `{"ok":true}`

	fake := &agenttest.FakeLLM{
		ToolCallsByTurn: [][]llm.ToolCall{{
			{ID: "call_1", Type: "function", Function: llm.FunctionCall{Name: "echo", Arguments: `{"msg":"hi"}`}},
		}},
		Responses: []string{"final"},
	}

	a := startedAgentWithTools(t, fake, regularTool)

	events, err := a.AskStream(context.Background(), "say hi")
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}

	var foundConfirm bool
	var finalContent string

	for ev := range events {
		switch e := ev.(type) {
		case ToolNeedsConfirmEvent:
			foundConfirm = true
		case DoneEvent:
			finalContent = e.Content
		case ErrorEvent:
			t.Fatalf("unexpected error: %v", e.Err)
		}
	}

	if foundConfirm {
		t.Error("non-confirmable tool should not emit ToolNeedsConfirmEvent")
	}
	if finalContent != "final" {
		t.Errorf("final = %q, want final", finalContent)
	}
	if regularTool.CallCount() != 1 {
		t.Errorf("tool called %d times, want 1", regularTool.CallCount())
	}
}

// ─── Confirm returns error for an already responded callID ─────────────────

func TestAgent_Confirm_Duplicate(t *testing.T) {
	confirmTool := newFakeConfirmableTool("danger", true, "are you sure?")
	confirmTool.result = `{"ok":true}`

	fake := &agenttest.FakeLLM{
		ToolCallsByTurn: [][]llm.ToolCall{{
			{ID: "call_1", Type: "function", Function: llm.FunctionCall{Name: "danger", Arguments: `{"cmd":"rm -rf /"}`}},
		}},
		Responses: []string{"done"},
	}

	a := startedAgentWithTools(t, fake, confirmTool)

	events, err := a.AskStream(context.Background(), "do it")
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}

	var callID string
	for ev := range events {
		switch e := ev.(type) {
		case ToolNeedsConfirmEvent:
			callID = e.CallID
			if err := a.Confirm(callID, "yes"); err != nil {
				t.Errorf("first Confirm: %v", err)
			}
			// A second duplicate call should report an error
			if err := a.Confirm(callID, "yes"); err == nil {
				t.Error("second Confirm should error")
			}
		case DoneEvent:
			// ok
		case ErrorEvent:
			t.Fatalf("unexpected error: %v", e.Err)
		}
	}

	if callID == "" {
		t.Fatal("expected ToolNeedsConfirmEvent")
	}
}

// ─── Confirm returns error for a non-existent callID ───────────────────────

func TestAgent_Confirm_UnknownCallID(t *testing.T) {
	fake := &agenttest.FakeLLM{Responses: []string{"hello"}}
	a := startedAgent(t, fake)

	if err := a.Confirm("nonexistent", "yes"); err == nil {
		t.Error("Confirm for unknown callID should error")
	}
}

// ─── Pending confirm exits via ctx when Agent is stopped ───────────────────

func TestAgent_Confirmable_StopCancelsPending(t *testing.T) {
	confirmTool := newFakeConfirmableTool("danger", true, "are you sure?")

	fake := &agenttest.FakeLLM{
		ToolCallsByTurn: [][]llm.ToolCall{{
			{ID: "call_1", Type: "function", Function: llm.FunctionCall{Name: "danger", Arguments: `{"cmd":"rm -rf /"}`}},
		}},
		Responses: []string{"done"},
	}

	a := startedAgentWithTools(t, fake, confirmTool)

	events, err := a.AskStream(context.Background(), "do it")
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}

	var foundConfirm bool
	for ev := range events {
		if e, ok := ev.(ToolNeedsConfirmEvent); ok {
			foundConfirm = true
			_ = e
			// Do not call Confirm, directly Stop agent
			go func() {
				time.Sleep(50 * time.Millisecond)
				_ = a.Stop(time.Second)
			}()
		}
	}

	if !foundConfirm {
		t.Error("expected ToolNeedsConfirmEvent")
	}
	if confirmTool.CallCount() != 0 {
		t.Errorf("tool called %d times, want 0 (stopped before confirm)", confirmTool.CallCount())
	}
}

// ─── allow-in-session: after first confirmation, subsequent calls in the same session skip confirmation ──

func TestAgent_Confirmable_AllowInSession(t *testing.T) {
	confirmTool := newFakeConfirmableTool("danger", true, "are you sure?")
	confirmTool.result = `{"ok":true}`

	// LLM calls the danger tool in both rounds; first round requires confirmation, second round skips due to whitelist
	fake := &agenttest.FakeLLM{
		ToolCallsByTurn: [][]llm.ToolCall{
			{{ID: "call_1", Type: "function", Function: llm.FunctionCall{Name: "danger", Arguments: `{"cmd":"rm -rf /"}`}}},
			{{ID: "call_2", Type: "function", Function: llm.FunctionCall{Name: "danger", Arguments: `{"cmd":"rm -rf /tmp"}`}}},
		},
		Responses: []string{"done"},
	}

	a := startedAgentWithTools(t, fake, confirmTool)

	events, err := a.AskStream(context.Background(), "do it")
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}

	var (
		confirmCount int
		finalContent string
	)

	for ev := range events {
		switch e := ev.(type) {
		case ToolNeedsConfirmEvent:
			confirmCount++
			if !e.AllowInSession {
				t.Error("expected AllowInSession=true")
			}
			if err := a.Confirm(e.CallID, "allow-in-session"); err != nil {
				t.Errorf("Confirm: %v", err)
			}
		case DoneEvent:
			finalContent = e.Content
		case ErrorEvent:
			t.Fatalf("unexpected error: %v", e.Err)
		}
	}

	if confirmCount != 1 {
		t.Errorf("confirm events = %d, want 1", confirmCount)
	}
	if finalContent != "done" {
		t.Errorf("final = %q, want done", finalContent)
	}
	// The tool was called twice (both executions happened)
	if confirmTool.CallCount() != 2 {
		t.Errorf("tool called %d times, want 2", confirmTool.CallCount())
	}
}

// ─── Whitelist is cleared on Start ──────────────────────────────────────────

func TestAgent_Confirmable_WhitelistClearedOnStart(t *testing.T) {
	confirmTool := newFakeConfirmableTool("danger", true, "are you sure?")
	confirmTool.result = `{"ok":true}`

	fake := &agenttest.FakeLLM{
		ToolCallsByTurn: [][]llm.ToolCall{{
			{ID: "call_1", Type: "function", Function: llm.FunctionCall{Name: "danger", Arguments: `{"cmd":"rm -rf /"}`}},
		}},
		Responses: []string{"done"},
	}

	a := NewAgent(Definition{ID: "a1"}, fake, nil, WithTools(confirmTool))

	// First Start + Ask: user selects allow-in-session
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	events, err := a.AskStream(context.Background(), "do it")
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}

	var callID string
	for ev := range events {
		if e, ok := ev.(ToolNeedsConfirmEvent); ok {
			callID = e.CallID
			if err := a.Confirm(callID, "allow-in-session"); err != nil {
				t.Fatalf("Confirm: %v", err)
			}
		}
	}
	if callID == "" {
		t.Fatal("expected ToolNeedsConfirmEvent on first run")
	}
	if !a.confirmStore.IsConfirmed("danger") {
		t.Fatal("expected danger to be whitelisted after allow-in-session")
	}
	_ = a.Stop(time.Second)

	// Reset FakeLLM internal counter, otherwise the second Ask won't go through the tool_calls path
	fake.Reset()

	// Start again after Stop (simulating a new session): whitelist should be cleared
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	defer func() { _ = a.Stop(time.Second) }()

	if a.confirmStore.IsConfirmed("danger") {
		t.Fatal("whitelist should be cleared after Start")
	}

	events, err = a.AskStream(context.Background(), "do it again")
	if err != nil {
		t.Fatalf("second AskStream: %v", err)
	}

	var foundConfirm bool
	for ev := range events {
		if e, ok := ev.(ToolNeedsConfirmEvent); ok {
			foundConfirm = true
			// Must inject confirmation, otherwise agent will block forever
			if err := a.Confirm(e.CallID, "yes"); err != nil {
				t.Fatalf("second Confirm: %v", err)
			}
		}
	}
	if !foundConfirm {
		t.Fatal("expected ToolNeedsConfirmEvent after restart because whitelist was cleared")
	}
}

// ─── Parallel tools: some require confirmation, some do not ────────────────

func TestAgent_Confirmable_ParallelPartialConfirm(t *testing.T) {
	confirmTool := newFakeConfirmableTool("danger", true, "are you sure?")
	confirmTool.result = `{"danger_ok":true}`

	echoTool := newFakeTool("echo")
	echoTool.result = `{"echo_ok":true}`

	fake := &agenttest.FakeLLM{
		ToolCallsByTurn: [][]llm.ToolCall{{
			{ID: "call_1", Type: "function", Function: llm.FunctionCall{Name: "danger", Arguments: `{"cmd":"rm -rf /"}`}},
			{ID: "call_2", Type: "function", Function: llm.FunctionCall{Name: "echo", Arguments: `{"msg":"hi"}`}},
		}},
		Responses: []string{"done"},
	}

	a := startedAgentWithTools(t, fake, confirmTool, echoTool)
	a.parallelTools = true // Enable parallelism

	events, err := a.AskStream(context.Background(), "do both")
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}

	var (
		confirmCount int
		finalContent string
	)

	for ev := range events {
		switch e := ev.(type) {
		case ToolNeedsConfirmEvent:
			confirmCount++
			if err := a.Confirm(e.CallID, "yes"); err != nil {
				t.Errorf("Confirm: %v", err)
			}
		case DoneEvent:
			finalContent = e.Content
		case ErrorEvent:
			t.Fatalf("unexpected error: %v", e.Err)
		}
	}

	if confirmCount != 1 {
		t.Errorf("confirm events = %d, want 1", confirmCount)
	}
	if finalContent != "done" {
		t.Errorf("final = %q, want done", finalContent)
	}
	if confirmTool.CallCount() != 1 {
		t.Errorf("danger tool called %d times, want 1", confirmTool.CallCount())
	}
	if echoTool.CallCount() != 1 {
		t.Errorf("echo tool called %d times, want 1", echoTool.CallCount())
	}
}

// ─── memoryConfirmStore standalone unit tests ──────────────────────────────

func TestMemoryConfirmStore(t *testing.T) {
	s := NewMemoryConfirmStore()

	if s.IsConfirmed("Bash") {
		t.Error("fresh store should not confirm anything")
	}

	s.Confirm("Bash")
	if !s.IsConfirmed("Bash") {
		t.Error("Bash should be confirmed after Confirm")
	}
	if s.IsConfirmed("other") {
		t.Error("other should not be confirmed")
	}

	s.Clear()
	if s.IsConfirmed("Bash") {
		t.Error("Bash should not be confirmed after Clear")
	}
}

// ─── Task-level tool filtering and interception unit tests ─────────────────

func TestAgent_SharedConfirmStore(t *testing.T) {
	store := NewMemoryConfirmStore()

	a1 := NewAgent(Definition{ID: "agent-1"}, nil, nil)
	a2 := NewAgent(Definition{ID: "agent-2"}, nil, nil)
	a1.SetConfirmStore(store)
	a2.SetConfirmStore(store)

	if a1.ConfirmStore() != store || a2.ConfirmStore() != store {
		t.Error("expected both agents to share the provided confirm store")
	}

	if a1.confirmStore.IsConfirmed("Bash") || a2.confirmStore.IsConfirmed("Bash") {
		t.Error("expected Bash not to be confirmed initially")
	}

	// Confirm Bash on a1
	a1.confirmStore.Confirm("Bash")

	// Check if a2 also sees it as confirmed
	if !a2.confirmStore.IsConfirmed("Bash") {
		t.Error("expected Bash to be confirmed on a2 after confirming on a1")
	}
}
