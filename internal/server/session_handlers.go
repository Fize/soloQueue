package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/llm"
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
		Files  []struct {
			Name string `json:"name"`
			Path string `json:"path"`
		} `json:"files"`
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

	// Format prompt with uploaded files if present.
	// Image files are base64-encoded and passed via context for multimodal models.
	// Non-image files are injected as text path markers (existing behavior).
	finalPrompt := trimmed
	var images []llm.ImageContent
	if len(req.Files) > 0 {
		var fileBlocks []string
		for _, f := range req.Files {
			absPath, err := filepath.Abs(f.Path)
			if err != nil {
				absPath = f.Path
			}
			size := int64(0)
			isImage := false
			var fileContent []byte
			fi, err := os.Stat(absPath)
			if err == nil {
				size = fi.Size()
				fileContent, _ = os.ReadFile(absPath)
			}

			if len(fileContent) > 0 {
				mimeType := http.DetectContentType(fileContent)
				if strings.HasPrefix(mimeType, "image/") {
					isImage = true
					b64 := base64.StdEncoding.EncodeToString(fileContent)
					images = append(images, llm.ImageContent{
						Data:     b64,
						MimeType: mimeType,
					})
				}
			}

			if isImage {
				block := fmt.Sprintf("- File Name: %s\n  Save Path: %s (Size: %d bytes)\n  Type: Image (identified by vision model)", f.Name, absPath, size)
				fileBlocks = append(fileBlocks, block)
			} else {
				isText := true
				if len(fileContent) > 0 {
					isText = !isBinary(fileContent)
				}
				var block string
				if isText {
					block = fmt.Sprintf("- File Name: %s\n  Save Path: %s (Size: %d bytes)\n  Type: Text (please prioritize using the Read tool to read the contents of this text file to proceed with the task.)", f.Name, absPath, size)
				} else {
					block = fmt.Sprintf("- File Name: %s\n  Save Path: %s (Size: %d bytes)\n  Type: Binary (this file is in binary format and cannot be read directly with the Read tool. You can use other tools like shell to process it.)", f.Name, absPath, size)
				}
				fileBlocks = append(fileBlocks, block)
			}
		}
		if len(fileBlocks) > 0 {
			finalPrompt = fmt.Sprintf("%s\n\n[User has uploaded a file, saved locally at:\n%s]\n", trimmed, strings.Join(fileBlocks, "\n"))
		}
	}

	// Build context with image data for multimodal models.
	askCtx := context.Background()
	if len(images) > 0 {
		askCtx = context.WithValue(askCtx, ctxwin.ImageContextKey, images)
	}

	sess.SetIsQBot(false)
	// Trigger AskStream in a background context so it doesn't block HTTP response
	ch, err := sess.AskStream(askCtx, finalPrompt)
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
		Files     []struct {
			Name string `json:"name"`
			Path string `json:"path"`
		} `json:"files"`
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

	// Format prompt with uploaded files if present.
	// Image files are base64-encoded and passed via context for multimodal models.
	finalPrompt := trimmed
	var images []llm.ImageContent
	if len(req.Files) > 0 {
		var fileBlocks []string
		for _, f := range req.Files {
			absPath, err := filepath.Abs(f.Path)
			if err != nil {
				absPath = f.Path
			}
			size := int64(0)
			isImage := false
			var fileContent []byte
			fi, err := os.Stat(absPath)
			if err == nil {
				size = fi.Size()
				fileContent, _ = os.ReadFile(absPath)
			}

			if len(fileContent) > 0 {
				mimeType := http.DetectContentType(fileContent)
				if strings.HasPrefix(mimeType, "image/") {
					isImage = true
					b64 := base64.StdEncoding.EncodeToString(fileContent)
					images = append(images, llm.ImageContent{
						Data:     b64,
						MimeType: mimeType,
					})
				}
			}

			if isImage {
				block := fmt.Sprintf("- File Name: %s\n  Save Path: %s (Size: %d bytes)\n  Type: Image (identified by vision model)", f.Name, absPath, size)
				fileBlocks = append(fileBlocks, block)
			} else {
				isText := true
				if len(fileContent) > 0 {
					isText = !isBinary(fileContent)
				}
				var block string
				if isText {
					block = fmt.Sprintf("- File Name: %s\n  Save Path: %s (Size: %d bytes)\n  Type: Text (please prioritize using the Read tool to read the contents of this text file to proceed with the task.)", f.Name, absPath, size)
				} else {
					block = fmt.Sprintf("- File Name: %s\n  Save Path: %s (Size: %d bytes)\n  Type: Binary (this file is in binary format and cannot be read directly with the Read tool. You can use other tools like shell to process it.)", f.Name, absPath, size)
				}
				fileBlocks = append(fileBlocks, block)
			}
		}
		if len(fileBlocks) > 0 {
			finalPrompt = fmt.Sprintf("%s\n\n[User has uploaded a file, saved locally at:\n%s]\n", trimmed, strings.Join(fileBlocks, "\n"))
		}
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

	sess.SetIsQBot(false)
	// Build context with image data for multimodal models.
	askCtx := r.Context()
	if len(images) > 0 {
		askCtx = context.WithValue(askCtx, ctxwin.ImageContextKey, images)
	}
	// Use request context: client disconnect cancels the agent task.
	ch, err := sess.AskStream(askCtx, finalPrompt)
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

	var firstPrompt = finalPrompt
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
				"call_id":          e.CallID,
				"name":             e.Name,
				"prompt":           e.Prompt,
				"allow_in_session": e.AllowInSession,
			})

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
			writeSSEEvent(w, flusher, "delegation_done", map[string]string{
				"target_agent_id": e.TargetAgentID,
				"agent_name":      e.TargetAgentName,
				"result_content":  e.ResultContent,
			})
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
	// Use the first line or first 30 chars of the prompt as title.
	title := prompt
	if idx := strings.Index(title, "\n"); idx != -1 {
		title = title[:idx]
	}
	title = strings.TrimSpace(title)
	if len([]rune(title)) > 30 {
		runes := []rune(title)
		title = string(runes[:27]) + "..."
	}
	if title == "" {
		return ""
	}
	return title
}

