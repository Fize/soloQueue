package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/agent/agenttest"
	"github.com/xiaobaitu/soloqueue/internal/agenttools/tools"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/infra/telemetry"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/memory/timeline"
	"github.com/xiaobaitu/soloqueue/internal/transcript"
)

// transcriptHarness runs a real Session whose timeline and journal are
// inspected the same way the transcript API serves them.
type transcriptHarness struct {
	t       *testing.T
	session *Session
	agent   *agent.Agent
	cw      *ctxwin.ContextWindow
	dir     string
	journal *transcript.Journal
}

func newTranscriptHarness(t *testing.T, fake *agenttest.FakeLLM, toolset ...tools.Tool) *transcriptHarness {
	t.Helper()
	dir := t.TempDir()
	tl, err := timeline.NewWriter(dir, "timeline", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tl.Close() })
	opts := []agent.Option{}
	if len(toolset) > 0 {
		opts = append(opts, agent.WithTools(toolset...))
	}
	a := agent.NewAgent(agent.Definition{ID: "tx-agent"}, fake, nil, opts...)
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Stop(2 * time.Second) })
	cw := ctxwin.NewContextWindow(1<<20, 2000, 0, ctxwin.NewTokenizer(), ctxwin.WithPushHook(timelinePushHook(tl, "tx-agent", nil)))
	cw.SetReplayMode(true)
	cw.Push(ctxwin.RoleSystem, "sys")
	cw.SetReplayMode(false)
	s := NewSession("tx-"+strings.ReplaceAll(t.Name(), "/", "-"), "team", a, cw, tl, newTestLog(t))
	s.SetTranscriptRegistry(transcript.NewRegistry(transcript.Config{Grace: time.Hour}))
	return &transcriptHarness{t: t, session: s, agent: a, cw: cw, dir: dir, journal: s.Transcript()}
}

type capturedSnapshot struct {
	snap  transcript.Snapshot
	items []transcript.Item
}

// snapshot follows the API: watermark first, persisted rows second.
func (h *transcriptHarness) snapshot() capturedSnapshot {
	snap := h.journal.Snapshot()
	return capturedSnapshot{snap: snap, items: transcript.Assemble(h.persisted(), snap)}
}

func (h *transcriptHarness) persisted() []transcript.Item {
	events, _ := timeline.ReadAllEvents(h.dir)
	post, _ := transcript.PostClear(events)
	return transcript.Build(post, transcript.BuildOptions{NormalizeUserText: StripRecalledMemories})
}

// snapshotWhile captures snapshots continuously until stop is closed.
func (h *transcriptHarness) snapshotWhile(stop <-chan struct{}) <-chan []capturedSnapshot {
	done := make(chan []capturedSnapshot, 1)
	go func() {
		var snaps []capturedSnapshot
		for {
			snaps = append(snaps, h.snapshot())
			select {
			case <-stop:
				done <- snaps
				return
			case <-time.After(time.Millisecond):
			}
		}
	}()
	return done
}

