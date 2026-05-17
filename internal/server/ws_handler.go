package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

// ─── WebSocket Upgrader ─────────────────────────────────────────────────────

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Allow all origins — this is a local-only server.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// ─── WebSocket Handler ──────────────────────────────────────────────────────

// handleWebSocket upgrades an HTTP connection to WebSocket and manages the
// client lifecycle through readPump and writePump goroutines.
// Auth token is passed via ?token= query parameter.
func (m *Mux) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if m.hub == nil {
		http.Error(w, "websocket not available", http.StatusServiceUnavailable)
		return
	}

	// Auth check
	if m.authConfig.User != "" {
		tok := r.URL.Query().Get("token")
		if tok == "" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if _, ok := m.tokenStore.validateToken(tok); !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
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
// It enforces read limits and detects disconnections.
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(512) // We don't expect client messages, but allow small pings
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			break
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
