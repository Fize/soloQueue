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

	settings, err := m.configSvc.LoadFromDisk()
	if err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	m.writeJSON(w, http.StatusOK, settings)
}

// configPatchRequest uses pointers for all fields so we can distinguish
// "field not provided" (nil) from "field provided with zero/false value".
type configPatchRequest struct {
	Session       *sessionPatch       `json:"session,omitempty"`
	Log           *logPatch           `json:"log,omitempty"`
	Tools         *toolsPatch         `json:"tools,omitempty"`
	Providers     *[]config.LLMProvider   `json:"providers,omitempty"`
	Models        *[]config.LLMModel      `json:"models,omitempty"`
	Embedding     *embeddingPatch     `json:"embedding,omitempty"`
	DefaultModels *defaultModelsPatch `json:"defaultModels,omitempty"`
	QQBot         *qqBotPatch         `json:"qqbot,omitempty"`
	Agent         *agentPatch         `json:"agent,omitempty"`
}

type sessionPatch struct {
	TimelineMaxFileMB       *int `json:"timelineMaxFileMB,omitempty"`
	TimelineMaxFiles        *int `json:"timelineMaxFiles,omitempty"`
	ContextIdleThresholdMin *int `json:"contextIdleThresholdMin,omitempty"`
}

type logPatch struct {
	Level   *string `json:"level,omitempty"`
	Console *bool   `json:"console,omitempty"`
	File    *bool   `json:"file,omitempty"`
}

type toolsPatch struct {
	MaxFileSize  *int64 `json:"maxFileSize,omitempty"`
	MaxMatches   *int   `json:"maxMatches,omitempty"`
	MaxLineLen   *int   `json:"maxLineLen,omitempty"`
	MaxGlobItems *int   `json:"maxGlobItems,omitempty"`

	MaxWriteSize       *int64 `json:"maxWriteSize,omitempty"`
	MaxMultiWriteBytes *int64 `json:"maxMultiWriteBytes,omitempty"`
	MaxMultiWriteFiles *int   `json:"maxMultiWriteFiles,omitempty"`
	MaxReplaceEdits    *int   `json:"maxReplaceEdits,omitempty"`

	HTTPAllowedHosts *[]string `json:"httpAllowedHosts,omitempty"`
	HTTPMaxBody      *int64    `json:"httpMaxBody,omitempty"`
	HTTPTimeoutMs    *int      `json:"httpTimeoutMs,omitempty"`
	HTTPBlockPrivate *bool     `json:"httpBlockPrivate,omitempty"`

	ShellBlockRegexes   *[]string `json:"shellBlockRegexes,omitempty"`
	ShellConfirmRegexes *[]string `json:"shellConfirmRegexes,omitempty"`
	ShellMaxOutput      *int64    `json:"shellMaxOutput,omitempty"`

	WebSearchTimeoutMs *int `json:"webSearchTimeoutMs,omitempty"`
}

type embeddingPatch struct {
	Enabled   *bool                        `json:"enabled,omitempty"`
	Providers *[]config.EmbeddingProvider  `json:"providers,omitempty"`
	Models    *[]config.EmbeddingModel     `json:"models,omitempty"`
}

type defaultModelsPatch struct {
	Expert    *string `json:"expert,omitempty"`
	Superior  *string `json:"superior,omitempty"`
	Universal *string `json:"universal,omitempty"`
	Fast      *string `json:"fast,omitempty"`
	Fallback  *string `json:"fallback,omitempty"`
}

type qqBotPatch struct {
	Enabled   *bool   `json:"enabled,omitempty"`
	AppID     *string `json:"appId,omitempty"`
	AppSecret *string `json:"appSecret,omitempty"`
	Intents   *int    `json:"intents,omitempty"`
	Sandbox   *bool   `json:"sandbox,omitempty"`
}

type agentPatch struct {
	BuiltinMCPServers  *[]string `json:"builtinMcpServers,omitempty"`
	ExternalMCPServers *[]string `json:"externalMcpServers,omitempty"`
}

// handleUpdateConfig accepts a partial JSON body and merges it into current settings.
// PATCH /api/config
//
// The request body uses pointer fields — only non-nil fields are applied,
// which correctly handles bool/int zero values.
func (m *Mux) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	if m.configSvc == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "config service not available"})
		return
	}

	var patch configPatchRequest
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

