package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

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

	fake := &FakeLLM{
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

	fake := &FakeLLM{
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

	fake := &FakeLLM{
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

	fake := &FakeLLM{
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
	fake := &FakeLLM{Responses: []string{"hello"}}
	a := startedAgent(t, fake)

	if err := a.Confirm("nonexistent", "yes"); err == nil {
		t.Error("Confirm for unknown callID should error")
	}
}

// ─── Pending confirm exits via ctx when Agent is stopped ───────────────────

func TestAgent_Confirmable_StopCancelsPending(t *testing.T) {
	confirmTool := newFakeConfirmableTool("danger", true, "are you sure?")

	fake := &FakeLLM{
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
	fake := &FakeLLM{
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

	fake := &FakeLLM{
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
	fake.toolIdx = 0
	fake.streamIdx = 0
	fake.idx = 0

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

	fake := &FakeLLM{
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

func TestAgent_ToolPruningAndInterception(t *testing.T) {
	// Register various tools to cover different filtering levels
	readTool := newFakeTool("Read")
	writeTool := newFakeTool("Write")
	delegateTool := newFakeTool("delegate_task")
	skillTool := newFakeTool("Skill")

	fake := &FakeLLM{
		ToolCallsByTurn: [][]llm.ToolCall{{
			{ID: "call_1", Type: "function", Function: llm.FunctionCall{Name: "Write", Arguments: `{"file":"foo.txt","content":"hello"}`}},
		}},
		Responses: []string{"final answer"},
	}

	a := startedAgentWithTools(t, fake, readTool, writeTool, delegateTool, skillTool)

	// 1. Verify filtering under L0-Conversation
	// At L0 (conversation-only), only a whitelist of read-only tools is allowed.
	// delegate_* tools are not in this whitelist — they remain pruned at L0.
	a.modelOverride.Store(&ModelParams{Level: "L0-Conversation"})
	specsL0 := a.ToolSpecs()
	hasRead := false
	hasWrite := false
	hasDelegate := false
	for _, spec := range specsL0 {
		switch spec.Function.Name {
		case "Read":
			hasRead = true
		case "Write":
			hasWrite = true
		case "delegate_task":
			hasDelegate = true
		}
	}
	if !hasRead || hasWrite || hasDelegate || len(specsL0) != 1 {
		t.Errorf("L0 filter failed: specs count = %d, hasRead = %v, hasWrite = %v, hasDelegate = %v",
			len(specsL0), hasRead, hasWrite, hasDelegate)
	}

	// 2. Verify filtering under L1-SimpleSingleFile
	// delegate_* tools are always preserved for supervisors.
	// Skill is still pruned for L1 level.
	a.modelOverride.Store(&ModelParams{Level: "L1-SimpleSingleFile"})
	specsL1 := a.ToolSpecs()
	hasDelegate = false
	hasSkill := false
	hasRead = false
	hasWrite = false
	for _, spec := range specsL1 {
		switch spec.Function.Name {
		case "Read":
			hasRead = true
		case "Write":
			hasWrite = true
		case "delegate_task":
			hasDelegate = true
		case "Skill":
			hasSkill = true
		}
	}
	if !hasRead || !hasWrite || !hasDelegate || hasSkill || len(specsL1) != 3 {
		t.Errorf("L1 filter failed: specs count = %d, hasRead = %v, hasWrite = %v, hasDelegate = %v, hasSkill = %v",
			len(specsL1), hasRead, hasWrite, hasDelegate, hasSkill)
	}

	// 3. Verify filtering under L2 (all should be retained)
	a.modelOverride.Store(&ModelParams{Level: "L2"})
	specsL2 := a.ToolSpecs()
	if len(specsL2) != 4 {
		t.Errorf("L2 filter failed: specs count = %d, want 4", len(specsL2))
	}

	// 4. Verify filtering under empty level (all should be retained)
	a.modelOverride.Store(&ModelParams{Level: ""})
	specsEmpty := a.ToolSpecs()
	if len(specsEmpty) != 4 {
		t.Errorf("empty level filter failed: specs count = %d, want 4", len(specsEmpty))
	}

	// 5. Verify that execToolStream intercepts filtered tool calls and returns errors
	// We set the level to L0-Conversation and call Write (should be intercepted)
	a.modelOverride.Store(&ModelParams{Level: "L0-Conversation"})

	ch, err := a.AskStream(context.Background(), "write file")
	if err != nil {
		t.Fatalf("AskStream failed: %v", err)
	}

	events := drainEvents(t, ch, 2*time.Second)

	var (
		foundStart bool
		foundDone  bool
		doneErr    error
		doneResult string
	)

	for _, ev := range events {
		switch e := ev.(type) {
		case ToolExecStartEvent:
			if e.Name == "Write" {
				foundStart = true
			}
		case ToolExecDoneEvent:
			if e.Name == "Write" {
				foundDone = true
				doneErr = e.Err
				doneResult = e.Result
			}
		}
	}

	if !foundStart {
		t.Error("expected ToolExecStartEvent for intercepted tool")
	}
	if !foundDone {
		t.Error("expected ToolExecDoneEvent for intercepted tool")
	}
	if doneErr == nil || !strings.Contains(doneErr.Error(), "not available under the current classification level") {
		t.Errorf("unexpected done error: %v", doneErr)
	}
	expectedResult := "error: tool Write is not available under the current classification level L0-Conversation"
	if doneResult != expectedResult {
		t.Errorf("doneResult = %q, want %q", doneResult, expectedResult)
	}

	// Ensure the tool never actually executed
	if writeTool.CallCount() != 0 {
		t.Errorf("writeTool should not have been executed, got callCount = %d", writeTool.CallCount())
	}
}

