package runtime

import (
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// RegisterHotReload registers a config change callback to keep the Stack's
// cached configuration in sync with the GlobalService.
func RegisterHotReload(rt *Stack, cfg *config.GlobalService, log *logger.Logger, workDir string) {
	cfg.OnChange(func(old, new config.Settings) {
		rt.CfgMu.Lock()
		defer rt.CfgMu.Unlock()

		// 1. Tools config
		newToolsCfg := new.Tools.ToToolsConfig()
		newToolsCfg.PermanentManager = rt.ToolsCfg.PermanentManager
		newToolsCfg.Logger = rt.ToolsCfg.Logger
		newToolsCfg.Executor = rt.ToolsCfg.Executor
		newToolsCfg.PlanDir = rt.ToolsCfg.PlanDir
		rt.ToolsCfg = newToolsCfg
		if rt.AgentFactory != nil {
			rt.AgentFactory.SetToolsConfig(newToolsCfg)
		}

		// 2. Default model
		if newModel := cfg.DefaultModelByRole("fast"); newModel != nil {
			rt.DefaultModel = newModel
			rt.AgentFactory.SetDefaultModelID(newModel.ID)
		}

		// 3. LLM client (only rebuild when default provider changes)
		oldProv := findDefaultProvider(old.Providers)
		newProv := findDefaultProvider(new.Providers)
		if providerChanged(oldProv, newProv) {
			if newClient, err := BuildLLMClient(newProv, log); err == nil {
				rt.LLMClient = newClient
				rt.AgentFactory.SetLLMClient(newClient)
			} else {
				log.Warn(logger.CatConfig, "hot-reload: failed to rebuild LLM client", "err", err)
			}
		}

		// 4. Log level
		if new.Log.Level != old.Log.Level {
			log.SetLevel(logger.ParseLogLevel(new.Log.Level))
			log.Info(logger.CatConfig, "log level hot-reloaded", "level", new.Log.Level)
		}

		// 5. Embedding / Permanent memory
		if embeddingConfigChanged(old.Embedding, new.Embedding) {
			handleEmbeddingChange(rt, new.Embedding, cfg, log, workDir)
		}

		// 6. Agent MCP servers changed → rebuild L1 system prompt
		if !stringSlicesEqual(old.Agent.MCPServers, new.Agent.MCPServers) {
			// Unlock before calling RebuildPrompt (it may take time and
			// RebuildPrompt itself acquires promptRebuildMu).
			rt.CfgMu.Unlock()
			if err := rt.RebuildPrompt(); err != nil {
				log.Warn(logger.CatConfig, "hot-reload: failed to rebuild L1 prompt after agent.mcpServers change", "err", err)
			}
			rt.CfgMu.Lock()
		}

		log.Info(logger.CatConfig, "config hot-reload applied")
	})
}

// findDefaultProvider returns the first provider with IsDefault=true from a slice.
func findDefaultProvider(providers []config.LLMProvider) *config.LLMProvider {
	for i := range providers {
		if providers[i].IsDefault {
			p := providers[i]
			return &p
		}
	}
	return nil
}

// providerChanged returns true if the default provider configuration changed
// in a way that requires recreating the LLM client.
func providerChanged(old, new *config.LLMProvider) bool {
	if old == nil || new == nil {
		return old != new
	}
	return old.BaseURL != new.BaseURL ||
		old.APIKeyEnv != new.APIKeyEnv ||
		old.TimeoutMs != new.TimeoutMs ||
		old.Retry.MaxRetries != new.Retry.MaxRetries ||
		old.Retry.InitialDelayMs != new.Retry.InitialDelayMs ||
		old.Retry.MaxDelayMs != new.Retry.MaxDelayMs ||
		old.Retry.BackoffMultiplier != new.Retry.BackoffMultiplier ||
		!stringMapsEqual(old.Headers, new.Headers)
}

func stringMapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// embeddingConfigChanged returns true if the embedding configuration changed meaningfully.
func embeddingConfigChanged(old, new config.EmbeddingConfig) bool {
	if old.Enabled != new.Enabled {
		return true
	}
	oldModel := findDefaultEmbeddingModel(old.Models)
	newModel := findDefaultEmbeddingModel(new.Models)
	if (oldModel == nil) != (newModel == nil) {
		return true
	}
	if oldModel != nil && newModel != nil {
		if oldModel.ID != newModel.ID || oldModel.ProviderID != newModel.ProviderID {
			return true
		}
	}
	return false
}

func findDefaultEmbeddingModel(models []config.EmbeddingModel) *config.EmbeddingModel {
	for i := range models {
		if models[i].IsDefault {
			m := models[i]
			return &m
		}
	}
	return nil
}

// handleEmbeddingChange handles enabling/disabling/changing the embedding subsystem at runtime.
func handleEmbeddingChange(rt *Stack, emb config.EmbeddingConfig, cfg *config.GlobalService, log *logger.Logger, workDir string) {
	if !emb.Enabled && rt.PermanentMemory != nil {
		// Disable embedding: stop scheduler and remove PermanentManager from toolsCfg.
		log.Info(logger.CatConfig, "embedding disabled at runtime — stopping permanent memory scheduler")
		if rt.PermCancel != nil {
			rt.PermCancel()
		}
		rt.PermScheduler = nil
		rt.PermanentMemory = nil
		rt.ToolsCfg.PermanentManager = nil
		if rt.AgentFactory != nil {
			rt.AgentFactory.SetToolsConfig(rt.ToolsCfg)
		}
		return
	}

	if emb.Enabled && rt.PermanentMemory == nil {
		// Transition from disabled to enabled: full hot-start of embedding subsystem is complex, restart recommended.
		log.Info(logger.CatConfig, "embedding enabled at runtime — restart required to activate")
		return
	}

	// Model or provider changed: restart recommended as well.
	log.Info(logger.CatConfig, "embedding model/provider changed at runtime — restart required for full effect")
}

// stringSlicesEqual returns true if two string slices have the same elements in the same order.
func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

