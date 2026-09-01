package server

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/xiaobaitu/soloqueue/internal/infra/telemetry"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/memory/timeline"
	"github.com/xiaobaitu/soloqueue/internal/session"
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

	// Build context with file and image data.
	askCtx := telemetry.WithTelemetryMetadata(context.Background(), telemetry.Metadata{
		RequestID: uuid.NewString(),
		SessionID: sess.TargetID,
		Origin:    telemetry.OriginAPI,
	})
	var fileAttachments []ctxwin.FileAttachment
	for _, f := range req.Files {
		fileAttachments = append(fileAttachments, ctxwin.FileAttachment{
			Name: f.Name,
			Path: f.Path,
		})
	}
	if len(fileAttachments) > 0 {
		askCtx = context.WithValue(askCtx, ctxwin.FilesContextKey, fileAttachments)
	}
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

func (m *Mux) persistL2ContextUsage(sessionID string, sess *session.Session) {
	if sess == nil || sess.CW() == nil || !strings.HasPrefix(sessionID, "l2:") {
		return
	}
	id := strings.TrimPrefix(sessionID, "l2:")
	used, limit, _ := sess.CW().TokenUsage()
	_ = session.MergeAndSave(m.workDir, id, func(meta *session.SessionMeta) {
		meta.CtxwinUsed = used
		meta.CtxwinLimit = limit
		meta.CtxwinUpdated = time.Now().UTC()
	})
}

