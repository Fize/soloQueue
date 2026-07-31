package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/session"
)

// ─── Chat Request Handling ──────────────────────────────────────────────────

// handleChatSend processes a chat_send message from the client.
// It resolves the target session, calls AskStream, and forwards all agent events
// as WebSocket messages to the client.
func (h *Hub) handleChatSend(client *Client, msg *ClientMessage) {
	ref, parseErr := session.ParseSessionID(msg.SessionID)
	if parseErr != nil {
		client.sendJSON(WSMessage{
			Type:      "chat_error",
			RequestID: msg.RequestID,
			SessionID: msg.SessionID,
			Error:     "invalid session_id",
		})
		return
	}
	sessionID := ref.String()
	streamStarted := false

	if msg.Prompt == "" {
		client.sendJSON(WSMessage{
			Type:      "chat_error",
			RequestID: msg.RequestID,
			SessionID: sessionID,
			Error:     "prompt cannot be empty",
		})
		return
	}

	if h.mux == nil || h.mux.sessionMgr == nil {
		client.sendJSON(WSMessage{
			Type:      "chat_error",
			RequestID: msg.RequestID,
			SessionID: sessionID,
			Error:     "session manager not configured",
		})
		return
	}

	// Resolve session.
	sess, err := h.resolveSession(sessionID)
	if err != nil {
		client.sendJSON(WSMessage{
			Type:      "chat_error",
			RequestID: msg.RequestID,
			SessionID: sessionID,
			Error:     err.Error(),
		})
		return
	}
	if sess == nil {
		client.sendJSON(WSMessage{
			Type:      "chat_error",
			RequestID: msg.RequestID,
			SessionID: sessionID,
			Error:     "session not found",
		})
		return
	}

	// Format prompt with uploaded files.
	// Image files are base64-encoded and passed via context for multimodal models.
	finalPrompt := msg.Prompt
	var images []llm.ImageContent
	if len(msg.Files) > 0 {
		var blocks []string
		for _, f := range msg.Files {
			// Detect if this is an image file by reading and sniffing MIME type.
			if fileContent, err := os.ReadFile(f.Path); err == nil {
				mimeType := http.DetectContentType(fileContent)
				if strings.HasPrefix(mimeType, "image/") {
					b64 := base64.StdEncoding.EncodeToString(fileContent)
					images = append(images, llm.ImageContent{
						Data:     b64,
						MimeType: mimeType,
					})
					blocks = append(blocks, fmt.Sprintf("- %s: %s (image, recognized by visual model)", f.Name, f.Path))
					continue
				}
			}
			blocks = append(blocks, fmt.Sprintf("- %s: %s", f.Name, f.Path))
		}
		finalPrompt = fmt.Sprintf("%s\n\n[Uploaded files:\n%s\n]", msg.Prompt, strings.Join(blocks, "\n"))
	}

	// ── Pre-AskStream slash command interceptor (bypass LLM) ──
	trimmed := strings.TrimSpace(finalPrompt)
	lowerTrimmed := strings.ToLower(trimmed)
	switch {
	case lowerTrimmed == "/cancel":
		_ = sess.CancelCurrent("User requested cancellation")
		client.sendJSON(WSMessage{
			Type:             "chat_done",
			RequestID:        msg.RequestID,
			Content:          "Task cancelled.",
			ReasoningContent: "",
		})
		return

	case lowerTrimmed == "/help" || lowerTrimmed == "/?":
		text := "Available commands:\n" +
			"- `/help` or `/?` — View available commands\n" +
			"- `/cancel` — Cancel current task\n" +
			"- `/clear` — Clear dialogue history\n" +
			"- `/compact` — Compact context window (no memory save)\n" +
			"- `/init` — Create/update AGENTS.md in the project directory (L2 sessions only)\n" +
			"- `/version` — View version number\n" +
			"- `/l0` — Lock routing level to L0 (conversational)\n" +
			"- `/l1` — Lock routing level to L1 (single file modification)\n" +
			"- `/l2` — Lock routing level to L2 (multi-file modification)\n" +
			"- `/l3` — Lock routing level to L3 (complex architecture refactoring)"
		client.sendJSON(WSMessage{
			Type:      "chat_chunk",
			RequestID: msg.RequestID,
			Delta:     text,
		})
		client.sendJSON(WSMessage{
			Type:             "chat_done",
			RequestID:        msg.RequestID,
			Content:          text,
			ReasoningContent: "",
		})
		return

	// Note: /clear and /compact are handled by session.AskStream's pre-inFlight
	// interceptor, which properly serializes access via inFlight CAS.
	// Processing them here bypasses inFlight and causes CW corruption.

	case lowerTrimmed == "/version":
		v := session.Version
		if v == "" {
			v = "SoloQueue"
		} else {
			v = "SoloQueue " + v
		}
		client.sendJSON(WSMessage{
			Type:      "chat_chunk",
			RequestID: msg.RequestID,
			Delta:     v,
		})
		client.sendJSON(WSMessage{
			Type:             "chat_done",
			RequestID:        msg.RequestID,
			Content:          v,
			ReasoningContent: "",
		})
		return

	case lowerTrimmed == "/init":
		// /init is only available in L2 sessions with a project workDir.
		// Resolve workDir and build an LLM prompt for project initialization.
		// The prompt is then sent through AskStream so the agent can explore
		// the project with tools and produce a context-aware AGENTS.md.
		workDir := ""
		if strings.HasPrefix(msg.SessionID, "l2:") {
			id := strings.TrimPrefix(msg.SessionID, "l2:")
			if h.mux.l2Store != nil {
				if entry := h.mux.l2Store.GetEntry(id); entry != nil {
					if entry.WorkDir != "" {
						workDir = filepath.Clean(expandTilde(entry.WorkDir))
					}
				}
			}
		}
		if workDir == "" {
			errMsg := "/init is only available in L2 sessions with a project directory. Create an L2 session first."
			client.sendJSON(WSMessage{
				Type:      "chat_chunk",
				RequestID: msg.RequestID,
				Delta:     errMsg,
			})
			client.sendJSON(WSMessage{
				Type:             "chat_done",
				RequestID:        msg.RequestID,
				Content:          errMsg,
				ReasoningContent: "",
			})
			return
		}
		// Build the LLM prompt and let it fall through to AskStream.
		finalPrompt = session.BuildInitPrompt(workDir)
		// Skip DesignMode additions — they would corrupt the init prompt.
		msg.DesignMode = false

	}

	if msg.DesignMode {
		designDir := ""
		if strings.HasPrefix(msg.SessionID, "l2:") {
			id := strings.TrimPrefix(msg.SessionID, "l2:")
			if h.mux.l2Store != nil {
				if entry := h.mux.l2Store.GetEntry(id); entry != nil {
					if entry.WorkDir != "" {
						designDir = filepath.Join(filepath.Clean(expandTilde(entry.WorkDir)), ".soloqueue", "design")
					} else {
						designDir = filepath.Join(h.mux.workDir, "workspace", entry.Group, "design")
					}
				}
			}
		} else {
			designDir = filepath.Join(h.mux.workDir, "design")
		}

		if msg.SelectedElement != nil && msg.SelectedElement.Selector != "" {
			elementBlock := fmt.Sprintf("\n\n[SELECTED DOM ELEMENT:\n- Selector: `%s`\n- Content: `%s`\n- HTML Hint: `%s`\n- File: `%s`]",
				msg.SelectedElement.Selector,
				msg.SelectedElement.Text,
				msg.SelectedElement.HTMLHint,
				msg.SelectedElement.FilePath,
			)
			finalPrompt += elementBlock
		}

		if msg.HasDrawings && msg.ActiveDesignFile != "" {
			filename := filepath.Base(msg.ActiveDesignFile)
			drawingBlock := fmt.Sprintf("\n\n[USER DRAWINGS/ANNOTATIONS DETECTED: The user has drawn visual markings/annotations on the HTML preview for file `%s`. The drawing coordinates/strokes are saved directly in `<script id=\"sketch-data\" type=\"application/json\">` inside that HTML file. You MUST read this file and pay close attention to where the user circled, pointed, or highlighted to correctly address the request.]", filename)
			finalPrompt += drawingBlock
		}

		if designDir != "" {
			directive := fmt.Sprintf("\n\n[CRITICAL DIRECTIVE: Design preview mode is active. You MUST save any previewable HTML, CSS, JS, asset files, or drawings directly to the designated design directory: `%s`. Storing them in any other directory is a STRICT PROTOCOL VIOLATION and will break the user's real-time interface rendering. Ensure your files are generated or modified exactly in this location.]", designDir)
			finalPrompt += directive
		}
	}

	// Reserve request in global ActiveRequestRegistry.
	// Single-flight sessions (L2 and the default session) are serial, but a
	// concurrent user message is NOT rejected: it is queued into the session's
	// pending queue and injected before the agent's next LLM API call.
	_, err = h.requests.Reserve(sessionID, msg.RequestID, "")
	if err != nil {
		if errors.Is(err, ErrSessionBusy) {
			sess.QueueMessage(finalPrompt)
			client.sendJSON(WSMessage{
				Type:      "chat_queued",
				RequestID: msg.RequestID,
				SessionID: sessionID,
				Error:     "session is busy; message queued and will be processed in the current turn",
			})
			return
		}
		client.sendJSON(WSMessage{
			Type:      "session_busy",
			RequestID: msg.RequestID,
			SessionID: sessionID,
			Error:     "session is currently busy processing another request",
		})
		return
	}
	defer func() {
		if !streamStarted {
			h.finalizeRequest(sessionID, msg.RequestID)
		}
	}()
	h.NextSessionRevision(sessionID)
	h.Notify()

	// Request lifetime is owned by the session/global registry, not by the
	// WebSocket connection. A disconnected client merely loses its forwarder.
	reqCtx, reqCancel := context.WithCancel(context.Background())
	reqCtx = session.WithRejectBusyQueue(reqCtx)
	if len(images) > 0 {
		reqCtx = context.WithValue(reqCtx, ctxwin.ImageContextKey, images)
	}
	client.addActiveRequest(msg.RequestID, reqCancel)
	forwarderStarted := false
	defer func() {
		if !forwarderStarted {
			reqCancel()
			client.removeActiveRequest(msg.RequestID)
		}
	}()

	sess.SetIsQBot(false)

	// Call AskStream.
	ch, askErr := sess.AskStream(reqCtx, finalPrompt)
	if askErr != nil {
		if askErr == session.ErrQueued {
			// AskStream got queued behind an in-flight request (e.g. the L1
			// session, which the registry permits to be concurrent). Accept
			// the message into the pending queue instead of rejecting it.
			sess.QueueMessage(finalPrompt)
			client.sendJSON(WSMessage{
				Type:      "chat_queued",
				RequestID: msg.RequestID,
				SessionID: sessionID,
				Error:     "session is busy; message queued and will be processed in the current turn",
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

	// Routing is complete before AskStream returns. Send its request-scoped
	// result before any reasoning/content/tool event can be forwarded.
	client.sendJSON(buildChatRouteMessage(sess, msg.RequestID, msg.SessionID))
	_ = h.requests.SetRoute(msg.RequestID, sess.Agent.InstanceID)
	h.NextSessionRevision(sessionID)
	h.Notify()

	// Notify desktop when classification degraded (LLM error, fallback used).
	if cw := sess.ClassifierWarning(); cw != "" {
		client.sendJSON(WSMessage{
			Type: "notification",
			Notification: &NotificationPayload{
				Category:  "classifier",
				Level:     "warning",
				Title:     "Task classification degraded",
				Body:      cw,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			},
		})
	}

	// Consume agent events and forward to client.
	streamStarted = true
	forwarderStarted = true
	go h.forwardAgentEvents(client, msg.RequestID, reqCancel, ch, msg.SessionID, msg.Prompt)
}

func (h *Hub) finalizeRequest(sessionID, requestID string) {
	if h.requests != nil && h.requests.Finalize(sessionID, requestID) {
		h.NextSessionRevision(sessionID)
		h.Notify()
	}
}

func buildChatRouteMessage(sess *session.Session, requestID, sessionID string) WSMessage {
	modelID := sess.Agent.EffectiveModelID()
	if modelID == "" {
		modelID = sess.Agent.Def.ModelID
	}
	providerID := sess.Agent.EffectiveProviderID()
	if providerID == "" {
		providerID = sess.Agent.Def.ProviderID
	}
	return WSMessage{
		Type:            "chat_route",
		RequestID:       requestID,
		SessionID:       sessionID,
		TaskType:        sess.Agent.EffectiveTaskType(),
		ModelID:         modelID,
		ProviderID:      providerID,
		AgentInstanceID: sess.Agent.InstanceID,
	}
}

func (h *Hub) handleChatCancel(client *Client, msg *ClientMessage) {
	ref, err := session.ParseSessionID(msg.SessionID)
	if err != nil {
		return
	}
	sessionID := ref.String()

	// Validate ownership via global ActiveRequestRegistry
	req, err := h.requests.Validate(sessionID, msg.RequestID)
	if err != nil {
		return
	}

	_ = h.requests.SetState(req.RequestID, RequestStateCancelling)
	h.NextSessionRevision(sessionID)
	h.Notify()

	// Send immediate confirmation to client so it knows cancel was received.
	client.sendJSON(WSMessage{
		Type:      "chat_cancel_confirmed",
		RequestID: req.RequestID,
		SessionID: sessionID,
	})

	if h.mux != nil {
		sess, err := h.resolveSession(sessionID)
		if err == nil {
			_ = sess.CancelCurrent("User cancelled")
		}
	}

	// Notify client that the task is done (cancelled).
	client.sendJSON(WSMessage{
		Type:             "chat_done",
		RequestID:        req.RequestID,
		SessionID:        sessionID,
		Content:          "Task cancelled.",
		ReasoningContent: "",
	})

	h.requests.Finalize(sessionID, req.RequestID)
	h.NextSessionRevision(sessionID)
	h.Notify()
}

// handleToolConfirm forwards a tool confirmation choice to the agent.
func (h *Hub) handleToolConfirm(client *Client, msg *ClientMessage) {
	if h.mux == nil || h.mux.sessionMgr == nil {
		return
	}

	ref, err := session.ParseSessionID(msg.SessionID)
	if err != nil {
		return
	}
	sessionID := ref.String()

	if msg.RequestID != "" {
		if _, err := h.requests.Validate(sessionID, msg.RequestID); err != nil {
			return
		}
		if err := h.requests.ResolvePendingCall(msg.RequestID, msg.CallID); err != nil {
			// Call ID does not belong to active request — drop
			return
		}
	}

	sess, err := h.resolveSession(sessionID)
	if err != nil {
		return
	}

	_ = sess.Agent.Confirm(msg.CallID, msg.Choice)
}

// ─── Event Forwarding ───────────────────────────────────────────────────────

// forwardAgentEvents reads from the agent event channel and converts each event
// to a WSMessage pushed directly to the client's send channel.
// The goroutine exits when the channel closes or the request context is cancelled.
func (h *Hub) forwardAgentEvents(client *Client, requestID string, cancel context.CancelFunc, ch <-chan iface.AgentEvent, sessionID string, prompt string) {
	defer cancel()
	defer client.removeActiveRequest(requestID)
	defer h.finalizeRequest(sessionID, requestID)
	defer func() {
		if h.mux != nil && h.mux.l2Store != nil && strings.HasPrefix(sessionID, "l2:") {
			id := strings.TrimPrefix(sessionID, "l2:")
			h.mux.persistL2ContextUsage(sessionID, h.mux.l2Store.GetActivated(id))
		}
	}()

	// streamBatchInterval is the maximum time a chat_chunk/reasoning_chunk
	// delta is held before being flushed. This microbatch lets us collapse N
	// per-token deltas that arrive within a single frame into one WebSocket
	// frame, which dramatically reduces React commit frequency on the client
	// (and the MarkdownPreview reparse cost that comes with each commit).
	// Other event types (tool_*, chat_done, chat_error, ...) bypass the
	// batcher and are flushed immediately.
	const streamBatchInterval = 30 * time.Millisecond

	// Batcher state. A batch contains only one delta type: keeping independent
	// content/reasoning buffers and flushing them in a fixed order would reorder
	// a reasoning→content transition that occurs within the same 30 ms window.
	var (
		pendingDelta strings.Builder
		pendingType  string
		flushTimer   *time.Timer
		flushC       <-chan time.Time
	)

	deliveryAttached := true
	flush := func() {
		if pendingType != "" {
			if deliveryAttached && !client.sendJSON(WSMessage{
				Type:      pendingType,
				RequestID: requestID,
				SessionID: sessionID,
				Delta:     pendingDelta.String(),
			}) {
				deliveryAttached = false
			}
			pendingDelta.Reset()
			pendingType = ""
		}
		if flushTimer != nil {
			flushTimer.Stop()
			flushTimer = nil
		}
		flushC = nil
	}

	appendDelta := func(msgType, delta string) {
		if pendingType != "" && pendingType != msgType {
			flush()
		}
		pendingType = msgType
		pendingDelta.WriteString(delta)
		if flushTimer == nil {
			flushTimer = time.NewTimer(streamBatchInterval)
			flushC = flushTimer.C
		}
	}

	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				flush()
				return
			}
			agEv, ok := ev.(agent.AgentEvent)
			if !ok {
				continue
			}

			// Fast path: batchable high-frequency deltas.
			if cd, ok := agEv.(agent.ContentDeltaEvent); ok {
				appendDelta("chat_chunk", cd.Delta)
				continue
			}
			if rd, ok := agEv.(agent.ReasoningDeltaEvent); ok {
				appendDelta("reasoning_chunk", rd.Delta)
				continue
			}

			// Structural event: flush pending text first so the client sees
			// the new structural frame AFTER the full text preceding it.
			flush()

			// Auto-generate session name after first exchange for L2 sessions.
			if doneEv, ok := agEv.(agent.DoneEvent); ok {
				if strings.HasPrefix(sessionID, "l2:") && h.mux.l2Store != nil {
					l2ID := strings.TrimPrefix(sessionID, "l2:")
					if h.mux.l2Store.GetName(l2ID) == "" {
						title := generateSessionTitle(prompt, doneEv.Content)
						if title != "" {
							h.mux.l2Store.SetName(l2ID, title)
							client.sendJSON(WSMessage{
								Type:      "session_name",
								RequestID: requestID,
								SessionID: sessionID,
								Name:      title,
							})
						}
					}
				}
			}

			if toolDoneEv, ok := agEv.(agent.ToolExecDoneEvent); ok && toolDoneEv.Err == nil {
				if strings.HasPrefix(sessionID, "l2:") && h.mux.l2Store != nil {
					l2ID := strings.TrimPrefix(sessionID, "l2:")
					var paths []string
					if toolDoneEv.Name == "Write" || toolDoneEv.Name == "Edit" || toolDoneEv.Name == "MultiEdit" {
						var res struct {
							Path string `json:"path"`
						}
						if json.Unmarshal([]byte(toolDoneEv.Result), &res) == nil && res.Path != "" {
							paths = append(paths, res.Path)
						}
					} else if toolDoneEv.Name == "MultiWrite" {
						var res struct {
							Files []struct {
								Path string `json:"path"`
							} `json:"files"`
						}
						if json.Unmarshal([]byte(toolDoneEv.Result), &res) == nil {
							for _, f := range res.Files {
								if f.Path != "" {
									paths = append(paths, f.Path)
								}
							}
						}
					}

					if len(paths) > 0 {
						entry := h.mux.l2Store.GetEntry(l2ID)
						if entry != nil && entry.Group != "" {
							planDir := filepath.Join(h.mux.workDir, "plan", entry.Group)
							prefix := planDir + string(filepath.Separator)
							updated := false
							for _, p := range paths {
								if strings.HasPrefix(p, prefix) {
									h.mux.l2Store.UpdatePlanStatus(l2ID, p)
									updated = true
								}
							}
							if updated {
								updatedEntry := h.mux.l2Store.GetEntry(l2ID)
								if updatedEntry != nil {
									client.sendJSON(WSMessage{
										Type:      "session_plans",
										RequestID: requestID,
										SessionID: sessionID,
										Plans:     updatedEntry.Plans,
									})
								}
							}
						}
					}
				}
			}

			if confirmEv, ok := agEv.(agent.ToolNeedsConfirmEvent); ok {
				_ = h.requests.RegisterPendingCall(requestID, confirmEv.CallID)
			}

			wsMsg := convertAgentEvent(agEv, requestID, sessionID)
			if wsMsg == nil {
				continue
			}

			// Track delegation state for Stop button and global registry.
			if wsMsg.Type == "delegation_start" {
				client.setRequestDelegating(requestID, true)
				_ = h.requests.SetDelegating(requestID, true)
				h.NextSessionRevision(sessionID)
				h.Notify()
			}
			if wsMsg.Type == "delegation_done" {
				client.setRequestDelegating(requestID, false)
				_ = h.requests.SetDelegating(requestID, false)
				h.NextSessionRevision(sessionID)
				h.Notify()
			}

			if deliveryAttached && !client.sendJSON(*wsMsg) {
				deliveryAttached = false
			}

		case <-flushC:
			flush()
		}
	}
}

// convertAgentEvent maps an internal AgentEvent to a WSMessage.
// Returns nil for events that should not be forwarded (e.g., IterationDoneEvent).
func convertAgentEvent(ev agent.AgentEvent, requestID, sessionID string) *WSMessage {
	switch e := ev.(type) {
	case agent.ToolExecStartEvent:
		return &WSMessage{
			Type:          "tool_start",
			RequestID:     requestID,
			SessionID:     sessionID,
			CallID:        e.CallID,
			Name:          e.Name,
			Args:          e.Args,
			TargetAgentID: e.TargetAgentID,
		}

	case agent.ToolExecDoneEvent:
		errStr := ""
		if e.Err != nil {
			errStr = e.Err.Error()
		}
		return &WSMessage{
			Type:       "tool_done",
			RequestID:  requestID,
			SessionID:  sessionID,
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
			SessionID:      sessionID,
			CallID:         e.CallID,
			Name:           e.Name,
			Prompt:         e.Prompt,
			AllowInSession: e.AllowInSession,
		}

	case agent.DoneEvent:
		return &WSMessage{
			Type:             "chat_done",
			RequestID:        requestID,
			SessionID:        sessionID,
			Content:          e.Content,
			ReasoningContent: e.ReasoningContent,
		}

	case agent.ErrorEvent:
		return &WSMessage{
			Type:      "chat_error",
			RequestID: requestID,
			SessionID: sessionID,
			Error:     e.Err.Error(),
		}

	case agent.DelegationStartedEvent:
		return &WSMessage{
			Type:      "delegation_start",
			RequestID: requestID,
			SessionID: sessionID,
			NumTasks:  e.NumTasks,
		}

	case agent.DelegationCompletedEvent:
		return &WSMessage{
			Type:          "delegation_done",
			RequestID:     requestID,
			SessionID:     sessionID,
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
// Uses recover to handle send on closed channel — the send channel is closed
// by removeClient/hub shutdown, which can race with forwardAgentEvents goroutines.
func (c *Client) sendJSON(msg WSMessage) (ok bool) {
	data, err := json.Marshal(msg)
	if err != nil {
		return false
	}
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	select {
	case c.send <- data:
		return true
	default:
		return false
	}
}
