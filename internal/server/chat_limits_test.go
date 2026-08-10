package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestValidateChatPromptRejectsOversizedPrompt(t *testing.T) {
	err := validateChatPrompt(strings.Repeat("a", maxChatPromptBytes+1))
	if err == nil {
		t.Fatal("validateChatPrompt() error = nil, want a business size error")
	}
	if !strings.Contains(err.Error(), "4 MiB") {
		t.Fatalf("validateChatPrompt() error = %q, want the configured limit", err)
	}
}

func TestHandleChatSendReturnsPromptSizeError(t *testing.T) {
	h := NewHub(nil)
	client := &Client{send: make(chan []byte, 1)}
	h.handleChatSend(client, &ClientMessage{
		Type:      "chat_send",
		RequestID: "req-large",
		SessionID: "l1",
		Prompt:    strings.Repeat("a", maxChatPromptBytes+1),
	})

	var msg WSMessage
	if err := json.Unmarshal(<-client.send, &msg); err != nil {
		t.Fatalf("decode server response: %v", err)
	}
	if msg.Type != "chat_error" || !strings.Contains(msg.Error, "4 MiB") {
		t.Fatalf("response = %#v, want a 4 MiB chat_error", msg)
	}
}

func TestWebSocketAcceptsMessagesAboveLegacyLimit(t *testing.T) {
	mux := NewMux(t.TempDir(), nil)
	h := NewHub(mux)
	mux.SetHub(h)
	go h.Run()
	t.Cleanup(func() {
		h.Close()
		_ = mux.Close()
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial WebSocket: %v", err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatalf("read connected message: %v", err)
	}

	if err := conn.WriteMessage(websocket.TextMessage, []byte(strings.Repeat("x", 70<<10))); err != nil {
		t.Fatalf("write message above the legacy limit: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
		t.Fatalf("write ping after large message: %v", err)
	}
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read pong after large message: %v", err)
		}
		var msg WSMessage
		if json.Unmarshal(data, &msg) == nil && msg.Type == "pong" {
			return
		}
	}
}
