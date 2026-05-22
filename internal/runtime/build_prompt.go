package runtime

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/prompt"
)

// buildPrompt initializes the prompt configuration, groups, templates, and the L1 system prompt.
func (bc *buildContext) buildPrompt() error {
	promptStart := time.Now()
	promptCfg := &prompt.PromptConfig{
		RolesDir:  filepath.Join(bc.workDir, "prompts", "roles"),
		GlobalDir: filepath.Join(bc.workDir, "prompts", "global"),
	}
	rulesCreated, err := promptCfg.EnsureFiles()
	if err != nil {
		var profileErr *prompt.SoulNeededError
		if errors.As(err, &profileErr) {
			if writeErr := bc.profileSetup(promptCfg); writeErr != nil {
				return fmt.Errorf("write soul: %w", writeErr)
			}
			rulesCreated, err = promptCfg.EnsureFiles()
			if err != nil {
				return fmt.Errorf("ensure prompt files: %w", err)
			}
		} else {
			return fmt.Errorf("ensure prompt files: %w", err)
		}
	}
	bc.promptCfg = promptCfg
	bc.rulesCreated = rulesCreated
	bc.log.Debug(logger.CatApp, "build: prompt system ready", "duration", time.Since(promptStart).String())

	// ── Groups ─────────────────────────────────────────────────────────────
	groups, err := prompt.LoadGroups(filepath.Join(bc.workDir, "groups"))
	if err != nil {
		bc.log.Warn(logger.CatApp, "failed to load groups", "err", err.Error())
		groups = nil
	}
	bc.groups = groups

	// ── Leaders + Agent Templates ────────────────────────────────────────────────
	leaders, err := prompt.LoadLeaders(filepath.Join(bc.workDir, "agents"), groups)
	if err != nil {
		bc.log.Warn(logger.CatApp, "failed to load leaders", "err", err.Error())
		leaders = nil
	}
	bc.leaders = leaders

	allTemplates, err := agent.LoadAgentTemplates(filepath.Join(bc.workDir, "agents"))
	if err != nil {
		bc.log.Warn(logger.CatApp, "failed to load agent templates", "err", err.Error())
		allTemplates = nil
	}
	bc.allTemplates = allTemplates

	// ── Build L1 System Prompt ───────────────────────────────────────────────
	var mcpServers []string
	if bc.mcpMgr != nil {
		// External MCP servers (from mcp.json)
		externalAllowed := bc.cfg.Get().Agent.ExternalMCPServers
		var externalSet map[string]bool
		if externalAllowed != nil {
			externalSet = make(map[string]bool, len(externalAllowed))
			for _, name := range externalAllowed {
				externalSet[name] = true
			}
		}
		for _, srv := range bc.mcpMgr.Loader().Get().Servers {
			if srv.Enabled && (externalSet == nil || externalSet[srv.Name]) {
				mcpServers = append(mcpServers, srv.Name)
			}
		}

		// Builtin MCP servers (e.g. builtin-lsp)
		builtinAllowed := bc.cfg.Get().Agent.BuiltinMCPServers
		var builtinSet map[string]bool
		if builtinAllowed != nil {
			builtinSet = make(map[string]bool, len(builtinAllowed))
			for _, name := range builtinAllowed {
				builtinSet[name] = true
			}
		}
		if bc.lspMgr != nil && (builtinSet == nil || builtinSet["builtin-lsp"]) {
			mcpServers = append(mcpServers, "builtin-lsp")
		}
	}
	bc.mcpServers = mcpServers

	systemPrompt, err := promptCfg.BuildPrompt(leaders, groups, bc.memoryDir, bc.memoryDir, bc.planDir, mcpServers)
	if err != nil {
		return fmt.Errorf("build system prompt: %w", err)
	}
	bc.systemPrompt = systemPrompt

	return nil
}
