package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/xiaobaitu/soloqueue/internal/session"
)

// sessionModelView is the wire shape of a model selection. Empty fields mean
// "use the configured default model".
type sessionModelView struct {
	ProviderID      string `json:"provider_id"`
	ModelID         string `json:"model_id"`
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	Name            string `json:"name,omitempty"`
}

// sessionModelResponse reports the stored selection and the model that will
// actually run the next turn.
type sessionModelResponse struct {
	Selection *sessionModelView `json:"selection"`
	Effective *sessionModelView `json:"effective"`
	Source    string            `json:"source"`
}

type sessionModelRequest struct {
	ProviderID      string `json:"provider_id"`
	ModelID         string `json:"model_id"`
	ReasoningEffort string `json:"reasoning_effort"`
}

func (m *Mux) sessionModelResponseFor(sel session.ModelSelection) sessionModelResponse {
	if m.configSvc == nil {
		return sessionModelResponse{Source: "default"}
	}
	lookup := session.ModelLookupForConfig(m.configSvc)
	resp := sessionModelResponse{Source: "default"}

	if sel.IsAuto() {
		if model := m.configSvc.DefaultModel(); model != nil {
			resp.Effective = &sessionModelView{
				ProviderID: model.ProviderID,
				ModelID:    model.ID,
				Name:       model.Name,
			}
		}
		return resp
	}

	resp.Selection = &sessionModelView{
		ProviderID:      sel.ProviderID,
		ModelID:         sel.ModelID,
		ReasoningEffort: sel.ReasoningEffort,
	}
	resp.Source = "user"
	if spec, ok := lookup(sel.ProviderID, sel.ModelID); ok {
		resp.Effective = &sessionModelView{
			ProviderID:      spec.ProviderID,
			ModelID:         spec.APIModel,
			ReasoningEffort: sel.ReasoningEffort,
			Name:            spec.Name,
		}
	}
	return resp
}

// decodeSessionModelRequest reads and validates a model selection body.
func (m *Mux) decodeSessionModelRequest(r *http.Request) (session.ModelSelection, error) {
	var req sessionModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return session.ModelSelection{}, err
	}
	sel, err := session.NormalizeModelSelection(session.ModelSelection{
		ProviderID:      req.ProviderID,
		ModelID:         req.ModelID,
		ReasoningEffort: req.ReasoningEffort,
	}, session.ModelLookupForConfig(m.configSvc))
	if err != nil {
		return session.ModelSelection{}, err
	}
	return sel, nil
}

// GET /api/session/model
func (m *Mux) handleGetSessionModel(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}
	if m.sessionMgr == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "session manager not configured"})
		return
	}
	sess := m.sessionMgr.Session()
	if sess == nil {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": "no active session"})
		return
	}
	m.writeJSON(w, http.StatusOK, m.sessionModelResponseFor(sess.ModelSelection()))
}

// PUT /api/session/model
func (m *Mux) handleSetSessionModel(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}
	if m.sessionMgr == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "session manager not configured"})
		return
	}
	sess := m.sessionMgr.Session()
	if sess == nil {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": "no active session"})
		return
	}
	sel, err := m.decodeSessionModelRequest(r)
	if err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := sess.SetModelSelection(sel); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	m.writeJSON(w, http.StatusOK, m.sessionModelResponseFor(sess.ModelSelection()))
}

// GET /api/session/l2/{id}/model
func (m *Mux) handleGetL2SessionModel(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}
	id := strings.TrimPrefix(chi.URLParam(r, "id"), "l2:")
	if id == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session id is required"})
		return
	}
	if sess := m.activatedL2Session(id); sess != nil {
		m.writeJSON(w, http.StatusOK, m.sessionModelResponseFor(sess.ModelSelection()))
		return
	}
	meta, err := session.LoadMeta(m.workDir, id)
	if err != nil {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	m.writeJSON(w, http.StatusOK, m.sessionModelResponseFor(meta.Model))
}

// PUT /api/session/l2/{id}/model
func (m *Mux) handleSetL2SessionModel(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}
	id := strings.TrimPrefix(chi.URLParam(r, "id"), "l2:")
	if id == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session id is required"})
		return
	}
	sel, err := m.decodeSessionModelRequest(r)
	if err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if sess := m.activatedL2Session(id); sess != nil {
		if err := sess.SetModelSelection(sel); err != nil {
			m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		m.writeJSON(w, http.StatusOK, m.sessionModelResponseFor(sess.ModelSelection()))
		return
	}
	// The session is not loaded: persist the choice so the next activation
	// picks it up.
	if _, err := session.LoadMeta(m.workDir, id); err != nil {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	if err := session.MergeAndSave(m.workDir, id, func(meta *session.SessionMeta) {
		meta.Model = sel
	}); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	m.writeJSON(w, http.StatusOK, m.sessionModelResponseFor(sel))
}

func (m *Mux) activatedL2Session(id string) *session.Session {
	if m.l2Store == nil {
		return nil
	}
	return m.l2Store.GetActivated(id)
}
