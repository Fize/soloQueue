package supervised

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent/llmtypes"
	"github.com/xiaobaitu/soloqueue/internal/infra/telemetryctx"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/runwatch"
)

type scriptedClient struct {
	called atomic.Bool
}

type blockedStreamClient struct{}

type silentStreamClient struct{}

func (*blockedStreamClient) Chat(context.Context, llmtypes.LLMRequest) (*llmtypes.LLMResponse, error) {
	return nil, fmt.Errorf("unused")
}

func (*blockedStreamClient) ChatStream(context.Context, llmtypes.LLMRequest) (<-chan llm.Event, error) {
	ch := make(chan llm.Event, 65)
	for i := 0; i < 65; i++ {
		ch <- llm.Event{Type: llm.EventDelta, ContentDelta: "x"}
	}
	close(ch)
	return ch, nil
}

func (*silentStreamClient) Chat(context.Context, llmtypes.LLMRequest) (*llmtypes.LLMResponse, error) {
	return nil, fmt.Errorf("unused")
}

func (*silentStreamClient) ChatStream(context.Context, llmtypes.LLMRequest) (<-chan llm.Event, error) {
	return make(chan llm.Event), nil
}

func (c *scriptedClient) Chat(ctx context.Context, _ llmtypes.LLMRequest) (*llmtypes.LLMResponse, error) {
	c.called.Store(true)
	if runwatch.HandleFromContext(ctx) == nil {
		return nil, context.Canceled
	}
	return &llmtypes.LLMResponse{Content: "ok"}, nil
}

func (c *scriptedClient) ChatStream(context.Context, llmtypes.LLMRequest) (<-chan llm.Event, error) {
	return nil, context.Canceled
}

func TestClientCreatesSupervisionRootForUnscopedChat(t *testing.T) {
	manager := runwatch.NewManager(runwatch.Policy{})
	defer manager.Close()
	inner := &scriptedClient{}
	client := New(inner, manager)
	ctx := telemetryctx.WithMetadata(context.Background(), telemetryctx.Metadata{RunID: "chat-root"})
	if _, err := client.Chat(ctx, llmtypes.LLMRequest{ProviderID: "test", Model: "m"}); err != nil {
		t.Fatal(err)
	}
	if !inner.called.Load() {
		t.Fatal("inner client was not called")
	}
	if _, ok := manager.Snapshot("chat-root"); ok {
		t.Fatal("completed implicit root was left active")
	}
}

func TestClientCleansImplicitRootWhenStreamConsumerStops(t *testing.T) {
	manager := runwatch.NewManager(runwatch.Policy{ScanInterval: time.Hour})
	defer manager.Close()
	client := New(&blockedStreamClient{}, manager)
	ctx, cancel := context.WithCancel(telemetryctx.WithMetadata(context.Background(), telemetryctx.Metadata{RunID: "stream-cancel-root"}))
	defer cancel()
	if _, err := client.ChatStream(ctx, llmtypes.LLMRequest{ProviderID: "test", Model: "m"}); err != nil {
		t.Fatal(err)
	}
	cancel()
	deadline := time.After(time.Second)
	for {
		if _, ok := manager.Snapshot("stream-cancel-root"); !ok {
			return
		}
		select {
		case <-deadline:
			t.Fatal("implicit root remained active after stream cancellation")
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func TestClientCleansImplicitRootWhenSilentProviderIsCancelled(t *testing.T) {
	manager := runwatch.NewManager(runwatch.Policy{ScanInterval: time.Hour})
	defer manager.Close()
	client := New(&silentStreamClient{}, manager)
	ctx, cancel := context.WithCancel(telemetryctx.WithMetadata(context.Background(), telemetryctx.Metadata{RunID: "silent-stream-root"}))
	stream, err := client.ChatStream(ctx, llmtypes.LLMRequest{ProviderID: "test", Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case _, ok := <-stream:
		if ok {
			t.Fatal("cancelled silent stream produced an event")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled silent provider stream did not close")
	}
	if _, ok := manager.Snapshot("silent-stream-root"); ok {
		t.Fatal("implicit root remained active after silent stream cancellation")
	}
}

func TestClientDoesNotCompleteBorrowedModelHandle(t *testing.T) {
	manager := runwatch.NewManager(runwatch.Policy{ScanInterval: time.Hour})
	defer manager.Close()
	ctx, root, err := manager.Start(context.Background(), runwatch.Metadata{RunID: "borrowed-root"})
	if err != nil {
		t.Fatal(err)
	}
	model, err := root.BeginOperation(runwatch.KindModel, "borrowed-model", runwatch.Policy{})
	if err != nil {
		t.Fatal(err)
	}
	client := New(&scriptedClient{}, manager)
	if _, err := client.Chat(runwatch.ContextWithHandle(ctx, model), llmtypes.LLMRequest{ProviderID: "test", Model: "m"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := model.Snapshot(); !ok {
		t.Fatal("supervised client completed a borrowed model handle")
	}
	root.Complete()
}
