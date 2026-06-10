package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/session"
	"github.com/xiaobaitu/soloqueue/internal/timeline"
)

// SessionStatusResponse represents the current session status and context window history.
type SessionStatusResponse struct {
	Busy     bool                 `json:"busy"`
	Messages []SessionMessageInfo `json:"messages"`
}

type SessionMessageInfo struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

func (m *Mux) handleGetSessionStatus(w http.ResponseWriter, r *http.Request) {
	if m.sessionMgr == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "session manager not configured"})
		return
	}
	sess := m.sessionMgr.Session()
	if sess == nil {
		m.writeJSON(w, http.StatusOK, SessionStatusResponse{Busy: false, Messages: []SessionMessageInfo{}})
		return
	}

	payload := sess.CW().BuildPayload()
	msgs := make([]SessionMessageInfo, 0, len(payload))
	for _, msg := range payload {
		if msg.Role == "system" {
			continue // skip system prompt messages for cleaner chat UI
		}
		msgs = append(msgs, SessionMessageInfo{
			Role:      msg.Role,
			Content:   msg.Content,
			Timestamp: msg.Timestamp,
		})
	}

	m.writeJSON(w, http.StatusOK, SessionStatusResponse{
		Busy:     !sess.Idle(),
		Messages: msgs,
	})
}

func (m *Mux) handleAskSession(w http.ResponseWriter, r *http.Request) {
	if m.sessionMgr == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "session manager not configured"})
		return
	}
	sess := m.sessionMgr.Session()
	if sess == nil {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": "no active session"})
		return
	}

	var req struct {
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	trimmed := req.Prompt
	if trimmed == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "prompt cannot be empty"})
		return
	}

	// Trigger AskStream in a background context so it doesn't block HTTP response
	ch, err := sess.AskStream(context.Background(), trimmed)
	if err != nil {
		if errors.Is(err, session.ErrSessionBusy) {
			m.writeJSON(w, http.StatusConflict, map[string]string{"error": "session is busy"})
			return
		}
		if errors.Is(err, session.ErrQueued) {
			m.writeJSON(w, http.StatusOK, map[string]string{"status": "queued"})
			return
		}
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Consume the stream in a background goroutine so the agent actually runs
	go func() {
		for range ch {
			// consume all events to run agent task
		}
	}()

	m.writeJSON(w, http.StatusOK, map[string]string{"status": "processing"})
}

// ─── SSE Streaming endpoint ────────────────────────────────────────────────

