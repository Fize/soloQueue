package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/xiaobaitu/soloqueue/internal/agent"
)

// ─── genPhase.String ────────────────────────────────────────────────────────

func TestGenPhase_String(t *testing.T) {
	tests := []struct {
		phase genPhase
		want  string
	}{
		{phaseWaiting, "waiting for model"},
		{phaseThinking, "thinking"},
		{phaseGenerating, "generating"},
		{phaseToolCall, "running tools"},
		{genPhase(99), ""},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("phase_%d", tt.phase), func(t *testing.T) {
			got := tt.phase.String()
			if got != tt.want {
				t.Errorf("genPhase(%d).String() = %q, want %q", tt.phase, got, tt.want)
			}
		})
	}
}

// ─── summarizeError ─────────────────────────────────────────────────────────

func TestSummarizeError(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		wantSub string
	}{
		{"nil error", nil, ""},
		{"no such host", fmt.Errorf("dial tcp: lookup api.example.com: no such host"), "Network error: cannot resolve host"},
		{"connection refused", fmt.Errorf("dial tcp 127.0.0.1:8080: connection refused"), "Network error: connection refused"},
		{"timeout", fmt.Errorf("context deadline exceeded"), "Network error: request timed out"},
		{"timeout keyword", fmt.Errorf("request timeout after 30s"), "Network error: request timed out"},
		{"TLS handshake", fmt.Errorf("TLS handshake failure"), "Network error: TLS failure"},
		{"certificate", fmt.Errorf("x509: certificate signed by unknown authority"), "Network error: TLS failure"},
		{"connection reset", fmt.Errorf("read tcp: connection reset by peer"), "Network error: connection lost"},
		{"broken pipe", fmt.Errorf("write tcp: broken pipe"), "Network error: connection lost"},
		{"429 rate limit", fmt.Errorf("HTTP 429 Too Many Requests"), "Rate limited"},
		{"401 unauthorized", fmt.Errorf("HTTP 401 Unauthorized"), "Auth error: invalid API key"},
		{"403 forbidden", fmt.Errorf("HTTP 403 Forbidden"), "Auth error: access denied"},
		{"500 server error", fmt.Errorf("HTTP 500 Internal Server Error"), "Server error"},
		{"502 bad gateway", fmt.Errorf("HTTP 502 Bad Gateway"), "Server error"},
		{"503 unavailable", fmt.Errorf("HTTP 503 Service Unavailable"), "Server error"},
		{"short unknown error", fmt.Errorf("something went wrong"), "something went wrong"},
		{"long unknown error", fmt.Errorf("%s", strings.Repeat("x", 100)), "x..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := summarizeError(tt.err)
			if !strings.Contains(got, tt.wantSub) {
				t.Errorf("summarizeError() = %q, want to contain %q", got, tt.wantSub)
			}
		})
	}
}

// ─── layout / computeLayout / chromeLines ───────────────────────────────────

func TestComputeLayout(t *testing.T) {
	m := newTestModel()
	m.width = 80
	ly := m.computeLayout()
	if ly.mode != layoutCompact {
		t.Errorf("mode = %v, want compact", ly.mode)
	}
	if ly.mainW != 80 {
		t.Errorf("mainW = %d, want 80", ly.mainW)
	}

	m.width = 120
	ly = m.computeLayout()
	if ly.mode != layoutTwoPane {
		t.Errorf("mode = %v, want two-pane", ly.mode)
	}
	if ly.leftW == 0 || ly.mainW == 0 {
		t.Error("two-pane layout should allocate main and left panes")
	}
}

func TestChromeLines(t *testing.T) {
	m := newTestModel()
	got := m.chromeLines()
	if got <= 0 {
		t.Errorf("chromeLines() = %d, want positive", got)
	}
}

// ─── resetGenState ──────────────────────────────────────────────────────────

func TestResetGenState(t *testing.T) {
	m := &model{
		isGenerating:    true,
		genStartTime:    time.Now(),
		genPhase:        phaseGenerating,
		promptTokens:    1000,
		outputTokens:    500,
		cacheHitTokens:  200,
		cacheMissTokens: 300,
		reasoningTokens: 100,
	}
	m.resetGenState()
	if m.isGenerating || !m.genStartTime.IsZero() || m.genPhase != phaseWaiting {
		t.Error("resetGenState should clear all generation state")
	}
	if m.promptTokens != 0 || m.outputTokens != 0 || m.cacheHitTokens != 0 {
		t.Error("token counts should be zero after reset")
	}
}

