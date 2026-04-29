package ctxwin

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// ─── MockCompactor ──────────────────────────────────────────────────────────

type mockCompactor struct {
	compactFn func(ctx context.Context, msgs []Message) (string, error)
	called    int
	mu        sync.Mutex
}

func (m *mockCompactor) Compact(ctx context.Context, msgs []Message) (string, error) {
	m.mu.Lock()
	m.called++
	m.mu.Unlock()
	if m.compactFn != nil {
		return m.compactFn(ctx, msgs)
	}
	return "This is a compressed summary of the conversation.", nil
}

func (m *mockCompactor) Called() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.called
}

// ─── Tests ──────────────────────────────────────────────────────────────────

func TestCompactBasic(t *testing.T) {
	mc := &mockCompactor{}
	cw := NewContextWindow(100000, 2000, 50000, NewTokenizer(), WithCompactor(mc))

	cw.Push(RoleSystem, "You are a helpful assistant.")
	cw.Push(RoleUser, "Hello")
	cw.Push(RoleAssistant, "Hi there")

	// Manually call Compact
	summary, err := mc.Compact(context.Background(), cw.messages)
	if err != nil {
		t.Fatalf("Compact failed: %v", err)
	}
	if summary == "" {
		t.Error("Compact returned empty summary")
	}
}

func TestCompactErrorDoesNotModifyCW(t *testing.T) {
	mc := &mockCompactor{
		compactFn: func(ctx context.Context, msgs []Message) (string, error) {
			return "", context.DeadlineExceeded
		},
	}
	cw := NewContextWindow(100000, 2000, 50000, NewTokenizer(), WithCompactor(mc))

	cw.Push(RoleSystem, "System")
	cw.Push(RoleUser, "Hello")

	beforeLen := cw.Len()
	beforeTokens, _, _ := cw.TokenUsage()

	// Trigger async compact — it should fail silently
	cw.asyncCompact()

	// Wait briefly for goroutine
	time.Sleep(50 * time.Millisecond)

	afterLen := cw.Len()
	afterTokens, _, _ := cw.TokenUsage()

	if beforeLen != afterLen {
		t.Errorf("Messages should not change on compact error: before=%d, after=%d", beforeLen, afterLen)
	}
	if beforeTokens != afterTokens {
		t.Errorf("Tokens should not change on compact error: before=%d, after=%d", beforeTokens, afterTokens)
	}
}

func TestAsyncCompactReducesTokens(t *testing.T) {
	mc := &mockCompactor{
		compactFn: func(ctx context.Context, msgs []Message) (string, error) {
			return "Brief summary.", nil
		},
	}
	// summaryTokens=1: any Push will trigger async compact
	cw := NewContextWindow(100000, 2000, 1, NewTokenizer(), WithCompactor(mc))

	cw.Push(RoleSystem, "System prompt for testing.")
	cw.Push(RoleUser, "Tell me a long story about programming.")
	cw.Push(RoleAssistant, "Once upon a time, there was a programmer who loved to code.")

	beforeTokens, _, _ := cw.TokenUsage()

	// Wait for async compact to complete
	time.Sleep(200 * time.Millisecond)

	afterTokens, _, _ := cw.TokenUsage()

	if afterTokens >= beforeTokens {
		t.Errorf("Tokens should decrease after async compact: before=%d, after=%d", beforeTokens, afterTokens)
	}

	// System prompt should still be at index 0
	msg, ok := cw.MessageAt(0)
	if !ok || msg.Role != RoleSystem {
		t.Error("First message should still be system prompt after compact")
	}

	// Second message should be the summary
	if cw.Len() > 1 {
		summary, _ := cw.MessageAt(1)
		if summary.Role != RoleSystem {
			t.Errorf("Second message should be system (summary), got %q", summary.Role)
		}
	}
}

func TestSoftWaterlineTriggersCompact(t *testing.T) {
	mc := &mockCompactor{
		compactFn: func(ctx context.Context, msgs []Message) (string, error) {
			return "Summary.", nil
		},
	}
	// summaryTokens=1: any message Push will exceed soft waterline
	cw := NewContextWindow(100000, 2000, 1, NewTokenizer(), WithCompactor(mc))

	cw.Push(RoleSystem, "System")
	cw.Push(RoleUser, "Hello")

	// Wait for async compact
	time.Sleep(200 * time.Millisecond)

	if mc.Called() == 0 {
		t.Error("Compactor should have been called when soft waterline was crossed")
	}
}

