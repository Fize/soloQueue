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
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/infra/telemetry"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/runwatch"
	"github.com/xiaobaitu/soloqueue/internal/session"
)

// ─── Chat Request Handling ──────────────────────────────────────────────────

// handleChatSend processes a chat_send message from the client.
// It resolves the target session, calls AskStream, and forwards all agent events
// as WebSocket messages to the client.
func (h *Hub) handleChatSend(client *Client, msg *ClientMessage) {
	ref, parseErr := session.ParseSessionID(msg.SessionID)
	if parseErr != nil {
		client.sendAck(msg.RequestID, msg.SessionID, "rejected", "invalid session_id")
		return
	}
	sessionID := ref.String()

	if msg.Prompt == "" {
		client.sendAck(msg.RequestID, sessionID, "rejected", "prompt cannot be empty")
		return
	}
	if err := validateChatPrompt(msg.Prompt); err != nil {
		client.sendAck(msg.RequestID, msg.SessionID, "rejected", err.Error())
		return
	}

	if h.mux == nil || h.mux.sessionMgr == nil {
		client.sendAck(msg.RequestID, sessionID, "rejected", "session manager not configured")
		return
	}

	// Resolve session.
	sess, err := h.resolveSession(sessionID)
	if err != nil {
		client.sendAck(msg.RequestID, sessionID, "rejected", err.Error())
		return
	}
	if sess == nil {
		client.sendAck(msg.RequestID, sessionID, "rejected", "session not found")
		return
	}

	// Format prompt with uploaded files.
	// Image files are base64-encoded and passed via context for multimodal models.
	finalPrompt := msg.Prompt
	var images []llm.ImageContent
	if len(msg.Files) > 0 {
		var blocks []string
		for _, f := range msg.Files {
			absPath, err := filepath.Abs(f.Path)
			if err != nil {
				absPath = f.Path
			}
			var fileContent []byte
			fileContent, err = os.ReadFile(absPath)
			size := int64(0)
			if err == nil {
				size = int64(len(fileContent))
				mimeType := http.DetectContentType(fileContent)
				if strings.HasPrefix(mimeType, "image/") {
					b64 := base64.StdEncoding.EncodeToString(fileContent)
					images = append(images, llm.ImageContent{
						Data:     b64,
						MimeType: mimeType,
					})
					blocks = append(blocks, fmt.Sprintf("- File Name: %s\n  Save Path: %s (Size: %d bytes)\n  Type: Image (identified by vision model)", f.Name, absPath, size))
					continue
				}
			}
			blocks = append(blocks, fmt.Sprintf("- File Name: %s\n  Save Path: %s (Size: %d bytes)", f.Name, absPath, size))
		}
		finalPrompt = fmt.Sprintf("%s\n\n[Uploaded files:\n%s\n]", msg.Prompt, strings.Join(blocks, "\n"))
	}

	// ── Pre-AskStream slash command interceptor (bypass LLM) ──
	trimmed := strings.TrimSpace(finalPrompt)
	lowerTrimmed := strings.ToLower(trimmed)
	switch {
	case lowerTrimmed == "/cancel":
		// Cancel the request-owned roots through the same registry used by the
		// stop button. This keeps concurrent L1 requests isolated.
		for _, active := range h.requests.GetBySessionAll(sessionID) {
			if _, owner, cancelErr := h.requests.CancelAndWaitOwned(context.Background(), sessionID, active.RequestID, client.ownerID); owner && cancelErr == nil {
				h.refreshRequestWatchdog(sessionID, active.RequestID, runwatch.CodeCancelledByUser)
				h.finalizeRequest(sessionID, active.RequestID)
			}
		}
		client.sendCommandResult(msg.RequestID, sessionID, "Task cancelled.")
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
		client.sendCommandResult(msg.RequestID, sessionID, text)
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
		client.sendCommandResult(msg.RequestID, sessionID, v)
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
			client.sendCommandResult(msg.RequestID, sessionID, errMsg)
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

	client.sendAck(msg.RequestID, sessionID, "accepted", "")

	// Request lifetime is owned by the session/global registry, not by the
	// WebSocket connection. A disconnected client merely loses its forwarder.
	reqCtx, reqCancel := context.WithCancel(context.Background())
	reqCtx = session.WithRejectBusyQueue(reqCtx)
	reqCtx = telemetry.WithTelemetryMetadata(reqCtx, telemetry.Metadata{
		RequestID: msg.RequestID,
		SessionID: sessionID,
		Origin:    telemetry.OriginDesktop,
		OwnerID:   client.ownerID,
	})
	reqCtx, generationCapture := withRequestGenerationCapture(reqCtx)
	if len(images) > 0 {
		reqCtx = context.WithValue(reqCtx, ctxwin.ImageContextKey, images)
	}
	if len(msg.Files) > 0 {
		var fileAttachments []ctxwin.FileAttachment
		for _, f := range msg.Files {
			fileAttachments = append(fileAttachments, ctxwin.FileAttachment{Name: f.Name, Path: f.Path})
		}
		reqCtx = context.WithValue(reqCtx, ctxwin.FilesContextKey, fileAttachments)
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
	requestGeneration, generationCaptured := takeRequestGeneration(generationCapture)
	if askErr != nil {
		if askErr == session.ErrQueued {
			// AskStream got queued behind an in-flight request (e.g. the L1
			// session, which the registry permits to be concurrent). Accept
			// the message into the pending queue instead of rejecting it.
			root, rootOK := h.requests.GetBySession(sessionID)
			if !rootOK {
				client.sendAck(msg.RequestID, sessionID, "rejected", ErrRequestNotFound.Error())
				client.removeActiveRequest(msg.RequestID)
				return
			}
			if reserveErr := h.requests.ReserveQueued(sessionID, msg.RequestID, root.Generation); reserveErr != nil {
				client.sendAck(msg.RequestID, sessionID, "rejected", reserveErr.Error())
				client.removeActiveRequest(msg.RequestID)
				return
			}
			sess.QueueMessage(finalPrompt, msg.RequestID)
			client.sendAck(msg.RequestID, sessionID, "queued", "")
			client.removeActiveRequest(msg.RequestID)
			return
		}
		client.sendAck(msg.RequestID, msg.SessionID, "rejected", askErr.Error())
		if generationCaptured {
			h.requests.ReleaseQueuedForGeneration(sessionID, requestGeneration)
		}
		client.removeActiveRequest(msg.RequestID)
		return
	}
	if !generationCaptured {
		h.handleMissingRequestGeneration(client, sess, msg.RequestID, sessionID, ch)
		return
	}
	// The request's route, output and outcome reach every subscriber through
	// the session transcript; this connection only drains the stream.
	forwarderStarted = true
	go h.forwardAgentEvents(client, msg.RequestID, requestGeneration, reqCancel, ch, msg.SessionID)
}

func (h *Hub) finalizeRequest(sessionID, requestID string) {
	if h.requests != nil && h.requests.Finalize(sessionID, requestID) {
		h.NextSessionRevision(sessionID)
		h.Notify()
	}
}

func (h *Hub) refreshRequestWatchdog(sessionID, requestID string, terminal runwatch.Code) {
	if h == nil || h.requests == nil {
		return
	}
	var state WatchdogState
	if h.mux != nil {
		if sess, err := h.resolveSession(sessionID); err == nil {
			if snapshot, ok := sess.WatchdogSnapshot(requestID); ok {
				state = WatchdogState{
					RunID:          snapshot.RunID,
					Phase:          snapshot.Phase,
					LastProgressAt: snapshot.LastProgressAt,
					WatchdogDueAt:  snapshot.WatchdogDueAt,
					TerminalCode:   string(snapshot.TerminalCode),
				}
			}
		}
	}
	if state.RunID == "" {
		req, validateErr := h.requests.Validate(sessionID, requestID)
		if validateErr != nil {
			return
		}
		state = WatchdogState{
			RunID: req.RunID, Phase: req.Phase, LastProgressAt: req.LastProgressAt,
			WatchdogDueAt: req.WatchdogDueAt, TerminalCode: req.TerminalCode,
		}
	}
	if terminal != "" {
		state.TerminalCode = string(terminal)
		state.WatchdogDueAt = time.Time{}
	}
	significant, setErr := h.requests.SetWatchdogState(requestID, state)
	if setErr != nil || !significant {
		return
	}
	h.NextSessionRevision(sessionID)
	h.Notify()
}

// projectLiveWatchdog overlays the authoritative RunWatch snapshot on the
// registry copy. The registry remains the request ownership source, while the
// supervisor owns rapidly changing lease timestamps and child deadlines.
func (h *Hub) projectLiveWatchdog(req ActiveRequest) ActiveRequest {
	if h == nil || h.mux == nil || req.RunID == "" {
		return req
	}
	sess, err := h.resolveSession(req.SessionID)
	if err != nil {
		return req
	}
	snapshot, ok := sess.WatchdogSnapshot(req.RunID)
	if !ok {
		return req
	}
	req.RunID = snapshot.RunID
	req.Phase = snapshot.Phase
	req.LastProgressAt = snapshot.LastProgressAt
	req.WatchdogDueAt = snapshot.WatchdogDueAt
	req.TerminalCode = string(snapshot.TerminalCode)
	return req
}

func (h *Hub) handleChatCancel(client *Client, msg *ClientMessage) {
	ref, err := session.ParseSessionID(msg.SessionID)
	if err != nil {
		return
	}
	sessionID := ref.String()

	req, owner, err := h.requests.CancelAndWaitOwned(context.Background(), sessionID, msg.RequestID, client.ownerID)
	if err != nil || !owner {
		return
	}
	if h.mux != nil && h.mux.runtimeMetrics != nil {
		h.mux.runtimeMetrics.TerminalizeAgentStream(req.AgentInstanceID, req.RequestID, "", "Task cancelled.")
	}
	// Pin the exact request's terminal state synchronously. Session cancellation
	// completion can precede the event forwarder consuming ErrorEvent.
	h.refreshRequestWatchdog(sessionID, req.RequestID, runwatch.CodeCancelledByUser)
	h.NextSessionRevision(sessionID)
	h.Notify()

	// Send immediate confirmation to client so it knows cancel was received.
	client.sendJSON(WSMessage{
		Type:      "chat_cancel_confirmed",
		RequestID: req.RequestID,
		SessionID: sessionID,
	})

	h.requests.Finalize(sessionID, req.RequestID)
	h.NextSessionRevision(sessionID)
	h.Notify()
}

type requestCanceller interface {
	CancelRun(runID, reason string) error
}

func cancelRequest(canceller requestCanceller, requestID, reason string) error {
	return canceller.CancelRun(requestID, reason)
}

const unownedRequestStreamDrainTimeout = 2 * time.Second

func (h *Hub) handleMissingRequestGeneration(client *Client, sess *session.Session, requestID, sessionID string, ch <-chan iface.AgentEvent) {
	if cleanupErr := cancelAndDrainUnownedRequestStream(sess, requestID, ch); cleanupErr != nil && h.mux != nil && h.mux.log != nil {
		h.mux.log.WarnContext(context.Background(), logger.CatApp, "chat stream cleanup failed after missing generation capture",
			"session_id", sessionID,
			"request_id", requestID,
			"err", cleanupErr.Error(),
		)
	}
	client.sendAck(requestID, sessionID, "rejected", errRequestGenerationCaptureMissing.Error())
	client.removeActiveRequest(requestID)
}

// cancelAndDrainUnownedRequestStream terminates a run that started successfully
// but cannot be assigned to an immutable request generation. Draining must begin
// before cancellation because Session's event forwarder owns a bounded output
// channel and cannot finish while that channel is full.
func cancelAndDrainUnownedRequestStream(sess *session.Session, requestID string, ch <-chan iface.AgentEvent) error {
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for range ch {
		}
	}()

	cancelErr := sess.CancelRun(requestID, "missing request generation capture")
	if errors.Is(cancelErr, session.ErrNoActiveTask) {
		// The run may have completed between AskStream returning and cancellation.
		// A naturally closed stream is still a successful cleanup.
		cancelErr = nil
	}

	timer := time.NewTimer(unownedRequestStreamDrainTimeout)
	defer timer.Stop()
	select {
	case <-drained:
		if cancelErr != nil {
			return fmt.Errorf("cancel unowned request stream: %w", cancelErr)
		}
		return nil
	case <-timer.C:
		drainErr := fmt.Errorf("timed out after %s waiting for unowned request stream to close", unownedRequestStreamDrainTimeout)
		if cancelErr != nil {
			return errors.Join(fmt.Errorf("cancel unowned request stream: %w", cancelErr), drainErr)
		}
		return drainErr
	}
}

// ─── Event Draining ─────────────────────────────────────────────────────────

// forwardAgentEvents consumes the event stream of a browser request. Output is
// delivered to every subscriber by the session transcript at the moment the
// agent produces it, so this goroutine only keeps the request's runtime
// projection current and releases request-scoped resources when the stream
// ends. The Session requires its stream to be drained.
func (h *Hub) forwardAgentEvents(client *Client, requestID string, requestGeneration uint64, cancel context.CancelFunc, ch <-chan iface.AgentEvent, sessionID string) {
	defer cancel()
	defer client.removeActiveRequest(requestID)
	defer func() {
		if h.requests != nil {
			h.requests.ReleaseQueuedForGeneration(sessionID, requestGeneration)
		}
	}()
	defer func() {
		if h.mux != nil && h.mux.l2Store != nil && strings.HasPrefix(sessionID, "l2:") {
			id := strings.TrimPrefix(sessionID, "l2:")
			h.mux.persistL2ContextUsage(sessionID, h.mux.l2Store.GetActivated(id))
		}
	}()
	for ev := range ch {
		agEv, ok := ev.(agent.AgentEvent)
		if !ok {
			continue
		}
		terminal := runwatch.Code("")
		if errorEvent, ok := agEv.(agent.ErrorEvent); ok {
			terminal = runwatch.CodeOf(errorEvent.Err)
		}
		// Token deltas only advance progress timestamps, which the Hub's
		// periodic refresh publishes; refresh on structural events.
		if !isStreamDelta(agEv) || terminal != "" {
			h.refreshRequestWatchdog(sessionID, requestID, terminal)
		}
	}
}

func isStreamDelta(ev agent.AgentEvent) bool {
	switch ev.(type) {
	case agent.ContentDeltaEvent, agent.ReasoningDeltaEvent, agent.ToolCallDeltaEvent:
		return true
	}
	return false
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
		if h.mux != nil {
			h.configureRequestLifecycle()
		}
		return sess, nil
	}

	sess := h.mux.sessionMgr.Session()
	if sess == nil {
		return nil, fmt.Errorf("no active L1 session")
	}
	h.configureRequestLifecycle()
	return sess, nil
}

// ─── Client Helpers ─────────────────────────────────────────────────────────

// sendAck tells the sender how a chat_send was handled. It is point-to-point:
// the shared outcome (queued input, committed input) travels in the session
// transcript.
func (c *Client) sendAck(requestID, sessionID, status, errText string) {
	c.sendJSON(WSMessage{Type: "chat_ack", RequestID: requestID, SessionID: sessionID, Status: status, Error: errText})
}

// sendCommandResult returns the output of a command that is not part of the
// conversation (help, version, cancel) to its sender only.
func (c *Client) sendCommandResult(requestID, sessionID, text string) {
	c.sendAck(requestID, sessionID, "accepted", "")
	c.sendJSON(WSMessage{Type: "command_result", RequestID: requestID, SessionID: sessionID, Content: text})
}

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