// ─── currentMessage ─────────────────────────────────────────────────────────

func TestCurrentMessage(t *testing.T) {
	m := &model{messages: []message{}}
	if msg := m.currentMessage(); msg != nil {
		t.Error("should be nil with no messages")
	}
	m.messages = append(m.messages, message{role: "user", content: "hello"})
	if msg := m.currentMessage(); msg != nil {
		t.Error("should be nil when no agent message exists")
	}
	m.messages = append(m.messages, message{role: "agent", content: "hi"})
	if msg := m.currentMessage(); msg == nil || msg.content != "hi" {
		t.Error("should return last agent message")
	}
}

// ─── Init ───────────────────────────────────────────────────────────────────

func TestInit(t *testing.T) {
	m := newTestModel()
	if cmd := m.Init(); cmd == nil {
		t.Error("Init should return a non-nil command")
	}
}

func TestInit_WithRulesCreated(t *testing.T) {
	m := newTestModel()
	m.cfg.RulesCreated = true
	m.cfg.RulesPath = "/tmp/rules.md"
	if cmd := m.Init(); cmd == nil {
		t.Error("Init with rules should return a non-nil command")
	}
}

// ─── newRenderer ────────────────────────────────────────────────────────────

func TestNewRenderer_DarkBg(t *testing.T) {
	m := &model{width: 80, darkBg: true}
	if r := m.newRenderer(); r == nil {
		t.Error("newRenderer should return non-nil with valid width")
	}
}

func TestNewRenderer_LightBg(t *testing.T) {
	m := &model{width: 80, darkBg: false}
	if r := m.newRenderer(); r == nil {
		t.Error("newRenderer should return non-nil with valid width")
	}
}

func TestNewRenderer_ZeroWidth(t *testing.T) {
	m := &model{width: 0, darkBg: true}
	if r := m.newRenderer(); r == nil {
		t.Error("newRenderer should use fallback width")
	}
}

// ─── resizeViewport ─────────────────────────────────────────────────────────

func TestResizeViewport(t *testing.T) {
	m := newTestModel()
	m.width = 100
	m.height = 30
	m.resizeViewport()
	if m.viewport.Width() <= 0 || m.viewport.Height() <= 0 {
		t.Error("viewport dimensions should be positive after resize")
	}
}

func TestResizeViewport_SmallWindow(t *testing.T) {
	m := newTestModel()
	m.width = 30
	m.height = 10
	m.resizeViewport()
	if m.viewport.Width() < 20 {
		t.Error("viewport width should be at least 20")
	}
	if m.viewport.Height() < 3 {
		t.Error("viewport height should be at least 3")
	}
}

func TestResizeViewport_TwoPaneComposer(t *testing.T) {
	m := newTestModel()
	m.width = 120
	m.height = 30
	m.resizeViewport()
	// Viewport width is mainW-4 to account for paneStyle's Width(mainW-2).Padding(0,1)
	// Content area = (mainW-2) - 2 = mainW-4
	expectedW := max(m.computeLayout().mainW-4, 1)
	if m.viewport.Width() != expectedW {
		t.Errorf("viewport width = %d, want %d", m.viewport.Width(), expectedW)
	}
}

// ─── rebuildViewportContent ────────────────────────────────────────────────

func TestRebuildViewportContent_Empty(t *testing.T) {
	m := newTestModel()
	m.rebuildViewportContent() // should not panic
}

func TestRebuildViewportContent_WithMessages(t *testing.T) {
	m := newTestModel()
	m.messages = []message{
		{role: "user", content: "hello", dirty: true},
		{role: "agent", content: "world", dirty: true},
	}
	m.rebuildViewportContent()
	for i := range m.messages {
		if m.messages[i].dirty {
			t.Errorf("message %d should not be dirty after rebuild", i)
		}
		if m.messages[i].rendered == "" {
			t.Errorf("message %d should have rendered content", i)
		}
	}
}

func TestRebuildViewportContent_WithStreaming(t *testing.T) {
	m := newTestModel()
	m.messages = []message{
		{role: "user", content: "hello"},
		{role: "agent", content: "", dirty: true},
	}
	m.current = &streamState{toolExecMap: make(map[string]*toolExecInfo)}
	m.current.content.WriteString("streaming content")
	m.rebuildViewportContent() // should not panic
}

