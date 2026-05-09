package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/xiaobaitu/soloqueue/internal/config"
)

// ─── Config Handlers ────────────────────────────────────────────────────────

// handleGetConfig returns the current settings as JSON.
// GET /api/config
func (m *Mux) handleGetConfig(w http.ResponseWriter, _ *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}

	settings := m.configSvc.Get()
	m.writeJSON(w, http.StatusOK, settings)
}

// handleUpdateConfig accepts a partial JSON body and merges it into current settings.
// PATCH /api/config
//
// The request body is a partial config.Settings struct — only fields present in
// the JSON will be applied. Uses the Loader's Set method which atomically updates
// the in-memory config and persists to disk.
func (m *Mux) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}

	var patch config.Settings
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid request body: %v", err)})
		return
	}

	// Apply the patch using Set — this atomically updates + persists.
	if err := m.configSvc.Set(func(current *config.Settings) {
		applyConfigPatch(current, patch)
	}); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to update config: %v", err)})
		return
	}

	// Return the updated settings.
	updated := m.configSvc.Get()
	m.writeJSON(w, http.StatusOK, updated)
}

// applyConfigPatch merges non-zero fields from patch into current settings.
// Only top-level sections that are explicitly provided (non-zero) are applied.
func applyConfigPatch(current *config.Settings, patch config.Settings) {
	// Session
	if patch.Session.TimelineMaxFileMB > 0 {
		current.Session.TimelineMaxFileMB = patch.Session.TimelineMaxFileMB
	}
	if patch.Session.TimelineMaxFiles > 0 {
		current.Session.TimelineMaxFiles = patch.Session.TimelineMaxFiles
	}
	if patch.Session.ContextIdleThresholdMin > 0 {
		current.Session.ContextIdleThresholdMin = patch.Session.ContextIdleThresholdMin
	}

	// Log
	if patch.Log.Level != "" {
		current.Log.Level = patch.Log.Level
	}
	if patch.Log.Console {
		current.Log.Console = patch.Log.Console
	}
	if patch.Log.File {
		current.Log.File = patch.Log.File
	}

	// Tools — only apply if any field is set
	if patch.Tools.MaxFileSize > 0 {
		current.Tools.MaxFileSize = patch.Tools.MaxFileSize
	}
	if patch.Tools.MaxWriteSize > 0 {
		current.Tools.MaxWriteSize = patch.Tools.MaxWriteSize
	}
	if patch.Tools.HTTPTimeoutMs > 0 {
		current.Tools.HTTPTimeoutMs = patch.Tools.HTTPTimeoutMs
	}
	if patch.Tools.ShellMaxOutput > 0 {
		current.Tools.ShellMaxOutput = patch.Tools.ShellMaxOutput
	}

	// Providers — replace entirely if provided
	if len(patch.Providers) > 0 {
		current.Providers = patch.Providers
	}

	// Models — replace entirely if provided
	if len(patch.Models) > 0 {
		current.Models = patch.Models
	}

	// DefaultModels
	if patch.DefaultModels.Expert != "" {
		current.DefaultModels.Expert = patch.DefaultModels.Expert
	}
	if patch.DefaultModels.Superior != "" {
		current.DefaultModels.Superior = patch.DefaultModels.Superior
	}
	if patch.DefaultModels.Universal != "" {
		current.DefaultModels.Universal = patch.DefaultModels.Universal
	}
	if patch.DefaultModels.Fast != "" {
		current.DefaultModels.Fast = patch.DefaultModels.Fast
	}
	if patch.DefaultModels.Fallback != "" {
		current.DefaultModels.Fallback = patch.DefaultModels.Fallback
	}
}
