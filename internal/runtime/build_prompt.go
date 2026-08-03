package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/prompt"
	"github.com/xiaobaitu/soloqueue/internal/team/store"
	"gopkg.in/yaml.v3"
)

// buildPrompt initializes the prompt configuration, groups, templates, and the L1 system prompt.
func (bc *buildContext) buildPrompt() error {
	promptStart := time.Now()
	promptCfg := &prompt.PromptConfig{
		RolesDir:  filepath.Join(bc.workDir, "persona", "roles"),
		GlobalDir: filepath.Join(bc.workDir, "persona", "global"),
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

	// ── L1 channel config from persona/roles/channels.yaml ─────────────────
	if l1Ch, l1Nc := loadL1ChannelConfig(bc.log, filepath.Join(bc.workDir, "persona", "roles")); l1Ch != nil {
		bc.l1Channels = l1Ch
		bc.l1NotifyChannel = l1Nc
	}

	// ── Validate channel bindings ──────────────────────────────────────────
	// Warn about mismatches between agent channel declarations and channel configs.
	validateAndWarnChannelBindings(bc.log, bc.allTemplates, bc.cfg.Get())

	// ── DB-backed override (teamstore) ────────────────────────────────────
	if bc.teamstore != nil {
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
		cfg := bc.cfg.Get()
		mcpServers = gatherMCPServerNames(bc.mcpMgr, bc.lspMgr, cfg.Agent.ExternalMCPServers, cfg.Agent.BuiltinMCPServers)
	}
	bc.mcpServers = mcpServers

	systemPrompt, err := promptCfg.BuildPrompt(bc.leaders, bc.groups, bc.memoryDir, bc.memoryDir, bc.planDir, mcpServers)
	if err != nil {
		return fmt.Errorf("build system prompt: %w", err)
	}
	bc.systemPrompt = systemPrompt

	return nil
}

// loadFromTeamStore loads teams and agents from the DB-backed store and converts
// them to the in-memory types used by the prompt and agent systems.
func loadFromTeamStore(store *store.Store) (map[string]prompt.GroupFile, []prompt.LeaderInfo, []agent.AgentTemplate, error) {
	// Load teams → GroupFile map
	teams, err := store.ListTeams(context.Background())
	if err != nil {
		return nil, nil, nil, fmt.Errorf("list teams: %w", err)
	}

	groups := make(map[string]prompt.GroupFile, len(teams))
	for _, t := range teams {
		groups[t.Name] = prompt.GroupFile{
			Frontmatter: prompt.GroupFrontmatter{
				Name: t.Name,
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
			ID:            dbTmpl.ID,
			Name:          dbTmpl.Name,
			Description:   dbTmpl.Description,
			SystemPrompt:  dbTmpl.SystemPrompt,
			ModelID:       dbTmpl.ModelID,
			IsLeader:      dbTmpl.IsLeader,
			Group:         dbTmpl.Group,
			Permission:    dbTmpl.Permission,
			MCPServers:    dbTmpl.MCPServers,
			SkillIDs:      dbTmpl.SkillIDs,
			Channels:      dbTmpl.Channels,
			NotifyChannel: dbTmpl.NotifyChannel,
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

// validateAndWarnChannelBindings checks bidirectional consistency between agent
// channel declarations and channel config bind_agent settings. Mismatches are
// logged as warnings rather than blocking startup, allowing gradual adoption.
func validateAndWarnChannelBindings(log *logger.Logger, templates []agent.AgentTemplate, settings config.Settings) {
	for _, tmpl := range templates {
		if len(tmpl.Channels) == 0 && tmpl.NotifyChannel == "" {
			continue
		}

		// 1. notify_channel must exist in channels
		if tmpl.NotifyChannel != "" {
			if _, ok := tmpl.Channels[tmpl.NotifyChannel]; !ok {
				log.Warn(logger.CatConfig, "agent notify_channel not in channels",
					"agent", tmpl.ID, "notify_channel", tmpl.NotifyChannel)
			}
		}

		// 2. agent↔channel config bidirectional checks
		for chType, instID := range tmpl.Channels {
			switch chType {
			case "qq":
				found := false
				for _, qb := range settings.QQBots {
					if qb.ID == instID {
						if qb.BindAgent != "" && strings.EqualFold(qb.BindAgent, tmpl.ID) {
							found = true
						}
						break
					}
				}
				if !found {
					log.Warn(logger.CatConfig, "agent channel binding mismatch",
						"agent", tmpl.ID, "channel_type", "qq", "instance_id", instID,
						"hint", "ensure qqbot config has bind_agent="+tmpl.ID)
				}
			case "wechat":
				found := false
				for _, wb := range settings.WechatBots {
					if wb.ID == instID {
						if wb.BindAgent != "" && strings.EqualFold(wb.BindAgent, tmpl.ID) {
							found = true
						}
						break
					}
				}
				if !found {
					log.Warn(logger.CatConfig, "agent channel binding mismatch",
						"agent", tmpl.ID, "channel_type", "wechat", "instance_id", instID,
						"hint", "ensure wechat bot config has bind_agent="+tmpl.ID)
				}
			}
		}
	}
}

// loadL1ChannelConfig reads L1 agent's channel bindings from
// persona/roles/channels.yaml. Returns nil channels if the file doesn't exist.
func loadL1ChannelConfig(log *logger.Logger, rolesDir string) (map[string]string, string) {
	path := filepath.Join(rolesDir, "channels.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Warn(logger.CatConfig, "failed to read L1 channel config", "path", path, "err", err.Error())
		}
		return nil, ""
	}

	var l1ch struct {
		Channels      map[string]string `yaml:"channels"`
		NotifyChannel string            `yaml:"notify_channel"`
	}
	if err := yaml.Unmarshal(data, &l1ch); err != nil {
		log.Warn(logger.CatConfig, "failed to parse L1 channel config", "path", path, "err", err.Error())
		return nil, ""
	}
	if len(l1ch.Channels) == 0 && l1ch.NotifyChannel == "" {
		return nil, ""
	}
	return l1ch.Channels, l1ch.NotifyChannel
}