func TestRebuildViewportContent_StreamingSkipsLastAgent(t *testing.T) {
	m := newTestModel()
	m.messages = []message{
		{role: "user", content: "hello"},
		{role: "agent", content: "placeholder", dirty: true},
	}
	m.current = &streamState{toolExecMap: make(map[string]*toolExecInfo)}
	m.current.content.WriteString("live content")
	m.rebuildViewportContent()
}

func TestRebuildViewportContent_CachedMessages(t *testing.T) {
	m := newTestModel()
	m.messages = []message{
		{role: "user", content: "hello", dirty: false, rendered: "cached-render"},
		{role: "agent", content: "world", dirty: false, rendered: "cached-agent"},
	}
	m.rebuildViewportContent()
	if m.messages[0].rendered != "cached-render" {
		t.Error("cached message should keep its rendered content")
	}
}

func TestRebuildViewportContent_StreamingWithThinking(t *testing.T) {
	m := newTestModel()
	m.messages = []message{{role: "agent"}}
	m.current = &streamState{toolExecMap: make(map[string]*toolExecInfo)}
	m.current.thinkingBuf.WriteString("I need to analyze this...")
	m.rebuildViewportContent()
}

// ─── finalizeCurrentStream ─────────────────────────────────────────────────

func TestFinalizeCurrentStream_NilCurrent(t *testing.T) {
	m := newTestModel()
	m.current = nil
	m.finalizeCurrentStream() // should not panic
}

func TestFinalizeCurrentStream_NoAgentMessage(t *testing.T) {
	m := newTestModel()
	m.current = &streamState{toolExecMap: make(map[string]*toolExecInfo)}
	m.messages = []message{}
	m.finalizeCurrentStream()
	// currentMessage() returns nil → early return; current remains non-nil per actual behavior
}

func TestFinalizeCurrentStream_WithContent(t *testing.T) {
	m := newTestModel()
	m.current = &streamState{toolExecMap: make(map[string]*toolExecInfo)}
	m.current.content.WriteString("response text")
	m.current.thinkingBuf.WriteString("thinking text")
	m.messages = []message{{role: "agent"}}
	cancelCalled := false
	m.streamCancel = func() { cancelCalled = true }

	m.finalizeCurrentStream()

	if m.current != nil {
		t.Error("current should be nil after finalize")
	}
	if !cancelCalled {
		t.Error("streamCancel should be called")
	}
	if m.streamCancel != nil {
		t.Error("streamCancel should be nil after finalize")
	}
	msg := m.currentMessage()
	if msg == nil {
		t.Fatal("should have an agent message")
	}
	if len(msg.timeline) != 2 {
		t.Errorf("timeline should have 2 entries, got %d", len(msg.timeline))
	}
	if !msg.dirty {
		t.Error("message should be dirty after finalize")
	}
}

// ─── handleConfirmEnter ────────────────────────────────────────────────────

func TestHandleConfirmEnter_NilState(t *testing.T) {
	m := newTestModel()
	_, cmd := m.handleConfirmEnter()
	if cmd != nil {
		t.Error("nil confirmState should return nil cmd")
	}
}

func TestHandleConfirmEnter_Approve(t *testing.T) {
	m := newTestModel()
	m.confirmState = &confirmState{callID: "c1", options: []string{"[y] confirm", "[n] deny"}, selected: 0}
	_, cmd := m.handleConfirmEnter()
	crm := cmd().(confirmResultMsg)
	if crm.callID != "c1" || crm.choice != "yes" {
		t.Errorf("got callID=%q choice=%q, want c1/yes", crm.callID, crm.choice)
	}
}

func TestHandleConfirmEnter_Deny(t *testing.T) {
	m := newTestModel()
	m.confirmState = &confirmState{callID: "c1", options: []string{"[y] confirm", "[n] deny"}, selected: 1}
	_, cmd := m.handleConfirmEnter()
	crm := cmd().(confirmResultMsg)
	if crm.choice != "" {
		t.Errorf("choice = %q, want empty string (deny)", crm.choice)
	}
}

