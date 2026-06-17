package server

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/xiaobaitu/soloqueue/internal/simulation"
)

// ─── WebSocket Message Types ────────────────────────────────────────────────

// WSMessage is the envelope for all WebSocket messages sent to clients.
type WSMessage struct {
	Type string `json:"type"`

	// State broadcast fields.
	Runtime  *RuntimeStatusResponse         `json:"runtime,omitempty"`
	Agents   *AgentListResponse             `json:"agents,omitempty"`
	Event    *simulation.SimulationEvent    `json:"event,omitempty"`
	Progress *simulation.SimulationProgress `json:"progress,omitempty"`

	// Chat streaming fields.
	RequestID        string `json:"request_id,omitempty"`
	SessionID        string `json:"session_id,omitempty"`
	Delta            string `json:"delta,omitempty"`
	CallID           string `json:"call_id,omitempty"`
	Name             string `json:"name,omitempty"`
	Args             string `json:"args,omitempty"`
	Result           string `json:"result,omitempty"`
	Error            string `json:"error,omitempty"`
	DurationMS       int64  `json:"duration_ms,omitempty"`
	Content          string `json:"content,omitempty"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
	Prompt           string `json:"prompt,omitempty"`
	AllowInSession   bool   `json:"allow_in_session,omitempty"`
	TargetAgentID    string `json:"target_agent_id,omitempty"`
	AgentName        string `json:"agent_name,omitempty"`
	ResultContent    string `json:"result_content,omitempty"`
	NumTasks         int    `json:"num_tasks,omitempty"`
}

// ClientMessage is decoded from incoming WebSocket text frames.
type ClientMessage struct {
	Type      string       `json:"type"`
	RequestID string       `json:"request_id,omitempty"`
	SessionID string       `json:"session_id,omitempty"`
	Prompt    string       `json:"prompt,omitempty"`
	Files     []ClientFile `json:"files,omitempty"`
	CallID    string       `json:"call_id,omitempty"`
	Choice    string       `json:"choice,omitempty"`
}

// ClientFile references an uploaded file.
type ClientFile struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// activeRequest tracks a single chat request within the client's lifecycle.
type activeRequest struct {
	RequestID  string
	Cancel     context.CancelFunc
	Delegating bool // true while async delegation is in progress
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
	var simEvents chan simulation.SimulationEvent
	if h.mux != nil && h.mux.simEngine != nil {
		simEvents = make(chan simulation.SimulationEvent, 128)
		h.mux.simEngine.Subscribe(simEvents)
		defer h.mux.simEngine.Unsubscribe(simEvents)
	}

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

		case ev, ok := <-simEvents:
			if !ok {
				simEvents = nil
				continue
			}
			if ev.Type == "progress" {
				if p, ok := ev.Data.(*simulation.SimulationProgress); ok {
					msg := &WSMessage{
						Type:     "simulation_progress",
						Progress: p,
					}
					select {
					case h.broadcast <- msg:
					case <-h.done:
					}
				}
			} else {
				msg := &WSMessage{
					Type:  "simulation_event",
					Event: &ev,
				}
				select {
				case h.broadcast <- msg:
				case <-h.done:
				}
			}

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
	client.cancelAllRequests()
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
	hub            *Hub
	conn           *websocket.Conn
	send           chan []byte
	ctx            context.Context
	cancel         context.CancelFunc
	mu             sync.Mutex
	activeRequests map[string]*activeRequest // request_id → request
}

// newClient creates a new Client for the given WebSocket connection.
func newClient(hub *Hub, conn *websocket.Conn) *Client {
	ctx, cancel := context.WithCancel(context.Background())
	return &Client{
		hub:            hub,
		conn:           conn,
		send:           make(chan []byte, 256),
		ctx:            ctx,
		cancel:         cancel,
		activeRequests: make(map[string]*activeRequest),
	}
}

// addActiveRequest registers a chat request with the client.
func (c *Client) addActiveRequest(reqID string, cancel context.CancelFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.activeRequests[reqID] = &activeRequest{RequestID: reqID, Cancel: cancel}
}

// removeActiveRequest removes a chat request registration.
func (c *Client) removeActiveRequest(reqID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.activeRequests, reqID)
}

// setRequestDelegating marks a request as delegating or not.
func (c *Client) setRequestDelegating(reqID string, v bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if r, ok := c.activeRequests[reqID]; ok {
		r.Delegating = v
	}
}

// cancelAllRequests cancels all active chat requests.
func (c *Client) cancelAllRequests() {
	c.cancel() // kill client ctx → all request contexts cascade
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range c.activeRequests {
		r.Cancel()
	}
	c.activeRequests = make(map[string]*activeRequest)
}
