package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

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

func (m *Mux) handleCancelSession(w http.ResponseWriter, r *http.Request) {
	if m.sessionMgr == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "session manager not configured"})
		return
	}
	sess := m.sessionMgr.Session()
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
	if m.sessionMgr == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "session manager not configured"})
		return
	}
	sess := m.sessionMgr.Session()
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
