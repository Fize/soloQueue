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

// CronTaskStatus is a read-only representation of a scheduled task for the portal.
type CronTaskStatus struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	TaskType    string  `json:"task_type"`
	Expression  string  `json:"expression"`
	Instruction string  `json:"instruction"`
	TargetAgent string  `json:"target_agent"`
	Status      string  `json:"status"`
	LastRunAt   *string `json:"last_run_at,omitempty"`
	NextRunAt   string  `json:"next_run_at"`
	IsOneTime   bool    `json:"is_one_time"`
}

// NotificationPayload is the unified payload for desktop notifications.
// Any backend module can push a notification by calling Hub.SendNotification.
type NotificationPayload struct {
	Category  string `json:"category"` // "cron", "system", "agent", ...
	Level     string `json:"level"`    // "info" | "success" | "warning" | "error"
	Title     string `json:"title"`
	Body      string `json:"body"`
	Timestamp string `json:"timestamp"` // ISO8601
}

// WSMessage is the envelope for all WebSocket messages sent to clients.
type WSMessage struct {
	Type string `json:"type"`

	// State broadcast fields.
	Runtime  *RuntimeStatusResponse         `json:"runtime,omitempty"`
	Agents   *AgentListResponse             `json:"agents,omitempty"`
	Event    *simulation.SimulationEvent    `json:"event,omitempty"`
	Progress *simulation.SimulationProgress `json:"progress,omitempty"`

	// Chat streaming fields.
	RequestID        string               `json:"request_id,omitempty"`
	SessionID        string               `json:"session_id,omitempty"`
	TaskType         string               `json:"task_type,omitempty"`
	ModelID          string               `json:"model_id,omitempty"`
	ProviderID       string               `json:"provider_id,omitempty"`
	AgentInstanceID  string               `json:"agent_instance_id,omitempty"`
	Delta            string               `json:"delta,omitempty"`
	CallID           string               `json:"call_id,omitempty"`
	Name             string               `json:"name,omitempty"`
	Args             string               `json:"args,omitempty"`
	Result           string               `json:"result,omitempty"`
	Error            string               `json:"error,omitempty"`
	DurationMS       int64                `json:"duration_ms,omitempty"`
	Content          string               `json:"content,omitempty"`
	ReasoningContent string               `json:"reasoning_content,omitempty"`
	Prompt           string               `json:"prompt,omitempty"`
	AllowInSession   bool                 `json:"allow_in_session,omitempty"`
	TargetAgentID    string               `json:"target_agent_id,omitempty"`
	AgentName        string               `json:"agent_name,omitempty"`
	ResultContent    string               `json:"result_content,omitempty"`
	NumTasks         int                  `json:"num_tasks,omitempty"`
	Plans            []string             `json:"plans,omitempty"`
	CronTasks        []CronTaskStatus     `json:"cron_tasks,omitempty"`
	Notification     *NotificationPayload `json:"notification,omitempty"`
}

type ClientMessage struct {
	Type             string           `json:"type"`
	RequestID        string           `json:"request_id,omitempty"`
	SessionID        string           `json:"session_id,omitempty"`
	Prompt           string           `json:"prompt,omitempty"`
	Files            []ClientFile     `json:"files,omitempty"`
	CallID           string           `json:"call_id,omitempty"`
	Choice           string           `json:"choice,omitempty"`
	DesignMode       bool             `json:"design_mode,omitempty"`
	SelectedElement  *SelectedElement `json:"selected_element,omitempty"`
	ActiveDesignFile string           `json:"active_design_file,omitempty"`
	HasDrawings      bool             `json:"has_drawings,omitempty"`
}

// SelectedElement represents a selected DOM element in Design Mode
type SelectedElement struct {
	FilePath string `json:"file_path,omitempty"`
	Selector string `json:"selector,omitempty"`
	Text     string `json:"text,omitempty"`
	HTMLHint string `json:"html_hint,omitempty"`
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
	mu               sync.RWMutex
	clients          map[*Client]bool
	broadcast        chan *WSMessage
	register         chan *Client
	unregister       chan *Client
	notify           chan wsNotify // external signal: data changed
	mux              *Mux          // read-only access to runtime/agent data
	done             chan struct{}
	requests         *ActiveRequestRegistry
	sessionRevisions map[string]uint64
	revMu            sync.Mutex
}

