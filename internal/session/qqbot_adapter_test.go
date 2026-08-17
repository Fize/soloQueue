package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/agent/agenttest"
	"github.com/xiaobaitu/soloqueue/internal/agenttools/tools"
	"github.com/xiaobaitu/soloqueue/internal/channel"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/memory/timeline"
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

func TestSessionAskAdapterPersistsFilesContextOnUserMessage(t *testing.T) {
	testLog := newTestLog(t)
	fake := &agenttest.FakeLLM{Responses: []string{"final-response"}}
	pushed := make(chan ctxwin.Message, 4)
	factory := func(ctx context.Context, teamID string) (*agent.Agent, *ctxwin.ContextWindow, *timeline.Writer, error) {
		a := agent.NewAgent(agent.Definition{ID: "agent-" + teamID}, fake, nil)
		if err := a.Start(ctx); err != nil {
			return nil, nil, nil, err
		}
		cw := ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer(), ctxwin.WithPushHook(func(msg ctxwin.Message) {
			pushed <- msg
		}))
		return a, cw, nil, nil
	}
	mgr := NewSessionManager(factory, testLog)
	t.Cleanup(func() { mgr.Shutdown(time.Second) })
	if _, err := mgr.Init(context.Background(), "team-a"); err != nil {
		t.Fatalf("Init: %v", err)
	}

	adapter := NewQQBotAdapter(mgr, testLog)
	ctx := channel.ContextWithChatMeta(context.Background(), channel.ChatMeta{Channel: "qq", AccountID: "bot", ConversationID: "chat", UserID: "user"})
	ctx = context.WithValue(ctx, ctxwin.FilesContextKey, []ctxwin.FileAttachment{{Name: "image.jpg", Path: "/tmp/image.jpg"}})
	if _, err := adapter.AskStream(ctx, "describe", nil); err != nil {
		t.Fatalf("AskStream: %v", err)
	}

	close(pushed)
	for msg := range pushed {
		if msg.Role == ctxwin.RoleUser && len(msg.Files) == 1 && msg.Files[0].Path == "/tmp/image.jpg" {
			return
		}
	}
	t.Fatal("user file metadata missing from timeline push hook")
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
	if len(result.MediaList) > 0 && result.MediaList[0].Kind != channel.MediaImage {
		t.Errorf("Kind = %q, want image", result.MediaList[0].Kind)
	}
	if len(result.MediaList) > 0 && result.MediaList[0].Path != "/tmp/test.png" {
		t.Errorf("Path = %q, want exported path", result.MediaList[0].Path)
	}
}

func TestChannelTurnDoesNotEnterRouteLessPendingQueue(t *testing.T) {
	testLog := newTestLog(t)
	sess := &Session{logger: testLog, pending: &PendingQueue{}}
	sess.inFlight.Store(1)
	base := &channelAdapterBase{log: testLog}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	_, _, err := base.askChannelStream(ctx, sess, "from-qq")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if got := sess.pending.Len(); got != 0 {
		t.Fatalf("route-less pending length = %d, want 0", got)
	}
}

func TestSameChannelRoutePreservesPendingMerge(t *testing.T) {
	testLog := newTestLog(t)
	sess := &Session{logger: testLog, pending: &PendingQueue{}, channelRouteKey: "qq\x00bot\x00chat\x00user", channelRouteOwners: 1}
	sess.inFlight.Store(1)
	base := &channelAdapterBase{log: testLog}
	ctx := channel.ContextWithChatMeta(context.Background(), channel.ChatMeta{Channel: "qq", AccountID: "bot", ConversationID: "chat", UserID: "user"})
	_, _, err := base.askChannelStream(ctx, sess, "follow-up")
	if !errors.Is(err, ErrQueued) {
		t.Fatalf("err = %v", err)
	}
	if got := sess.pending.Len(); got != 1 {
		t.Fatalf("pending length = %d, want 1", got)
	}
}

func TestSameChannelRouteMediaWaitsWithoutPendingMerge(t *testing.T) {
	testLog := newTestLog(t)
	sess := &Session{logger: testLog, pending: &PendingQueue{}, channelRouteKey: "qq\x00bot\x00chat\x00user", channelRouteOwners: 1}
	sess.inFlight.Store(1)
	base := &channelAdapterBase{log: testLog}
	ctx := channel.ContextWithChatMeta(context.Background(), channel.ChatMeta{Channel: "qq", AccountID: "bot", ConversationID: "chat", UserID: "user"})
	ctx = context.WithValue(ctx, ctxwin.FilesContextKey, []ctxwin.FileAttachment{{Name: "image.jpg", Path: "/tmp/image.jpg"}})
	ctx, cancel := context.WithTimeout(ctx, 60*time.Millisecond)
	defer cancel()

	_, _, err := base.askChannelStream(ctx, sess, "media follow-up")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if got := sess.pending.Len(); got != 0 {
		t.Fatalf("pending length = %d, want 0", got)
	}
}

