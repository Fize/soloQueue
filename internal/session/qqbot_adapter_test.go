package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync"
	"sync/atomic"
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
	"github.com/xiaobaitu/soloqueue/internal/runwatch"
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

func TestSessionAskAdapter_CancelCurrentReturnsTaskCancelledWithoutLeaking(t *testing.T) {
	testLog := newTestLog(t)
	fake := &agenttest.FakeLLM{}
	mgr := NewSessionManager(factoryFromFake(t, fake), testLog)
	t.Cleanup(func() { mgr.Shutdown(time.Second) })
	sess, err := mgr.Init(context.Background(), "team-a")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	started := make(chan struct{})
	sess.askStreamHistory = func(ctx context.Context, _ *ctxwin.ContextWindow, _ string) (<-chan agent.AgentEvent, error) {
		source := make(chan agent.AgentEvent, 1)
		close(started)
		go func() {
			<-ctx.Done()
			source <- agent.ErrorEvent{Err: context.Cause(ctx)}
			close(source)
		}()
		return source, nil
	}

	adapter := NewQQBotAdapter(mgr, testLog)
	result := make(chan error, 1)
	go func() {
		_, askErr := adapter.AskStream(context.Background(), "cancel me", nil)
		result <- askErr
	}()
	<-started
	if err := adapter.CancelCurrent("user requested cancellation"); err != nil {
		t.Fatalf("CancelCurrent: %v", err)
	}
	if err := <-result; !errors.Is(err, channel.ErrTaskCancelled) {
		t.Fatalf("AskStream cancellation error = %v, want ErrTaskCancelled", err)
	}

	sess.askStreamHistory = func(context.Context, *ctxwin.ContextWindow, string) (<-chan agent.AgentEvent, error) {
		source := make(chan agent.AgentEvent, 2)
		source <- agent.ContentDeltaEvent{Delta: "next run"}
		source <- agent.DoneEvent{Content: "next run"}
		close(source)
		return source, nil
	}
	next, err := adapter.AskStream(context.Background(), "next", nil)
	if err != nil {
		t.Fatalf("next AskStream inherited cancellation: %v", err)
	}
	if next.Content != "next run" {
		t.Fatalf("next content = %q, want next run", next.Content)
	}
}

func TestChannelCancelTargetsOnlyLatestOwnedRunTree(t *testing.T) {
	testLog := newTestLog(t)
	watchdog := runwatch.NewManager(runwatch.Policy{ScanInterval: time.Hour, RootIdle: time.Hour})
	defer watchdog.Close()
	ctxA, rootA, err := watchdog.Start(context.Background(), runwatch.Metadata{RunID: "web-root"})
	if err != nil {
		t.Fatal(err)
	}
	childA, err := rootA.BeginOperation(runwatch.KindDelegation, "web-child", runwatch.Policy{})
	if err != nil {
		t.Fatal(err)
	}
	ctxB, rootB, err := watchdog.Start(context.Background(), runwatch.Metadata{RunID: "channel-root"})
	if err != nil {
		t.Fatal(err)
	}
	childB, err := rootB.BeginOperation(runwatch.KindDelegation, "channel-child", runwatch.Policy{})
	if err != nil {
		t.Fatal(err)
	}
	s := &Session{activeCancels: make(map[string]activeTurnCancel), logger: testLog}
	s.registerActiveCancel("web-root", func(cause error) {
		rootA.Fail(cause)
		go s.unregisterActiveCancel("web-root")
	})
	s.registerActiveCancel("channel-root", func(cause error) {
		rootB.Fail(cause)
		go s.unregisterActiveCancel("channel-root")
	})
	base := &channelAdapterBase{log: testLog}
	base.trackChannelRun("channel-root")
	if err := base.cancelCurrent(s, "channel cancel"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctxB.Done():
	case <-time.After(time.Second):
		t.Fatal("owned channel root was not cancelled")
	}
	if runwatch.CodeOf(context.Cause(ctxB)) != runwatch.CodeCancelledByUser {
		t.Fatalf("channel root cause = %v", context.Cause(ctxB))
	}
	if snapshot, ok := watchdog.Snapshot("channel-root"); !ok || snapshot.TerminalCode != runwatch.CodeCancelledByUser {
		t.Fatalf("channel subtree terminal snapshot = %+v ok=%v", snapshot, ok)
	}
	select {
	case <-ctxA.Done():
		t.Fatalf("unowned concurrent root was cancelled: %v", context.Cause(ctxA))
	default:
	}
	if snapshot, ok := watchdog.Snapshot("web-root"); !ok || snapshot.TerminalCode != "" {
		t.Fatalf("unowned root/child did not continue: %+v ok=%v", snapshot, ok)
	}
	if got := childA.Kind(); got != runwatch.KindDelegation {
		t.Fatalf("unowned child stopped with sibling root cancellation: %q", got)
	}
	childA.Complete()
	childB.Complete()
	rootA.Complete()
	s.unregisterActiveCancel("web-root")
}