// handleAskStream handles SSE-based streaming chat requests.
// Accepts {"prompt": "...", "session_id": "l1" | "l2:<uuid>"}
// Streams agent events as SSE frames in real time.
func (m *Mux) handleAskStream(w http.ResponseWriter, r *http.Request) {
	if m.sessionMgr == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "session manager not configured"})
		return
	}

	var req struct {
		Prompt    string `json:"prompt"`
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	trimmed := req.Prompt
	if trimmed == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "prompt cannot be empty"})
		return
	}

	// Resolve target session.
	var sess *session.Session
	isL2 := false
	l2ID := ""

	if strings.HasPrefix(req.SessionID, "l2:") {
		if m.l2Store == nil {
			m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "L2 sessions not available"})
			return
		}
		l2ID = strings.TrimPrefix(req.SessionID, "l2:")
		var err error
		sess, err = m.l2Store.Get(r.Context(), l2ID)
		if err != nil {
			m.writeJSON(w, http.StatusNotFound, map[string]string{"error": fmt.Sprintf("L2 session not found: %s", l2ID)})
			return
		}
		isL2 = true
	} else {
		sess = m.sessionMgr.Session()
		if sess == nil {
			m.writeJSON(w, http.StatusNotFound, map[string]string{"error": "no active L1 session"})
			return
		}
	}

	// Set SSE headers.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering
	flusher, ok := w.(http.Flusher)
	if !ok {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "streaming not supported"})
		return
	}

	// Use request context: client disconnect cancels the agent task.
	ch, err := sess.AskStream(r.Context(), trimmed)
	if err != nil {
		if errors.Is(err, session.ErrSessionBusy) {
			writeSSEEvent(w, flusher, "error", map[string]string{"error": "session is busy"})
			return
		}
		if errors.Is(err, session.ErrQueued) {
			writeSSEEvent(w, flusher, "error", map[string]string{"error": "message queued"})
			return
		}
		writeSSEEvent(w, flusher, "error", map[string]string{"error": err.Error()})
		return
	}

	var firstPrompt = trimmed
	var finalContent string

	// Consume events synchronously in the handler goroutine.
	for ev := range ch {
		switch e := ev.(type) {
		case agent.ContentDeltaEvent:
			writeSSEEvent(w, flusher, "content_delta", map[string]string{"delta": e.Delta})

		case agent.ReasoningDeltaEvent:
			writeSSEEvent(w, flusher, "reasoning_delta", map[string]string{"delta": e.Delta})

		case agent.ToolExecStartEvent:
			writeSSEEvent(w, flusher, "tool_start", map[string]interface{}{
				"call_id": e.CallID,
				"name":    e.Name,
				"args":    e.Args,
			})

		case agent.ToolExecDoneEvent:
			errStr := ""
			if e.Err != nil {
				errStr = e.Err.Error()
			}
			writeSSEEvent(w, flusher, "tool_done", map[string]interface{}{
				"call_id":     e.CallID,
				"name":        e.Name,
				"result":      e.Result,
				"error":       errStr,
				"duration_ms": e.Duration.Milliseconds(),
			})

		case agent.ToolNeedsConfirmEvent:
			writeSSEEvent(w, flusher, "tool_confirm", map[string]interface{}{
				"call_id": e.CallID,
				"name":    e.Name,
				"prompt":  e.Prompt,
			})
			// Auto-approve in web chat for now.
			sess.Agent.Confirm(e.CallID, "yes")

		case agent.DoneEvent:
			finalContent = e.Content
			writeSSEEvent(w, flusher, "done", map[string]string{
				"content":           e.Content,
				"reasoning_content": e.ReasoningContent,
			})

		case agent.ErrorEvent:
			writeSSEEvent(w, flusher, "error", map[string]string{"error": e.Err.Error()})

		case agent.DelegationStartedEvent:
			writeSSEEvent(w, flusher, "delegation_start", map[string]int{"num_tasks": e.NumTasks})

		case agent.DelegationCompletedEvent:
			writeSSEEvent(w, flusher, "delegation_done", map[string]string{"target_agent_id": e.TargetAgentID})
		}
		flusher.Flush()
	}

	// Auto-generate session name after first exchange for L2 sessions.
	if isL2 && l2ID != "" && firstPrompt != "" {
		title := generateSessionTitle(firstPrompt, finalContent)
		if title != "" && m.l2Store != nil {
			m.l2Store.SetName(l2ID, title)
			// Notify frontend of title change via SSE.
			writeSSEEvent(w, flusher, "session_name", map[string]string{
				"name": title,
			})
		}
	}
}

// generateSessionTitle creates a concise title from the first exchange.
// Uses the user prompt directly if short enough, otherwise returns empty.
func generateSessionTitle(prompt, response string) string {
	if prompt == "" {
		return ""
	}
	// Use the first line or first 80 chars of the prompt as title.
	title := prompt
	if idx := strings.Index(title, "\n"); idx != -1 {
		title = title[:idx]
	}
	title = strings.TrimSpace(title)
	if len([]rune(title)) > 80 {
		runes := []rune(title)
		title = string(runes[:77]) + "..."
	}
	if title == "" {
		return ""
	}
	return title
}

// ─── L2 Session Management ─────────────────────────────────────────────────

