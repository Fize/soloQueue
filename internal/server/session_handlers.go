package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/infra/telemetry"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
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
		if msg.Role == "system" || msg.IsSummary || msg.IsPersonaState || msg.IsProjectInstructions {
			continue // skip non-conversation context for a cleaner chat UI
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
// The team store is read on every call so teams installed at runtime resolve
// without a restart.
func (m *Mux) leaderAgentName(ctx context.Context, group string) string {
	if m.teamstore == nil {
		return ""
	}
	leaders, err := m.teamstore.ListLeaders(ctx)
	if err != nil {
		return ""
	}
	for _, a := range leaders {
		if strings.EqualFold(a.TeamName, group) {
			return a.Name
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
		DesignDir   string               `json:"design_dir,omitempty"`
		CreatedAt   time.Time            `json:"created_at"`
		IsQBot      bool                 `json:"is_qbot"`
		CtxwinUsed  int                  `json:"ctxwin_used"`
		CtxwinLimit int                  `json:"ctxwin_limit"`
		Usage       session.SessionUsage `json:"usage,omitempty"`
		Plans       []string             `json:"plans,omitempty"`
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
			Usage:           l1Sess.UsageSnapshot(),
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
				AgentName:       m.leaderAgentName(r.Context(), info.Group),
				AgentInstanceID: info.AgentInstanceID,
				ProjectPath:     info.WorkDir,
				DesignDir:       resolveDesignDir(info.WorkDir, info.Group),
				CreatedAt:       info.CreatedAt,
				CtxwinUsed:      ctxwinUsed,
				CtxwinLimit:     ctxwinLimit,
				Usage:           info.Usage,
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
				AgentName:   m.leaderAgentName(r.Context(), group),
				ProjectPath: projectPath,
				DesignDir:   resolveDesignDir(projectPath, group),
				CreatedAt:   createdAt,
				CtxwinUsed:  ctxwinUsed,
				CtxwinLimit: ctxwinLimit,
				Usage:       loaded.Usage,
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
		"agent_name":   m.leaderAgentName(r.Context(), info.Group),
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
	seen := map[string]bool{}
	add := func(name string) {
		key := strings.ToLower(name)
		if name == "" || seen[key] {
			return
		}
		seen[key] = true
		groups = append(groups, name)
	}
	if m.l2Store != nil {
		// Existing sessions stay selectable even if their team file was removed.
		for _, s := range m.l2Store.List() {
			add(s.Group)
		}
	}
	// Teams are read per request: a team installed at runtime must be
	// selectable without restarting the backend.
	if m.teamstore != nil {
		teams, err := m.teamstore.ListTeams(r.Context())
		if err != nil {
			m.logCatalogError("list teams for L2 groups", err)
		} else {
			leaders, lerr := m.teamstore.ListLeaders(r.Context())
			if lerr != nil {
				m.logCatalogError("list leader agents for L2 groups", lerr)
			} else {
				leaderTeams := make(map[string]bool, len(leaders))
				for _, a := range leaders {
					leaderTeams[strings.ToLower(a.TeamName)] = true
				}
				for _, t := range teams {
					if leaderTeams[strings.ToLower(t.Name)] {
						add(t.Name)
					}
				}
			}
		}
	}
	if groups == nil {
		groups = []string{}
	}
	m.writeJSON(w, http.StatusOK, map[string]interface{}{"groups": groups})
}

func (m *Mux) logCatalogError(action string, err error) {
	if m.log == nil {
		return
	}
	m.log.Warn(logger.CatApp, action+" failed", "err", err.Error())
}

// ─── Cancel / Clear (with session_id support) ──────────────────────────────

func (m *Mux) handleCancelSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string `json:"session_id"`
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

	// Capture registered requests before stopping roots: a request may still be
	// starting, before its Session cancellation root is bound.
	var requests []ActiveRequest
	logicalID := logicalRuntimeSessionID(sess.TargetID)
	if m.hub != nil && m.hub.requests != nil {
		requests = m.hub.requests.GetBySessionAll(logicalID)
	}
	cancelErr := sess.CancelCurrentByUser("User requested cancellation")
	if errors.Is(cancelErr, session.ErrNoActiveTask) {
		cancelErr = nil
	}
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	for _, request := range requests {
		if request.State == "idle" {
			continue
		}
		_, _, err := m.hub.requests.CancelAndWait(ctx, logicalID, request.RequestID)
		if err != nil && !errors.Is(err, ErrRequestNotFound) {
			cancelErr = errors.Join(cancelErr, err)
			continue
		}
		m.hub.requests.ReleaseQueuedForGeneration(logicalID, request.Generation)
	}
	if cancelErr != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": cancelErr.Error()})
		return
	}
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
