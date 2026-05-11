package server

import (
	"encoding/json"
	"net/http"

	"github.com/xiaobaitu/soloqueue/internal/mcp"
)

// handleGetMCPConfig returns the current mcp.json contents.
// GET /api/mcp
func (m *Mux) handleGetMCPConfig(w http.ResponseWriter, _ *http.Request) {
	if m.mcpLoader == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "MCP not configured"})
		return
	}
	cfg := m.mcpLoader.Get()
	m.writeJSON(w, http.StatusOK, cfg)
}

// handleUpdateMCPConfig replaces the full MCP server list.
// PATCH /api/mcp
func (m *Mux) handleUpdateMCPConfig(w http.ResponseWriter, r *http.Request) {
	if m.mcpLoader == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "MCP not configured"})
		return
	}

	var cfg mcp.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}

	if err := m.mcpLoader.Set(func(current *mcp.Config) {
		current.Servers = cfg.Servers
	}); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	m.writeJSON(w, http.StatusOK, m.mcpLoader.Get())
}
