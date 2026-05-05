package agent

import (
	"context"

	"github.com/xiaobaitu/soloqueue/internal/iface"
)

// --- Shared test mocks ---

// mockLocatable implements iface.Locatable for testing.
// Supports both blocking Ask and streaming AskStream via callbacks.
type mockLocatable struct {
	askFunc       func(ctx context.Context, prompt string) (string, error)
	askStreamFunc func(ctx context.Context, prompt string) (<-chan iface.AgentEvent, error)
}

func (m *mockLocatable) Ask(ctx context.Context, prompt string) (string, error) {
	if m.askFunc != nil {
		return m.askFunc(ctx, prompt)
	}
	return "mock-result", nil
}

func (m *mockLocatable) AskStream(ctx context.Context, prompt string) (<-chan iface.AgentEvent, error) {
	if m.askStreamFunc != nil {
		return m.askStreamFunc(ctx, prompt)
	}
	ch := make(chan iface.AgentEvent, 1)
	go func() {
		defer close(ch)
		result, err := m.Ask(ctx, prompt)
		if err != nil {
			ch <- ErrorEvent{Err: err}
		} else {
			ch <- DoneEvent{Content: result}
		}
	}()
	return ch, nil
}

func (m *mockLocatable) Confirm(callID string, choice string) error {
	return nil
}

func (m *mockLocatable) ErrorCount() int32 { return 0 }
func (m *mockLocatable) LastError() string { return "" }
