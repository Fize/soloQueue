package agent

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// newTestLogger 返回一个写到临时目录的 Session 层 logger
func newTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	dir := t.TempDir()
	log, err := logger.Session(dir, "test-team", "test-sess",
		logger.WithConsole(false),
		logger.WithLevel(slog.LevelDebug),
	)
	if err != nil {
		t.Fatalf("logger.Session: %v", err)
	}
	t.Cleanup(func() { _ = log.Close() })
	return log
}

func TestAgent_Run_Happy(t *testing.T) {
	a := NewAgent(
		Definition{ID: "a1", Kind: KindChat, SystemPrompt: "you are helpful"},
		&FakeLLM{Responses: []string{"hello"}},
		newTestLogger(t),
	)

	reply, err := a.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if reply != "hello" {
		t.Errorf("reply = %q, want hello", reply)
	}
}

func TestAgent_Run_NilLLM(t *testing.T) {
	a := NewAgent(Definition{ID: "a1"}, nil, nil)
	_, err := a.Run(context.Background(), "hi")
	if err == nil {
		t.Fatal("Run with nil LLM should error")
	}
}

func TestAgent_Run_LLMError_Propagated(t *testing.T) {
	myErr := errors.New("kaboom")
	a := NewAgent(
		Definition{ID: "a1", Kind: KindChat},
		&FakeLLM{Err: myErr},
		newTestLogger(t),
	)

	_, err := a.Run(context.Background(), "hi")
	if !errors.Is(err, myErr) {
		t.Errorf("err = %v, want %v", err, myErr)
	}
}

func TestAgent_Run_CtxCancel_StopsLLMCall(t *testing.T) {
	a := NewAgent(
		Definition{ID: "a1", Kind: KindChat},
		&FakeLLM{Responses: []string{"ignored"}, Delay: 5 * time.Second},
		newTestLogger(t),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := a.Run(ctx, "hi")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected ctx cancel error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want DeadlineExceeded", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("cancel did not propagate promptly: took %v", elapsed)
	}
}

func TestAgent_Run_SystemPromptIncluded(t *testing.T) {
	var seenReq LLMRequest
	var captured bool
	fake := &FakeLLM{
		Responses: []string{"ok"},
		Hook: func(req LLMRequest) {
			if !captured {
				seenReq = req
				captured = true
			}
		},
	}

	a := NewAgent(
		Definition{
			ID:           "a1",
			Kind:         KindChat,
			ModelID:      "deepseek-chat",
			SystemPrompt: "you are a poetic assistant",
			Temperature:  0.4,
			MaxTokens:    512,
		},
		fake,
		newTestLogger(t),
	)

	_, err := a.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if seenReq.Model != "deepseek-chat" {
		t.Errorf("Model = %q, want deepseek-chat", seenReq.Model)
	}
	if seenReq.Temperature != 0.4 {
		t.Errorf("Temperature = %v, want 0.4", seenReq.Temperature)
	}
	if seenReq.MaxTokens != 512 {
		t.Errorf("MaxTokens = %d, want 512", seenReq.MaxTokens)
	}
	if len(seenReq.Messages) != 2 {
		t.Fatalf("Messages len = %d, want 2", len(seenReq.Messages))
	}
	if seenReq.Messages[0].Role != "system" || seenReq.Messages[0].Content != "you are a poetic assistant" {
		t.Errorf("messages[0] = %+v", seenReq.Messages[0])
	}
	if seenReq.Messages[1].Role != "user" || seenReq.Messages[1].Content != "hello" {
		t.Errorf("messages[1] = %+v", seenReq.Messages[1])
	}
}

func TestAgent_Run_NoSystemPrompt_OmittedFromMessages(t *testing.T) {
	var seenReq LLMRequest
	fake := &FakeLLM{
		Responses: []string{"ok"},
		Hook:      func(req LLMRequest) { seenReq = req },
	}

	a := NewAgent(Definition{ID: "a1"}, fake, nil)
	_, _ = a.Run(context.Background(), "hi")

	if len(seenReq.Messages) != 1 {
		t.Fatalf("Messages len = %d, want 1 (system prompt should be omitted)", len(seenReq.Messages))
	}
	if seenReq.Messages[0].Role != "user" {
		t.Errorf("sole message should be user, got %q", seenReq.Messages[0].Role)
	}
}

func TestAgent_Run_NilLogger_LLMError_NoPanic(t *testing.T) {
	// 确保 nil logger 下 LLM error 路径也不 panic
	a := NewAgent(
		Definition{ID: "a1"},
		&FakeLLM{Err: errors.New("boom")},
		nil,
	)
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("nil logger + LLM error panicked: %v", r)
		}
	}()
	_, err := a.Run(context.Background(), "hi")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAgent_Run_NilLogger_NoPanic(t *testing.T) {
	a := NewAgent(
		Definition{ID: "a1"},
		&FakeLLM{Responses: []string{"ok"}},
		nil, // nil logger
	)
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("nil logger caused panic: %v", r)
		}
	}()
	reply, err := a.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if reply != "ok" {
		t.Errorf("reply = %q", reply)
	}
}

func TestAgent_Run_Concurrent(t *testing.T) {
	// 验证：Agent 本身无状态，Run 可并发
	a := NewAgent(
		Definition{ID: "a1"},
		&FakeLLM{Responses: []string{"r1", "r2", "r3"}},
		newTestLogger(t),
	)

	const N = 100
	var wg sync.WaitGroup
	errs := make([]error, N)
	replies := make([]string, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, err := a.Run(context.Background(), "hi")
			errs[i] = err
			replies[i] = r
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: %v", i, err)
		}
		if replies[i] == "" {
			t.Errorf("goroutine %d: empty reply", i)
		}
	}
}

func TestAgent_Run_LogsLLMCategory(t *testing.T) {
	dir := t.TempDir()
	log, err := logger.Session(dir, "team", "sess", logger.WithConsole(false))
	if err != nil {
		t.Fatalf("logger.Session: %v", err)
	}
	defer log.Close()

	a := NewAgent(
		Definition{ID: "a1", Kind: KindChat},
		&FakeLLM{Responses: []string{"hi"}},
		log.Child(slog.String("actor_id", "a1")),
	)
	_, err = a.Run(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	_ = log.Close() // flush

	// 日志文件应在 logs/sessions/team/sess/llm.jsonl
	path := filepath.Join(dir, "logs", "sessions", "team", "sess", "llm.jsonl")
	found, err := checkFileHasCategory(path, "llm")
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !found {
		t.Errorf("expected 'llm' category in log file %s", path)
	}
}

// checkFileHasCategory 轻量检查日志文件中出现指定 category
func checkFileHasCategory(path, cat string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	needle := []byte(`"category":"` + cat + `"`)
	return bytes.Contains(data, needle), nil
}
