package server

import (
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