type channelBlockingTarget struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (t *channelBlockingTarget) Ask(ctx context.Context, prompt string) (string, error) {
	t.once.Do(func() { close(t.started) })
	select {
	case <-t.release:
		return "qq delegation result", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (t *channelBlockingTarget) AskStream(ctx context.Context, prompt string) (<-chan iface.AgentEvent, error) {
	out := make(chan iface.AgentEvent, 2)
	go func() {
		defer close(out)
		result, err := t.Ask(ctx, prompt)
		if err != nil {
			out <- agent.ErrorEvent{Err: err}
			return
		}
		out <- agent.ContentDeltaEvent{Delta: result}
		out <- agent.DoneEvent{Content: result}
	}()
	return out, nil
}

func (t *channelBlockingTarget) Confirm(callID, choice string) error { return nil }
func (t *channelBlockingTarget) ErrorCount() int32                   { return 0 }
func (t *channelBlockingTarget) LastError() string                   { return "" }

type channelDelegationTool struct {
	target iface.Locatable
}

func (t *channelDelegationTool) Name() string        { return "delegate" }
func (t *channelDelegationTool) Description() string { return "delegate a blocked channel test task" }
func (t *channelDelegationTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (t *channelDelegationTool) Execute(ctx context.Context, args string) (string, error) {
	return "", nil
}
func (t *channelDelegationTool) ExecuteAsync(ctx context.Context, args string) (*tools.AsyncAction, error) {
	return &tools.AsyncAction{Target: t.target, Prompt: "blocked channel task", Timeout: 5 * time.Second}, nil
}

func TestL1ChannelAllowsWechatWhileQQDelegatesWithoutCrossingResponses(t *testing.T) {
	testLog := newTestLog(t)
	target := &channelBlockingTarget{started: make(chan struct{}), release: make(chan struct{})}
	fakeLLM := &agenttest.FakeLLM{
		ToolCallDeltasByTurn: [][]llm.ToolCallDelta{{{
			Index:     0,
			ID:        "call-delegate",
			Name:      "delegate",
			Arguments: `{"task":"blocked"}`,
		}}},
		StreamDeltas: [][]string{nil, {"wechat response"}, {"qq response"}},
	}
	a := agent.NewAgent(
		agent.Definition{ID: "channel-l1", Name: "Channel L1"},
		fakeLLM,
		testLog,
		agent.WithTools(&channelDelegationTool{target: target}),
		agent.WithPriorityMailbox(),
	)
	if err := a.Start(context.Background()); err != nil {
		t.Fatalf("start agent: %v", err)
	}
	cw := ctxwin.NewContextWindow(1048576, 2000, 0, ctxwin.NewTokenizer())
	mgr := NewSessionManager(func(context.Context, string) (*agent.Agent, *ctxwin.ContextWindow, *timeline.Writer, error) {
		return a, cw, nil, nil
	}, testLog)
	if _, err := mgr.Init(context.Background(), "default"); err != nil {
		t.Fatalf("init manager: %v", err)
	}
	t.Cleanup(func() { mgr.Shutdown(2 * time.Second) })
	var releaseOnce sync.Once
	releaseDelegation := func() { releaseOnce.Do(func() { close(target.release) }) }
	t.Cleanup(releaseDelegation)

	adapter := NewChannelAdapter(mgr, testLog)
	qqCtx := channel.ContextWithChatMeta(context.Background(), channel.ChatMeta{
		Channel: "qq", AccountID: "qq-bot", ConversationID: "qq-chat", UserID: "qq-user",
	})
	wechatBase := channel.ContextWithChatMeta(context.Background(), channel.ChatMeta{
		Channel: "wechat", AccountID: "wechat-bot", ConversationID: "wechat-chat", UserID: "wechat-user",
	})

	type callResult struct {
		result *channel.AskStreamResult
		err    error
	}
	qqDone := make(chan callResult, 1)
	go func() {
		result, err := adapter.AskStream(qqCtx, "delegate from qq", nil)
		qqDone <- callResult{result: result, err: err}
	}()

	select {
	case <-target.started:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for QQ delegation to start")
	}

	wechatCtx, cancelWechat := context.WithTimeout(wechatBase, 500*time.Millisecond)
	defer cancelWechat()
	wechatResult, err := adapter.AskStream(wechatCtx, "answer from wechat", nil)
	if err != nil {
		t.Fatalf("WeChat request did not enter L1 during QQ delegation: %v", err)
	}
	if wechatResult == nil || wechatResult.Content != "wechat response" {
		t.Fatalf("WeChat result = %#v, want isolated wechat response", wechatResult)
	}

	releaseDelegation()
	select {
	case qqResult := <-qqDone:
		if qqResult.err != nil {
			t.Fatalf("QQ request failed: %v", qqResult.err)
		}
		if qqResult.result == nil || qqResult.result.Content != "qq response" {
			t.Fatalf("QQ result = %#v, want isolated qq response", qqResult.result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for QQ response after delegation release")
	}
}