// applyConfigPatch merges non-nil fields from patch into current settings.
func applyConfigPatch(current *config.Settings, patch configPatchRequest) {
	// Session
	if patch.Session != nil {
		if patch.Session.TimelineMaxFileMB != nil {
			current.Session.TimelineMaxFileMB = *patch.Session.TimelineMaxFileMB
		}
		if patch.Session.TimelineMaxFiles != nil {
			current.Session.TimelineMaxFiles = *patch.Session.TimelineMaxFiles
		}
		if patch.Session.ContextIdleThresholdMin != nil {
			current.Session.ContextIdleThresholdMin = *patch.Session.ContextIdleThresholdMin
		}
	}

	// Log
	if patch.Log != nil {
		if patch.Log.Level != nil {
			current.Log.Level = *patch.Log.Level
		}
		if patch.Log.Console != nil {
			current.Log.Console = *patch.Log.Console
		}
		if patch.Log.File != nil {
			current.Log.File = *patch.Log.File
		}
	}

	// Tools
	if patch.Tools != nil {
		if patch.Tools.MaxFileSize != nil {
			current.Tools.MaxFileSize = *patch.Tools.MaxFileSize
		}
		if patch.Tools.MaxMatches != nil {
			current.Tools.MaxMatches = *patch.Tools.MaxMatches
		}
		if patch.Tools.MaxLineLen != nil {
			current.Tools.MaxLineLen = *patch.Tools.MaxLineLen
		}
		if patch.Tools.MaxGlobItems != nil {
			current.Tools.MaxGlobItems = *patch.Tools.MaxGlobItems
		}
		if patch.Tools.MaxWriteSize != nil {
			current.Tools.MaxWriteSize = *patch.Tools.MaxWriteSize
		}
		if patch.Tools.MaxMultiWriteBytes != nil {
			current.Tools.MaxMultiWriteBytes = *patch.Tools.MaxMultiWriteBytes
		}
		if patch.Tools.MaxMultiWriteFiles != nil {
			current.Tools.MaxMultiWriteFiles = *patch.Tools.MaxMultiWriteFiles
		}
		if patch.Tools.MaxReplaceEdits != nil {
			current.Tools.MaxReplaceEdits = *patch.Tools.MaxReplaceEdits
		}
		if patch.Tools.HTTPAllowedHosts != nil {
			current.Tools.HTTPAllowedHosts = *patch.Tools.HTTPAllowedHosts
		}
		if patch.Tools.HTTPMaxBody != nil {
			current.Tools.HTTPMaxBody = *patch.Tools.HTTPMaxBody
		}
		if patch.Tools.HTTPTimeoutMs != nil {
			current.Tools.HTTPTimeoutMs = *patch.Tools.HTTPTimeoutMs
		}
		if patch.Tools.HTTPBlockPrivate != nil {
			current.Tools.HTTPBlockPrivate = *patch.Tools.HTTPBlockPrivate
		}
		if patch.Tools.ShellBlockRegexes != nil {
			current.Tools.ShellBlockRegexes = *patch.Tools.ShellBlockRegexes
		}
		if patch.Tools.ShellConfirmRegexes != nil {
			current.Tools.ShellConfirmRegexes = *patch.Tools.ShellConfirmRegexes
		}
		if patch.Tools.ShellMaxOutput != nil {
			current.Tools.ShellMaxOutput = *patch.Tools.ShellMaxOutput
		}
		if patch.Tools.WebSearchTimeoutMs != nil {
			current.Tools.WebSearchTimeoutMs = *patch.Tools.WebSearchTimeoutMs
		}
	}

	// Providers — replace entirely if provided
	if patch.Providers != nil {
		current.Providers = *patch.Providers
	}

	// Models — replace entirely if provided
	if patch.Models != nil {
		current.Models = *patch.Models
	}

	// Embedding
	if patch.Embedding != nil {
		if patch.Embedding.Enabled != nil {
			current.Embedding.Enabled = *patch.Embedding.Enabled
		}
		if patch.Embedding.Providers != nil {
			current.Embedding.Providers = *patch.Embedding.Providers
		}
		if patch.Embedding.Models != nil {
			current.Embedding.Models = *patch.Embedding.Models
		}
	}

	// DefaultModels
	if patch.DefaultModels != nil {
		if patch.DefaultModels.Expert != nil {
			current.DefaultModels.Expert = *patch.DefaultModels.Expert
		}
		if patch.DefaultModels.Superior != nil {
			current.DefaultModels.Superior = *patch.DefaultModels.Superior
		}
		if patch.DefaultModels.Universal != nil {
			current.DefaultModels.Universal = *patch.DefaultModels.Universal
		}
		if patch.DefaultModels.Fast != nil {
			current.DefaultModels.Fast = *patch.DefaultModels.Fast
		}
		if patch.DefaultModels.Fallback != nil {
			current.DefaultModels.Fallback = *patch.DefaultModels.Fallback
		}
	}

	// QQBot
	if patch.QQBot != nil {
		if patch.QQBot.Enabled != nil {
			current.QQBot.Enabled = *patch.QQBot.Enabled
		}
		if patch.QQBot.AppID != nil {
			current.QQBot.AppID = *patch.QQBot.AppID
		}
		if patch.QQBot.AppSecret != nil {
			current.QQBot.AppSecret = *patch.QQBot.AppSecret
		}
		if patch.QQBot.Intents != nil {
			current.QQBot.Intents = *patch.QQBot.Intents
		}
		if patch.QQBot.Sandbox != nil {
			current.QQBot.Sandbox = *patch.QQBot.Sandbox
		}
	}

	// Agent (L1 orchestrator)
	if patch.Agent != nil {
		if patch.Agent.BuiltinMCPServers != nil {
			current.Agent.BuiltinMCPServers = *patch.Agent.BuiltinMCPServers
		}
		if patch.Agent.ExternalMCPServers != nil {
			current.Agent.ExternalMCPServers = *patch.Agent.ExternalMCPServers
		}
	}
}
