package server

import (
	"net/http"
)

// handleGetConfig returns the current settings as JSON.
// GET /api/config
func (m *Mux) handleGetConfig(w http.ResponseWriter, _ *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}

	settings, err := m.configSvc.LoadFromDisk()
	if err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	m.writeJSON(w, http.StatusOK, settings)
}
