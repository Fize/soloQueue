package server

import (
	"net/http"
	"os"
	"path/filepath"
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

// handleGetConfigToml returns the raw settings.toml content.
// GET /api/config/toml
func (m *Mux) handleGetConfigToml(w http.ResponseWriter, _ *http.Request) {
	path := filepath.Join(m.workDir, "settings.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			m.writeJSON(w, http.StatusNotFound, map[string]string{"error": "settings.toml not found"})
			return
		}
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}
