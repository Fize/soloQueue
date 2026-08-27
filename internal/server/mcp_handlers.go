package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
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

// GET /api/mcp/policies?scope=global
func (m *Mux) handleGetMCPPolicies(w http.ResponseWriter, r *http.Request) {
	if m.mcpLoader == nil || m.mcpManager == nil || m.mcpManager.PolicyStore() == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "MCP policy store unavailable"})
		return
	}
	scope := mcp.NormalizePolicyScope(r.URL.Query().Get("scope"))
	cfg, err := m.mcpConfigForPolicyScope(scope)
	if err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	policies := make([]mcp.Policy, 0, len(cfg.Servers))
	for _, serverCfg := range cfg.Servers {
		policy, err := m.mcpManager.PolicyStore().Effective(r.Context(), scope, serverCfg)
		if err != nil {
			m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		policies = append(policies, policy)
	}
	m.writeJSON(w, http.StatusOK, map[string]any{"policies": policies})
}

type updateMCPPolicyRequest struct {
	Scope             string `json:"scope"`
	ConfirmHostAccess bool   `json:"confirm_host_access"`
}

// PUT /api/mcp/policies/{serverName}
func (m *Mux) handleApproveMCPPolicy(w http.ResponseWriter, r *http.Request) {
	if m.mcpLoader == nil || m.mcpManager == nil || m.mcpManager.PolicyStore() == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "MCP policy store unavailable"})
		return
	}
	var req updateMCPPolicyRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: request body must contain exactly one object"})
		return
	}
	req.Scope = mcp.NormalizePolicyScope(req.Scope)
	if !req.ConfirmHostAccess {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "MCP host access requires confirm_host_access=true"})
		return
	}

	serverName := strings.TrimSpace(chi.URLParam(r, "serverName"))
	if serverName == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "MCP server name is required"})
		return
	}
	cfg, err := m.mcpConfigForPolicyScope(req.Scope)
	if err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	var serverCfg *mcp.ServerConfig
	for i := range cfg.Servers {
		if cfg.Servers[i].Name == serverName {
			serverCfg = &cfg.Servers[i]
			break
		}
	}
	if serverCfg == nil {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": "MCP server not found"})
		return
	}
	policy, err := m.mcpManager.PolicyStore().Approve(r.Context(), req.Scope, *serverCfg)
	if err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	m.mcpManager.StopInstance(req.Scope, serverName)
	m.writeJSON(w, http.StatusOK, policy)
}

// DELETE /api/mcp/policies/{serverName}?scope=global
func (m *Mux) handleRevokeMCPPolicy(w http.ResponseWriter, r *http.Request) {
	if m.mcpManager == nil || m.mcpManager.PolicyStore() == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "MCP policy store unavailable"})
		return
	}
	scope := mcp.NormalizePolicyScope(r.URL.Query().Get("scope"))
	serverName := strings.TrimSpace(chi.URLParam(r, "serverName"))
	if err := m.mcpManager.PolicyStore().Revoke(r.Context(), scope, serverName); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	m.mcpManager.StopInstance(scope, serverName)
	w.WriteHeader(http.StatusNoContent)
}

func (m *Mux) mcpConfigForPolicyScope(scope string) (mcp.Config, error) {
	scope = mcp.NormalizePolicyScope(scope)
	if scope == "global" {
		return m.mcpLoader.ReadFromDisk()
	}
	if !strings.HasPrefix(scope, "project:") {
		return mcp.Config{}, fmt.Errorf("invalid MCP policy scope")
	}
	projectPath := strings.TrimSpace(strings.TrimPrefix(scope, "project:"))
	if !filepath.IsAbs(projectPath) {
		return mcp.Config{}, fmt.Errorf("project MCP scope must use an absolute path")
	}
	projectPath = filepath.Clean(projectPath)
	info, err := os.Stat(projectPath)
	if err != nil || !info.IsDir() {
		return mcp.Config{}, fmt.Errorf("project MCP scope is not an accessible directory")
	}
	cfg, err := mcp.ReadConfigFile(filepath.Join(projectPath, ".claude", "mcp.json"))
	if err != nil {
		return mcp.Config{}, fmt.Errorf("read project MCP config: %w", err)
	}
	return cfg, nil
}
