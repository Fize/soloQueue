package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/config"
	"gopkg.in/yaml.v3"
)

// Prepare before writing the agent so invalid or conflicting selections cannot
// be saved. L2 channel sessions are addressed by team, not agent template name.
func (m *Mux) prepareTelegramBinding(ctx context.Context, owner, oldID, newID, team string) ([]config.TelegramBotConfig, error) {
	if oldID == "" && newID == "" {
		return nil, nil
	}
	if m.configSvc == nil {
		return nil, fmt.Errorf("config service not available")
	}
	bots := append([]config.TelegramBotConfig(nil), m.configSvc.Get().TelegramBots...)
	found := newID == ""
	for _, bot := range bots {
		found = found || bot.ID == newID
	}
	if !found {
		return nil, fmt.Errorf("Telegram bot no longer exists; refresh the bot list")
	}
	if newID != "" {
		if owner != "" {
			var l1 struct {
				Channels map[string]string `yaml:"channels"`
			}
			data, err := os.ReadFile(filepath.Join(m.workDir, "persona", "roles", "channels.yaml"))
			if err != nil && !os.IsNotExist(err) {
				return nil, err
			}
			if err == nil {
				if err := yaml.Unmarshal(data, &l1); err != nil {
					return nil, err
				}
				if l1.Channels["telegram"] == newID {
					return nil, fmt.Errorf("Telegram bot is bound to L1; unbind it first")
				}
			}
		}
		agents, err := m.teamstore.ListAgents(ctx)
		if err != nil {
			return nil, err
		}
		for _, a := range agents {
			if !strings.EqualFold(a.Name, owner) && a.Channels["telegram"] == newID {
				return nil, fmt.Errorf("Telegram bot is bound to %s; unbind it first", a.Name)
			}
		}
	}
	changed := false
	for i := range bots {
		previous := bots[i]
		if bots[i].ID == oldID && oldID != newID {
			bots[i].Enabled = false
			bots[i].BindType, bots[i].BindAgent = "l1", ""
		}
		if bots[i].ID == newID {
			bots[i].BindType, bots[i].BindAgent = "l1", ""
			if owner != "" {
				bots[i].BindType, bots[i].BindAgent = "l2", team
			}
		}
		changed = changed || previous.Enabled != bots[i].Enabled || previous.BindType != bots[i].BindType || previous.BindAgent != bots[i].BindAgent
	}
	if !changed {
		return nil, nil
	}
	return bots, nil
}

func (m *Mux) saveTelegramBinding(bots []config.TelegramBotConfig) error {
	if bots == nil {
		return nil
	}
	if err := m.configSvc.UpdateTelegramBots(bots); err != nil {
		return fmt.Errorf("agent settings saved, but Telegram routing could not be saved; retry: %w", err)
	}
	m.triggerOnConfigChange()
	return nil
}