func TestHandleConfirmEnter_AllowInSession(t *testing.T) {
	m := newTestModel()
	m.confirmState = &confirmState{callID: "c1", options: []string{"[y] confirm", "[n] deny", "[a] allow in session"}, selected: 2}
	_, cmd := m.handleConfirmEnter()
	crm := cmd().(confirmResultMsg)
	if crm.choice != "allow-in-session" {
		t.Errorf("choice = %q, want allow-in-session", crm.choice)
	}
}

func TestHandleConfirmEnter_CustomOption(t *testing.T) {
	m := newTestModel()
	m.confirmState = &confirmState{callID: "c1", options: []string{"option A", "option B"}, selected: 1}
	_, cmd := m.handleConfirmEnter()
	crm := cmd().(confirmResultMsg)
	if crm.choice != "option B" {
		t.Errorf("choice = %q, want option B", crm.choice)
	}
}

// ─── Update: WindowSizeMsg ─────────────────────────────────────────────────

func TestUpdate_WindowSizeMsg(t *testing.T) {
	m := newTestModel()
	updated, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if cmd != nil {
		t.Error("WindowSizeMsg should not return a cmd")
	}
	um := updated.(model)
	if um.width != 120 || um.height != 40 {
		t.Errorf("dimensions not updated: width=%d height=%d", um.width, um.height)
	}
}

// ─── Update: KeyPressMsg ────────────────────────────────────────────────────

func TestUpdate_CtrlC_Generating(t *testing.T) {
	m := newTestModel()
	m.isGenerating = true
	m.current = &streamState{toolExecMap: make(map[string]*toolExecInfo)}
	m.streamCancel = func() {}

	updated, _ := m.Update(keyPress("ctrl+c"))
	um := updated.(model)
	if um.isGenerating {
		t.Error("Ctrl+C during generation should stop generating")
	}
	if um.current != nil {
		t.Error("Ctrl+C during generation should nil current")
	}
}

func TestUpdate_CtrlC_QuitSequence(t *testing.T) {
	m := newTestModel()
	updated, _ := m.Update(keyPress("ctrl+c"))
	um := updated.(model)
	if um.quitCount != 1 {
		t.Errorf("quitCount = %d, want 1", um.quitCount)
	}
	// Second Ctrl+C should quit
	updated2, cmd := um.Update(keyPress("ctrl+c"))
	_ = updated2
	if cmd == nil {
		t.Error("second Ctrl+C should return tea.Quit")
	}
}

func TestUpdate_CtrlC_ResetOnOtherKey(t *testing.T) {
	m := newTestModel()
	m.quitCount = 1
	updated, _ := m.Update(keyPress("j"))
	um := updated.(model)
	if um.quitCount != 0 {
		t.Error("non-Ctrl+C key should reset quitCount")
	}
}

func TestUpdate_EscDuringGeneration(t *testing.T) {
	m := newTestModel()
	m.isGenerating = true
	m.current = &streamState{toolExecMap: make(map[string]*toolExecInfo)}
	m.streamCancel = func() {}
	updated, cmd := m.Update(keyPress("esc"))
	um := updated.(model)
	if um.isGenerating {
		t.Error("Esc during generation should stop generating")
	}
	if um.cancelReason != "Esc" {
		t.Errorf("cancelReason = %q, want %q", um.cancelReason, "Esc")
	}
	if cmd == nil {
		t.Error("Esc should return a clearCancelMsg timer cmd")
	}
}

func TestUpdate_EnterWithConfirmState(t *testing.T) {
	m := newTestModel()
	m.confirmState = &confirmState{callID: "c1", options: []string{"[y] confirm", "[n] deny"}, selected: 0}
	_, cmd := m.Update(keyPress("enter"))
	if cmd == nil {
		t.Error("Enter with confirmState should return a cmd")
	}
}

func TestUpdate_EnterDuringGeneration(t *testing.T) {
	m := newTestModel()
	m.isGenerating = true
	updated, cmd := m.Update(keyPress("enter"))
	um := updated.(model)
	if !um.isGenerating {
		t.Error("Enter during generation should be no-op")
	}
	if cmd != nil {
		t.Error("Enter during generation should return nil cmd")
	}
}

func TestUpdate_EnterEmptyInput(t *testing.T) {
	m := newTestModel()
	_, cmd := m.Update(keyPress("enter"))
	if cmd != nil {
		t.Error("Enter with empty input should return nil cmd")
	}
}