func (h *transcriptHarness) waitRequestsEnded(ids ...string) {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ended := true
		snap := h.journal.Snapshot()
		for _, id := range ids {
			found := false
			for _, r := range snap.Requests {
				if r.RequestID == id && r.Ended {
					found = true
				}
			}
			ended = ended && found
		}
		if ended {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	h.t.Fatalf("requests %v did not end", ids)
}

func normalizeItems(items []transcript.Item) []string {
	var out []string
	for _, item := range items {
		if item.Ephemeral {
			continue
		}
		var content, thinking, compact, errText, toolText strings.Builder
		for _, seg := range item.Segments {
			switch seg.Type {
			case transcript.SegmentContent:
				content.WriteString(seg.Text)
			case transcript.SegmentThinking:
				thinking.WriteString(seg.Text)
			case transcript.SegmentCompact:
				compact.WriteString(seg.Text)
			case transcript.SegmentError:
				errText.WriteString(seg.Text)
			case transcript.SegmentToolCall:
				done := seg.Done != nil && *seg.Done
				fmt.Fprintf(&toolText, "[%s %s done=%v result=%q err=%q]", seg.CallID, seg.Name, done, seg.Result, seg.Error)
			}
		}
		out = append(out, fmt.Sprintf("%s kind=%s req=%s content=%q thinking=%q compact=%q error=%q tools=%s",
			item.ID, item.Kind, item.RequestID, content.String(), thinking.String(), compact.String(), errText.String(), toolText.String()))
	}
	return out
}

func diffNormalized(a, b []string) string {
	if strings.Join(a, "\n") == strings.Join(b, "\n") {
		return ""
	}
	return fmt.Sprintf("got:\n  %s\nwant:\n  %s", strings.Join(a, "\n  "), strings.Join(b, "\n  "))
}

// verify checks the core property: every snapshot plus the frames after its
// watermark reproduces the final snapshot, and the persisted rows alone
// reproduce it as well (a reload after the in-memory items expired).
func (h *transcriptHarness) verify(snaps []capturedSnapshot) transcript.Snapshot {
	h.t.Helper()
	final := h.snapshot()
	want := normalizeItems(final.items)
	if len(want) == 0 {
		h.t.Fatal("final transcript is empty")
	}
	if diff := diffNormalized(normalizeItems(h.persisted()), want); diff != "" {
		h.t.Fatalf("persisted rows differ from the live transcript:\n%s", diff)
	}
	for i, captured := range snaps {
		view := transcript.ViewFromSnapshot(captured.items, captured.snap.Pending, captured.snap.Requests)
		frames, err := h.journal.ReadAfter(captured.snap.Epoch, captured.snap.Seq, 0)
		if err != nil {
			h.t.Fatalf("snapshot %d: read after %d: %v", i, captured.snap.Seq, err)
		}
		for _, f := range frames {
			view.Apply(f)
		}
		if diff := diffNormalized(normalizeItems(view.List()), want); diff != "" {
			h.t.Fatalf("snapshot %d (seq %d of %d) + frames differs from final:\n%s", i, captured.snap.Seq, final.snap.Seq, diff)
		}
	}
	return final.snap
}

func requestCtx(id string) context.Context {
	return telemetry.WithTelemetryMetadata(context.Background(), telemetry.Metadata{RequestID: id})
}

func drain(t *testing.T, out <-chan iface.AgentEvent) {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-out:
			if !ok {
				return
			}
		case <-timeout:
			t.Fatal("stream did not finish")
		}
	}
}

func TestTranscriptSnapshotsReplayToolLoop(t *testing.T) {
	fake := &agenttest.FakeLLM{
		ReasoningDeltasByTurn: [][]string{{"plan ", "it"}, {"check"}},
		StreamDeltas:          [][]string{{"first ", "part"}, {"final ", "answer"}},
		ToolCallDeltasByTurn:  [][]llm.ToolCallDelta{{{Index: 0, ID: "call_1", Name: "echo", Arguments: `{}`}}, nil},
		FinishByTurn:          []llm.FinishReason{llm.FinishToolCalls, llm.FinishStop},
	}
	h := newTranscriptHarness(t, fake, syncEchoTool{})
	stop := make(chan struct{})
	snaps := h.snapshotWhile(stop)
	out, err := h.session.AskStream(requestCtx("req-1"), "hello")
	if err != nil {
		t.Fatal(err)
	}
	drain(t, out)
	h.waitRequestsEnded("req-1")
	close(stop)
	h.verify(<-snaps)

	items := h.snapshot().items
	var ids []string
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	if got := strings.Join(ids, ","); got != "in:req-1,out:req-1:0,out:req-1:1" {
		t.Fatalf("items = %s", got)
	}
}

func TestTranscriptOrdersQueuedInputAtItsCommit(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	fake := &agenttest.FakeLLM{
		StreamDeltas:         [][]string{{"first"}, {"second"}},
		ToolCallDeltasByTurn: [][]llm.ToolCallDelta{{{Index: 0, ID: "call_1", Name: "echo", Arguments: `{}`}}, nil},
		FinishByTurn:         []llm.FinishReason{llm.FinishToolCalls, llm.FinishStop},
		Hook:                 func(agent.LLMRequest) { once.Do(func() { close(started); <-release }) },
	}
	h := newTranscriptHarness(t, fake, syncEchoTool{})
	stop := make(chan struct{})
	snaps := h.snapshotWhile(stop)
	out, err := h.session.AskStream(requestCtx("req-1"), "one")
	if err != nil {
		t.Fatal(err)
	}
	<-started
	if _, err := h.session.AskStream(requestCtx("req-2"), "two"); !errors.Is(err, ErrQueued) {
		t.Fatalf("second request: %v", err)
	}
	if pending := h.journal.Snapshot().Pending; len(pending) != 1 || pending[0].RequestID != "req-2" {
		t.Fatalf("pending = %#v", pending)
	}
	close(release)
	drain(t, out)
	h.waitRequestsEnded("req-1")
	close(stop)
	final := h.verify(<-snaps)

	var ids []string
	for _, item := range h.snapshot().items {
		ids = append(ids, item.ID)
	}
	if got := strings.Join(ids, ","); got != "in:req-1,out:req-1:0,in:req-2,out:req-2:1" {
		t.Fatalf("items = %s", got)
	}
	if len(final.Pending) != 0 {
		t.Fatalf("pending after drain = %#v", final.Pending)
	}
}

