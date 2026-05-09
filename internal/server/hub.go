package server

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ─── WebSocket Message Types ────────────────────────────────────────────────

// WSMessage is the envelope for all WebSocket messages sent to clients.
type WSMessage struct {
	Type    string                  `json:"type"`              // "connected" | "state"
	Runtime *RuntimeStatusResponse  `json:"runtime,omitempty"`
	Agents  *AgentListResponse      `json:"agents,omitempty"`
}

// wsNotify is an internal signal that state has changed and needs broadcasting.
type wsNotify struct{}

// ─── Hub ────────────────────────────────────────────────────────────────────

// Hub manages WebSocket client connections and broadcasts state updates.
type Hub struct {
	mu        sync.RWMutex
	clients   map[*Client]bool
	broadcast chan *WSMessage
	register  chan *Client
	unregister chan *Client
	notify    chan wsNotify     // external signal: data changed
	mux       *Mux             // read-only access to runtime/agent data
	done      chan struct{}
}

// NewHub creates a new Hub. The Hub is not started until Run is called.
func NewHub(m *Mux) *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan *WSMessage, 64),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		notify:     make(chan wsNotify, 16),
		mux:        m,
		done:       make(chan struct{}),
	}
}

// Run starts the Hub's main loop. It should be called in a dedicated goroutine.
func (h *Hub) Run() {
	// Debounce timer: collects rapid-fire notifications and sends one update.
	var debounce *time.Timer
	debounceC := make(<-chan time.Time)

	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			// Send initial state immediately upon connection.
			msg := h.buildStateMessage()
			data, err := json.Marshal(msg)
			if err == nil {
				select {
				case client.send <- data:
				default:
					// Slow client; close connection.
					h.removeClient(client)
				}
			}

		case client := <-h.unregister:
			h.removeClient(client)

		case msg := <-h.broadcast:
			data, err := json.Marshal(msg)
			if err != nil {
				continue
			}
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.send <- data:
				default:
					// Slow client; schedule removal outside read lock.
					go func(c *Client) { h.unregister <- c }(client)
				}
			}
			h.mu.RUnlock()

		case <-debounceC:
			debounce = nil
			debounceC = make(<-chan time.Time)
			msg := h.buildStateMessage()
			data, err := json.Marshal(msg)
			if err != nil {
				continue
			}
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.send <- data:
				default:
					go func(c *Client) { h.unregister <- c }(client)
				}
			}
			h.mu.RUnlock()

		case <-h.notify:
			// Debounce: reset timer to 50ms. If another notify arrives
			// before the timer fires, it gets merged into one update.
			if debounce != nil {
				debounce.Stop()
			}
			debounce = time.NewTimer(50 * time.Millisecond)
			debounceC = debounce.C

		case <-h.done:
			if debounce != nil {
				debounce.Stop()
			}
			h.mu.Lock()
			for client := range h.clients {
				close(client.send)
				client.conn.Close()
			}
			h.clients = make(map[*Client]bool)
			h.mu.Unlock()
			return
		}
	}
}

// Notify signals the Hub that runtime or agent data has changed.
// It is non-blocking: if the notify channel is full, the signal is dropped
// (the next debounce cycle will still pick up the change).
func (h *Hub) Notify() {
	select {
	case h.notify <- wsNotify{}:
	default:
	}
}

// Close stops the Hub and closes all client connections.
func (h *Hub) Close() {
	close(h.done)
}

// ClientCount returns the number of connected WebSocket clients (for diagnostics).
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// buildStateMessage constructs a WSMessage with the current runtime and agent state.
func (h *Hub) buildStateMessage() *WSMessage {
	msg := &WSMessage{Type: "state"}
	if h.mux.runtimeMetrics != nil {
		msg.Runtime = h.mux.buildRuntimeStatus()
	}
	if h.mux.registry != nil {
		msg.Agents = h.mux.buildAgentList()
	}
	return msg
}

// removeClient removes a client from the Hub and cleans up its resources.
func (h *Hub) removeClient(client *Client) {
	h.mu.Lock()
	if _, ok := h.clients[client]; ok {
		delete(h.clients, client)
		close(client.send)
	}
	h.mu.Unlock()
}

// ─── Client ─────────────────────────────────────────────────────────────────

// Client represents a single WebSocket client connection.
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte // buffered outbound messages
}

// newClient creates a new Client for the given WebSocket connection.
func newClient(hub *Hub, conn *websocket.Conn) *Client {
	return &Client{
		hub:  hub,
		conn: conn,
		send: make(chan []byte, 64),
	}
}