func TestUpdate_CtrlA_ToggleAgentsPane(t *testing.T) {
	m := newTestModel()
	if !m.showAgents {
		t.Error("agents pane should start visible")
	}
	updated, _ := m.Update(keyPress("ctrl+a"))
	um := updated.(model)
	if um.showAgents {
		t.Error("Ctrl+A should hide agents pane")
	}
	updated2, _ := um.Update(keyPress("ctrl+a"))
	um2 := updated2.(model)
	if !um2.showAgents {
		t.Error("second Ctrl+A should show agents pane")
	}
}

func TestUpdate_UpWithConfirmState(t *testing.T) {
	m := newTestModel()
	m.confirmState = &confirmState{callID: "c1", options: []string{"a", "b", "c"}, selected: 1}
	updated, _ := m.Update(keyPress("up"))
	um := updated.(model)
	if um.confirmState.selected != 0 {
		t.Errorf("selected = %d, want 0 after up", um.confirmState.selected)
	}
}

func TestUpdate_UpWithConfirmStateAtTop(t *testing.T) {
	m := newTestModel()
	m.confirmState = &confirmState{callID: "c1", options: []string{"a", "b"}, selected: 0}
	updated, _ := m.Update(keyPress("up"))
	um := updated.(model)
	if um.confirmState.selected != 0 {
		t.Errorf("selected should stay at 0, got %d", um.confirmState.selected)
	}
}

func TestUpdate_DownWithConfirmState(t *testing.T) {
	m := newTestModel()
	m.confirmState = &confirmState{callID: "c1", options: []string{"a", "b", "c"}, selected: 1}
	updated, _ := m.Update(keyPress("down"))
	um := updated.(model)
	if um.confirmState.selected != 2 {
		t.Errorf("selected = %d, want 2 after down", um.confirmState.selected)
	}
}

func TestUpdate_UpHistoryNav(t *testing.T) {
	m := newTestModel()
	m.history = []string{"first", "second"}
	updated, _ := m.Update(keyPress("up"))
	um := updated.(model)
	if um.historyIdx != 1 {
		t.Errorf("historyIdx = %d, want 1 after up", um.historyIdx)
	}
}

func TestUpdate_DownHistoryNav(t *testing.T) {
	m := newTestModel()
	m.history = []string{"first", "second"}
	m.historyIdx = 2
	updated, _ := m.Update(keyPress("down"))
	um := updated.(model)
	if um.historyIdx != 1 {
		t.Errorf("historyIdx = %d, want 1 after down", um.historyIdx)
	}
}

// ─── Update: MouseWheelMsg ─────────────────────────────────────────────────

func TestUpdate_MouseWheel(t *testing.T) {
	m := newTestModel()
	updated, _ := m.Update(tea.MouseWheelMsg{Y: -1})
	_ = updated.(model)
}

// ─── Update: streamStartMsg ─────────────────────────────────────────────────

func TestUpdate_StreamStartSuccess(t *testing.T) {
	m := newTestModel()
	ch := make(chan agent.AgentEvent, 1)
	cancel := func() {}
	updated, cmd := m.Update(streamStartMsg{evCh: ch, cancel: cancel, streamID: 1})
	um := updated.(model)
	if um.streamCancel == nil {
		t.Error("streamCancel should be set on success")
	}
	if cmd == nil {
		t.Error("should return waitForAgentEvent cmd")
	}
}

func TestUpdate_StreamStartError(t *testing.T) {
	m := newTestModel()
	cancel := func() {}
	updated, cmd := m.Update(streamStartMsg{err: errors.New("HTTP 429 Too Many Requests"), cancel: cancel, streamID: 1})
	um := updated.(model)
	if um.errMsg == "" {
		t.Error("errMsg should be set on error")
	}
	if cmd == nil {
		t.Error("should return a clearErrMsg timer cmd")
	}
}

// ─── Update: agentEventMsg ──────────────────────────────────────────────────

func TestUpdate_AgentEventMsg(t *testing.T) {
	m := newTestModel()
	m.current = &streamState{toolExecMap: make(map[string]*toolExecInfo)}
	ch := make(chan agent.AgentEvent, 1)
	cancel := func() {}
	updated, cmd := m.Update(agentEventMsg{
		event: agent.ContentDeltaEvent{Delta: "hello"}, evCh: ch, cancel: cancel, streamID: 1,
	})
	um := updated.(model)
	if um.genPhase != phaseGenerating {
		t.Error("should be in generating phase after content delta")
	}
	if cmd == nil {
		t.Error("should return waitForAgentEvent cmd")
	}
}