// handleListSessions returns L1 + all L2 sessions with metadata.
// Also scans disk for past L2 sessions not currently in memory.
func (m *Mux) handleListSessions(w http.ResponseWriter, r *http.Request) {
	type sessionInfo struct {
		ID          string    `json:"id"`
		Type        string    `json:"type"`
		Name        string    `json:"name"`
		Group       string    `json:"group,omitempty"`
		ProjectPath string    `json:"project_path,omitempty"`
		CreatedAt   time.Time `json:"created_at"`
	}

	sessions := []sessionInfo{}

	// L1 is always present if initialized.
	if m.sessionMgr != nil && m.sessionMgr.Session() != nil {
		sessions = append(sessions, sessionInfo{
			ID:        "l1",
			Type:      "l1",
			Name:      "L1 Orchestrator",
			CreatedAt: m.sessionMgr.Session().Created,
		})
	}

	// L2 sessions in memory.
	if m.l2Store != nil {
		for _, info := range m.l2Store.List() {
			name := info.Name
			if name == "" {
				name = fmt.Sprintf("New session (%s)", info.Group)
			}
			sessions = append(sessions, sessionInfo{
				ID:          "l2:" + info.ID,
				Type:        "l2",
				Name:        name,
				Group:       info.Group,
				ProjectPath: info.WorkDir,
				CreatedAt:   info.CreatedAt,
			})
		}
	}

	// Scan disk for past L2 sessions not currently in memory.
	seenInMemory := map[string]bool{}
	for _, s := range sessions {
		if strings.HasPrefix(s.ID, "l2:") {
			seenInMemory[strings.TrimPrefix(s.ID, "l2:")] = true
		}
	}
	timelinesDir := filepath.Join(m.workDir, "logs", "timelines")
	entries, err := os.ReadDir(timelinesDir)
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "l2-") {
				continue
			}
			id := strings.TrimPrefix(entry.Name(), "l2-")
			if seenInMemory[id] {
				continue
			}

			// Read meta JSON (preferred) or legacy "group" file.
			group := ""
			projectPath := ""
			metaFile := filepath.Join(timelinesDir, entry.Name(), "meta")
			if data, rerr := os.ReadFile(metaFile); rerr == nil {
				var meta struct {
					Group   string `json:"group"`
					WorkDir string `json:"work_dir"`
				}
				if json.Unmarshal(data, &meta) == nil {
					group = meta.Group
					projectPath = meta.WorkDir
				}
			}
			if group == "" {
				groupFile := filepath.Join(timelinesDir, entry.Name(), "group")
				if data, rerr := os.ReadFile(groupFile); rerr == nil {
					group = strings.TrimSpace(string(data))
				}
			}
			if group == "" {
				group = "unknown"
			}

			createdAt := time.Now()
			if info, rerr := entry.Info(); rerr == nil {
				createdAt = info.ModTime()
			}

			name := ""
			segments, _, _ := timeline.ReadTail(
				filepath.Join(timelinesDir, entry.Name()), "timeline", 1, "")
			for _, seg := range segments {
				for _, msg := range seg.Messages {
					if msg.Role == "user" && msg.Content != "" {
						name = msg.Content
						if len(name) > 80 {
							name = name[:77] + "..."
						}
						break
					}
				}
				if name != "" {
					break
				}
			}
			if name == "" {
				name = fmt.Sprintf("Past session (%s)", group)
			}

			sessions = append(sessions, sessionInfo{
				ID:          "l2:" + id,
				Type:        "l2",
				Name:        name,
				Group:       group,
				ProjectPath: projectPath,
				CreatedAt:   createdAt,
			})
		}
	}

	m.writeJSON(w, http.StatusOK, map[string]interface{}{"sessions": sessions})
}

