package server

import (
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/memory/timeline"
	"github.com/xiaobaitu/soloqueue/internal/session"
	"github.com/xiaobaitu/soloqueue/internal/transcript"
)

const defaultTranscriptPageSize = 30

// transcriptResponse is the snapshot served by GET /api/session/transcript.
// Seq is the watermark: applying the session's frames with a greater sequence
// to Items reproduces any later snapshot.
type transcriptResponse struct {
	SessionID   string                             `json:"session_id"`
	Epoch       string                             `json:"epoch"`
	Seq         uint64                             `json:"seq"`
	Items       []transcript.Item                  `json:"items"`
	Pending     []transcript.PendingInput          `json:"pending"`
	Requests    []transcript.RequestState          `json:"requests"`
	HasMore     bool                               `json:"has_more"`
	Cursor      string                             `json:"cursor,omitempty"`
	Compactions []timeline.CompactionStatusPayload `json:"compactions"`
	CtxwinUsed  int                                `json:"ctxwin_used"`
	CtxwinLimit int                                `json:"ctxwin_limit"`
}

// sessionTimelineDir resolves the timeline directory of a logical session.
func (m *Mux) sessionTimelineDir(sessionID string) string {
	if sessionID == "l1" || sessionID == "" {
		return filepath.Join(m.workDir, "logs", "timelines", "default")
	}
	return filepath.Join(m.workDir, "logs", "timelines", "l2-"+strings.TrimPrefix(sessionID, "l2:"))
}

// sessionContextUsage reports the context window usage and the compaction
// currently running for a logical session.
func (m *Mux) sessionContextUsage(r *http.Request, sessionID string) (used, limit int, activeCompactionID string) {
	if sessionID == "l1" || sessionID == "" {
		if m.sessionMgr != nil && m.sessionMgr.Session() != nil {
			if cw := m.sessionMgr.Session().CW(); cw != nil {
				used, limit, _ = cw.TokenUsage()
				activeCompactionID = cw.ActiveCompactionID()
			}
		}
		return used, limit, activeCompactionID
	}
	id := strings.TrimPrefix(sessionID, "l2:")
	if m.l2Store == nil {
		return 0, 0, ""
	}
	entry := m.l2Store.GetEntry(id)
	if entry == nil {
		return 0, m.l2Store.DefaultContextLimit(), ""
	}
	if sess := m.l2Store.GetActivated(id); sess != nil && sess.CW() != nil {
		used, limit, _ = sess.CW().TokenUsage()
		return used, limit, sess.CW().ActiveCompactionID()
	}
	if builder := m.l2Store.Builder(); builder != nil {
		used, limit, _ = builder.ReadL2ContextUsage(r.Context(), id, entry.Group, entry.WorkDir)
	}
	return used, limit, ""
}

// compactionStatuses aggregates compaction lifecycle rows after the latest
// clear. A run still marked running that is not active was interrupted.
func compactionStatuses(events []timeline.Event, clearAt time.Time, activeCompactionID string) []timeline.CompactionStatusPayload {
	byID := make(map[string]timeline.CompactionStatusPayload)
	started := make(map[string]bool)
	var order []string
	for _, evt := range events {
		if evt.EventType != timeline.EventControl || evt.Control == nil || evt.Control.Action != "compaction_status" || evt.Control.Compaction == nil {
			continue
		}
		cp := *evt.Control.Compaction
		if cp.StartedAt != "" && !clearAt.IsZero() {
			if startedAt, err := time.Parse(time.RFC3339Nano, cp.StartedAt); err == nil && startedAt.Before(clearAt) {
				continue
			}
		}
		if cp.Status != "running" && cp.StartedAt == "" && !started[cp.ID] {
			continue
		}
		if _, ok := byID[cp.ID]; !ok {
			order = append(order, cp.ID)
		}
		if cp.Status == "running" {
			started[cp.ID] = true
		}
		prev := byID[cp.ID]
		if cp.StartedAt == "" {
			cp.StartedAt = prev.StartedAt
		}
		if cp.FinishedAt == "" && prev.FinishedAt != "" {
			cp.FinishedAt = prev.FinishedAt
		}
		if cp.Error == "" && prev.Error != "" {
			cp.Error = prev.Error
		}
		byID[cp.ID] = cp
	}
	out := make([]timeline.CompactionStatusPayload, 0, len(order))
	for _, id := range order {
		status := byID[id]
		if status.Status == "running" && status.ID != activeCompactionID {
			status.Status = "interrupted"
			status.Error = "process ended before completion status was recorded"
		}
		out = append(out, status)
	}
	return out
}

// handleSessionTranscript serves a transcript snapshot with its watermark.
// GET /api/session/transcript?session_id=l1|l2:<uuid>[&before=<item id>&limit=<n>]
func (m *Mux) handleSessionTranscript(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session_id is required"})
		return
	}
	key := transcript.SessionKey(sessionID)
	limit := defaultTranscriptPageSize
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	before := r.URL.Query().Get("before")
	dir := m.sessionTimelineDir(key)

	// The watermark is captured before the timeline is read; Assemble drops
	// persisted items that opened after it.
	snap := m.transcripts.Journal(key, dir).Snapshot()
	used, ctxLimit, activeCompactionID := m.sessionContextUsage(r, key)

	events, _ := timeline.ReadAllEvents(dir)
	post, clearAt := transcript.PostClear(events)
	var dispatches []transcript.DispatchOutcome
	for _, observed := range m.findObservationDispatchesLimit(key, "", 0) {
		dispatches = append(dispatches, transcript.DispatchOutcome{
			RequestID:  observed.RequestID,
			ToolCallID: observed.ToolCallID,
			DispatchID: observed.ID,
			Status:     string(observed.Status),
		})
	}
	persisted := transcript.Build(post, transcript.BuildOptions{
		NormalizeUserText: session.StripRecalledMemories,
		Dispatches:        dispatches,
	})
	items := transcript.Assemble(persisted, snap)
	page, hasMore, cursor := transcript.Page(items, limit, before)

	pending := snap.Pending
	if pending == nil {
		pending = []transcript.PendingInput{}
	}
	requests := snap.Requests
	if requests == nil {
		requests = []transcript.RequestState{}
	}
	m.writeJSON(w, http.StatusOK, transcriptResponse{
		SessionID:   key,
		Epoch:       snap.Epoch,
		Seq:         snap.Seq,
		Items:       page,
		Pending:     pending,
		Requests:    requests,
		HasMore:     hasMore,
		Cursor:      cursor,
		Compactions: compactionStatuses(post, clearAt, activeCompactionID),
		CtxwinUsed:  used,
		CtxwinLimit: ctxLimit,
	})
}