func TestTranscriptContinuesPendingAfterFinalCall(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	fake := &agenttest.FakeLLM{
		StreamDeltas: [][]string{{"first reply"}, {"queued reply"}},
		Hook:         func(agent.LLMRequest) { once.Do(func() { close(started); <-release }) },
	}
	h := newTranscriptHarness(t, fake)
	stop := make(chan struct{})
	snaps := h.snapshotWhile(stop)
	out, err := h.session.AskStream(requestCtx("root"), "first")
	if err != nil {
		t.Fatal(err)
	}
	<-started
	if _, err := h.session.AskStream(requestCtx("late"), "second"); !errors.Is(err, ErrQueued) {
		t.Fatalf("late request: %v", err)
	}
	close(release)
	drain(t, out)
	h.waitRequestsEnded("root")
	close(stop)
	h.verify(<-snaps)

	var ids []string
	for _, item := range h.snapshot().items {
		ids = append(ids, item.ID)
	}
	if got := strings.Join(ids, ","); got != "in:root,out:root:0,in:late,out:late:0" {
		t.Fatalf("items = %s", got)
	}
}

func TestTranscriptCancelledIterationMatchesPersistedRow(t *testing.T) {
	fake := &agenttest.FakeLLM{}
	h := newTranscriptHarness(t, fake)
	source := make(chan agent.AgentEvent)
	var sinkRef atomic.Value
	h.session.askStreamHistory = func(ctx context.Context, _ *ctxwin.ContextWindow, _ string) (<-chan agent.AgentEvent, error) {
		sinkRef.Store(iface.TranscriptSinkFromContext(ctx))
		return source, nil
	}
	emit := func(ev agent.AgentEvent) {
		if sink, _ := sinkRef.Load().(iface.TranscriptSink); sink != nil {
			sink.PublishAgentEvent(h.agent.InstanceID, ev)
		}
		source <- ev
	}
	stop := make(chan struct{})
	snaps := h.snapshotWhile(stop)
	out, err := h.session.AskStream(requestCtx("req-c"), "go")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range out {
		}
	}()
	emit(agent.ReasoningDeltaEvent{RequestID: "req-c", Iter: 0, Delta: "first thought"})
	emit(agent.ContentDeltaEvent{RequestID: "req-c", Iter: 0, Delta: "step one"})
	emit(agent.ToolExecStartEvent{RequestID: "req-c", Iter: 0, CallID: "c1", Name: "echo", Args: "{}"})
	emit(agent.ToolExecDoneEvent{RequestID: "req-c", Iter: 0, CallID: "c1", Name: "echo", Result: "echoed"})
	// The agent persists the completed iteration before the next one starts.
	h.cw.Push(ctxwin.RoleAssistant, "step one", ctxwin.WithRequestID("req-c"), ctxwin.WithReasoningContent("first thought"),
		ctxwin.WithToolCalls([]llm.ToolCall{{ID: "c1", Function: llm.FunctionCall{Name: "echo", Arguments: "{}"}}}), ctxwin.WithIter(0))
	h.cw.Push(ctxwin.RoleTool, "echoed", ctxwin.WithRequestID("req-c"), ctxwin.WithToolCallID("c1"), ctxwin.WithToolName("echo"))
	emit(agent.ReasoningDeltaEvent{RequestID: "req-c", Iter: 1, Delta: "second thought"})
	emit(agent.ContentDeltaEvent{RequestID: "req-c", Iter: 1, Delta: "half of step two"})
	if err := h.session.CancelCurrent("test"); err != nil {
		t.Fatal(err)
	}
	close(source)
	h.waitRequestsEnded("req-c")
	close(stop)
	h.verify(<-snaps)

	var ids []string
	for _, item := range h.snapshot().items {
		ids = append(ids, item.ID)
	}
	if got := strings.Join(ids, ","); got != "in:req-c,out:req-c:0,out:req-c:1" {
		t.Fatalf("items = %s", got)
	}
}

