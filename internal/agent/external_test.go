package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/tools"
)

type mockExternalBackend struct {
	executeFunc func(ctx context.Context, prompt string, opts externalExecOptions) (*externalSession, error)
}

func (m *mockExternalBackend) Execute(ctx context.Context, prompt string, opts externalExecOptions) (*externalSession, error) {
	if m.executeFunc != nil {
		return m.executeFunc(ctx, prompt, opts)
	}
	msgCh := make(chan externalMessage, 10)
	resCh := make(chan externalResult, 1)
	go func() {
		msgCh <- externalMessage{Type: externalMessageText, Content: "Hello from mock external!"}
		resCh <- externalResult{Status: "completed", Output: "Hello from mock external!"}
		close(msgCh)
		close(resCh)
	}()
	return &externalSession{Messages: msgCh, Result: resCh}, nil
}

func TestExternalAgent_CreationValidation(t *testing.T) {
	registry := NewRegistry(nil)
	fakeLLM := &FakeLLM{}
	f := NewDefaultFactory(registry, fakeLLM, tools.Config{}, nil)

	// L1 (not leader, no group) configured with ExternalType should fail
	tmplL1 := AgentTemplate{
		ID:           "l1-external",
		Name:         "L1 External",
		ExternalType: "claude",
		IsLeader:     false,
		Group:        "",
	}
	_, _, err := f.Create(context.Background(), tmplL1, "")
	if err == nil {
		t.Error("expected error when creating L1 external agent, got nil")
	} else if !strings.Contains(err.Error(), "only allowed for L2 leaders or L3 workers") {
		t.Errorf("unexpected error message: %v", err)
	}

	// L2 (leader) configured with ExternalType should succeed
	tmplL2 := AgentTemplate{
		ID:           "l2-external",
		Name:         "L2 External",
		ExternalType: "claude",
		IsLeader:     true,
		Group:        "",
	}
	a, _, err := f.Create(context.Background(), tmplL2, "")
	if err != nil {
		t.Fatalf("unexpected error creating L2 external agent: %v", err)
	}
	defer func() { _ = a.Stop(time.Second) }()
	if a.Def.Kind != KindExternal {
		t.Errorf("expected Kind to be KindExternal, got %s", a.Def.Kind)
	}
	if a.Def.ExternalType != "claude" {
		t.Errorf("expected ExternalType to be claude, got %s", a.Def.ExternalType)
	}

	// L3 (worker in group) configured with ExternalType should succeed
	tmplL3 := AgentTemplate{
		ID:           "l3-external",
		Name:         "L3 External",
		ExternalType: "claude",
		IsLeader:     false,
		Group:        "worker-group",
	}
	a3, _, err := f.Create(context.Background(), tmplL3, "")
	if err != nil {
		t.Fatalf("unexpected error creating L3 external agent: %v", err)
	}
	defer func() { _ = a3.Stop(time.Second) }()
	if a3.Def.Kind != KindExternal {
		t.Errorf("expected Kind to be KindExternal, got %s", a3.Def.Kind)
	}

	// Test external model resolution bypass:
	// If a resolver is set, it would normally throw an error on unknown models
	// or substitute defaults. For external agents, it should bypass resolution entirely.
	resolverCalled := false
	fWithResolver := NewDefaultFactory(registry, fakeLLM, tools.Config{}, nil,
		WithModelResolver(func(modelID string) (ModelInfo, error) {
			resolverCalled = true
			return ModelInfo{}, fmt.Errorf("should not be called")
		}),
	)

	// Case A: Model specified explicitly
	tmplWithModel := AgentTemplate{
		ID:           "l2-with-model",
		Name:         "L2 With Model",
		ExternalType: "claude",
		IsLeader:     true,
		ModelID:      "claude-3-5-sonnet-latest",
	}
	aWithModel, _, err := fWithResolver.Create(context.Background(), tmplWithModel, "")
	if err != nil {
		t.Fatalf("failed to create L2 external agent with explicit model: %v", err)
	}
	defer func() { _ = aWithModel.Stop(time.Second) }()
	if aWithModel.Def.ModelID != "claude-3-5-sonnet-latest" {
		t.Errorf("expected model ID to be preserved, got %q", aWithModel.Def.ModelID)
	}
	if resolverCalled {
		t.Error("model resolver should not be called for external agent")
	}

	// Case B: Model is empty (should stay empty, not resolve to default system model)
	tmplEmptyModel := AgentTemplate{
		ID:           "l2-empty-model",
		Name:         "L2 Empty Model",
		ExternalType: "claude",
		IsLeader:     true,
		ModelID:      "",
	}
	aEmptyModel, _, err := fWithResolver.Create(context.Background(), tmplEmptyModel, "")
	if err != nil {
		t.Fatalf("failed to create L2 external agent with empty model: %v", err)
	}
	defer func() { _ = aEmptyModel.Stop(time.Second) }()
	if aEmptyModel.Def.ModelID != "" {
		t.Errorf("expected model ID to remain empty, got %q", aEmptyModel.Def.ModelID)
	}
}

