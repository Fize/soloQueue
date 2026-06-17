package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// ─── WebSocket Upgrader ─────────────────────────────────────────────────────

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	// Allow all origins — this is a local-only server.
	CheckOrigin: func(r *http.Request) bool { return true },
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

	c.conn.SetReadLimit(65536) // allow large prompt messages
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		messageType, p, err := c.conn.ReadMessage()
		if err != nil {
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