// ─── L2 Session Management ─────────────────────────────────────────────────

// leaderAgentName returns the display name of the leader agent for the given group.
func (m *Mux) leaderAgentName(group string) string {
	for _, t := range m.templates {
		if t.IsLeader && t.Group == group {
			return t.Name
		}
	}
	return ""
}

func (m *Mux) handleListSessions(w http.ResponseWriter, r *http.Request) {
	type sessionInfo struct {
		ID              string    `json:"id"`
		Type            string    `json:"type"`
		Name            string    `json:"name"`
		Group           string    `json:"group,omitempty"`
		AgentName       string    `json:"agent_name,omitempty"`
		AgentInstanceID string    `json:"agent_instance_id,omitempty"`
		ProjectPath     string    `json:"project_path,omitempty"`
		CreatedAt       time.Time `json:"created_at"`
		IsQBot          bool      `json:"is_qbot"`
		CtxwinUsed      int       `json:"ctxwin_used"`
		CtxwinLimit     int       `json:"ctxwin_limit"`
	}

	sessions := []sessionInfo{}

	// L1 is always present if initialized.
	if m.sessionMgr != nil && m.sessionMgr.Session() != nil {
		l1Sess := m.sessionMgr.Session()
		name := "L1 Orchestrator"
		agentInstanceID := ""
		if l1Sess.Agent != nil {
			if l1Sess.Agent.Def.Name != "" {
				name = l1Sess.Agent.Def.Name
			}
			agentInstanceID = l1Sess.Agent.InstanceID
		}
		var ctxwinUsed, ctxwinLimit int
		if l1Sess.CW() != nil {
			ctxwinUsed, ctxwinLimit, _ = l1Sess.CW().TokenUsage()
		}
		sessions = append(sessions, sessionInfo{
			ID:              "l1",
			Type:            "l1",
			Name:            name,
			AgentName:       name,
			AgentInstanceID: agentInstanceID,
			CreatedAt:       l1Sess.Created,
			IsQBot:          l1Sess.IsQBot(),
			CtxwinUsed:      ctxwinUsed,
			CtxwinLimit:     ctxwinLimit,
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
				ID:              "l2:" + info.ID,
				Type:            "l2",
				Name:            name,
				Group:           info.Group,
				AgentName:       m.leaderAgentName(info.Group),
				AgentInstanceID: info.AgentInstanceID,
				ProjectPath:     info.WorkDir,
				CreatedAt:       info.CreatedAt,
				CtxwinUsed:      info.CtxwinUsed,
				CtxwinLimit:     info.CtxwinLimit,
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
			name := ""
			metaFile := filepath.Join(timelinesDir, entry.Name(), "meta")
			if data, rerr := os.ReadFile(metaFile); rerr == nil {
				var meta struct {
					Name    string `json:"name"`
					Group   string `json:"group"`
					WorkDir string `json:"work_dir"`
				}
				if json.Unmarshal(data, &meta) == nil {
					name = meta.Name
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

			if name == "" {
				segments, _, _ := timeline.ReadTail(
					filepath.Join(timelinesDir, entry.Name()), "timeline", 1, "")
				for _, seg := range segments {
					for _, msg := range seg.Messages {
						if msg.Role == "user" && msg.Content != "" {
							name = msg.Content
							if len([]rune(name)) > 30 {
								name = string([]rune(name)[:27]) + "..."
							}
							break
						}
					}
					if name != "" {
						break
					}
				}
			} else {
				if len([]rune(name)) > 30 {
					name = string([]rune(name)[:27]) + "..."
				}
			}
			if name == "" {
				name = fmt.Sprintf("Past session (%s)", group)
			}

			ctxwinLimit := 0
			if m.l2Store != nil {
				ctxwinLimit = m.l2Store.DefaultContextLimit()
			}

			sessions = append(sessions, sessionInfo{
				ID:          "l2:" + id,
				Type:        "l2",
				Name:        name,
				Group:       group,
				AgentName:   m.leaderAgentName(group),
				ProjectPath: projectPath,
				CreatedAt:   createdAt,
				CtxwinLimit: ctxwinLimit,
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

	m.writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":           info.ID,
		"name":         info.Name,
		"group":        info.Group,
		"agent_name":   m.leaderAgentName(info.Group),
		"project_path": info.WorkDir,
		"created_at":   info.CreatedAt,
	})
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

func (m *Mux) handleConfirmSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string `json:"session_id"`
		CallID    string `json:"call_id"`
		Choice    string `json:"choice"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.CallID == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "call_id is required"})
		return
	}

	var sess *session.Session
	if strings.HasPrefix(req.SessionID, "l2:") && m.l2Store != nil {
		id := strings.TrimPrefix(req.SessionID, "l2:")
		sess, _ = m.l2Store.Get(r.Context(), id)
	} else if m.sessionMgr != nil {
		sess = m.sessionMgr.Session()
	}

	if sess == nil {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}

	if err := sess.Agent.Confirm(req.CallID, req.Choice); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	m.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ─── Session History ───────────────────────────────────────────────────────

// handleSessionHistory returns conversation history for a session.
// GET /api/session/history?session_id=l1|"l2:<uuid>"[&before=<cursor>&limit=<n>]
func (m *Mux) handleSessionHistory(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session_id is required"})
		return
	}

	before := r.URL.Query().Get("before") // cursor: message ID to load older messages before
	limitStr := r.URL.Query().Get("limit")
	limit := 0 // 0 = no pagination, return all
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	var dir string
	if sessionID == "l1" {
		dir = filepath.Join(m.workDir, "logs", "timelines", "default")
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
		msgIdx int
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
		// Skip ephemeral non-tool messages (delegation result summaries) —
		// only the final LLM reply should appear in visible conversation history.
		// Tool results are kept so tool_call segments get their result content.
		// We also keep delegation completed user messages to reconstruct completion state.
		if msg.Role != "tool" && msg.IsEphemeral && !strings.HasPrefix(msg.Content, "[Delegation Completed]") {
			continue
		}

		msgTimestamp := msg.Timestamp
		if msgTimestamp == "" {
			msgTimestamp = evt.Timestamp
		}

		msgID := fmt.Sprintf("hist-%d", len(msgs))

		switch msg.Role {
		case "user":
			// If this is a delegation result user message, match it back to the corresponding
			// pending delegation tool call segment by CallID and mark it as completed.
			if strings.HasPrefix(msg.Content, "[Delegation Completed]") {
				parsedResults := parseDelegationResults(msg.Content)
				for _, ptc := range pendingToolCalls {
					if !strings.HasPrefix(ptc.name, "delegate_") {
						continue
					}
					resultText, ok := parsedResults[ptc.callID]
					if ok && ptc.msgIdx < len(msgs) && ptc.segIdx < len(msgs[ptc.msgIdx].Segments) {
						msgs[ptc.msgIdx].Segments[ptc.segIdx]["result"] = resultText
						msgs[ptc.msgIdx].Segments[ptc.segIdx]["done"] = true
					}
				}
				break // skip creating a separate user message bubble
			}
			isDuplicate := false
			if len(msgs) > 0 && msgs[len(msgs)-1].Role == "user" {
				lastMsg := msgs[len(msgs)-1]
				if len(lastMsg.Segments) == 1 && lastMsg.Segments[0]["type"] == "content" {
					lastText, _ := lastMsg.Segments[0]["text"].(string)
					newText := session.StripRecalledMemories(msg.Content)
					if lastText == newText {
						t1, err1 := time.Parse(time.RFC3339Nano, lastMsg.Timestamp)
						t2, err2 := time.Parse(time.RFC3339Nano, msgTimestamp)
						if err1 == nil && err2 == nil && t2.Sub(t1) < 5*time.Second {
							isDuplicate = true
						}
					}
				}
			}
			if isDuplicate {
				break
			}

			segments := []map[string]interface{}{}
			if msg.Content != "" {
				segments = append(segments, map[string]interface{}{
					"type": "content",
					"text": session.StripRecalledMemories(msg.Content),
				})
			}
			msgs = append(msgs, historyMsg{
				ID:        msgID,
				Role:      "user",
				Segments:  segments,
				Timestamp: msgTimestamp,
			})
		case "assistant":
			segments := []map[string]interface{}{}
			newPendingStart := len(pendingToolCalls) // track new tool calls added in this batch
			if msg.ReasoningContent != "" {
				segments = append(segments, map[string]interface{}{
					"type": "thinking",
					"text": msg.ReasoningContent,
				})
			}

			lastIdx := len(msgs) - 1
			var targetMsgIdx int
			if lastIdx >= 0 && msgs[lastIdx].Role == "assistant" {
				targetMsgIdx = lastIdx
			} else {
				targetMsgIdx = len(msgs)
			}

			for _, tc := range msg.ToolCalls {
				segIdx := len(segments)
				segments = append(segments, map[string]interface{}{
					"type":    "tool_call",
					"call_id": tc.ID,
					"name":    tc.Name,
					"args":    tc.Arguments,
					"done":    false,
				})
				pendingToolCalls = append(pendingToolCalls, pendingToolCall{
					callID: tc.ID,
					name:   tc.Name,
					args:   tc.Arguments,
					msgIdx: targetMsgIdx,
					segIdx: segIdx,
				})
			}
			if msg.Content != "" {
				segments = append(segments, map[string]interface{}{
					"type": "content",
					"text": msg.Content,
				})
			}
			// Merge consecutive assistant messages to match streaming behavior.
			// The streaming frontend creates ONE assistant message per turn.
			// But the timeline may split assistant events across tool results.
			if lastIdx >= 0 && msgs[lastIdx].Role == "assistant" {
				offset := len(msgs[lastIdx].Segments)
				msgs[lastIdx].Segments = append(msgs[lastIdx].Segments, segments...)
				// Fix segIdx for newly added pending tool calls (they were computed
				// against local 'segments' but now live inside a longer merged slice).
				for i := newPendingStart; i < len(pendingToolCalls); i++ {
					pendingToolCalls[i].segIdx += offset
				}
			} else {
				msgs = append(msgs, historyMsg{
					ID:        msgID,
					Role:      "assistant",
					Segments:  segments,
					Timestamp: msgTimestamp,
				})
			}
		case "tool":
			for _, ptc := range pendingToolCalls {
				if ptc.callID == msg.ToolCallID {
					if ptc.msgIdx < len(msgs) && ptc.segIdx < len(msgs[ptc.msgIdx].Segments) {
						if strings.HasPrefix(ptc.name, "delegate_") && (msg.Content == "" || strings.HasPrefix(msg.Content, "Delegation started:")) {
							// For delegation tools, the initial tool event is just a startup placeholder.
							// Keep it as not done.
							msgs[ptc.msgIdx].Segments[ptc.segIdx]["done"] = false
						} else {
							msgs[ptc.msgIdx].Segments[ptc.segIdx]["result"] = msg.Content
							msgs[ptc.msgIdx].Segments[ptc.segIdx]["done"] = true
						}
					}
					break
				}
			}
		}
	}

	if msgs == nil {
		msgs = []historyMsg{}
	}

	// ── Cursor-based pagination ──────────────────────────────────────────────
	// msgs is ordered oldest → newest. before=cursor means "load older than cursor".
	// The cursor is the message ID of the oldest visible message.
	// When limit=0 (not specified), return all messages (backward compat).

	hasMore := false
	var cursor string

	if limit > 0 && len(msgs) > 0 {
		if before != "" {
			// Find the message with this cursor ID
			beforeIdx := -1
			for i, msg := range msgs {
				if msg.ID == before {
					beforeIdx = i
					break
				}
			}
			if beforeIdx > 0 {
				start := beforeIdx - limit
				if start < 0 {
					start = 0
				}
				msgs = msgs[start:beforeIdx]
				cursor = msgs[0].ID
				hasMore = start > 0
			} else {
				// Cursor not found or is the first message → nothing more to load
				msgs = []historyMsg{}
				hasMore = false
			}
		} else {
			// First page: return the last `limit` messages (most recent)
			if len(msgs) > limit {
				msgs = msgs[len(msgs)-limit:]
				cursor = msgs[0].ID
				hasMore = true
			} else {
				cursor = ""
				hasMore = false
			}
		}
	}

	m.writeJSON(w, http.StatusOK, map[string]interface{}{
		"messages": msgs,
		"has_more": hasMore,
		"cursor":   cursor,
	})
}

// readAllTimelineEvents reads all events from all timeline files in a directory.
// extractDelegationAgentName parses the agent name from a delegation result message.
// Format: "[Delegation Completed]
//
// Task: ...
// Assigned to: agentName
// ..."
func extractDelegationAgentName(content string) string {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "Assigned to:") {
			return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "Assigned to:"))
		}
	}
	return "Subagent"
}

// parseDelegationResults parses callID→result pairs from a "[Delegation Completed]" message content.
// Returns a map of callID -> result.
func parseDelegationResults(content string) map[string]string {
	results := make(map[string]string)
	lines := strings.Split(content, "\n")

	var currentCallID string
	var resultLines []string
	inResult := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "Task:") {
			if currentCallID != "" && inResult {
				results[currentCallID] = strings.TrimSpace(strings.Join(resultLines, "\n"))
			}
			currentCallID = ""
			resultLines = nil
			inResult = false
		} else if strings.HasPrefix(trimmed, "CallID:") {
			currentCallID = strings.TrimSpace(strings.TrimPrefix(trimmed, "CallID:"))
		} else if trimmed == "Result:" {
			inResult = true
		} else if inResult {
			resultLines = append(resultLines, line)
		}
	}
	if currentCallID != "" && inResult {
		results[currentCallID] = strings.TrimSpace(strings.Join(resultLines, "\n"))
	}
	return results
}


func readAllTimelineEvents(dir string) ([]timeline.Event, error) {
	files, err := timeline.ListTimelineFiles(dir, "timeline")
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

// handleUploadFile handles multipart file uploads.
// Saves the file to `<session_work_dir>/downloads/<filename>`.
// Accepts optional `session_id` to resolve L2 session workspace; defaults to L1.
func (m *Mux) handleUploadFile(w http.ResponseWriter, r *http.Request) {
	if m.sessionMgr == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "session manager not configured"})
		return
	}

	// Parse multipart form (max 10MB memory, larger files stored in temp)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to parse multipart form: " + err.Error()})
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing file parameter: " + err.Error()})
		return
	}
	defer file.Close()

	sessionID := r.FormValue("session_id")
	var sess *session.Session
	if strings.HasPrefix(sessionID, "l2:") {
		if m.l2Store == nil {
			m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "L2 sessions not available"})
			return
		}
		l2ID := strings.TrimPrefix(sessionID, "l2:")
		sess, err = m.l2Store.Get(r.Context(), l2ID)
		if err != nil {
			m.writeJSON(w, http.StatusNotFound, map[string]string{"error": fmt.Sprintf("L2 session not found: %s", l2ID)})
			return
		}
	} else {
		sess = m.sessionMgr.Session()
		if sess == nil {
			m.writeJSON(w, http.StatusNotFound, map[string]string{"error": "no active L1 session"})
			return
		}
	}

	workDir := sess.Agent.WorkDir
	if workDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		workDir = filepath.Join(home, ".soloqueue")
	}

	downloadsDir := filepath.Join(workDir, "downloads")
	if err := os.MkdirAll(downloadsDir, 0o755); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create downloads directory: " + err.Error()})
		return
	}

	filename := filepath.Base(header.Filename)
	destPath := filepath.Join(downloadsDir, filename)

	out, err := os.Create(destPath)
	if err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create local file: " + err.Error()})
		return
	}
	defer out.Close()

	size, err := io.Copy(out, file)
	if err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save file: " + err.Error()})
		return
	}

	m.writeJSON(w, http.StatusOK, map[string]any{
		"name": filename,
		"path": destPath,
		"size": size,
	})
}

// isBinary checks if the file content contains NUL bytes in the first 512 bytes.
func isBinary(data []byte) bool {
	n := len(data)
	if n > 512 {
		n = 512
	}
	for i := 0; i < n; i++ {
		if data[i] == 0 {
			return true
		}
	}
	return false
}