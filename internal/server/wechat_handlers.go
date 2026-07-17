package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/xiaobaitu/soloqueue/internal/channel/wechat"
)

func (m *Mux) handleStartWechatLogin(w http.ResponseWriter, r *http.Request) {
	if m.wechatLogin == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wechat login is not available"})
		return
	}
	var request wechat.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	snapshot, err := m.wechatLogin.Start(r.Context(), request)
	if err != nil {
		status := http.StatusBadGateway
		switch {
		case errors.Is(err, wechat.ErrLoginConflict):
			status = http.StatusConflict
		case errors.Is(err, wechat.ErrLoginCapacity):
			status = http.StatusTooManyRequests
		case err.Error() == "invalid wechat login request":
			status = http.StatusBadRequest
		}
		m.writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	m.writeJSON(w, http.StatusCreated, snapshot)
}

func (m *Mux) handleGetWechatLogin(w http.ResponseWriter, r *http.Request) {
	if m.wechatLogin == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wechat login is not available"})
		return
	}
	snapshot, err := m.wechatLogin.Snapshot(chi.URLParam(r, "sessionID"))
	if err != nil {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	m.writeJSON(w, http.StatusOK, snapshot)
}

func (m *Mux) handleSubmitWechatVerification(w http.ResponseWriter, r *http.Request) {
	if m.wechatLogin == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wechat login is not available"})
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := m.wechatLogin.SubmitVerification(chi.URLParam(r, "sessionID"), body.Code); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, wechat.ErrLoginNotFound) {
			status = http.StatusNotFound
		}
		m.writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *Mux) handleCancelWechatLogin(w http.ResponseWriter, r *http.Request) {
	if m.wechatLogin == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "wechat login is not available"})
		return
	}
	if err := m.wechatLogin.Cancel(chi.URLParam(r, "sessionID")); err != nil {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
