package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/llm"
)

// ─── streamState ────────────────────────────────────────────────────────────

func TestStreamState_FlushThinking(t *testing.T) {
	s := &streamState{}
	s.thinkingBuf.WriteString("I'm thinking...")
	s.flushThinking()
	if len(s.timeline) != 1 || s.timeline[0].kind != timelineThinking {
		t.Error("flushThinking should add timeline entry")
	}
	if s.timeline[0].text != "I'm thinking..." {
		t.Errorf("text = %q, want %q", s.timeline[0].text, "I'm thinking...")
	}
	if s.thinkingBuf.Len() != 0 {
		t.Error("thinkingBuf should be empty after flush")
	}
}

func TestStreamState_FlushThinking_Empty(t *testing.T) {
	s := &streamState{}
	s.flushThinking()
	if len(s.timeline) != 0 {
		t.Error("flushThinking on empty buffer should not add entry")
	}
}

func TestStreamState_FlushContent(t *testing.T) {
	s := &streamState{}
	s.content.WriteString("Hello world")
	s.flushContent()
	if len(s.timeline) != 1 || s.timeline[0].kind != timelineContent {
		t.Error("flushContent should add timeline entry")
	}
	if s.timeline[0].text != "Hello world" {
		t.Errorf("text = %q, want %q", s.timeline[0].text, "Hello world")
	}
	if s.content.Len() != 0 {
		t.Error("content should be empty after flush")
	}
}

func TestStreamState_FlushContent_Empty(t *testing.T) {
	s := &streamState{}
	s.flushContent()
	if len(s.timeline) != 0 {
		t.Error("flushContent on empty buffer should not add entry")
	}
}

func TestStreamState_FlushOrder(t *testing.T) {
	s := &streamState{}
	s.thinkingBuf.WriteString("think first")
	s.flushThinking()
	s.content.WriteString("then content")
	s.flushContent()
	s.thinkingBuf.WriteString("think again")
	s.flushThinking()
	if len(s.timeline) != 3 {
		t.Fatalf("timeline length = %d, want 3", len(s.timeline))
	}
	if s.timeline[0].kind != timelineThinking || s.timeline[1].kind != timelineContent || s.timeline[2].kind != timelineThinking {
		t.Error("timeline order wrong")
	}
}

// ─── handleToolConfirm ──────────────────────────────────────────────────────

func TestHandleToolConfirm_WithOptions(t *testing.T) {
	m := &model{}
	ev := agent.ToolNeedsConfirmEvent{CallID: "call-1", Prompt: "Allow file write?", Options: []string{"yes", "no"}}
	m.handleToolConfirm(ev)
	if m.confirmState == nil || m.confirmState.callID != "call-1" {
		t.Error("confirmState should be set with correct callID")
	}
	if len(m.confirmState.options) != 2 || m.confirmState.selected != 0 {
		t.Error("confirmState options wrong")
	}
}

func TestHandleToolConfirm_BinaryChoice(t *testing.T) {
	m := &model{}
	ev := agent.ToolNeedsConfirmEvent{CallID: "call-2", Prompt: "Run command?"}
	m.handleToolConfirm(ev)
	if m.confirmState == nil || len(m.confirmState.options) < 2 {
		t.Error("binary options should have at least 2 choices")
	}
}

func TestHandleToolConfirm_AllowInSession(t *testing.T) {
	m := &model{}
	ev := agent.ToolNeedsConfirmEvent{CallID: "call-3", Prompt: "Run command?", AllowInSession: true}
	m.handleToolConfirm(ev)
	if m.confirmState == nil || len(m.confirmState.options) != 3 {
		t.Errorf("with AllowInSession, options = %d, want 3", len(m.confirmState.options))
	}
	found := false
	for _, opt := range m.confirmState.options {
		if strings.HasPrefix(opt, "[a]") {
			found = true
		}
	}
	if !found {
		t.Error("should have [a] allow in session option")
	}
}

// ─── agentTickInterval ──────────────────────────────────────────────────────