func TestUpdate_AgentEvent_DelegationStarted(t *testing.T) {
	m := newTestModel()
	m.isGenerating = true
	m.current = &streamState{toolExecMap: make(map[string]*toolExecInfo)}
	ch := make(chan agent.AgentEvent, 1)
	cancel := func() {}
	updated, _ := m.Update(agentEventMsg{
		event: agent.DelegationStartedEvent{}, evCh: ch, cancel: cancel, streamID: 1,
	})
	um := updated.(model)
	if um.isGenerating {
		t.Error("DelegationStartedEvent should set isGenerating=false")
	}
}

// ─── Update: streamDoneMsg ──────────────────────────────────────────────────

func TestUpdate_StreamDoneMsg(t *testing.T) {
	m := newTestModel()
	m.isGenerating = true
	m.genStartTime = time.Now().Add(-5 * time.Second)
	m.promptTokens = 100
	m.outputTokens = 50
	m.cacheHitTokens = 20
	m.cacheMissTokens = 80
	m.reasoningTokens = 10
	m.current = &streamState{toolExecMap: make(map[string]*toolExecInfo)}
	m.current.content.WriteString("response")
	m.messages = append(m.messages, message{role: "agent"})
	m.nextStreamID = 1

	updated, cmd := m.Update(streamDoneMsg{streamID: 1})
	um := updated.(model)
	if um.isGenerating {
		t.Error("should stop generating after stream done")
	}
	if um.genSummary == "" {
		t.Error("should have generation summary")
	}
	if !strings.Contains(um.genSummary, "✓") {
		t.Error("summary should contain checkmark")
	}
	if cmd == nil {
		t.Error("should return a clearSummaryMsg timer cmd")
	}
}

func TestUpdate_StreamDoneMsg_NoTokens(t *testing.T) {
	m := newTestModel()
	m.isGenerating = true
	m.genStartTime = time.Now()
	m.current = &streamState{toolExecMap: make(map[string]*toolExecInfo)}
	m.messages = append(m.messages, message{role: "agent"})
	m.nextStreamID = 1

	updated, _ := m.Update(streamDoneMsg{streamID: 1})
	um := updated.(model)
	if !strings.Contains(um.genSummary, "✓") {
		t.Error("summary should contain checkmark even with no tokens")
	}
}

func TestUpdate_StreamDoneMsg_StaleStream(t *testing.T) {
	m := newTestModel()
	m.isGenerating = true
	m.current = &streamState{toolExecMap: make(map[string]*toolExecInfo)}
	m.messages = append(m.messages, message{role: "agent"})
	m.nextStreamID = 2 // stream done is for old stream

	updated, _ := m.Update(streamDoneMsg{streamID: 1})
	um := updated.(model)
	if um.current != nil {
		t.Error("should finalize even stale stream")
	}
}

// ─── Update: spinnerMsg ─────────────────────────────────────────────────────

func TestUpdate_SpinnerMsg_Generating(t *testing.T) {
	m := newTestModel()
	m.isGenerating = true
	initialFrame := m.spinner.frame
	updated, cmd := m.Update(spinnerMsg{})
	um := updated.(model)
	if um.spinner.frame == initialFrame {
		t.Error("spinner should advance during generation")
	}
	if cmd == nil {
		t.Error("should return another spinnerCmd during generation")
	}
}

func TestUpdate_SpinnerMsg_Idle(t *testing.T) {
	m := newTestModel()
	m.isGenerating = false
	_, cmd := m.Update(spinnerMsg{})
	if cmd != nil {
		t.Error("should not return spinnerCmd when idle")
	}
}

// ─── Update: clearCancelMsg / clearErrMsg / clearSummaryMsg / resetQuitMsg ─

func TestUpdate_ClearCancelMsg(t *testing.T) {
	m := newTestModel()
	m.cancelReason = "Esc"
	um, _ := m.Update(clearCancelMsg{})
	if um.(model).cancelReason != "" {
		t.Error("cancelReason should be cleared")
	}
}

func TestUpdate_ClearErrMsg(t *testing.T) {
	m := newTestModel()
	m.errMsg = "some error"
	um, _ := m.Update(clearErrMsg{})
	if um.(model).errMsg != "" {
		t.Error("errMsg should be cleared")
	}
}

