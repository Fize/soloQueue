package session

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/agent/agenttest"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

// newTestLog creates a silent logger for adapter tests.
func newTestLog(t *testing.T) *logger.Logger {
	t.Helper()
	dir := t.TempDir()
	l, err := logger.System(dir, logger.WithConsole(false), logger.WithFile(false))
	if err != nil {
		t.Fatalf("create test logger: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	return l
}

func TestErrorQQBotAdapter(t *testing.T) {
	errMsg := "Test error message"
	adapter := NewErrorQQBotAdapter(errMsg)

	res, err := adapter.AskStream(context.Background(), "hello", nil)
	if err != nil {
		t.Fatalf("unexpected error in AskStream: %v", err)
	}
	if res.Content != errMsg {
		t.Errorf("expected Content to be %q, got %q", errMsg, res.Content)
	}

	if err := adapter.CancelCurrent("test"); err != nil {
		t.Errorf("unexpected error in CancelCurrent: %v", err)
	}
	if err := adapter.Clear(context.Background()); err != nil {
		t.Errorf("unexpected error in Clear: %v", err)
	}

	_, err = adapter.SaveUploadedFile(context.Background(), "test.txt", []byte("hello"))
	if err == nil {
		t.Fatal("expected error in SaveUploadedFile, got nil")
	}
	if err.Error() != errMsg {
		t.Errorf("expected error %q, got %q", errMsg, err.Error())
	}
}

func TestSessionAskAdapter_NoSessionErrors(t *testing.T) {
	testLog := newTestLog(t)
	fake := &agenttest.FakeLLM{Responses: []string{"r"}}
	mgr := NewSessionManager(factoryFromFake(t, fake), testLog)
	t.Cleanup(func() { mgr.Shutdown(time.Second) })

	adapter := NewQQBotAdapter(mgr, testLog)

	if err := adapter.CancelCurrent("reason"); err == nil {
		t.Error("CancelCurrent with no session should return error")
	}
	if err := adapter.Clear(context.Background()); err == nil {
		t.Error("Clear with no session should return error")
	}
	if _, err := adapter.AskStream(context.Background(), "hi", nil); err == nil {
		t.Error("AskStream with no session should return error")
	}
	_, err := adapter.SaveUploadedFile(context.Background(), "f.txt", []byte("x"))
	if err == nil {
		t.Error("SaveUploadedFile with no session should return error")
	}
}

func TestSessionAskAdapter_WithSession(t *testing.T) {
	testLog := newTestLog(t)
	fake := &agenttest.FakeLLM{Responses: []string{"ok"}}
	mgr := NewSessionManager(factoryFromFake(t, fake), testLog)
	t.Cleanup(func() { mgr.Shutdown(time.Second) })

	_, err := mgr.Init(context.Background(), "team-a")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	adapter := NewQQBotAdapter(mgr, testLog)
	if err := adapter.Clear(context.Background()); err != nil {
		t.Errorf("Clear: %v", err)
	}
	if err := adapter.CancelCurrent("cancel-reason"); err != nil {
		t.Errorf("CancelCurrent: %v", err)
	}
}

func TestSessionAskAdapter_AskStreamWithSession(t *testing.T) {
	testLog := newTestLog(t)
	fake := &agenttest.FakeLLM{Responses: []string{"final-response"}}
	mgr := NewSessionManager(factoryFromFake(t, fake), testLog)
	t.Cleanup(func() { mgr.Shutdown(time.Second) })

	_, err := mgr.Init(context.Background(), "team-a")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	adapter := NewQQBotAdapter(mgr, testLog)
	result, err := adapter.AskStream(context.Background(), "hello", func(ctx context.Context, intermediate string) {
		_ = intermediate
	})
	if err != nil {
		t.Fatalf("AskStream: %v", err)
	}
	if result == nil {
		t.Fatal("AskStream returned nil result")
	}
}

func TestSessionAskAdapter_SaveUploadedFile(t *testing.T) {
	testLog := newTestLog(t)
	fake := &agenttest.FakeLLM{Responses: []string{"r"}}
	mgr := NewSessionManager(factoryFromFake(t, fake), testLog)
	t.Cleanup(func() { mgr.Shutdown(time.Second) })

	_, err := mgr.Init(context.Background(), "team-a")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	adapter := NewQQBotAdapter(mgr, testLog)
	path, err := adapter.SaveUploadedFile(context.Background(), "hello.txt", []byte("world"))
	if err != nil {
		t.Fatalf("SaveUploadedFile: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}
	if string(data) != "world" {
		t.Errorf("file content = %q, want world", string(data))
	}
}

func TestConsumeAskStreamEvents_Basic(t *testing.T) {
	testLog := newTestLog(t)
	base := &channelAdapterBase{log: testLog}
	ag := startAgent(t, &agenttest.FakeLLM{Responses: []string{"ok"}})
	sess := &Session{Agent: ag, logger: testLog}
	t.Cleanup(func() { sess.Close() })

	ch := make(chan iface.AgentEvent, 4)
	ch <- agent.ContentDeltaEvent{Delta: "hello"}
	ch <- agent.ContentDeltaEvent{Delta: " world"}
	ch <- agent.DoneEvent{}
	close(ch)

	result, err := base.consumeAskStreamEvents(context.Background(), sess, ch, nil)
	if err != nil {
		t.Fatalf("consumeAskStreamEvents: %v", err)
	}
	if result.Content != "hello world" {
		t.Errorf("Content = %q, want 'hello world'", result.Content)
	}
}

func TestConsumeAskStreamEvents_Error(t *testing.T) {
	testLog := newTestLog(t)
	base := &channelAdapterBase{log: testLog}
	ag := startAgent(t, &agenttest.FakeLLM{Responses: []string{"ok"}})
	sess := &Session{Agent: ag, logger: testLog}
	t.Cleanup(func() { sess.Close() })

	ch := make(chan iface.AgentEvent, 2)
	ch <- agent.ErrorEvent{Err: &llm.APIError{StatusCode: 500, Message: "boom"}}
	close(ch)

	_, err := base.consumeAskStreamEvents(context.Background(), sess, ch, nil)
	if err == nil {
		t.Error("expected error from ErrorEvent, got nil")
	}
}

func TestConsumeAskStreamEvents_IntermediateCallback(t *testing.T) {
	testLog := newTestLog(t)
	base := &channelAdapterBase{log: testLog}
	ag := startAgent(t, &agenttest.FakeLLM{Responses: []string{"ok"}})
	sess := &Session{Agent: ag, logger: testLog}
	t.Cleanup(func() { sess.Close() })

	var intermediates []string
	onInt := func(ctx context.Context, s string) {
		intermediates = append(intermediates, s)
	}

	ch := make(chan iface.AgentEvent, 5)
	ch <- agent.ContentDeltaEvent{Delta: "thinking"}
	ch <- agent.ToolExecStartEvent{Name: "ReadFile", CallID: "c1"}
	ch <- agent.ContentDeltaEvent{Delta: "result"}
	ch <- agent.DoneEvent{}
	close(ch)

	result, err := base.consumeAskStreamEvents(context.Background(), sess, ch, onInt)
	if err != nil {
		t.Fatalf("consumeAskStreamEvents: %v", err)
	}
	if len(intermediates) != 1 {
		t.Errorf("expected 1 intermediate, got %d", len(intermediates))
	}
	if len(intermediates) > 0 && intermediates[0] != "thinking" {
		t.Errorf("intermediate = %q, want 'thinking'", intermediates[0])
	}
	if result.Content != "result" {
		t.Errorf("final Content = %q, want 'result'", result.Content)
	}
}

func TestConsumeAskStreamEvents_EmptyContentUsesReasoning(t *testing.T) {
	testLog := newTestLog(t)
	base := &channelAdapterBase{log: testLog}
	ag := startAgent(t, &agenttest.FakeLLM{Responses: []string{"ok"}})
	sess := &Session{Agent: ag, logger: testLog}
	t.Cleanup(func() { sess.Close() })

	ch := make(chan iface.AgentEvent, 2)
	ch <- agent.DoneEvent{ReasoningContent: "reasoning text"}
	close(ch)

	result, err := base.consumeAskStreamEvents(context.Background(), sess, ch, nil)
	if err != nil {
		t.Fatalf("consumeAskStreamEvents: %v", err)
	}
	if result.Content != "reasoning text" {
		t.Errorf("Content = %q, want 'reasoning text'", result.Content)
	}
}

func TestSupervisorsFn_SetAndReap(t *testing.T) {
	testLog := newTestLog(t)
	base := &channelAdapterBase{log: testLog}
	called := false
	base.SetSupervisorsFn(func() []*agent.Supervisor {
		called = true
		return nil
	})
	base.reapSupervisorChildren("test")
	if !called {
		t.Error("supervisorsFn was not called")
	}
}

func TestConsumeAskStreamEvents_ImageGenResult(t *testing.T) {
	testLog := newTestLog(t)
	base := &channelAdapterBase{log: testLog}
	ag := startAgent(t, &agenttest.FakeLLM{Responses: []string{"ok"}})
	sess := &Session{Agent: ag, logger: testLog}
	t.Cleanup(func() { sess.Close() })

	ch := make(chan iface.AgentEvent, 3)
	ch <- agent.ToolExecDoneEvent{
		Name:   "ImageGenerate",
		Result: `{"status":"completed","image_urls":["https://example.com/img.png"]}`,
	}
	ch <- agent.ToolExecDoneEvent{
		Name:   "ImageEdit",
		Result: `{"status":"completed","image_urls":["https://example.com/edit.png"]}`,
	}
	ch <- agent.DoneEvent{}
	close(ch)

	result, err := base.consumeAskStreamEvents(context.Background(), sess, ch, nil)
	if err != nil {
		t.Fatalf("consumeAskStreamEvents: %v", err)
	}
	if len(result.ImageURLs) != 2 {
		t.Errorf("expected 2 image URLs, got %d: %v", len(result.ImageURLs), result.ImageURLs)
	}
	if len(result.MediaList) != 2 {
		t.Errorf("expected 2 media items, got %d", len(result.MediaList))
	}
}

func TestConsumeAskStreamEvents_SendFileResult(t *testing.T) {
	testLog := newTestLog(t)
	base := &channelAdapterBase{log: testLog}
	ag := startAgent(t, &agenttest.FakeLLM{Responses: []string{"ok"}})
	sess := &Session{Agent: ag, logger: testLog}
	t.Cleanup(func() { sess.Close() })

	ch := make(chan iface.AgentEvent, 2)
	ch <- agent.ToolExecDoneEvent{
		Name:   "SendFile",
		Result: `{"status":"success","file_type":"image","file_name":"test.png","url":"https://example.com/test.png","path":"/tmp/test.png"}`,
	}
	ch <- agent.DoneEvent{}
	close(ch)

	result, err := base.consumeAskStreamEvents(context.Background(), sess, ch, nil)
	if err != nil {
		t.Fatalf("consumeAskStreamEvents: %v", err)
	}
	if len(result.MediaList) != 1 {
		t.Errorf("expected 1 media item, got %d", len(result.MediaList))
	}
	if len(result.MediaList) > 0 && result.MediaList[0].FileType != 1 {
		t.Errorf("FileType = %d, want 1 (image)", result.MediaList[0].FileType)
	}
}