func TestAgentTickInterval(t *testing.T) {
	genInterval := agentTickInterval(true)
	idleInterval := agentTickInterval(false)
	if genInterval != 500*time.Millisecond {
		t.Errorf("generating interval = %v, want 500ms", genInterval)
	}
	if idleInterval != 2*time.Second {
		t.Errorf("idle interval = %v, want 2s", idleInterval)
	}
	if genInterval >= idleInterval {
		t.Error("generating interval should be faster than idle")
	}
}

// ─── spinnerCmd / agentTickCmd ──────────────────────────────────────────────

func TestSpinnerCmd(t *testing.T) {
	if cmd := spinnerCmd(); cmd == nil {
		t.Error("spinnerCmd() should return non-nil command")
	}
}

func TestAgentTickCmd(t *testing.T) {
	if cmd := agentTickCmd(500 * time.Millisecond); cmd == nil {
		t.Error("agentTickCmd() should return non-nil command")
	}
}

// ─── handleAgentEvent ──────────────────────────────────────────────────────

func TestHandleAgentEvent_NilCurrent(t *testing.T) {
	m := newTestModel()
	m.current = nil
	m.handleAgentEvent(agent.ContentDeltaEvent{Delta: "hello"}) // should not panic
}

func TestHandleAgentEvent_ReasoningDelta(t *testing.T) {
	m := newTestModel()
	m.current = &streamState{toolExecMap: make(map[string]*toolExecInfo)}
	m.handleAgentEvent(agent.ReasoningDeltaEvent{Delta: "I'm thinking..."})
	if m.genPhase != phaseThinking {
		t.Error("phase should be thinking")
	}
	if m.current.thinkingBuf.String() != "I'm thinking..." {
		t.Errorf("thinkingBuf = %q, want %q", m.current.thinkingBuf.String(), "I'm thinking...")
	}
}

func TestHandleAgentEvent_ReasoningDeltaFlushesContent(t *testing.T) {
	m := newTestModel()
	m.current = &streamState{toolExecMap: make(map[string]*toolExecInfo)}
	m.current.content.WriteString("existing content")
	m.handleAgentEvent(agent.ReasoningDeltaEvent{Delta: "now thinking"})
	if m.current.content.Len() != 0 {
		t.Error("content should be flushed before new thinking")
	}
	if len(m.current.timeline) != 1 || m.current.timeline[0].kind != timelineContent {
		t.Error("existing content should be in timeline")
	}
}

func TestHandleAgentEvent_ContentDelta(t *testing.T) {
	m := newTestModel()
	m.current = &streamState{toolExecMap: make(map[string]*toolExecInfo)}
	m.handleAgentEvent(agent.ContentDeltaEvent{Delta: "Hello world"})
	if m.genPhase != phaseGenerating {
		t.Error("phase should be generating")
	}
	if m.current.content.String() != "Hello world" {
		t.Errorf("content = %q, want %q", m.current.content.String(), "Hello world")
	}
}

func TestHandleAgentEvent_ContentDeltaFlushesThinking(t *testing.T) {
	m := newTestModel()
	m.current = &streamState{toolExecMap: make(map[string]*toolExecInfo)}
	m.current.thinkingBuf.WriteString("thinking first")
	m.handleAgentEvent(agent.ContentDeltaEvent{Delta: "answer"})
	if m.current.thinkingBuf.Len() != 0 {
		t.Error("thinkingBuf should be flushed on first content")
	}
	if len(m.current.timeline) != 1 || m.current.timeline[0].kind != timelineThinking {
		t.Error("thinking should be in timeline before content")
	}
}

func TestHandleAgentEvent_ToolCallDelta(t *testing.T) {
	m := newTestModel()
	m.current = &streamState{toolExecMap: make(map[string]*toolExecInfo)}
	m.handleAgentEvent(agent.ToolCallDeltaEvent{Name: "file_read", CallID: "c1", ArgsDelta: `{"path":"a.go"}`})
	if m.genPhase != phaseToolCall {
		t.Error("phase should be toolCall")
	}
	if m.current.curToolName != "file_read" || m.current.curToolCallID != "c1" {
		t.Error("tool call info not set correctly")
	}
}