func (m *Mux) handleListSessions(w http.ResponseWriter, r *http.Request) {
	type sessionInfo struct {
		ID              string `json:"id"`
		Type            string `json:"type"`
		Name            string `json:"name"`
		Group           string `json:"group,omitempty"`
		AgentName       string `json:"agent_name,omitempty"`
		AgentInstanceID string `json:"agent_instance_id,omitempty"`
		ProjectPath     string `json:"project_path,omitempty"`
		// DesignDir is the absolute path to the design assets directory for this
		// session. For project sessions: <project_path>/.soloqueue/design/.
		// For no-project sessions: <workDir>/design/.
		DesignDir   string    `json:"design_dir,omitempty"`
		CreatedAt   time.Time `json:"created_at"`
		IsQBot      bool      `json:"is_qbot"`
		CtxwinUsed  int       `json:"ctxwin_used"`
		CtxwinLimit int       `json:"ctxwin_limit"`
		Plans       []string  `json:"plans,omitempty"`
	}

	// resolveDesignDir computes the design directory for a session.
	// Mirrors the effectiveWorkDir resolution in agent/factory.go DefaultFactory.Create:
	//   - projectPath != "": agent works in projectPath → design at <projectPath>/.soloqueue/design/
	//   - projectPath == "" && group != "": agent works in workDir/workspace/<group>/ → design at same path /design/
	//   - fallback: workDir/design/
	resolveDesignDir := func(projectPath, group string) string {
		if projectPath != "" {
			return filepath.Join(filepath.Clean(expandTilde(projectPath)), ".soloqueue", "design")
		}
		if group != "" {
			return filepath.Join(m.workDir, "workspace", group, "design")
		}
		return filepath.Join(m.workDir, "design")
	}

	sessions := []sessionInfo{}
	resolveL2ContextUsage := func(id, group, workDir string, activeUsed, activeLimit int) (int, int) {
		if activeLimit > 0 {
			return activeUsed, activeLimit
		}
		if meta, err := session.LoadMeta(m.workDir, id); err == nil && meta.CtxwinLimit > 0 {
			return meta.CtxwinUsed, meta.CtxwinLimit
		}
		if m.l2Store == nil || m.l2Store.Builder() == nil {
			return 0, 0
		}
		used, limit, err := m.l2Store.Builder().ReadL2ContextUsage(r.Context(), id, group, workDir)
		if err != nil {
			return 0, m.l2Store.DefaultContextLimit()
		}
		_ = session.MergeAndSave(m.workDir, id, func(meta *session.SessionMeta) {
			meta.CtxwinUsed = used
			meta.CtxwinLimit = limit
			meta.CtxwinUpdated = time.Now().UTC()
		})
		return used, limit
	}

	// L1 is always present if initialized.
	if m.sessionMgr != nil && m.sessionMgr.Session() != nil {
		l1Sess := m.sessionMgr.Session()
		name := "L1 Orchestrator"
		agentInstanceID := ""
		if a := l1Sess.CurrentAgent(); a != nil {
			if a.Def.Name != "" {
				name = a.Def.Name
			}
			agentInstanceID = a.InstanceID
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

	// L2 sessions in conversation.
	if m.l2Store != nil {
		for _, info := range m.l2Store.List() {
			activeUsed, activeLimit := 0, 0
			if info.AgentInstanceID != "" {
				activeUsed, activeLimit = info.CtxwinUsed, info.CtxwinLimit
			}
			ctxwinUsed, ctxwinLimit := resolveL2ContextUsage(
				info.ID, info.Group, info.WorkDir, activeUsed, activeLimit,
			)
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
				DesignDir:       resolveDesignDir(info.WorkDir, info.Group),
				CreatedAt:       info.CreatedAt,
				CtxwinUsed:      ctxwinUsed,
				CtxwinLimit:     ctxwinLimit,
				Plans:           info.Plans,
			})
		}
	}

	// Scan disk for past L2 sessions not currently in conversation.
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

			// Read meta.json (with legacy meta/level/baseline/group migration
			// handled inside LoadMeta). When group cannot be recovered the
			// session is skipped rather than fabricated.
			loaded, lerr := session.LoadMeta(m.workDir, id)
			if lerr != nil {
				continue
			}
			group := loaded.Group
			projectPath := loaded.WorkDir
			name := loaded.Name
			var plans []string
			if len(loaded.Plans) > 0 {
				plans = append(plans, loaded.Plans...)
			}

			createdAt := time.Now()
			if info, rerr := entry.Info(); rerr == nil {
				createdAt = info.ModTime()
			}

			if name == "" {
				// Scan oldest-first to find the first real user message.
				// Skip ephemeral messages (e.g. [Delegation Completed]) which carry role=user
				// but are not real user prompts.
				if resolved := session.ResolveSessionNameFromTimeline(m.workDir, id); resolved != "" {
					name = resolved
					// Write back to meta.json so future calls don't rescan the timeline.
					// MergeAndSave preserves every other field (group, plans, level, baseline).
					// Failure is non-fatal: the next list call will re-resolve the name.
					_ = session.MergeAndSave(m.workDir, id, func(sm *session.SessionMeta) {
						sm.Name = resolved
					})
				}
			} else if len([]rune(name)) > 30 {
				name = string([]rune(name)[:27]) + "..."
			}
			if name == "" {
				name = fmt.Sprintf("Past session (%s)", group)
			}

			ctxwinUsed, ctxwinLimit := resolveL2ContextUsage(id, group, projectPath, 0, 0)

			sessions = append(sessions, sessionInfo{
				ID:          "l2:" + id,
				Type:        "l2",
				Name:        name,
				Group:       group,
				AgentName:   m.leaderAgentName(group),
				ProjectPath: projectPath,
				DesignDir:   resolveDesignDir(projectPath, group),
				CreatedAt:   createdAt,
				CtxwinUsed:  ctxwinUsed,
				CtxwinLimit: ctxwinLimit,
				Plans:       plans,
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

	designDir := filepath.Join(m.workDir, "workspace", info.Group, "design")
	if info.WorkDir != "" {
		designDir = filepath.Join(filepath.Clean(expandTilde(info.WorkDir)), ".soloqueue", "design")
	}
	m.writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":           info.ID,
		"name":         info.Name,
		"group":        info.Group,
		"agent_name":   m.leaderAgentName(info.Group),
		"project_path": info.WorkDir,
		"design_dir":   designDir,
		"created_at":   info.CreatedAt,
		"plans":        []string{},
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
	var req struct {
		SessionID string `json:"session_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	sess := m.resolveSession(r.Context(), req.SessionID)
	if sess == nil {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": "no active session"})
		return
	}

	_ = sess.CancelCurrent("User requested cancellation")

	m.writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (m *Mux) handleClearSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string `json:"session_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	sess := m.resolveSession(r.Context(), req.SessionID)
	if sess == nil {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": "no active session"})
		return
	}

	if err := sess.Clear(); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	m.persistL2ContextUsage(req.SessionID, sess)

	m.writeJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}

func (m *Mux) handleRewindSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string `json:"session_id"`
		TargetTs  string `json:"target_ts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	sess := m.resolveSession(r.Context(), req.SessionID)
	if sess == nil {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": "no active session"})
		return
	}

	ts, err := time.Parse(time.RFC3339Nano, req.TargetTs)
	if err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid timestamp format"})
		return
	}

	if err := sess.Rewind(ts); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	m.persistL2ContextUsage(req.SessionID, sess)

	m.writeJSON(w, http.StatusOK, map[string]string{"status": "rewound"})
}

func (m *Mux) handleDeleteSessionMessages(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string   `json:"session_id"`
		TargetTs  []string `json:"target_ts_list"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	sess := m.resolveSession(r.Context(), req.SessionID)
	if sess == nil {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": "no active session"})
		return
	}

	var tsList []time.Time
	for _, tsStr := range req.TargetTs {
		ts, err := time.Parse(time.RFC3339Nano, tsStr)
		if err != nil {
			m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid timestamp format"})
			return
		}
		tsList = append(tsList, ts)
	}

	if err := sess.DeleteMessages(tsList); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	m.persistL2ContextUsage(req.SessionID, sess)

	m.writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
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
	var ctxwinUsed, ctxwinLimit int
	if sessionID == "l1" {
		dir = filepath.Join(m.workDir, "logs", "timelines", "default")
		if m.sessionMgr != nil && m.sessionMgr.Session() != nil {
			ctxwinUsed, ctxwinLimit, _ = m.sessionMgr.Session().CW().TokenUsage()
		}
	} else {
		id := strings.TrimPrefix(sessionID, "l2:")
		dir = filepath.Join(m.workDir, "logs", "timelines", "l2-"+id)
		if m.l2Store != nil {
			if entry := m.l2Store.GetEntry(id); entry != nil {
				if sess := m.l2Store.GetActivated(id); sess != nil && sess.CW() != nil {
					ctxwinUsed, ctxwinLimit, _ = sess.CW().TokenUsage()
				} else if builder := m.l2Store.Builder(); builder != nil {
					ctxwinUsed, ctxwinLimit, _ = builder.ReadL2ContextUsage(r.Context(), id, entry.Group, entry.WorkDir)
				}
			} else {
				ctxwinLimit = m.l2Store.DefaultContextLimit()
			}
		}
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
		Files     []map[string]interface{} `json:"files,omitempty"`
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

	validThreshold := ""
	hiddenByRewind := make(map[int]bool)
	deletedSet := make(map[string]bool)

	// Pre-process rewind and delete events going backwards. A rewind only
	// invalidates messages that precede the control event; messages appended
	// after the rewind must remain visible.
	for i := len(events) - 1; i >= 0; i-- {
		evt := events[i]
		if evt.EventType == timeline.EventControl && evt.Control != nil {
			if evt.Control.Action == "rewind" {
				if len(evt.Control.TargetTs) > 0 {
					ts := evt.Control.TargetTs[0]
					if validThreshold == "" || ts < validThreshold {
						validThreshold = ts
					}
				}
			} else if evt.Control.Action == "delete" {
				for _, ts := range evt.Control.TargetTs {
					deletedSet[ts] = true
				}
			}
			continue
		}
		if evt.EventType == timeline.EventMessage && evt.Message != nil && validThreshold != "" {
			msgTimestamp := evt.Message.Timestamp
			if msgTimestamp == "" {
				msgTimestamp = evt.Timestamp
			}
			if msgTimestamp >= validThreshold {
				hiddenByRewind[i] = true
			}
		}
	}

	for eventIdx, evt := range events {
		if evt.EventType == timeline.EventControl && evt.Control != nil && evt.Control.Action == "summary" {
			msgID := fmt.Sprintf("hist-%d", len(msgs))
			msgs = append(msgs, historyMsg{
				ID:   msgID,
				Role: "assistant",
				Segments: []map[string]interface{}{
					{
						"type": "compact",
						"text": evt.Control.Content,
					},
				},
				Timestamp: evt.Timestamp,
			})
			continue
		}

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

		if hiddenByRewind[eventIdx] {
			continue
		}
		if deletedSet[msgTimestamp] {
			continue
		}

		msgID := fmt.Sprintf("hist-%d", len(msgs))

		switch msg.Role {
		case "user":
			// If this is a delegation result user message, match it back to the corresponding
			// pending delegation tool call segment by CallID and mark it as completed.
			if strings.HasPrefix(msg.Content, "[Delegation Completed]") {
				parsedResults := parseDelegationResults(msg.Content)
				for _, ptc := range pendingToolCalls {
					if !isDelegationToolName(ptc.name) {
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
			var attachedFiles []map[string]interface{}
			for _, f := range msg.Files {
				attachedFiles = append(attachedFiles, map[string]interface{}{
					"name": f.Name,
					"path": f.Path,
				})
			}
			if len(attachedFiles) == 0 {
				attachedFiles = extractFilesFromPrompt(msg.Content)
			}
			msgs = append(msgs, historyMsg{
				ID:        msgID,
				Role:      "user",
				Segments:  segments,
				Timestamp: msgTimestamp,
				Files:     attachedFiles,
			})
		case "assistant":
			// Dedup: skip duplicate partial-flush events. A cancelled turn can
			// leave the timeline with the same assistant content written twice:
			// once by the agent's per-iteration push hook (with tool_calls), and
			// once by the session's partial flush (no tool_calls, same content).
			// Detect that signature and skip the redundant event so history does
			// not render the same content twice.
			//
			// Requires the PREVIOUS assistant row to carry tool_call segments:
			// that combination (assistant-with-tool_calls followed by identical
			// content without tool_calls) uniquely identifies a partial-flush
			// duplicate. Without this guard, a legitimate final reply that
			// happens to repeat an earlier iteration's text would be dropped.
			if len(msg.ToolCalls) == 0 && msg.Content != "" && len(msgs) > 0 && msgs[len(msgs)-1].Role == "assistant" {
				lastMsg := msgs[len(msgs)-1]
				prevHasToolCall := false
				for _, seg := range lastMsg.Segments {
					if seg["type"] == "tool_call" {
						prevHasToolCall = true
						break
					}
				}
				if prevHasToolCall {
					for j := len(lastMsg.Segments) - 1; j >= 0; j-- {
						if lastMsg.Segments[j]["type"] == "content" {
							if lastText, _ := lastMsg.Segments[j]["text"].(string); lastText == msg.Content {
								// Same content already rendered — skip this duplicate event.
								goto nextEvent
							}
							break
						}
					}
				}
			}
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
						if isDelegationToolName(ptc.name) && (msg.Content == "" || strings.HasPrefix(msg.Content, "Delegation started:")) {
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
	nextEvent:
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
		"messages":     msgs,
		"has_more":     hasMore,
		"cursor":       cursor,
		"ctxwin_used":  ctxwinUsed,
		"ctxwin_limit": ctxwinLimit,
	})
}

func isDelegationToolName(name string) bool {
	return name == "delegate" || strings.HasPrefix(name, "delegate_")
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

// resolveSession resolves a session from an optional session_id string.
func (m *Mux) resolveSession(ctx context.Context, sessionID string) *session.Session {
	if strings.HasPrefix(sessionID, "l2:") {
		if m.l2Store == nil {
			return nil
		}
		id := strings.TrimPrefix(sessionID, "l2:")
		sess, _ := m.l2Store.Get(ctx, id)
		return sess
	}

	// Only return L1 session if explicitly requested or implicitly default.
	if sessionID == "" || sessionID == "default" || sessionID == "l1" {
		if m.sessionMgr == nil {
			return nil
		}
		return m.sessionMgr.Session()
	}

	return nil
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

	a := sess.CurrentAgent()
	if a == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "session agent unavailable"})
		return
	}
	workDir := a.WorkDir
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
	// Repeated uploads (e.g. clipboard pastes) reuse the same filename and
	// would overwrite the previous file, making every preview show the same
	// image. Deduplicate so each upload keeps its own path.
	if _, err := os.Stat(destPath); err == nil {
		ext := filepath.Ext(filename)
		base := strings.TrimSuffix(filename, ext)
		destPath = filepath.Join(downloadsDir, fmt.Sprintf("%s-%d%s", base, time.Now().UnixNano(), ext))
	}

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

var fileBlockRegex = regexp.MustCompile(`- File Name:\s*(.+?)\n\s*Save Path:\s*(.+?)\s+\(Size:`)

// legacyFileBlockRegex matches the old WebSocket upload block format `- name: path (image, recognized by visual model)`.
var legacyFileBlockRegex = regexp.MustCompile(`- ([^:\n]+?):\s*(\S+?)\s+\(image, recognized by visual model\)`)

// extractFilesFromPrompt parses file attachments from prompt strings for old historical timeline events.
func extractFilesFromPrompt(content string) []map[string]interface{} {
	var files []map[string]interface{}
	matches := fileBlockRegex.FindAllStringSubmatch(content, -1)
	for _, m := range matches {
		if len(m) >= 3 {
			files = append(files, map[string]interface{}{
				"name": strings.TrimSpace(m[1]),
				"path": strings.TrimSpace(m[2]),
			})
		}
	}
	for _, m := range legacyFileBlockRegex.FindAllStringSubmatch(content, -1) {
		if len(m) >= 3 {
			files = append(files, map[string]interface{}{
				"name": strings.TrimSpace(m[1]),
				"path": strings.TrimSpace(m[2]),
			})
		}
	}
	return files
}
