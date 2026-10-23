package server

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/xiaobaitu/soloqueue/internal/config"
)

func (m *Mux) handleGetClassifierConfig(w http.ResponseWriter, _ *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}
	m.writeJSON(w, http.StatusOK, m.configSvc.Get().Classifier)
}

func (m *Mux) handleUpdateClassifierConfig(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}
	var classifier config.ClassifierConfig
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&classifier); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := m.configSvc.UpdateClassifier(classifier); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	m.triggerOnConfigChange()
	m.writeJSON(w, http.StatusOK, m.configSvc.Get().Classifier)
}

func (m *Mux) handleClassifierStatus(w http.ResponseWriter, _ *http.Request) {
	if m.classifier == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "classifier service not available"})
		return
	}
	m.writeJSON(w, http.StatusOK, m.classifier.Status())
}

func (m *Mux) handleClassifierAction(w http.ResponseWriter, r *http.Request) {
	if m.classifier == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "classifier service not available"})
		return
	}
	var body struct {
		Action string `json:"action"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid classifier action body"})
		return
	}
	if decoder.Decode(new(any)) != io.EOF || (body.Action != "retry" && body.Action != "cancel") {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid classifier action"})
		return
	}
	status, err := m.classifier.Action(body.Action)
	if err != nil {
		m.writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	m.writeJSON(w, http.StatusAccepted, status)
}
