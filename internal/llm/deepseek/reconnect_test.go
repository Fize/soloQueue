package deepseek

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/llm"
)

// A dropped connection is retried only while the consumer has seen no model
// output. Replaying after output has started would deliver the same answer
// twice, and the conversation view would render one message twice. After the
// first token a failure is therefore reported, and the output already shown
// stays as it is.

func roleChunk() string {
	return sseChunk(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant"}}}})
}

func deltaChunk(content string) string {
	return sseChunk(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": content}}}})
}

func finishChunk() string {
	return sseChunk(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}}})
}

func collectEvents(t *testing.T, c *Client) (events []llm.Event, text string) {
	t.Helper()
	ch, err := c.ChatStream(context.Background(), agent.LLMRequest{
		Model:    "deepseek-chat",
		Messages: []agent.LLMMessage{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	var out strings.Builder
	for ev := range ch {
		events = append(events, ev)
		out.WriteString(ev.ContentDelta)
	}
	return events, out.String()
}

func TestChatStream_DroppedBeforeAnyOutputReconnectsWithoutDuplicates(t *testing.T) {
	srv, rec := newFakeDeepSeek(t,
		// The first connection ends before the model said anything.
		script{SSE: []string{roleChunk()}},
		script{SSE: []string{roleChunk(), deltaChunk("Hello "), deltaChunk("world"), finishChunk(), "data: [DONE]\n\n"}},
	)
	events, text := collectEvents(t, newClient(t, srv))

	if text != "Hello world" {
		t.Fatalf("text = %q, want the answer exactly once", text)
	}
	if rec.Len() != 2 {
		t.Fatalf("requests = %d, want the dropped connection retried once", rec.Len())
	}
	for _, ev := range events {
		if ev.Type == llm.EventError {
			t.Fatalf("a retry that succeeded must not surface an error: %v", ev.Err)
		}
	}
	if last := events[len(events)-1]; last.Type != llm.EventDone || last.FinishReason != llm.FinishStop {
		t.Fatalf("last event = %+v, want done/stop", last)
	}
}

func TestChatStream_DroppedAfterOutputIsReportedNotReplayed(t *testing.T) {
	srv, rec := newFakeDeepSeek(t,
		// The first connection dies after the consumer already saw "Hello ".
		script{SSE: []string{roleChunk(), deltaChunk("Hel"), deltaChunk("lo ")}},
		// A replay would deliver the answer again; it must never be requested.
		script{SSE: []string{roleChunk(), deltaChunk("Hello "), deltaChunk("world"), finishChunk(), "data: [DONE]\n\n"}},
	)
	events, text := collectEvents(t, newClient(t, srv))

	if text != "Hello " {
		t.Fatalf("text = %q, want only what was streamed before the drop", text)
	}
	if rec.Len() != 1 {
		t.Fatalf("requests = %d, want no replay after output started", rec.Len())
	}
	last := events[len(events)-1]
	if last.Type != llm.EventError || !errors.Is(last.Err, io.ErrUnexpectedEOF) {
		t.Fatalf("last event = %+v, want the interruption reported as an error", last)
	}
}
