package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/session"
)

// ─── Chat Request Handling ──────────────────────────────────────────────────

// handleChatSend processes a chat_send message from the client.
// It resolves the target session, calls AskStream, and forwards all agent events
// as WebSocket messages to the client.
func (h *Hub) handleChatSend(client *Client, msg *ClientMessage) {
	if h.mux == nil || h.mux.sessionMgr == nil {
		client.sendJSON(WSMessage{
			Type:      "chat_error",
			RequestID: msg.RequestID,
			Error:     "session manager not configured",
		})
		return
	}

	if msg.Prompt == "" {
		client.sendJSON(WSMessage{
			Type:      "chat_error",
			RequestID: msg.RequestID,
			Error:     "prompt cannot be empty",
		})
		return
	}

	// Resolve session.
	sess, err := h.resolveSession(msg.SessionID)
	if err != nil {
		client.sendJSON(WSMessage{
			Type:      "chat_error",
			RequestID: msg.RequestID,
			Error:     err.Error(),
		})
		return
	}

	// Format prompt with uploaded files.
	finalPrompt := msg.Prompt
	if len(msg.Files) > 0 {
		var blocks []string
		for _, f := range msg.Files {
			blocks = append(blocks, fmt.Sprintf("- %s: %s", f.Name, f.Path))
		}
		finalPrompt = fmt.Sprintf("%s\n\n[上传文件:\n%s\n]", msg.Prompt, strings.Join(blocks, "\n"))
	}

	// Create a derived context from client ctx so disconnect cancels this request.
	reqCtx, reqCancel := context.WithCancel(client.ctx)
	client.addActiveRequest(msg.RequestID, reqCancel)

	sess.SetIsQBot(false)

	// Call AskStream.
	ch, askErr := sess.AskStream(reqCtx, finalPrompt)
	if askErr != nil {
		if askErr == session.ErrQueued {
			client.sendJSON(WSMessage{
				Type:      "chat_error",
				RequestID: msg.RequestID,
				Error:     "session is busy, message queued",
			})
			client.removeActiveRequest(msg.RequestID)
			return
		}
		client.sendJSON(WSMessage{
			Type:      "chat_error",
			RequestID: msg.RequestID,
			Error:     askErr.Error(),
		})
		client.removeActiveRequest(msg.RequestID)
		return
	}

	// Consume agent events and forward to client.
	go h.forwardAgentEvents(client, msg.RequestID, reqCancel, ch)
}

// handleChatCancel cancels an active chat request.
func (h *Hub) handleChatCancel(client *Client, msg *ClientMessage) {
	client.mu.Lock()
	req, ok := client.activeRequests[msg.RequestID]
	client.mu.Unlock()

	if !ok {
		return
	}

	req.Cancel()
	client.removeActiveRequest(msg.RequestID)

	// If delegating, also call session-level cancellation to stop the agent's
	// underlying work (e.g., LLM HTTP call). The context cancellation above
	// already breaks the forwardAgentEvents loop.
	if h.mux != nil {
		sess, err := h.resolveSession(msg.SessionID)
		if err == nil {
			_ = sess.CancelCurrent("User cancelled")
		}
	}
}

// handleToolConfirm forwards a tool confirmation choice to the agent.
func (h *Hub) handleToolConfirm(client *Client, msg *ClientMessage) {
	if h.mux == nil || h.mux.sessionMgr == nil {
		return
	}

	sess, err := h.resolveSession(msg.SessionID)
	if err != nil {
		return
	}

	_ = sess.Agent.Confirm(msg.CallID, msg.Choice)
}

// ─── Event Forwarding ───────────────────────────────────────────────────────

// forwardAgentEvents reads from the agent event channel and converts each event
// to a WSMessage pushed directly to the client's send channel.
// The goroutine exits when the channel closes or the request context is cancelled.
func (h *Hub) forwardAgentEvents(client *Client, requestID string, cancel context.CancelFunc, ch <-chan iface.AgentEvent) {
	defer cancel()
	defer client.removeActiveRequest(requestID)

	for ev := range ch {
		agEv, ok := ev.(agent.AgentEvent)
		if !ok {
			continue
		}
		wsMsg := convertAgentEvent(agEv, requestID)
		if wsMsg == nil {
			continue
		}

		// Track delegation state for Stop button logic.
		if wsMsg.Type == "delegation_start" {
			client.setRequestDelegating(requestID, true)
		}
		if wsMsg.Type == "delegation_done" {
			client.setRequestDelegating(requestID, false)
		}

		if !client.sendJSON(*wsMsg) {
			return // client disconnected
		}
	}
}