func TestUpdate_ClearSummaryMsg(t *testing.T) {
	m := newTestModel()
	m.genSummary = "✓ 5s"
	um, _ := m.Update(clearSummaryMsg{})
	if um.(model).genSummary != "" {
		t.Error("genSummary should be cleared")
	}
}

func TestUpdate_ResetQuitMsg(t *testing.T) {
	m := newTestModel()
	m.quitCount = 1
	um, _ := m.Update(resetQuitMsg{})
	if um.(model).quitCount != 0 {
		t.Error("quitCount should be reset")
	}
}

// ─── Update: confirmResultMsg ───────────────────────────────────────────────

func TestUpdate_ConfirmResultMsg_NilConfirmState(t *testing.T) {
	m := newTestModel()
	m.confirmState = nil
	um, _ := m.Update(confirmResultMsg{callID: "c1", choice: "approve"})
	if um.(model).confirmState != nil {
		t.Error("confirmState should remain nil")
	}
}

// ─── Update: agentTickMsg ───────────────────────────────────────────────────

func TestUpdate_AgentTickMsg(t *testing.T) {
	m := newTestModel()
	initialFrame := m.sidebar.spinner.frame
	updated, cmd := m.Update(agentTickMsg{})
	um := updated.(model)
	if um.sidebar.spinner.frame == initialFrame {
		t.Error("sidebar spinner should advance on tick")
	}
	if cmd == nil {
		t.Error("should return another agentTickCmd")
	}
}

// ─── Update: textarea passthrough ───────────────────────────────────────────

func TestUpdate_TextAreaPassthrough(t *testing.T) {
	m := newTestModel()
	m.confirmState = nil
	updated, _ := m.Update(keyPress("x"))
	_ = updated.(model)
}

func TestUpdate_TextAreaPassthrough_WithConfirm(t *testing.T) {
	m := newTestModel()
	m.confirmState = &confirmState{callID: "c1", options: []string{"a"}, selected: 0}
	updated, _ := m.Update(keyPress("x"))
	_ = updated.(model)
}

// ─── View integration ───────────────────────────────────────────────────────

func TestView_Idle(t *testing.T) {
	m := newTestModel()
	v := m.View()
	if !strings.Contains(v.Content, "ready") {
		t.Error("idle View should show 'ready'")
	}
}

func TestView_Generating(t *testing.T) {
	m := newTestModel()
	m.width = 100
	m.isGenerating = true
	m.genStartTime = time.Now()
	m.genPhase = phaseGenerating
	if !strings.Contains(m.View().Content, "generating") {
		t.Error("generating View should show 'generating'")
	}
}

func TestView_WithSidebar(t *testing.T) {
	m := newTestModel()
	m.width = 120
	if !strings.Contains(m.View().Content, "AGENTS") {
		t.Error("View with visible sidebar should show 'AGENTS'")
	}
}

func TestView_WithErrMessage(t *testing.T) {
	m := newTestModel()
	m.width = 100
	m.errMsg = "Network error"
	if !strings.Contains(m.View().Content, "Network error") {
		t.Error("View should show error message")
	}
}

func TestView_WithCancelReason(t *testing.T) {
	m := newTestModel()
	m.width = 100
	m.cancelReason = "Esc"
	if !strings.Contains(m.View().Content, "cancelled") {
		t.Error("View should show cancel reason")
	}
}

func TestView_WithGenSummary(t *testing.T) {
	m := newTestModel()
	m.width = 100
	m.genSummary = "✓ 5s"
	if !strings.Contains(m.View().Content, "✓") {
		t.Error("View should show generation summary")
	}
}

func TestView_WithQuitCount(t *testing.T) {
	m := newTestModel()
	m.width = 100
	m.quitCount = 1
	if !strings.Contains(m.View().Content, "confirm exit") {
		t.Error("View should show quit confirmation")
	}
}

func TestView_GeneratingWithTokens(t *testing.T) {
	m := newTestModel()
	m.width = 120
	m.isGenerating = true
	m.genStartTime = time.Now()
	m.genPhase = phaseGenerating
	m.promptTokens = 5000
	m.outputTokens = 2000
	m.cacheHitTokens = 1000
	m.cacheMissTokens = 4000
	m.reasoningTokens = 800
	v := m.View().Content
	if !strings.Contains(v, "generating") {
		t.Error("should show generating phase")
	}
	if !strings.Contains(v, "5.0k") {
		t.Error("should show prompt tokens")
	}
}

