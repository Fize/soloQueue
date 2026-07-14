package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/iface"
)

// TestForwardAgentEvents_BatchesContentDeltas verifies that a burst of
// ContentDeltaEvents arriving within streamBatchInterval is collapsed into a
// single chat_chunk WS frame, dramatically reducing commit frequency on the
// client.
func TestForwardAgentEvents_BatchesContentDeltas(t *testing.T) {
	client := &Client{
		send:           make(chan []byte, 64),
		ctx:            context.Background(),
		activeRequests: make(map[string]*activeRequest),
	}
	reqCancel := func() {}
	client.addActiveRequest("req-test", reqCancel)

	ch := make(chan iface.AgentEvent, 32)
	go func() {
		defer close(ch)
		// 5 deltas within ~5ms — well inside streamBatchInterval (30ms).
		for i := 0; i < 5; i++ {
			ch <- agent.ContentDeltaEvent{Iter: 0, Delta: "a"}
			time.Sleep(time.Millisecond)
		}
	}()

	done := make(chan struct{})
	h := &Hub{}
	go func() {
		h.forwardAgentEvents(client, "req-test", reqCancel, ch, "l1", "")
		close(done)
	}()

	chunkCount := 0
	var chunkBodies []string
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
loop:
	for {
		select {
		case data, ok := <-client.send:
			if !ok {
				break loop
			}
			var msg WSMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if msg.Type == "chat_chunk" {
				chunkCount++
				chunkBodies = append(chunkBodies, msg.Delta)
			}
		case <-done:
			// Drain remaining buffered frames.
			for {
				select {
				case data, ok := <-client.send:
					if !ok {
						break loop
					}
					var msg WSMessage
					_ = json.Unmarshal(data, &msg)
					if msg.Type == "chat_chunk" {
						chunkCount++
						chunkBodies = append(chunkBodies, msg.Delta)
					}
				default:
					break loop
				}
			}
		case <-deadline.C:
			break loop
		}
	}

	if chunkCount != 1 {
		t.Errorf("expected exactly 1 batched chat_chunk frame, got %d (bodies=%q)", chunkCount, chunkBodies)
	}
	if len(chunkBodies) == 1 && chunkBodies[0] != "aaaaa" {
		t.Errorf("expected concatenated delta %q, got %q", "aaaaa", chunkBodies[0])
	}
}

// TestForwardAgentEvents_FlushesOnStructuralEvent verifies that when a
// structural event (e.g. ToolExecStartEvent) arrives while content deltas are
// pending, the pending deltas are flushed BEFORE the structural frame so the
// client always sees consistent ordering.
func TestForwardAgentEvents_FlushesOnStructuralEvent(t *testing.T) {
	client := &Client{
		send:           make(chan []byte, 64),
		ctx:            context.Background(),
		activeRequests: make(map[string]*activeRequest),
	}
	reqCancel := func() {}
	client.addActiveRequest("req-test", reqCancel)

	ch := make(chan iface.AgentEvent, 16)
	go func() {
		defer close(ch)
		ch <- agent.ContentDeltaEvent{Iter: 0, Delta: "hello "}
		time.Sleep(5 * time.Millisecond)
		ch <- agent.ContentDeltaEvent{Iter: 0, Delta: "world"}
		// Structural event — must trigger a flush of pending text first.
		ch <- agent.ToolExecStartEvent{Iter: 0, CallID: "c1", Name: "Read", Args: "{}"}
	}()

	h := &Hub{}
	h.forwardAgentEvents(client, "req-test", reqCancel, ch, "l1", "")
	_ = time.Millisecond

	var types []string
	for {
		select {
		case data, ok := <-client.send:
			if !ok {
				goto done
			}
			var msg WSMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			types = append(types, msg.Type)
		default:
			goto done
		}
	}
done:
	if len(types) < 2 {
		t.Fatalf("expected at least 2 frames (chat_chunk + tool_start), got %d: %v", len(types), types)
	}
	if types[0] != "chat_chunk" {
		t.Errorf("expected first frame to be chat_chunk (flushed before tool_start), got %s", types[0])
	}
	if types[1] != "tool_start" {
		t.Errorf("expected second frame to be tool_start, got %s", types[1])
	}
}
