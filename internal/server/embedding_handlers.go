package server

import (
	"encoding/json"
	"io"
	"net/http"
)

func (m *Mux) handleEmbeddingStatus(w http.ResponseWriter, r *http.Request) {
	if m.embedding == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "embedding service not available"})
		return
	}
	m.writeJSON(w, http.StatusOK, m.embedding.Status())
}
func (m *Mux) handleEmbeddingAction(w http.ResponseWriter, r *http.Request) {
	if m.embedding == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "embedding service not available"})
		return
	}
	var body struct {
		Action string `json:"action"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid embedding action body"})
		return
	}
	if decoder.Decode(new(any)) != io.EOF || (body.Action != "retry" && body.Action != "cancel" && body.Action != "reindex") {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid embedding action"})
		return
	}
	status, err := m.embedding.Action(body.Action)
	if err != nil {
		m.writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	m.writeJSON(w, http.StatusAccepted, status)
}