// NewHub creates a new Hub. The Hub is not started until Run is called.
func NewHub(m *Mux) *Hub {
	return &Hub{
		clients:          make(map[*Client]bool),
		broadcast:        make(chan *WSMessage, 64),
		register:         make(chan *Client),
		unregister:       make(chan *Client),
		notify:           make(chan wsNotify, 16),
		mux:              m,
		done:             make(chan struct{}),
		requests:         NewActiveRequestRegistry(),
		sessionRevisions: make(map[string]uint64),
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
	watchdogTicker := time.NewTicker(time.Second)
	defer watchdogTicker.Stop()
	deliver := func(msg *WSMessage) {
		data, err := json.Marshal(msg)
		if err != nil {
			return
		}
		var slow []*Client
		h.mu.RLock()
		for client := range h.clients {
			select {
			case client.send <- data:
			default:
				slow = append(slow, client)
			}
		}
		h.mu.RUnlock()
		// Preserve the historical best-effort behavior for event/notification
		// messages: a saturated client drops that message but remains connected.
		// State is different because replaying it globally duplicated updates for
		// healthy clients; disconnect only the saturated state consumer instead.
		if msg.Type != "state" {
			return
		}
		for _, client := range slow {
			h.removeClient(client)
		}
	}

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
			deliver(msg)

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
			deliver(h.buildStateMessage())

		case <-h.notify:
			// 50ms non-resetting coalescing window per §5.8 of repair plan.
			// If a timer is already active, do NOT reset it — fires at most 50ms after first signal.
			if debounce == nil {
				debounce = time.NewTimer(50 * time.Millisecond)
				debounceC = debounce.C
			}

		case <-watchdogTicker.C:
			// Child model operations can be created before the first AgentEvent.
			// Refresh active runtime projections periodically so a silent request
			// still exposes its current child deadline to connected clients. A
			// terminal expiry also emits one final snapshot so the UI does not keep
			// displaying a tombstone after the registry observation window closes.
			if h.requests != nil {
				expired := h.requests.ExpireTerminals()
				if h.requests.ActiveCount() > 0 || expired {
					h.Notify()
				}
			}

		case <-h.done:
			if debounce != nil {
				debounce.Stop()
			}
			h.mu.Lock()
			for client := range h.clients {
				client.cancelAllRequests()
				close(client.send)
				if client.conn != nil {
					client.conn.Close()
				}
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

// BroadcastMessage sends an arbitrary WSMessage to all connected clients.
func (h *Hub) BroadcastMessage(msg *WSMessage) {
	select {
	case h.broadcast <- msg:
	default:
	}
}

// SendNotification pushes a desktop notification to all connected clients.
func (h *Hub) SendNotification(category, level, title, body string) {
	h.BroadcastMessage(&WSMessage{
		Type: "notification",
		Notification: &NotificationPayload{
			Category:  category,
			Level:     level,
			Title:     title,
			Body:      body,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		},
	})
}

// Close stops the Hub and closes all client connections.
func (h *Hub) Close() {
	close(h.done)
}

// NextSessionRevision increments and returns the monotonic revision counter for a session.
func (h *Hub) NextSessionRevision(sessionID string) uint64 {
	h.revMu.Lock()
	defer h.revMu.Unlock()
	if h.sessionRevisions == nil {
		h.sessionRevisions = make(map[string]uint64)
	}
	h.sessionRevisions[sessionID]++
	return h.sessionRevisions[sessionID]
}

// GetSessionRevision returns the current monotonic revision counter for a session.
func (h *Hub) GetSessionRevision(sessionID string) uint64 {
	h.revMu.Lock()
	defer h.revMu.Unlock()
	if h.sessionRevisions == nil {
		return 0
	}
	return h.sessionRevisions[sessionID]
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
		msg.Runtime = h.mux.buildRuntimeStatus(h)
	}
	if h.mux.registry != nil {
		msg.Agents = h.mux.buildAgentList()
	}
	// Include cron task list in state broadcast.
	if h.mux.toolsCfg != nil && h.mux.toolsCfg.CronStore != nil {
		tasks, err := h.mux.toolsCfg.CronStore.ListTasks(context.Background())
		if err == nil && len(tasks) > 0 {
			cronTasks := make([]CronTaskStatus, 0, len(tasks))
			for _, t := range tasks {
				cts := CronTaskStatus{
					ID:          t.ID,
					Title:       t.Title,
					TaskType:    t.TaskType,
					Expression:  t.Expression,
					Instruction: t.Instruction,
					TargetAgent: t.TargetAgent,
					Status:      t.Status,
					NextRunAt:   t.NextRunAt.Format("2006-01-02 15:04:05"),
					IsOneTime:   t.IsOneTime(),
				}
				if t.LastRunAt != nil {
					s := t.LastRunAt.Format("2006-01-02 15:04:05")
					cts.LastRunAt = &s
				}
				cronTasks = append(cronTasks, cts)
			}
			msg.CronTasks = cronTasks
		}
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
		send:           make(chan []byte, 4096),
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

// cancelAllRequests detaches all active request forwarders from this client.
// Request execution is owned by the global registry/session and survives a
// WebSocket disconnect.
func (c *Client) cancelAllRequests() {
	c.cancel()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.activeRequests = make(map[string]*activeRequest)
}