func TestTranscriptClearResetsHistory(t *testing.T) {
	fake := &agenttest.FakeLLM{StreamDeltas: [][]string{{"before clear"}, {"after clear"}}}
	h := newTranscriptHarness(t, fake)
	out, err := h.session.AskStream(requestCtx("r1"), "hello")
	if err != nil {
		t.Fatal(err)
	}
	drain(t, out)
	h.waitRequestsEnded("r1")
	head := h.journal.Head()
	out, err = h.session.AskStream(requestCtx("r2"), "/clear")
	if err != nil {
		t.Fatal(err)
	}
	drain(t, out)
	h.waitRequestsEnded("r2")
	frames, err := h.journal.ReadAfter(h.journal.Epoch(), head, 0)
	if err != nil {
		t.Fatal(err)
	}
	reset := false
	for _, f := range frames {
		reset = reset || (f.Kind == transcript.KindTranscriptReset && f.Reason == "clear")
	}
	if !reset {
		t.Fatalf("no reset frame in %#v", frames)
	}
	if items := h.snapshot().items; len(items) != 0 {
		t.Fatalf("items after clear = %#v", items)
	}
	out, err = h.session.AskStream(requestCtx("r3"), "again")
	if err != nil {
		t.Fatal(err)
	}
	drain(t, out)
	h.waitRequestsEnded("r3")
	var ids []string
	for _, item := range h.snapshot().items {
		ids = append(ids, item.ID)
	}
	if got := strings.Join(ids, ","); got != "in:r3,out:r3:0" {
		t.Fatalf("items = %s", got)
	}
}

func TestTranscriptIgnoresOtherAgents(t *testing.T) {
	h := newTranscriptHarness(t, &agenttest.FakeLLM{})
	sink := h.session.newTranscriptSink(h.agent.InstanceID, "req-x")
	head := h.journal.Head()
	sink.PublishAgentEvent("child-agent", agent.ContentDeltaEvent{RequestID: "req-x", Delta: "child output"})
	if h.journal.Head() != head {
		t.Fatal("a delegated child agent published into the parent transcript")
	}
	sink.PublishAgentEvent(h.agent.InstanceID, agent.ContentDeltaEvent{RequestID: "req-x", Delta: "leader output"})
	if h.journal.Head() != head+1 {
		t.Fatal("the leader agent was not published")
	}
}

func TestAskIsolatedStaysOutOfTheTranscript(t *testing.T) {
	h := newTranscriptHarness(t, &agenttest.FakeLLM{StreamDeltas: [][]string{{"isolated answer"}}})
	head := h.journal.Head()
	out, err := h.session.AskIsolated(requestCtx("iso"), "background task")
	if err != nil {
		t.Fatal(err)
	}
	drain(t, out)
	if got := h.journal.Head(); got != head {
		frames, _ := h.journal.ReadAfter(h.journal.Epoch(), head, 0)
		t.Fatalf("isolated ask published %d frames: %+v", got-head, frames)
	}
}

func TestTranscriptSessionWithoutRegistryDoesNotStamp(t *testing.T) {
	fake := &agenttest.FakeLLM{StreamDeltas: [][]string{{"plain"}}}
	s, readAssistants := startTimelineSession(t, fake)
	out, err := s.AskStream(requestCtx("r"), "hi")
	if err != nil {
		t.Fatal(err)
	}
	drain(t, out)
	for _, msg := range readAssistants() {
		if msg.ItemID != "" || msg.Seq != 0 {
			t.Fatalf("row stamped without a registry: %#v", msg)
		}
	}
}

func TestCompactionStatusIsPublishedOnlyToTheOwningSession(t *testing.T) {
	compactor := &staticSessionCompactor{summary: "summary"}
	newSession := func(id string, withRegistry bool) (*Session, *ctxwin.ContextWindow) {
		cw := ctxwin.NewContextWindow(1<<20, 2000, 0, ctxwin.NewTokenizer(), ctxwin.WithCompactor(compactor))
		cw.Push(ctxwin.RoleSystem, "sys")
		cw.Push(ctxwin.RoleUser, "context to summarize")
		s := NewSession(id, "team", nil, cw, nil, newTestLog(t))
		if withRegistry {
			s.SetTranscriptRegistry(transcript.NewRegistry(transcript.Config{Grace: time.Hour}))
		}
		return s, cw
	}
	live, liveCW := newSession("l1", true)
	if _, err := liveCW.CompactAndReplace(context.Background()); err != nil {
		t.Fatal(err)
	}
	j := live.Transcript()
	frames, err := j.ReadAfter(j.Epoch(), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var statuses []string
	for _, f := range frames {
		if f.Kind == transcript.KindCompactionStatus && f.Compaction != nil {
			statuses = append(statuses, f.Compaction.Status)
		}
	}
	if strings.Join(statuses, ",") != "running,completed" {
		t.Fatalf("compaction frames = %v, want running,completed", statuses)
	}

	// A headless session (cron) has no registry: its compaction stays out of
	// every live transcript.
	headless, headlessCW := newSession("cron-l1-task-session", false)
	if _, err := headlessCW.CompactAndReplace(context.Background()); err != nil {
		t.Fatal(err)
	}
	if headless.Transcript() != nil {
		t.Fatal("headless session resolved a journal")
	}
}
