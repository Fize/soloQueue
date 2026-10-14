package server

import (
	"encoding/json"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/agent"
)

func TestRuntimeMetricsKeepsConcurrentAgentStreamsByRequest(t *testing.T) {
	rm := &RuntimeMetrics{
		agentStreams: make(map[string]*AgentStreamState),
		agentCancels: make(map[string]func()),
	}

	rm.updateAgentStream("agent-1", agent.ContentDeltaEvent{RequestID: "req-a", Delta: "first"})
	rm.updateAgentStream("agent-1", agent.ContentDeltaEvent{RequestID: "req-b", Delta: "second"})

	streams := rm.AgentStreams()
	if len(streams) != 2 {
		t.Fatalf("stream count = %d, want 2", len(streams))
	}

	seen := map[string]string{}
	for _, stream := range streams {
		seen[stream.RequestID] = stream.Segments[0].Text
	}
	if seen["req-a"] != "first" || seen["req-b"] != "second" {
		t.Fatalf("request streams = %#v, want independent req-a/req-b snapshots", seen)
	}
	if !streams[agentStreamKey("agent-1", "req-a")].Processing || !streams[agentStreamKey("agent-1", "req-b")].Processing {
		t.Fatalf("concurrent request streams were retired: %#v", streams)
	}
}

func TestRuntimeMetricsSwitchesDeliveryOwnershipAtPendingBoundary(t *testing.T) {
	rm := &RuntimeMetrics{
		agentStreams: make(map[string]*AgentStreamState),
		agentCancels: make(map[string]func()),
	}
	rm.updateAgentStream("agent-1", agent.ContentDeltaEvent{RequestID: "req-root", Delta: "root output"})
	rm.updateAgentStream("agent-1", agent.PendingInputStartedEvent{
		RequestID:         "req-queued",
		PreviousRequestID: "req-root",
		RequestIDs:        []string{"req-queued"},
	})
	rm.updateAgentStream("agent-1", agent.ContentDeltaEvent{RequestID: "req-queued", Delta: "queued output"})

	streams := rm.AgentStreams()
	if _, ok := streams[agentStreamKey("agent-1", "req-root")]; ok {
		t.Fatalf("root runtime stream remained recoverable after delivery moved: %#v", streams)
	}
	queued := streams[agentStreamKey("agent-1", "req-queued")]
	if queued == nil || len(queued.Segments) != 1 || queued.Segments[0].Text != "queued output" {
		t.Fatalf("queued runtime stream = %#v, want only queued output", queued)
	}

	rm.updateAgentStream("agent-1", agent.PendingInputStartedEvent{
		RequestID:         "req-next",
		PreviousRequestID: "req-queued",
		RequestIDs:        []string{"req-next"},
	})
	rm.updateAgentStream("agent-1", agent.ContentDeltaEvent{RequestID: "req-next", Delta: "next output"})
	streams = rm.AgentStreams()
	if _, ok := streams[agentStreamKey("agent-1", "req-queued")]; ok {
		t.Fatalf("first queued stream remained recoverable after second boundary: %#v", streams)
	}
	next := streams[agentStreamKey("agent-1", "req-next")]
	if next == nil || len(next.Segments) != 1 || next.Segments[0].Text != "next output" {
		t.Fatalf("next runtime stream = %#v, want only next output", next)
	}
}

func TestRuntimeMetricsPopulatesAgentStreamStartedAt(t *testing.T) {
	rm := &RuntimeMetrics{
		agentStreams: make(map[string]*AgentStreamState),
		agentCancels: make(map[string]func()),
	}

	rm.updateAgentStream("agent-1", agent.ContentDeltaEvent{RequestID: "req-1", Delta: "first"})
	stream := rm.AgentStreams()["agent-1\x00req-1"]
	if stream == nil || stream.StartedAt.IsZero() {
		t.Fatalf("stream started_at = %#v, want a populated timestamp", stream)
	}
	payload, err := json.Marshal(stream)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) == "{}" || !containsJSONField(payload, "started_at") {
		t.Fatalf("stream JSON = %s, want started_at", payload)
	}
}

