package wechat

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/channel"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

func TestMessageBufferMergesTextThenMediaOnSameRoute(t *testing.T) {
	log, err := logger.New(t.TempDir(), logger.WithConsole(false), logger.WithFile(false))
	if err != nil {
		t.Fatal(err)
	}
	received := make(chan channel.Message, 2)
	buffer := newMessageBuffer(100*time.Millisecond, log, channel.HandlerFunc(func(_ context.Context, msg channel.Message) {
		received <- msg
	}))
	t.Cleanup(buffer.Close)

	route := channel.Message{Channel: "wechat", AccountID: "bot", ConversationID: "chat", UserID: "user"}
	text := route
	text.Text = "describe this"
	media := route
	media.Attachments = []channel.Attachment{{Kind: channel.AttachmentImage, Name: "image.jpg", Data: []byte("image")}}

	buffer.Push(context.Background(), text)
	buffer.Push(context.Background(), media)

	select {
	case got := <-received:
		if got.Text != text.Text || len(got.Attachments) != 1 {
			t.Fatalf("merged message = %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for merged message")
	}
	select {
	case got := <-received:
		t.Fatalf("unexpected second message: %#v", got)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestMessageBufferMergesMediaThenTextOnSameRoute(t *testing.T) {
	log, err := logger.New(t.TempDir(), logger.WithConsole(false), logger.WithFile(false))
	if err != nil {
		t.Fatal(err)
	}
	received := make(chan channel.Message, 2)
	buffer := newMessageBuffer(100*time.Millisecond, log, channel.HandlerFunc(func(_ context.Context, msg channel.Message) {
		received <- msg
	}))
	t.Cleanup(buffer.Close)

	route := channel.Message{Channel: "wechat", AccountID: "bot", ConversationID: "chat", UserID: "user"}
	media := route
	media.Attachments = []channel.Attachment{{Kind: channel.AttachmentImage, Name: "image.jpg", Data: []byte("image")}}
	text := route
	text.Text = "describe this"

	buffer.Push(context.Background(), media)
	buffer.Push(context.Background(), text)

	select {
	case got := <-received:
		if got.Text != text.Text || len(got.Attachments) != 1 {
			t.Fatalf("merged message = %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for merged message")
	}
}

func TestMessageBufferDoesNotMergeTextWithText(t *testing.T) {
	log, _ := logger.New(t.TempDir(), logger.WithConsole(false), logger.WithFile(false))
	received := make(chan channel.Message, 2)
	buffer := newMessageBuffer(40*time.Millisecond, log, channel.HandlerFunc(func(_ context.Context, msg channel.Message) {
		received <- msg
	}))
	t.Cleanup(buffer.Close)

	route := channel.Message{Channel: "wechat", AccountID: "bot", ConversationID: "chat", UserID: "user"}
	first := route
	first.Text = "first"
	second := route
	second.Text = "second"
	buffer.Push(context.Background(), first)
	buffer.Push(context.Background(), second)

	var got []string
	deadline := time.After(500 * time.Millisecond)
	for len(got) < 2 {
		select {
		case msg := <-received:
			got = append(got, msg.Text)
		case <-deadline:
			t.Fatalf("received texts = %v, want both messages", got)
		}
	}
}

func TestMessageBufferKeepsNonComplementaryDispatchConcurrent(t *testing.T) {
	log, _ := logger.New(t.TempDir(), logger.WithConsole(false), logger.WithFile(false))
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	received := make(chan channel.Message, 2)
	buffer := newMessageBuffer(time.Second, log, channel.HandlerFunc(func(_ context.Context, msg channel.Message) {
		if msg.Text == "first" {
			close(firstStarted)
			<-releaseFirst
		}
		received <- msg
	}))
	t.Cleanup(buffer.Close)

	route := channel.Message{Channel: "wechat", AccountID: "bot", ConversationID: "chat", UserID: "user"}
	first := route
	first.Text = "first"
	second := route
	second.Text = "second"
	buffer.Push(context.Background(), first)
	secondReturned := make(chan struct{})
	go func() {
		buffer.Push(context.Background(), second)
		close(secondReturned)
	}()
	select {
	case <-firstStarted:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("first message did not start")
	}
	select {
	case <-secondReturned:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("second non-complementary dispatch blocked behind the first handler")
	}
	close(releaseFirst)
}

func TestMessageBufferBypassesBuiltInCommands(t *testing.T) {
	log, _ := logger.New(t.TempDir(), logger.WithConsole(false), logger.WithFile(false))
	received := make(chan channel.Message, 1)
	buffer := newMessageBuffer(time.Second, log, channel.HandlerFunc(func(_ context.Context, msg channel.Message) {
		received <- msg
	}))
	t.Cleanup(buffer.Close)

	command := channel.Message{Channel: "wechat", AccountID: "bot", ConversationID: "chat", UserID: "user", Text: "/cancel"}
	buffer.Push(context.Background(), command)

	select {
	case got := <-received:
		if got.Text != command.Text {
			t.Fatalf("command = %#v", got)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("built-in command was buffered")
	}
}

func TestMessageBufferCancelDropsSameRoutePendingTurn(t *testing.T) {
	log, _ := logger.New(t.TempDir(), logger.WithConsole(false), logger.WithFile(false))
	received := make(chan channel.Message, 2)
	buffer := newMessageBuffer(30*time.Millisecond, log, channel.HandlerFunc(func(_ context.Context, msg channel.Message) {
		received <- msg
	}))
	t.Cleanup(buffer.Close)

	route := channel.Message{Channel: "wechat", AccountID: "bot", ConversationID: "chat", UserID: "user"}
	pending := route
	pending.Text = "start a task"
	cancel := route
	cancel.Text = "/cancel"
	buffer.Push(context.Background(), pending)
	buffer.Push(context.Background(), cancel)

	select {
	case got := <-received:
		if got.Text != "/cancel" {
			t.Fatalf("first dispatched message = %q", got.Text)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("cancel command was not dispatched immediately")
	}
	select {
	case got := <-received:
		t.Fatalf("buffered turn ran after cancel: %#v", got)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestMessageBufferDispatchesAlreadyCombinedMessageImmediately(t *testing.T) {
	log, _ := logger.New(t.TempDir(), logger.WithConsole(false), logger.WithFile(false))
	received := make(chan channel.Message, 1)
	buffer := newMessageBuffer(time.Second, log, channel.HandlerFunc(func(_ context.Context, msg channel.Message) {
		received <- msg
	}))
	t.Cleanup(buffer.Close)

	combined := channel.Message{
		Channel: "wechat", AccountID: "bot", ConversationID: "chat", UserID: "user", Text: "describe this",
		Attachments: []channel.Attachment{{Kind: channel.AttachmentImage, Name: "image.jpg"}},
	}
	buffer.Push(context.Background(), combined)

	select {
	case got := <-received:
		if got.Text != combined.Text || len(got.Attachments) != 1 {
			t.Fatalf("combined message = %#v", got)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("already combined message was buffered")
	}
}

func TestMessageBufferLogsMergeWithoutSensitiveFields(t *testing.T) {
	dir := t.TempDir()
	log, _ := logger.New(dir, logger.WithConsole(false), logger.WithFile(true))
	received := make(chan channel.Message, 1)
	buffer := newMessageBuffer(time.Second, log, channel.HandlerFunc(func(_ context.Context, msg channel.Message) {
		received <- msg
	}))

	route := channel.Message{Channel: "wechat", AccountID: "bot", ConversationID: "chat", UserID: "secret-user"}
	text := route
	text.MessageID = "message-1"
	text.Text = "secret-content"
	text.ReplyToken = "secret-token-1"
	media := route
	media.MessageID = "message-2"
	media.ReplyToken = "secret-token-2"
	media.Attachments = []channel.Attachment{{Kind: channel.AttachmentImage, Name: "image.jpg"}}
	buffer.Push(context.Background(), text)
	buffer.Push(context.Background(), media)
	<-received
	buffer.Close()
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "logs", "system", "app-"+time.Now().Format("2006-01-02")+".jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	logs := string(data)
	for _, want := range []string{"wechat_turn_buffered", "wechat_turn_merged", "wait_ms", "route_hash", "reason", "text_present", "attachment_count"} {
		if !strings.Contains(logs, want) {
			t.Errorf("logs missing %q: %s", want, logs)
		}
	}
	for _, secret := range []string{"message-1", "message-2", "secret-content", "secret-user", "secret-token-1", "secret-token-2"} {
		if strings.Contains(logs, secret) {
			t.Errorf("logs contain sensitive value %q: %s", secret, logs)
		}
	}
}

func TestMessageBufferLogsTimeoutAndFlush(t *testing.T) {
	dir := t.TempDir()
	log, _ := logger.New(dir, logger.WithConsole(false), logger.WithFile(true))
	received := make(chan channel.Message, 1)
	buffer := newMessageBuffer(20*time.Millisecond, log, channel.HandlerFunc(func(_ context.Context, msg channel.Message) {
		received <- msg
	}))
	msg := channel.Message{MessageID: "message-1", Channel: "wechat", AccountID: "bot", ConversationID: "chat", UserID: "secret-user", Text: "secret-content", ReplyToken: "secret-token"}
	buffer.Push(context.Background(), msg)
	<-received
	buffer.Close()
	if err := log.Close(); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "logs", "system", "app-"+time.Now().Format("2006-01-02")+".jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	logs := string(data)
	for _, want := range []string{"wechat_turn_timeout", "wechat_turn_flushed", "wait_ms", "route_hash", "reason", "text_present", "attachment_count"} {
		if !strings.Contains(logs, want) {
			t.Errorf("logs missing %q: %s", want, logs)
		}
	}
	for _, secret := range []string{"message-1", "secret-content", "secret-user", "secret-token"} {
		if strings.Contains(logs, secret) {
			t.Errorf("logs contain sensitive value %q: %s", secret, logs)
		}
	}
}

func TestMessageBufferDoesNotMergeMediaWithMedia(t *testing.T) {
	log, _ := logger.New(t.TempDir(), logger.WithConsole(false), logger.WithFile(false))
	received := make(chan channel.Message, 2)
	buffer := newMessageBuffer(30*time.Millisecond, log, channel.HandlerFunc(func(_ context.Context, msg channel.Message) {
		received <- msg
	}))
	t.Cleanup(buffer.Close)

	route := channel.Message{Channel: "wechat", AccountID: "bot", ConversationID: "chat", UserID: "user"}
	first := route
	first.Attachments = []channel.Attachment{{Kind: channel.AttachmentImage, Name: "first.jpg"}}
	second := route
	second.Attachments = []channel.Attachment{{Kind: channel.AttachmentImage, Name: "second.jpg"}}
	buffer.Push(context.Background(), first)
	buffer.Push(context.Background(), second)

	for count := 0; count < 2; count++ {
		select {
		case got := <-received:
			if len(got.Attachments) != 1 {
				t.Fatalf("message = %#v", got)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("received %d media messages, want 2", count)
		}
	}
}

func TestMessageBufferDoesNotMergeDifferentRoutes(t *testing.T) {
	log, _ := logger.New(t.TempDir(), logger.WithConsole(false), logger.WithFile(false))
	received := make(chan channel.Message, 2)
	buffer := newMessageBuffer(30*time.Millisecond, log, channel.HandlerFunc(func(_ context.Context, msg channel.Message) {
		received <- msg
	}))
	t.Cleanup(buffer.Close)

	text := channel.Message{Channel: "wechat", AccountID: "bot", ConversationID: "chat-a", UserID: "user", Text: "describe this"}
	media := channel.Message{Channel: "wechat", AccountID: "bot", ConversationID: "chat-b", UserID: "user", Attachments: []channel.Attachment{{Kind: channel.AttachmentImage, Name: "image.jpg"}}}
	buffer.Push(context.Background(), text)
	buffer.Push(context.Background(), media)

	for count := 0; count < 2; count++ {
		select {
		case <-received:
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("received %d messages, want 2", count)
		}
	}
}

func TestMessageBufferDoesNotMergeOutsideWindow(t *testing.T) {
	log, _ := logger.New(t.TempDir(), logger.WithConsole(false), logger.WithFile(false))
	received := make(chan channel.Message, 2)
	buffer := newMessageBuffer(20*time.Millisecond, log, channel.HandlerFunc(func(_ context.Context, msg channel.Message) {
		received <- msg
	}))
	t.Cleanup(buffer.Close)

	route := channel.Message{Channel: "wechat", AccountID: "bot", ConversationID: "chat", UserID: "user"}
	text := route
	text.Text = "describe this"
	media := route
	media.Attachments = []channel.Attachment{{Kind: channel.AttachmentImage, Name: "image.jpg"}}
	buffer.Push(context.Background(), text)
	select {
	case <-received:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("text message did not flush at the window")
	}
	buffer.Push(context.Background(), media)
	select {
	case <-received:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("media message did not flush at the window")
	}
}

func TestMessageBufferDoesNotMergeAfterWindowWhenTimeoutCallbackIsDelayed(t *testing.T) {
	log, _ := logger.New(t.TempDir(), logger.WithConsole(false), logger.WithFile(false))
	received := make(chan channel.Message, 2)
	buffer := newMessageBuffer(20*time.Millisecond, log, channel.HandlerFunc(func(_ context.Context, msg channel.Message) {
		received <- msg
	}))
	t.Cleanup(buffer.Close)

	route := channel.Message{Channel: "wechat", AccountID: "bot", ConversationID: "chat", UserID: "user"}
	text := route
	text.Text = "describe this"
	media := route
	media.Attachments = []channel.Attachment{{Kind: channel.AttachmentImage, Name: "image.jpg"}}
	buffer.Push(context.Background(), text)

	key := messageRouteKey(text)
	buffer.mu.Lock()
	entry := buffer.pending[key]
	if entry == nil || !entry.timer.Stop() {
		buffer.mu.Unlock()
		t.Fatal("failed to hold the pending timeout before callback execution")
	}
	time.Sleep(40 * time.Millisecond)
	buffer.mu.Unlock()

	buffer.Push(context.Background(), media)
	for count := 0; count < 2; count++ {
		select {
		case <-received:
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("received %d messages, want two turns outside the window", count)
		}
	}
}