func TestChannelCancelPendingNewRouteDoesNotMaskLiveOwnedRun(t *testing.T) {
	testLog := newTestLog(t)
	mgr := NewSessionManager(factoryFromFake(t, &agenttest.FakeLLM{}), testLog)
	t.Cleanup(func() { mgr.Shutdown(time.Second) })
	sess, err := mgr.Init(context.Background(), "team-a")
	if err != nil {
		t.Fatal(err)
	}

	firstStarted := make(chan struct{})
	var calls atomic.Int32
	sess.askStreamHistory = func(ctx context.Context, _ *ctxwin.ContextWindow, _ string) (<-chan agent.AgentEvent, error) {
		out := make(chan agent.AgentEvent, 2)
		if calls.Add(1) == 1 {
			close(firstStarted)
			go func() {
				defer close(out)
				<-ctx.Done()
				out <- agent.ErrorEvent{Err: context.Cause(ctx)}
			}()
			return out, nil
		}
		out <- agent.ContentDeltaEvent{Delta: "second completed"}
		out <- agent.DoneEvent{Content: "second completed"}
		close(out)
		return out, nil
	}

	adapter := NewChannelAdapter(mgr, testLog)
	qqCtx := channel.ContextWithChatMeta(context.Background(), channel.ChatMeta{
		Channel: "qq", AccountID: "qq-bot", ConversationID: "qq-chat", UserID: "qq-user",
	})
	wechatCtx := channel.ContextWithChatMeta(context.Background(), channel.ChatMeta{
		Channel: "wechat", AccountID: "wechat-bot", ConversationID: "wechat-chat", UserID: "wechat-user",
	})

	firstDone := make(chan error, 1)
	go func() {
		_, askErr := adapter.AskStream(qqCtx, "live first run", nil)
		firstDone <- askErr
	}()
	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first channel run did not start")
	}

	secondDone := make(chan error, 1)
	go func() {
		_, askErr := adapter.AskStream(wechatCtx, "pending newer route", nil)
		secondDone <- askErr
	}()
	time.Sleep(100 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("newer route entered Session before live route released: calls=%d", got)
	}
	select {
	case err := <-secondDone:
		t.Fatalf("newer route unexpectedly completed before cancel: %v", err)
	default:
	}

	if err := adapter.CancelCurrent("cancel latest live owned run"); err != nil {
		t.Fatalf("CancelCurrent: %v", err)
	}
	select {
	case err := <-firstDone:
		if !errors.Is(err, channel.ErrTaskCancelled) {
			t.Fatalf("live run cancellation error = %v, want ErrTaskCancelled", err)
		}
	case <-time.After(time.Second):
		_ = sess.CancelCurrent("test cleanup")
		t.Fatal("pending newer route masked cancellation of live owned run")
	}
	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatalf("pending route after live cancel: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("pending route did not proceed after live run cancellation")
	}

	adapter.runMu.Lock()
	owned := len(adapter.channelRuns)
	adapter.runMu.Unlock()
	if owned != 0 {
		t.Fatalf("channel owned run state leaked: %d", owned)
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

func TestSessionAskAdapterMarksL1ChannelTemporalContext(t *testing.T) {
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

	adapter := NewChannelAdapter(mgr, testLog)
	before := time.Now()
	if _, err := adapter.AskStream(context.Background(), "remind me", nil); err != nil {
		t.Fatalf("AskStream: %v", err)
	}
	after := time.Now()

	close(pushed)
	for msg := range pushed {
		if msg.Role != ctxwin.RoleUser {
			continue
		}
		if msg.Content != "remind me" || !msg.ExposeTimestamp {
			t.Fatalf("user message = %#v, want raw eligible channel input", msg)
		}
		if msg.Timestamp.Before(before) || msg.Timestamp.After(after) {
			t.Fatalf("timestamp = %v, want original receive time between %v and %v", msg.Timestamp, before, after)
		}
		return
	}
	t.Fatal("user message was not pushed")
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
	sess := NewSession("consume-basic", "team", ag, nil, nil, testLog)
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
	sess := NewSession("consume-error", "team", ag, nil, nil, testLog)
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
	sess := NewSession("consume-intermediate", "team", ag, nil, nil, testLog)
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
	sess := NewSession("consume-reasoning", "team", ag, nil, nil, testLog)
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
	sess := NewSession("consume-image", "team", ag, nil, nil, testLog)
	t.Cleanup(func() { sess.Close() })

	ch := make(chan iface.AgentEvent, 3)
	ch <- agent.ToolExecDoneEvent{
		Name:   "ImageTool",
		Result: `{"status":"completed","image_urls":["https://example.com/img.png"]}`,
	}
	ch <- agent.ToolExecDoneEvent{
		Name:   "LegacyImageTool",
		Result: `{"status":"completed","image_urls":["https://example.com/edit.png"]}`,
	}
	ch <- agent.DoneEvent{}
	close(ch)

	result, err := base.consumeAskStreamEvents(context.Background(), sess, ch, nil)
	if err != nil {
		t.Fatalf("consumeAskStreamEvents: %v", err)
	}
	if len(result.ImageURLs) != 1 {
		t.Errorf("expected only the ImageTool URL, got %d: %v", len(result.ImageURLs), result.ImageURLs)
	}
	if len(result.MediaList) != 1 {
		t.Errorf("expected only the ImageTool media item, got %d", len(result.MediaList))
	}
}

func TestConsumeAskStreamEvents_SendFileResult(t *testing.T) {
	testLog := newTestLog(t)
	base := &channelAdapterBase{log: testLog}
	ag := startAgent(t, &agenttest.FakeLLM{Responses: []string{"ok"}})
	sess := NewSession("consume-file", "team", ag, nil, nil, testLog)
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
	route := "qq\x00bot\x00chat\x00user"
	sess := &Session{logger: testLog, pending: &PendingQueue{}, channelRouteKey: route, channelRouteOwners: 1}
	sess.inFlight.Store(1)
	base := &channelAdapterBase{log: testLog}
	receivedAt := time.Date(2026, time.August, 27, 9, 35, 59, 0, time.Local)
	ctx := channel.ContextWithChatMeta(context.Background(), channel.ChatMeta{Channel: "qq", AccountID: "bot", ConversationID: "chat", UserID: "user"})
	ctx = withTemporalExposure(ctx, receivedAt)
	_, _, err := base.askChannelStream(ctx, sess, "follow-up")
	if !errors.Is(err, ErrQueued) {
		t.Fatalf("err = %v", err)
	}
	if got := sess.pending.Len(); got != 1 {
		t.Fatalf("pending length = %d, want 1", got)
	}
	drained := sess.pending.Drain()
	if len(drained.Parts) != 1 {
		t.Fatalf("pending drain = %#v, want one message", drained)
	}
	message := drained.Parts[0]
	if message.Prompt != "follow-up" || !message.ExposeTimestamp || !message.ReceivedAt.Equal(receivedAt) {
		t.Fatalf("pending message = %#v, want original temporal metadata", message)
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