func TestExternalAgent_ExecutionLoop(t *testing.T) {
	// Setup test backend
	mockBackend := &mockExternalBackend{
		executeFunc: func(ctx context.Context, prompt string, opts externalExecOptions) (*externalSession, error) {
			msgCh := make(chan externalMessage, 10)
			resCh := make(chan externalResult, 1)

			go func() {
				msgCh <- externalMessage{Type: externalMessageThinking, Content: "Thinking..."}
				time.Sleep(10 * time.Millisecond)
				msgCh <- externalMessage{Type: externalMessageText, Content: "Response text"}
				resCh <- externalResult{
					Status:    "completed",
					Output:    "Response text",
					SessionID: "session-12345",
				}
				close(msgCh)
				close(resCh)
			}()

			return &externalSession{Messages: msgCh, Result: resCh}, nil
		},
	}

	testExternalBackend = mockBackend
	defer func() { testExternalBackend = nil }()

	// Create and start external agent
	def := Definition{
		ID:           "test-external",
		Kind:         KindExternal,
		ExternalType: "mock",
	}
	a := NewAgent(def, nil, newTestLogger(t))
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("failed to start agent: %v", err)
	}
	defer func() { _ = a.Stop(time.Second) }()

	// Ask the agent (Ask will run the stream and wait for finish)
	content, err := a.Ask(context.Background(), "do something")
	if err != nil {
		t.Fatalf("Ask failed: %v", err)
	}

	if content != "Response text" {
		t.Errorf("unexpected content response: %q", content)
	}

	if a.externalSessionID != "session-12345" {
		t.Errorf("expected externalSessionID to be updated to session-12345, got %q", a.externalSessionID)
	}
}

func TestExternalAgent_ExecutionLoop_Error(t *testing.T) {
	// Setup test backend that reports error
	mockBackend := &mockExternalBackend{
		executeFunc: func(ctx context.Context, prompt string, opts externalExecOptions) (*externalSession, error) {
			msgCh := make(chan externalMessage, 10)
			resCh := make(chan externalResult, 1)

			go func() {
				msgCh <- externalMessage{Type: externalMessageError, Content: "something went wrong"}
				resCh <- externalResult{
					Status:    "failed",
					Error:     "something went wrong",
					SessionID: "session-54321",
				}
				close(msgCh)
				close(resCh)
			}()

			return &externalSession{Messages: msgCh, Result: resCh}, nil
		},
	}

	testExternalBackend = mockBackend
	defer func() { testExternalBackend = nil }()

	def := Definition{
		ID:           "test-external-err",
		Kind:         KindExternal,
		ExternalType: "mock",
	}
	a := NewAgent(def, nil, newTestLogger(t))
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("failed to start agent: %v", err)
	}
	defer func() { _ = a.Stop(time.Second) }()

	_, err := a.Ask(context.Background(), "error prompt")
	if err == nil {
		t.Error("expected error, got nil")
	} else if !strings.Contains(err.Error(), "something went wrong") {
		t.Errorf("unexpected error: %v", err)
	}

	// Session ID should still be captured
	if a.externalSessionID != "session-54321" {
		t.Errorf("expected externalSessionID to be session-54321, got %q", a.externalSessionID)
	}
}