func TestConcurrentPushAndBuildPayload(t *testing.T) {
	cw := NewContextWindow(100000, 2000, 0, NewTokenizer())

	cw.Push(RoleSystem, "System")

	var wg sync.WaitGroup
	const writers = 10
	const readers = 10

	// Concurrent writers
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cw.Push(RoleUser, "message from writer")
			cw.Push(RoleAssistant, "response from writer")
		}(i)
	}

	// Concurrent readers
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			payload := cw.BuildPayload()
			_ = payload
			cw.TokenUsage()
			cw.Len()
		}()
	}

	wg.Wait()

	// Should not panic or deadlock
	if cw.Len() < 2 {
		t.Errorf("Expected at least 2 messages, got %d", cw.Len())
	}
}

func TestWithCompactorOption(t *testing.T) {
	mc := &mockCompactor{}
	cw := NewContextWindow(100000, 2000, 0, NewTokenizer(), WithCompactor(mc))

	if cw.compactor == nil {
		t.Error("WithCompactor should set the compactor")
	}
}

func TestSummaryTokensAutoCalculation(t *testing.T) {
	// Small capacity: 128k → 85%
	cw1 := NewContextWindow(128000, 2000, 0, NewTokenizer())
	if cw1.summaryTokens != 128000*85/100 {
		t.Errorf("Small capacity summaryTokens = %d, want %d", cw1.summaryTokens, 128000*85/100)
	}

	// Large capacity: 1M → 75%
	cw2 := NewContextWindow(1048576, 2000, 0, NewTokenizer())
	if cw2.summaryTokens != 1048576*75/100 {
		t.Errorf("Large capacity summaryTokens = %d, want %d", cw2.summaryTokens, 1048576*75/100)
	}

	// Explicit value: should not be overridden
	cw3 := NewContextWindow(128000, 2000, 50000, NewTokenizer())
	if cw3.summaryTokens != 50000 {
		t.Errorf("Explicit summaryTokens = %d, want 50000", cw3.summaryTokens)
	}
}

func TestAsyncCompactPreservesSystemPrompt(t *testing.T) {
	mc := &mockCompactor{
		compactFn: func(ctx context.Context, msgs []Message) (string, error) {
			return "Compressed.", nil
		},
	}
	cw := NewContextWindow(100000, 2000, 1, NewTokenizer(), WithCompactor(mc))

	systemContent := "You are a helpful assistant with specific instructions."
	cw.Push(RoleSystem, systemContent)
	cw.Push(RoleUser, "Hello")
	cw.Push(RoleAssistant, "Hi")

	// Wait for async compact
	time.Sleep(200 * time.Millisecond)

	// System prompt content should be unchanged
	msg, ok := cw.MessageAt(0)
	if !ok {
		t.Fatal("MessageAt(0) returned false")
	}
	if msg.Content != systemContent {
		t.Errorf("System prompt changed: got %q, want %q", msg.Content, systemContent)
	}
}

func TestAsyncCompactDeduplication(t *testing.T) {
	mc := &mockCompactor{
		compactFn: func(ctx context.Context, msgs []Message) (string, error) {
			// Simulate slow compression
			time.Sleep(100 * time.Millisecond)
			return "Summary.", nil
		},
	}
	cw := NewContextWindow(100000, 2000, 1, NewTokenizer(), WithCompactor(mc))

	cw.Push(RoleSystem, "System")
	// Multiple pushes while compact is running — only one compact should execute
	for i := 0; i < 5; i++ {
		cw.Push(RoleUser, "Message "+strings.Repeat("x", 100))
	}

	time.Sleep(500 * time.Millisecond)

	// Compactor should have been called at most 2-3 times (not 5+)
	// The first push triggers compact, subsequent pushes see summarizing=true
	called := mc.Called()
	if called > 3 {
		t.Errorf("Compactor called %d times, expected at most 3 (deduplication should prevent excessive calls)", called)
	}
}
