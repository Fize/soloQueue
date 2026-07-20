package qq

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// testEventHandler captures the last QQMessage received.
type testEventHandler struct {
	lastMessage QQMessage
	called      bool
}

func (h *testEventHandler) OnQQMessage(_ context.Context, msg QQMessage) {
	h.lastMessage = msg
	h.called = true
}

func testLogger(t *testing.T) *logger.Logger {
	t.Helper()
	l, err := logger.New(t.TempDir(), logger.WithFile(false), logger.WithConsole(false))
	if err != nil {
		t.Fatalf("create test logger: %v", err)
	}
	return l
}

func TestHandleAudioMessage_ValidJSON(t *testing.T) {
	handler := &testEventHandler{}
	gw := &Gateway{
		handler: handler,
		log:     testLogger(t),
	}

	raw := json.RawMessage(`{
		"id": "msg_001",
		"author": {"user_openid": "user_abc"},
		"attachments": [
			{"content_type": "audio/silk", "url": "https://example.com/audio.silk"}
		],
		"timestamp": "2025-01-01T00:00:00Z"
	}`)

	gw.handleAudioMessage(context.Background(), raw)

	if !handler.called {
		t.Fatal("handler.OnQQMessage was not called")
	}
	msg := handler.lastMessage
	if msg.AudioURL != "https://example.com/audio.silk" {
		t.Errorf("AudioURL = %q, want https://example.com/audio.silk", msg.AudioURL)
	}
	if msg.OpenID != "user_abc" {
		t.Errorf("OpenID = %q, want user_abc", msg.OpenID)
	}
	if msg.EventID != "msg_001" {
		t.Errorf("EventID = %q, want msg_001", msg.EventID)
	}
	if msg.Source != SourceC2C {
		t.Errorf("Source = %d, want SourceC2C", msg.Source)
	}
}

func TestHandleAudioMessage_InvalidJSON(t *testing.T) {
	handler := &testEventHandler{}
	gw := &Gateway{
		handler: handler,
		log:     testLogger(t),
	}

	raw := json.RawMessage(`{invalid json`)

	// Must not panic, must not call handler
	gw.handleAudioMessage(context.Background(), raw)

	if handler.called {
		t.Error("handler should not be called for invalid JSON")
	}
}

func TestHandleAudioMessage_NoAudioAttachments(t *testing.T) {
	handler := &testEventHandler{}
	gw := &Gateway{
		handler: handler,
		log:     testLogger(t),
	}

	raw := json.RawMessage(`{
		"id": "msg_002",
		"author": {"user_openid": "user_xyz"},
		"attachments": [
			{"content_type": "image/png", "url": "https://example.com/img.png"}
		],
		"timestamp": "2025-01-01T00:00:00Z"
	}`)

	gw.handleAudioMessage(context.Background(), raw)

	if !handler.called {
		t.Fatal("handler.OnQQMessage was not called")
	}
	msg := handler.lastMessage
	if msg.AudioURL != "" {
		t.Errorf("AudioURL = %q, want empty (no audio attachment)", msg.AudioURL)
	}
	if msg.OpenID != "user_xyz" {
		t.Errorf("OpenID = %q, want user_xyz", msg.OpenID)
	}
}

func TestHandleAudioMessage_EmptyAttachments(t *testing.T) {
	handler := &testEventHandler{}
	gw := &Gateway{
		handler: handler,
		log:     testLogger(t),
	}

	raw := json.RawMessage(`{
		"id": "msg_003",
		"author": {"user_openid": "user_def"}
	}`)

	gw.handleAudioMessage(context.Background(), raw)

	if !handler.called {
		t.Fatal("handler.OnQQMessage was not called")
	}
	msg := handler.lastMessage
	if msg.AudioURL != "" {
		t.Errorf("AudioURL = %q, want empty", msg.AudioURL)
	}
}