func TestHandleAgentEvent_ToolCallDelta_SameName(t *testing.T) {
	m := newTestModel()
	m.current = &streamState{toolExecMap: make(map[string]*toolExecInfo)}
	m.current.curToolName = "file_read"
	m.handleAgentEvent(agent.ToolCallDeltaEvent{Name: "file_read", ArgsDelta: "more args"})
	if m.current.curToolArgs.String() != "more args" {
		t.Errorf("curToolArgs = %q, want %q", m.current.curToolArgs.String(), "more args")
	}
}

func TestHandleAgentEvent_ToolCallDelta_DifferentName(t *testing.T) {
	m := newTestModel()
	m.current = &streamState{toolExecMap: make(map[string]*toolExecInfo)}
	m.current.curToolName = "file_read"
	m.current.curToolArgs.WriteString("old args")
	m.handleAgentEvent(agent.ToolCallDeltaEvent{Name: "grep", ArgsDelta: "new"})
	if m.current.curToolName != "grep" || m.current.curToolArgs.String() != "new" {
		t.Error("curToolName/curToolArgs should be reset for new tool")
	}
}

func TestHandleAgentEvent_ToolExecStart(t *testing.T) {
	m := newTestModel()
	m.current = &streamState{toolExecMap: make(map[string]*toolExecInfo)}
	m.current.content.WriteString("some content")
	m.current.thinkingBuf.WriteString("some thought")
	m.handleAgentEvent(agent.ToolExecStartEvent{Name: "bash", Args: `{"command":"ls"}`, CallID: "c1"})
	if m.genPhase != phaseToolCall {
		t.Error("phase should be toolCall")
	}
	if m.current.content.Len() != 0 || m.current.thinkingBuf.Len() != 0 {
		t.Error("content and thinking should be flushed before tool")
	}
	tb := m.current.timeline[len(m.current.timeline)-1].tool
	if tb == nil || tb.name != "bash" {
		t.Error("tool block should have correct name")
	}
}

func TestHandleAgentEvent_ToolExecDone(t *testing.T) {
	m := newTestModel()
	m.current = &streamState{toolExecMap: make(map[string]*toolExecInfo)}
	tb := &toolBlock{name: "bash", args: `{"command":"ls"}`, callID: "c1"}
	m.current.toolExecMap["c1"] = &toolExecInfo{name: "bash", start: time.Now(), callID: "c1", tb: tb}
	m.handleAgentEvent(agent.ToolExecDoneEvent{CallID: "c1", Result: "line1\nline2\n"})
	if !tb.done || tb.lineCount != 2 {
		t.Errorf("done=%v lineCount=%d", tb.done, tb.lineCount)
	}
}

func TestHandleAgentEvent_ToolExecDone_UnknownCallID(t *testing.T) {
	m := newTestModel()
	m.current = &streamState{toolExecMap: make(map[string]*toolExecInfo)}
	m.handleAgentEvent(agent.ToolExecDoneEvent{CallID: "unknown", Result: "result"}) // should not panic
}

func TestHandleAgentEvent_ToolExecDone_WithErr(t *testing.T) {
	m := newTestModel()
	m.current = &streamState{toolExecMap: make(map[string]*toolExecInfo)}
	tb := &toolBlock{name: "bash", callID: "c1"}
	m.current.toolExecMap["c1"] = &toolExecInfo{name: "bash", start: time.Now(), tb: tb}
	m.handleAgentEvent(agent.ToolExecDoneEvent{CallID: "c1", Err: errors.New("exit status 1")})
	if tb.err == nil {
		t.Error("tool block should have error")
	}
}

