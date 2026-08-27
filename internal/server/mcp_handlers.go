package server

import (
	"encoding/json"
	"net/http"

	"github.com/xiaobaitu/soloqueue/internal/agenttools/mcp"
)

// handleGetMCPConfig returns the current mcp.json contents.
// GET /api/mcp
func (m *Mux) handleGetMCPConfig(w http.ResponseWriter, _ *http.Request) {
	if m.mcpLoader == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "MCP not configured"})
		return
	}
	cfg, err := m.mcpLoader.ReadFromDisk()
	if err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	m.writeJSON(w, http.StatusOK, cfg)
}

// availableMCPServer represents an MCP server available for agent selection.
type availableMCPServer struct {
	Name    string `json:"name"`
	Source  string `json:"source"` // "builtin" or "external"
	Command string `json:"command,omitempty"`
}

// handleGetAvailableMCPServers returns all MCP servers (both external and built-in)
// that can be selected for agents.
// GET /api/mcp/available
func (m *Mux) handleGetAvailableMCPServers(w http.ResponseWriter, _ *http.Request) {
	var servers []availableMCPServer

	if m.mcpManager != nil {
		for _, name := range m.mcpManager.VirtualServerNames() {
			servers = append(servers, availableMCPServer{
				Name:   name,
				Source: "builtin",
			})
		}
	}

	if m.mcpLoader != nil {
		cfg, err := m.mcpLoader.ReadFromDisk()
		if err == nil {
			for _, s := range cfg.Servers {
				servers = append(servers, availableMCPServer{
					Name:    s.Name,
					Source:  "external",
					Command: s.Command,
				})
			}
		}
	}

	m.writeJSON(w, http.StatusOK, map[string]any{"servers": servers})
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
