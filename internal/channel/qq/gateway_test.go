package qq

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

type testTokenProvider struct {
	token string
}

func (p testTokenProvider) AccessToken(context.Context) (string, error) {
	return p.token, nil
}

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

func TestConnectAndIdentify_ResumePrefixesToken(t *testing.T) {
	received := make(chan GatewayPayload, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		hello := GatewayPayload{
			Op: OpHello,
			D:  json.RawMessage(`{"heartbeat_interval":60000}`),
		}
		if err := conn.WriteJSON(hello); err != nil {
			return
		}

		var payload GatewayPayload
		if err := conn.ReadJSON(&payload); err == nil {
			received <- payload
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	gw := &Gateway{
		cfg:        Config{},
		log:        testLogger(t),
		tokens:     testTokenProvider{token: "access-token"},
		sessionID:  "session-123",
		gatewayURL: func() string { return wsURL },
	}
	gw.seq.Store(42)
	defer gw.closeConn()

	if err := gw.connectAndIdentify(context.Background()); err != nil {
		t.Fatalf("connectAndIdentify() error = %v", err)
	}

	var payload GatewayPayload
	select {
	case payload = <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Resume payload")
	}

	if payload.Op != OpResume {
		t.Fatalf("gateway opcode = %d, want %d", payload.Op, OpResume)
	}
	var resume ResumeData
	if err := json.Unmarshal(payload.D, &resume); err != nil {
		t.Fatalf("decode resume payload: %v", err)
	}
	if resume.Token != "QQBot access-token" {
		t.Errorf("resume token = %q, want %q", resume.Token, "QQBot access-token")
	}
	if resume.SessionID != "session-123" || resume.Seq != 42 {
		t.Errorf("resume state = session %q seq %d, want session %q seq %d", resume.SessionID, resume.Seq, "session-123", 42)
	}
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

func TestHandleC2CMessage_AudioAttachment(t *testing.T) {
	handler := &testEventHandler{}
	gw := &Gateway{
		handler: handler,
		log:     testLogger(t),
	}

	raw := json.RawMessage(`{
		"id": "msg_c2c_audio",
		"author": {"user_openid": "user_abc"},
		"attachments": [
			{"content_type": "audio/silk", "url": "https://example.com/audio.silk"}
		]
	}`)

	gw.handleC2CMessage(context.Background(), raw)

	if !handler.called {
		t.Fatal("handler.OnQQMessage was not called")
	}
	msg := handler.lastMessage
	if msg.AudioURL != "https://example.com/audio.silk" {
		t.Errorf("AudioURL = %q, want https://example.com/audio.silk", msg.AudioURL)
	}
	if len(msg.Files) != 0 {
		t.Errorf("Files = %#v, want no ordinary file attachments", msg.Files)
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

func TestHeartbeatLifecycleConcurrentStartStop(t *testing.T) {
	g := &Gateway{
		heartbeatInterval: time.Millisecond,
		log:               testLogger(t),
	}
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if j%2 == 0 {
					g.startHeartbeat(ctx)
				} else {
					g.stopHeartbeat()
				}
			}
		}()
	}
	wg.Wait()
	g.stopHeartbeat()

	if g.heartbeatTicker != nil {
		t.Fatal("heartbeat ticker still running after stopHeartbeat")
	}
	if g.heartbeatDone != nil {
		t.Fatal("heartbeat done channel still set after stopHeartbeat")
	}
}
