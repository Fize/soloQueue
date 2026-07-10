package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

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

	// Create a derived context from client ctx so disconnect cancels this request.
	reqCtx, reqCancel := context.WithCancel(client.ctx)
	if len(images) > 0 {
		reqCtx = context.WithValue(reqCtx, ctxwin.ImageContextKey, images)
	}
	client.addActiveRequest(msg.RequestID, reqCancel)

	sess.SetIsQBot(false)

	// ── Pre-AskStream slash command interceptor (bypass LLM) ──
	trimmed := strings.TrimSpace(finalPrompt)
	lowerTrimmed := strings.ToLower(trimmed)
	switch {
	case lowerTrimmed == "/cancel":
		sess.ForceKill("User requested cancellation")
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
			"- `/cron <cron_expression/time> <task_instruction>` — Create scheduled task\n" +
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
		if err := session.InitProject(workDir); err != nil {
			client.sendJSON(WSMessage{
				Type:      "chat_chunk",
				RequestID: msg.RequestID,
				Delta:     "Init failed: " + err.Error(),
			})
			client.sendJSON(WSMessage{
				Type:             "chat_done",
				RequestID:        msg.RequestID,
				Content:          "Init failed: " + err.Error(),
				ReasoningContent: "",
			})
		} else {
			okMsg := "AGENTS.md created/updated in " + workDir
			client.sendJSON(WSMessage{
				Type:      "chat_chunk",
				RequestID: msg.RequestID,
				Delta:     okMsg,
			})
			client.sendJSON(WSMessage{
				Type:             "chat_done",
				RequestID:        msg.RequestID,
				Content:          okMsg,
				ReasoningContent: "",
			})
		}
		return

	case strings.HasPrefix(lowerTrimmed, "/cron"):
		// Cron commands need the cron handler — route through AskStream
		// fall through to normal AskStream below
	}

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
	go h.forwardAgentEvents(client, msg.RequestID, reqCancel, ch, msg.SessionID, msg.Prompt)
}

func (h *Hub) handleChatCancel(client *Client, msg *ClientMessage) {
	// Send immediate confirmation to client so it knows cancel was received.
	client.sendJSON(WSMessage{
		Type:      "chat_cancel_confirmed",
		RequestID: msg.RequestID,
	})

	// Force-kill the session (stops agent + all children immediately).
	if h.mux != nil {
		sess, err := h.resolveSession(msg.SessionID)
		if err == nil {
			sess.ForceKill("User cancelled")
			// NOTE: forceKill closes the session goroutine's out channel,
			// which causes forwardAgentEvents to receive its Done event.
			// The active request is cleaned up by forwardAgentEvents' defer.
		}
	}

	// Notify client that the task is done (cancelled).
	client.sendJSON(WSMessage{
		Type:             "chat_done",
		RequestID:        msg.RequestID,
		Content:          "Task cancelled.",
		ReasoningContent: "",
	})
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
func (h *Hub) forwardAgentEvents(client *Client, requestID string, cancel context.CancelFunc, ch <-chan iface.AgentEvent, sessionID string, prompt string) {
	defer cancel()
	defer client.removeActiveRequest(requestID)

	for ev := range ch {
		agEv, ok := ev.(agent.AgentEvent)
		if !ok {
			continue
		}

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
									Plans:     updatedEntry.Plans,
								})
							}
						}
					}
				}
			}
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
			Type:          "tool_start",
			RequestID:     requestID,
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