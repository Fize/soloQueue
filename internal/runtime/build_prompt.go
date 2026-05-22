package runtime

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/prompt"
	"github.com/xiaobaitu/soloqueue/internal/teamstore"
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

	// ── DB-backed override (teamstore) ────────────────────────────────────
	if bc.teamstore != nil {
		groupsDir := filepath.Join(bc.workDir, "groups")
		agentsDir := filepath.Join(bc.workDir, "agents")
		if err := bc.teamstore.MigrateFromFiles(groupsDir, agentsDir); err != nil {
			bc.log.Warn(logger.CatApp, "migrate teams/agents to DB failed", "err", err.Error())
		}
		// Override with DB data
		dbGroups, dbLeaders, dbTemplates, err := loadFromTeamStore(bc.teamstore)
		if err != nil {
			bc.log.Warn(logger.CatApp, "load from teamstore failed", "err", err.Error())
		} else {
			bc.groups = dbGroups
			bc.leaders = dbLeaders
			bc.allTemplates = dbTemplates
		}
	}

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

// loadFromTeamStore loads teams and agents from the DB-backed store and converts
// them to the in-memory types used by the prompt and agent systems.
func loadFromTeamStore(store *teamstore.Store) (map[string]prompt.GroupFile, []prompt.LeaderInfo, []agent.AgentTemplate, error) {
	// Load teams → GroupFile map
	teams, err := store.ListTeams(context.Background())
	if err != nil {
		return nil, nil, nil, fmt.Errorf("list teams: %w", err)
	}

	groups := make(map[string]prompt.GroupFile, len(teams))
	for _, t := range teams {
		workspaces := make([]prompt.Workspace, 0, len(t.Workspaces))
		for _, w := range t.Workspaces {
			workspaces = append(workspaces, prompt.Workspace{
				Name: w.Name,
				Path: w.Path,
				AutoWork: prompt.AutoWorkConfig{
					Enabled:                 w.AutoWork.Enabled,
					InitialCooldownMinutes:  w.AutoWork.InitialCooldownMinutes,
					PostTaskCooldownMinutes: w.AutoWork.PostTaskCooldownMinutes,
					MaxIntervalsPerDay:      w.AutoWork.MaxIntervalsPerDay,
				},
			})
		}
		groups[t.Name] = prompt.GroupFile{
			Frontmatter: prompt.GroupFrontmatter{
				Name:       t.Name,
				Workspaces: workspaces,
			},
			Body: t.Description,
		}
	}

	// Load agents → LeaderInfo + AgentTemplate slices
	agents, err := store.ListAgents(context.Background())
	if err != nil {
		return nil, nil, nil, fmt.Errorf("list agents: %w", err)
	}

	var leaders []prompt.LeaderInfo
	var templates []agent.AgentTemplate

	for _, a := range agents {
		dbTmpl := a.ToAgentTemplate()
		tmpl := agent.AgentTemplate{
			ID:           dbTmpl.ID,
			Name:         dbTmpl.Name,
			Description:  dbTmpl.Description,
			SystemPrompt: dbTmpl.SystemPrompt,
			ModelID:      dbTmpl.ModelID,
			IsLeader:     dbTmpl.IsLeader,
			Group:        dbTmpl.Group,
			Permission:   dbTmpl.Permission,
			MCPServers:   dbTmpl.MCPServers,
			SkillIDs:     dbTmpl.SkillIDs,
		}
		templates = append(templates, tmpl)

		if a.IsLeader {
			li := prompt.LeaderInfo{
				Name:        a.Name,
				Description: a.Description,
				Group:       a.TeamName,
			}
			if gf, ok := groups[a.TeamName]; ok {
				li.GroupDescription = gf.Body
			}
			leaders = append(leaders, li)
		}
	}

	return groups, leaders, templates, nil
}
