package telemetry

import (
	"context"
	"fmt"
	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/llm/deepseek"
	"github.com/xiaobaitu/soloqueue/internal/observability"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestTransportObservationCountsRealRetries(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			w.WriteHeader(503)
			_, _ = fmt.Fprint(w, `{"error":{"message":"temporary"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"answer\"},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":3,\"total_tokens\":5}}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()
	inner, e := deepseek.NewClient(deepseek.Config{BaseURL: srv.URL, APIKey: "test-only", Retry: llm.RetryPolicy{MaxRetries: 1, InitialDelay: time.Millisecond}})
	if e != nil {
		t.Fatal(e)
	}
	store := observability.ForWorkDir(t.TempDir())
	c := NewTelemetryClient(inner, nil, store)
	ctx := WithTelemetryMetadata(context.Background(), Metadata{SessionID: "l1", RequestID: "r"})
	if _, e = c.Chat(ctx, agent.LLMRequest{Model: "m"}); e != nil {
		t.Fatal(e)
	}
	r, e := store.Request("l1", "r")
	if e != nil || r.CallCount != 1 || r.Calls[0].AttemptCount != 2 || r.Calls[0].RetryCount != 1 || n.Load() != 2 {
		t.Fatalf("%#v %v", r, e)
	}
}
func TestTransportObservationRecordsCancellation(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)
	inner, e := deepseek.NewClient(deepseek.Config{BaseURL: srv.URL, APIKey: "test-only"})
	if e != nil {
		t.Fatal(e)
	}
	store := observability.ForWorkDir(t.TempDir())
	c := NewTelemetryClient(inner, nil, store)
	ctx, cancel := context.WithCancel(WithTelemetryMetadata(context.Background(), Metadata{SessionID: "l1", RequestID: "r"}))
	done := make(chan error, 1)
	go func() { _, e := c.Chat(ctx, agent.LLMRequest{Model: "m"}); done <- e }()
	<-started
	cancel()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("expected cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("call did not cancel")
	}
	r, e := store.Request("l1", "r")
	if e != nil || len(r.Calls) != 1 || r.Calls[0].Status != StatusCancelled || r.Calls[0].AttemptCount != 1 {
		t.Fatalf("%#v %v", r, e)
	}
}