func TestHandleAgentEvent_IterationDone(t *testing.T) {
	m := newTestModel()
	m.current = &streamState{toolExecMap: make(map[string]*toolExecInfo)}
	m.handleAgentEvent(agent.IterationDoneEvent{
		Usage: llm.Usage{PromptTokens: 100, CompletionTokens: 50, PromptCacheHitTokens: 20, PromptCacheMissTokens: 80, ReasoningTokens: 30},
	})
	if m.promptTokens != 100 || m.outputTokens != 50 || m.cacheHitTokens != 20 || m.cacheMissTokens != 80 || m.reasoningTokens != 30 {
		t.Errorf("tokens wrong: pt=%d ot=%d ch=%d cm=%d rt=%d", m.promptTokens, m.outputTokens, m.cacheHitTokens, m.cacheMissTokens, m.reasoningTokens)
	}
	if m.genPhase != phaseWaiting {
		t.Error("phase should reset to waiting")
	}
}

func TestHandleAgentEvent_ErrorEvent(t *testing.T) {
	m := newTestModel()
	m.current = &streamState{toolExecMap: make(map[string]*toolExecInfo)}
	m.handleAgentEvent(agent.ErrorEvent{Err: errors.New("HTTP 429 Too Many Requests")})
	if !strings.Contains(m.errMsg, "Rate limited") {
		t.Errorf("errMsg = %q, should contain 'Rate limited'", m.errMsg)
	}
}

func TestHandleAgentEvent_DoneEvent(t *testing.T) {
	m := newTestModel()
	m.current = &streamState{toolExecMap: make(map[string]*toolExecInfo)}
	m.handleAgentEvent(agent.DoneEvent{}) // should not panic
}

func TestHandleAgentEvent_ToolNeedsConfirm(t *testing.T) {
	m := newTestModel()
	m.current = &streamState{toolExecMap: make(map[string]*toolExecInfo)}
	m.handleAgentEvent(agent.ToolNeedsConfirmEvent{CallID: "c1", Prompt: "Allow?", Options: []string{"yes", "no"}})
	if m.confirmState == nil || m.confirmState.callID != "c1" {
		t.Error("confirmState should be set")
	}
}

// ─── handleAgentEvent: full sequence ──────────────────────────────────────

func TestHandleAgentEvent_Sequence(t *testing.T) {
	m := newTestModel()
	m.current = &streamState{toolExecMap: make(map[string]*toolExecInfo)}
	m.handleAgentEvent(agent.ReasoningDeltaEvent{Delta: "Let me think..."})
	m.handleAgentEvent(agent.ContentDeltaEvent{Delta: "Here's the answer: "})
	m.handleAgentEvent(agent.ContentDeltaEvent{Delta: "42"})
	m.handleAgentEvent(agent.IterationDoneEvent{Usage: llm.Usage{PromptTokens: 100, CompletionTokens: 10}})
	if m.genPhase != phaseWaiting {
		t.Errorf("phase = %v, want waiting", m.genPhase)
	}
	if m.promptTokens != 100 {
		t.Errorf("promptTokens = %d, want 100", m.promptTokens)
	}
}

// ─── waitForAgentEvent ────────────────────────────────────────────────────

func TestWaitForAgentEvent_ChannelClose(t *testing.T) {
	ch := make(chan agent.AgentEvent)
	close(ch)
	cmd := waitForAgentEvent(ch, func() {}, 1)
	msg := cmd()
	if _, ok := msg.(streamDoneMsg); !ok {
		t.Errorf("closed channel should produce streamDoneMsg, got %T", msg)
	}
}

func TestWaitForAgentEvent_EventReceived(t *testing.T) {
	ch := make(chan agent.AgentEvent, 1)
	ch <- agent.ContentDeltaEvent{Delta: "test"}
	close(ch)
	cmd := waitForAgentEvent(ch, func() {}, 1)
	msg := cmd()
	aem, ok := msg.(agentEventMsg)
	if !ok {
		t.Fatalf("should produce agentEventMsg, got %T", msg)
	}
	if _, ok := aem.event.(agent.ContentDeltaEvent); !ok {
		t.Error("event should be ContentDeltaEvent")
	}
}

// ─── _ import suppression ──────────────────────────────────────────────────

var _ = fmt.Sprintf
var _ = context.Background