// handleCreateL2Session creates a new L2 session.
// Request: {"group": "dev", "work_dir": "/path/to/project"}
// Response: {"id": "<uuid>", "group": "dev", "created_at": "..."}
func (m *Mux) handleCreateL2Session(w http.ResponseWriter, r *http.Request) {
	if m.l2Store == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "L2 sessions not available"})
		return
	}

	var req struct {
		Group   string `json:"group"`
		WorkDir string `json:"work_dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Group == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "group is required"})
		return
	}

	id := uuid.New().String()
	info, err := m.l2Store.Create(r.Context(), id, req.Group, "", req.WorkDir)
	if err != nil {
		m.writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}

	m.writeJSON(w, http.StatusCreated, info)
}

// handleDeleteL2Session destroys an L2 session by ID.
func (m *Mux) handleDeleteL2Session(w http.ResponseWriter, r *http.Request) {
	if m.l2Store == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "L2 sessions not available"})
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session id is required"})
		return
	}

	// Try in-memory removal first.
	if err := m.l2Store.Remove(r.Context(), id); err != nil {
		// Session not in memory — try removing from disk directly.
		tlDir := filepath.Join(m.workDir, "logs", "timelines", "l2-"+id)
		if info, statErr := os.Stat(tlDir); statErr == nil && info.IsDir() {
			if err := os.RemoveAll(tlDir); err != nil {
				m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			m.writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
			return
		}
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}

	m.writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ─── L2 Groups ──────────────────────────────────────────────────────────────

// handleListL2Groups returns the available leader groups for L2 session creation.
func (m *Mux) handleListL2Groups(w http.ResponseWriter, r *http.Request) {
	var groups []string
	if m.l2Store != nil {
		// Collect unique groups from all existing sessions.
		seen := map[string]bool{}
		for _, s := range m.l2Store.List() {
			if !seen[s.Group] {
				seen[s.Group] = true
				groups = append(groups, s.Group)
			}
		}
	}
	// Also include groups from templates that have leaders.
	for _, t := range m.templates {
		if t.IsLeader {
			found := false
			for _, g := range groups {
				if g == t.Group {
					found = true
					break
				}
			}
			if !found {
				groups = append(groups, t.Group)
			}
		}
	}
	if groups == nil {
		groups = []string{}
	}
	m.writeJSON(w, http.StatusOK, map[string]interface{}{"groups": groups})
}

// ─── Cancel / Clear (with session_id support) ──────────────────────────────

func (m *Mux) handleCancelSession(w http.ResponseWriter, r *http.Request) {
	sess := m.resolveSessionForModify(r)
	if sess == nil {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": "no active session"})
		return
	}

	if err := sess.CancelCurrent("User requested cancellation from desktop app"); err != nil {
		if errors.Is(err, session.ErrNoActiveTask) {
			m.writeJSON(w, http.StatusConflict, map[string]string{"error": "no active task to cancel"})
			return
		}
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	m.writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (m *Mux) handleClearSession(w http.ResponseWriter, r *http.Request) {
	sess := m.resolveSessionForModify(r)
	if sess == nil {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": "no active session"})
		return
	}

	if err := sess.Clear(); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	m.writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}

// ─── Session History ───────────────────────────────────────────────────────

// handleSessionHistory returns conversation history for a session.
// GET /api/session/history?session_id=l1|"l2:<uuid>"[&before=<cursor>]
func (m *Mux) handleSessionHistory(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session_id is required"})
		return
	}

	var dir string
	if sessionID == "l1" {
		dir = filepath.Join(m.workDir, "logs", "timelines", "session")
	} else {
		id := strings.TrimPrefix(sessionID, "l2:")
		dir = filepath.Join(m.workDir, "logs", "timelines", "l2-"+id)
	}

	allEvents, err := readAllTimelineEvents(dir)
	if err != nil {
		m.writeJSON(w, http.StatusOK, map[string]interface{}{
			"messages": []interface{}{},
			"has_more": false,
		})
		return
	}

	var lastClearIdx int = -1
	for i, evt := range allEvents {
		if evt.EventType == timeline.EventControl && evt.Control != nil && evt.Control.Action == "clear" {
			lastClearIdx = i
		}
	}
	events := allEvents
	if lastClearIdx >= 0 {
		events = allEvents[lastClearIdx+1:]
	}

	type historyMsg struct {
		ID        string                   `json:"id"`
		Role      string                   `json:"role"`
		Segments  []map[string]interface{} `json:"segments"`
		Timestamp string                   `json:"timestamp"`
	}

	type pendingToolCall struct {
		callID string
		name   string
		args   string
		segIdx int
	}

	var msgs []historyMsg
	var pendingToolCalls []pendingToolCall

	for _, evt := range events {
		if evt.EventType != timeline.EventMessage || evt.Message == nil {
			continue
		}
		msg := evt.Message
		if msg.Role == "system" {
			continue
		}

		msgID := fmt.Sprintf("hist-%d", len(msgs))

		switch msg.Role {
		case "user":
			segments := []map[string]interface{}{}
			if msg.Content != "" {
				segments = append(segments, map[string]interface{}{
					"type": "content",
					"text": msg.Content,
				})
			}
			msgs = append(msgs, historyMsg{
				ID:        msgID,
				Role:      "user",
				Segments:  segments,
				Timestamp: msg.Timestamp,
			})
		case "assistant":
			segments := []map[string]interface{}{}
			pendingToolCalls = pendingToolCalls[:0]
			if msg.ReasoningContent != "" {
				segments = append(segments, map[string]interface{}{
					"type": "thinking",
					"text": msg.ReasoningContent,
				})
			}
			for _, tc := range msg.ToolCalls {
				segIdx := len(segments)
				segments = append(segments, map[string]interface{}{
					"type":    "tool_call",
					"call_id": tc.ID,
					"name":    tc.Name,
					"args":    tc.Arguments,
					"done":    true,
				})
				pendingToolCalls = append(pendingToolCalls, pendingToolCall{
					callID: tc.ID,
					name:   tc.Name,
					args:   tc.Arguments,
					segIdx: segIdx,
				})
			}
			if msg.Content != "" {
				segments = append(segments, map[string]interface{}{
					"type": "content",
					"text": msg.Content,
				})
			}
			msgs = append(msgs, historyMsg{
				ID:        msgID,
				Role:      "assistant",
				Segments:  segments,
				Timestamp: msg.Timestamp,
			})
		case "tool":
			for _, ptc := range pendingToolCalls {
				if ptc.callID == msg.ToolCallID {
					msgs[len(msgs)-1].Segments[ptc.segIdx]["result"] = msg.Content
					msgs[len(msgs)-1].Segments[ptc.segIdx]["done"] = true
					break
				}
			}
		}
	}

	if msgs == nil {
		msgs = []historyMsg{}
	}

	m.writeJSON(w, http.StatusOK, map[string]interface{}{
		"messages": msgs,
		"has_more": false,
	})
}

// readAllTimelineEvents reads all events from all timeline files in a directory.
func readAllTimelineEvents(dir string) ([]timeline.Event, error) {
	files, err := listTimelineFiles(dir)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no timeline files")
	}
	var allEvents []timeline.Event
	for _, file := range files {
		events, err := readTimelineFile(file)
		if err != nil {
			continue
		}
		allEvents = append(allEvents, events...)
	}
	return allEvents, nil
}

// readTimelineFile reads all events from a single timeline JSONL file.
func readTimelineFile(path string) ([]timeline.Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var events []timeline.Event
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 64<<20)
	for scanner.Scan() {
		var evt timeline.Event
		if err := json.Unmarshal(scanner.Bytes(), &evt); err != nil {
			continue
		}
		events = append(events, evt)
	}
	return events, scanner.Err()
}

// listTimelineFiles finds timeline JSONL files in a directory.
// Matches: timeline.jsonl, timeline-*.jsonl (legacy), timeline-*-*.jsonl (date-size).
func listTimelineFiles(dir string) ([]string, error) {
	patterns := []string{
		filepath.Join(dir, "timeline.jsonl"),
		filepath.Join(dir, "timeline-*.jsonl"),
	}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, fmt.Errorf("timeline: glob %s: %w", pattern, err)
		}
		if len(matches) > 0 {
			return matches, nil
		}
	}
	return nil, fmt.Errorf("no timeline files in %s", dir)
}


// resolveSessionForModify resolves a session from an optional session_id field.
func (m *Mux) resolveSessionForModify(r *http.Request) *session.Session {
	var req struct {
		SessionID string `json:"session_id"`
	}
	// Best-effort parse; if body is empty, defaults to L1.
	_ = json.NewDecoder(r.Body).Decode(&req)

	if strings.HasPrefix(req.SessionID, "l2:") && m.l2Store != nil {
		id := strings.TrimPrefix(req.SessionID, "l2:")
		sess, _ := m.l2Store.Get(r.Context(), id)
		return sess
	}

	if m.sessionMgr == nil {
		return nil
	}
	return m.sessionMgr.Session()
}

// ─── SSE helpers ───────────────────────────────────────────────────────────

// sseEvent writes a single SSE event frame.
func writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, event string, data interface{}) {
	var buf bytes.Buffer
	buf.WriteString("event: ")
	buf.WriteString(event)
	buf.WriteString("\ndata: ")
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(data)
	buf.WriteString("\n")
	w.Write(buf.Bytes())
}