// convertAgentEvent maps an internal AgentEvent to a WSMessage.
// Returns nil for events that should not be forwarded (e.g., IterationDoneEvent).
func convertAgentEvent(ev agent.AgentEvent, requestID string) *WSMessage {
	switch e := ev.(type) {
	case agent.ContentDeltaEvent:
		return &WSMessage{
			Type:      "chat_chunk",
			RequestID: requestID,
			Delta:     e.Delta,
		}

	case agent.ReasoningDeltaEvent:
		return &WSMessage{
			Type:      "reasoning_chunk",
			RequestID: requestID,
			Delta:     e.Delta,
		}

	case agent.ToolExecStartEvent:
		return &WSMessage{
			Type:      "tool_start",
			RequestID: requestID,
			CallID:    e.CallID,
			Name:      e.Name,
			Args:      e.Args,
		}

	case agent.ToolExecDoneEvent:
		errStr := ""
		if e.Err != nil {
			errStr = e.Err.Error()
		}
		return &WSMessage{
			Type:       "tool_done",
			RequestID:  requestID,
			CallID:     e.CallID,
			Name:       e.Name,
			Result:     e.Result,
			Error:      errStr,
			DurationMS: e.Duration.Milliseconds(),
		}

	case agent.ToolNeedsConfirmEvent:
		return &WSMessage{
			Type:           "tool_confirm",
			RequestID:      requestID,
			CallID:         e.CallID,
			Name:           e.Name,
			Prompt:         e.Prompt,
			AllowInSession: e.AllowInSession,
		}

	case agent.DoneEvent:
		return &WSMessage{
			Type:             "chat_done",
			RequestID:        requestID,
			Content:          e.Content,
			ReasoningContent: e.ReasoningContent,
		}

	case agent.ErrorEvent:
		return &WSMessage{
			Type:      "chat_error",
			RequestID: requestID,
			Error:     e.Err.Error(),
		}

	case agent.DelegationStartedEvent:
		return &WSMessage{
			Type:      "delegation_start",
			RequestID: requestID,
			NumTasks:  e.NumTasks,
		}

	case agent.DelegationCompletedEvent:
		return &WSMessage{
			Type:          "delegation_done",
			RequestID:     requestID,
			TargetAgentID: e.TargetAgentID,
			AgentName:     e.TargetAgentName,
			ResultContent: e.ResultContent,
		}

	default:
		return nil
	}
}

// ─── Session Resolution ─────────────────────────────────────────────────────

// resolveSession resolves a session_id to a Session object.
// "l1" or empty → L1 session via SessionManager.
// "l2:<uuid>" → L2 session via L2SessionStore.
func (h *Hub) resolveSession(sessionID string) (*session.Session, error) {
	if strings.HasPrefix(sessionID, "l2:") {
		if h.mux.l2Store == nil {
			return nil, fmt.Errorf("L2 sessions not available")
		}
		id := strings.TrimPrefix(sessionID, "l2:")
		sess, err := h.mux.l2Store.Get(context.Background(), id)
		if err != nil {
			return nil, fmt.Errorf("L2 session not found: %s", id)
		}
		return sess, nil
	}

	sess := h.mux.sessionMgr.Session()
	if sess == nil {
		return nil, fmt.Errorf("no active L1 session")
	}
	return sess, nil
}

// ─── Client Helpers ─────────────────────────────────────────────────────────

// sendJSON marshals a WSMessage and sends it to the client's send channel.
// Returns false if the client is disconnected (send channel closed or full).
func (c *Client) sendJSON(msg WSMessage) bool {
	data, err := json.Marshal(msg)
	if err != nil {
		return true
	}
	select {
	case c.send <- data:
		return true
	default:
		return false
	}
}