func TestRuntimeMetricsTerminalizesCancelledRequestStream(t *testing.T) {
	rm := &RuntimeMetrics{
		agentStreams: make(map[string]*AgentStreamState),
		agentCancels: make(map[string]func()),
	}
	rm.updateAgentStream("agent-1", agent.ContentDeltaEvent{RequestID: "req-cancelled", Delta: "partial"})

	rm.TerminalizeAgentStream("agent-1", "req-cancelled", "Task cancelled.")

	stream := rm.AgentStreams()["agent-1\x00req-cancelled"]
	if stream == nil {
		t.Fatal("cancelled request stream is missing")
	}
	if stream.Processing {
		t.Fatal("cancelled request stream is still processing")
	}
	if stream.Error != "Task cancelled." {
		t.Fatalf("cancelled request error = %q, want Task cancelled.", stream.Error)
	}

	rm.updateAgentStream("agent-1", agent.ContentDeltaEvent{RequestID: "req-cancelled", Delta: "late"})
	stream = rm.AgentStreams()["agent-1\x00req-cancelled"]
	if stream.Processing || stream.Error != "Task cancelled." || len(stream.Segments) != 1 {
		t.Fatalf("late event resurrected cancelled runtime stream: %#v", stream)
	}
}

func containsJSONField(payload []byte, field string) bool {
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return false
	}
	_, ok := decoded[field]
	return ok
}

func TestRuntimeMetricsPrunesCompletedStreamsWhenNewRequestStarts(t *testing.T) {
	rm := &RuntimeMetrics{
		agentStreams: make(map[string]*AgentStreamState),
		agentCancels: make(map[string]func()),
	}

	rm.updateAgentStream("agent-1", agent.ContentDeltaEvent{RequestID: "req-old", Delta: "old"})
	rm.updateAgentStream("agent-1", agent.DoneEvent{RequestID: "req-old", Content: "old"})
	rm.updateAgentStream("agent-1", agent.ContentDeltaEvent{RequestID: "req-new", Delta: "new"})

	streams := rm.AgentStreams()
	if len(streams) != 1 {
		t.Fatalf("stream count after new request = %d, want 1", len(streams))
	}
	if streams["agent-1\x00req-new"] == nil {
		t.Fatalf("new request stream missing: %#v", streams)
	}
}

func TestRuntimeMetricsPreservesDelegatedAgentInstanceID(t *testing.T) {
	rm := &RuntimeMetrics{
		agentStreams: make(map[string]*AgentStreamState),
		agentCancels: make(map[string]func()),
	}

	rm.updateAgentStream("agent-1", agent.ToolExecStartEvent{
		RequestID:     "req-1",
		CallID:        "call-1",
		Name:          "delegate",
		Args:          `{"target":"research"}`,
		TargetAgentID: "child-instance-1",
	})

	stream := rm.AgentStreams()["agent-1\x00req-1"]
	if stream == nil || len(stream.Segments) != 1 {
		t.Fatalf("delegation stream = %#v, want one segment", stream)
	}
	if stream.Segments[0].AgentInstanceID != "child-instance-1" {
		t.Fatalf("agent instance ID = %q, want child-instance-1", stream.Segments[0].AgentInstanceID)
	}
}

func TestRuntimeMetricsDropsLateEventsFromReplacedOrStoppedWatch(t *testing.T) {
	a := agent.NewAgent(agent.Definition{ID: "watched-agent"}, nil, nil)
	rm := &RuntimeMetrics{}

	rm.StartAgentWatch(a)
	rm.agentStreamsMu.RLock()
	oldGeneration := rm.agentWatchGen[a.InstanceID]
	rm.agentStreamsMu.RUnlock()

	rm.StartAgentWatch(a)
	rm.agentStreamsMu.RLock()
	newGeneration := rm.agentWatchGen[a.InstanceID]
	rm.agentStreamsMu.RUnlock()
	if newGeneration <= oldGeneration {
		t.Fatalf("watch generation = %d after replacement, want greater than %d", newGeneration, oldGeneration)
	}

	// A cancelled watcher can still deliver an event that was already buffered
	// before replacement. It must not recreate a stream owned by the new watch.
	rm.updateAgentStreamForGeneration(a.InstanceID, oldGeneration, agent.ContentDeltaEvent{
		RequestID: "stale-request",
		Delta:     "stale",
	})
	if streams := rm.AgentStreams(); len(streams) != 0 {
		t.Fatalf("stale replacement event recreated streams: %#v", streams)
	}

	rm.updateAgentStreamForGeneration(a.InstanceID, newGeneration, agent.ContentDeltaEvent{
		RequestID: "current-request",
		Delta:     "current",
	})
	if streams := rm.AgentStreams(); len(streams) != 1 || streams[a.InstanceID+"\x00current-request"] == nil {
		t.Fatalf("current replacement event did not create its stream: %#v", streams)
	}

	rm.StopAgentWatch(a.InstanceID)
	rm.updateAgentStreamForGeneration(a.InstanceID, newGeneration, agent.ContentDeltaEvent{
		RequestID: "late-request",
		Delta:     "late",
	})
	if streams := rm.AgentStreams(); len(streams) != 0 {
		t.Fatalf("late stopped-watch event recreated streams: %#v", streams)
	}
}