func TestUpdate_CtrlY_EntersCopyMode(t *testing.T) {
	m := newTestModel()
	updated, _ := m.Update(keyPress("ctrl+y"))
	um := updated.(model)
	if !um.copyMode {
		t.Error("Ctrl+Y should enter copy mode")
	}
	if um.focus != focusCopy {
		t.Error("Ctrl+Y should set focusCopy")
	}
}

func TestUpdate_EscExitsCopyModeBeforeInterrupt(t *testing.T) {
	m := newTestModel()
	m.copyMode = true
	m.focus = focusCopy
	m.isGenerating = true
	updated, _ := m.Update(keyPress("esc"))
	um := updated.(model)
	if um.copyMode {
		t.Error("Esc should exit copy mode")
	}
	if !um.isGenerating {
		t.Error("Esc in copy mode should not interrupt generation")
	}
}

func TestView_CopyModeDisablesMouse(t *testing.T) {
	m := newTestModel()
	m.copyMode = true
	v := m.View()
	if v.MouseMode != tea.MouseModeNone {
		t.Errorf("copy mode MouseMode = %v, want MouseModeNone", v.MouseMode)
	}
	if !strings.Contains(v.Content, "COPY MODE") {
		t.Error("copy mode view should show copy mode footer")
	}
}

// ─── _ import suppression ───────────────────────────────────────────────────

var _ = fmt.Sprintf

// ─── Update: paste and input-mode key routing ────────────────────────────────

func TestUpdate_PasteMsg_ForwardedToTextarea(t *testing.T) {
	m := newTestModel()
	m.confirmState = nil
	m.isGenerating = false
	updated, _ := m.Update(tea.PasteMsg{Content: "hello world"})
	um := updated.(model)
	if !strings.Contains(um.textArea.Value(), "hello world") {
		t.Errorf("paste should be in textarea, got %q", um.textArea.Value())
	}
}

func TestUpdate_PasteMsg_DuringGeneration(t *testing.T) {
	m := newTestModel()
	m.isGenerating = true
	updated, _ := m.Update(tea.PasteMsg{Content: "hello"})
	um := updated.(model)
	// Paste should still go to textarea even during generation (just won't send)
	if !strings.Contains(um.textArea.Value(), "hello") {
		t.Errorf("paste during generation should still be in textarea, got %q", um.textArea.Value())
	}
}

func TestUpdate_Up_NotHistoryWhenTextareaHasContent(t *testing.T) {
	m := newTestModel()
	m.textArea.SetValue("typing")
	m.history = []string{"first", "second"}
	updated, _ := m.Update(keyPress("up"))
	um := updated.(model)
	if um.historyIdx != 0 {
		t.Errorf("up should not navigate history when textarea has content, historyIdx=%d", um.historyIdx)
	}
}

func TestUpdate_Down_NotHistoryWhenTextareaHasContent(t *testing.T) {
	m := newTestModel()
	m.textArea.SetValue("typing")
	m.history = []string{"first", "second"}
	m.historyIdx = 0 // Not navigating history
	updated, _ := m.Update(keyPress("down"))
	um := updated.(model)
	if um.historyIdx != 0 {
		t.Errorf("down should not navigate history when textarea has content and not navigating, historyIdx=%d", um.historyIdx)
	}
}

func TestUpdate_Up_NavigatesHistoryWhenHistoryIdxActive(t *testing.T) {
	m := newTestModel()
	m.textArea.SetValue("first")
	m.history = []string{"first", "second"}
	m.historyIdx = 1 // Currently viewing first history entry
	updated, _ := m.Update(keyPress("up"))
	um := updated.(model)
	if um.historyIdx != 2 {
		t.Errorf("up should navigate history when historyIdx > 0, got historyIdx=%d", um.historyIdx)
	}
}

func TestUpdate_Down_NavigatesHistoryWhenHistoryIdxActive(t *testing.T) {
	m := newTestModel()
	m.textArea.SetValue("second")
	m.history = []string{"first", "second"}
	m.historyIdx = 2 // Currently viewing second history entry
	updated, _ := m.Update(keyPress("down"))
	um := updated.(model)
	if um.historyIdx != 1 {
		t.Errorf("down should navigate history when historyIdx > 0, got historyIdx=%d", um.historyIdx)
	}
}
