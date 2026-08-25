package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

const (
	// Keep the envelope bounded independently from the model context window so a
	// single remote message cannot consume unbounded server memory.
	maxWSMessageBytes  int64 = 8 << 20
	maxChatPromptBytes       = 4 << 20
)

func validateChatPrompt(prompt string) error {
	if len(prompt) > maxChatPromptBytes {
		return fmt.Errorf("message is too large: prompt limit is 4 MiB")
	}
	return nil
}

// ─── WebSocket Upgrader ─────────────────────────────────────────────────────

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     checkWebSocketOrigin,
}

func checkWebSocketOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return false
	}
	// Standalone Web Console and Vite use different loopback ports from the
	// backend, so loopback origins are allowed independently of the port.
	if isLoopbackOrigin(origin) {
		return true
	}
	// A reverse proxy presents the browser's external Host to the backend.
	// Same-host origins are therefore valid without making arbitrary origins
	// valid for direct backend access.
	return strings.EqualFold(u.Host, r.Host)
}

// ─── WebSocket Handler ──────────────────────────────────────────────────────

// handleWebSocket upgrades an HTTP connection to WebSocket and manages the
// client lifecycle through readPump and writePump goroutines.
func (m *Mux) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if m.hub == nil {
		http.Error(w, "websocket not available", http.StatusServiceUnavailable)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	client := newClient(m.hub, conn)
	m.hub.register <- client

	// Send connected confirmation.
	connectedMsg := jsonMarshal(WSMessage{Type: "connected"})
	select {
	case client.send <- connectedMsg:
	default:
	}

	go client.writePump()
	go client.readPump()
}

// ─── Read Pump ──────────────────────────────────────────────────────────────

// readPump reads messages from the WebSocket connection.
// It handles client chat messages (chat_send, chat_cancel, tool_confirm) in
// addition to app-level ping-pong.
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxWSMessageBytes)
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		messageType, p, err := c.conn.ReadMessage()
		if err != nil {
			if errors.Is(err, websocket.ErrReadLimit) && c.hub != nil && c.hub.mux != nil && c.hub.mux.log != nil {
				c.hub.mux.log.WarnContext(c.ctx, logger.CatApp, "websocket message rejected: too large",
					"max_bytes", maxWSMessageBytes,
				)
			}
			break
		}

		// Check if client is being shut down before any send to c.send.
		// hub.removeClient closes c.send after cancelling ctx; sending to a
		// closed channel causes a fatal panic.
		if c.ctx.Err() != nil {
			break
		}

		if messageType == websocket.TextMessage {
			if string(p) == "ping" {
				c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
				select {
				case c.send <- jsonMarshal(WSMessage{Type: "pong"}):
				default:
				}
				continue
			}

			// Parse as ClientMessage.
			var msg ClientMessage
			if err := json.Unmarshal(p, &msg); err != nil {
				continue
			}

			switch msg.Type {
			case "chat_send":
				c.hub.handleChatSend(c, &msg)
			case "chat_cancel":
				c.hub.handleChatCancel(c, &msg)
			case "tool_confirm":
				c.hub.handleToolConfirm(c, &msg)
			}
		}
	}
}

// ─── Write Pump ─────────────────────────────────────────────────────────────

// writePump writes messages from the Hub to the WebSocket connection.
// It also sends periodic ping frames to keep the connection alive.
func (c *Client) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// ─── Helper ─────────────────────────────────────────────────────────────────

// jsonMarshal wraps json.Marshal, replacing any error with an empty JSON object.
// This is acceptable for WebSocket broadcasts where a missing message is tolerable.
func jsonMarshal(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		return []byte(`{"type":"error"}`)
	}
	return data
}
