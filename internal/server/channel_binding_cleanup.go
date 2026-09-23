package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/team/store"
	"gopkg.in/yaml.v3"
)

// clearChannelReferences removes a deleted channel account from L1 and L2
// agent configuration. It deliberately leaves channel runtime settings alone;
// deleting an account must not silently change another account's state.
func (m *Mux) clearChannelReferences(ctx context.Context, channelType, accountID string) error {
	if accountID == "" {
		return nil
	}
	path := filepath.Join(m.workDir, "persona", "roles", "channels.yaml")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	l1Changed := false
	var l1Out []byte
	var l1 struct {
		Channels      map[string]string `yaml:"channels"`
		NotifyChannel string            `yaml:"notify_channel"`
	}
	if err == nil {
		if err := yaml.Unmarshal(data, &l1); err != nil {
			return err
		}
		if l1.Channels[channelType] == accountID {
			delete(l1.Channels, channelType)
			if l1.NotifyChannel == channelType {
				l1.NotifyChannel = ""
			}
			l1Out, err = yaml.Marshal(l1)
			if err != nil {
				return err
			}
			l1Changed = true
		}
	}

	var changedAgents []store.Agent
	if m.teamstore != nil {
		agents, err := m.teamstore.ListAgents(ctx)
		if err != nil {
			return err
		}
		for i := range agents {
			if agents[i].Channels[channelType] != accountID {
				continue
			}
			changedAgents = append(changedAgents, agents[i])
		}
	}
	if len(changedAgents) == 0 && !l1Changed {
		return nil
	}
	rollbackAgents := func() {
		for i := range changedAgents {
			if rollbackErr := m.teamstore.UpdateAgent(ctx, changedAgents[i].Name, &changedAgents[i]); rollbackErr != nil && m.log != nil {
				m.log.Warn("failed to roll back agent channel reference cleanup", "agent", changedAgents[i].Name, "err", rollbackErr.Error())
			}
		}
	}
	for i := range changedAgents {
		updated := changedAgents[i]
		updated.Channels = cloneStringMap(updated.Channels)
		delete(updated.Channels, channelType)
		if updated.NotifyChannel == channelType {
			updated.NotifyChannel = ""
		}
		if err := m.teamstore.UpdateAgent(ctx, changedAgents[i].Name, &updated); err != nil {
			rollbackAgents()
			return fmt.Errorf("clear %s binding from agent %q: %w", channelType, changedAgents[i].Name, err)
		}
	}
	if l1Changed {
		if err := os.WriteFile(path, l1Out, 0644); err != nil {
			rollbackAgents()
			return err
		}
	}
	if m.rebuildPrompt != nil {
		if err := m.rebuildPrompt(); err != nil && m.log != nil {
			m.log.Warn("failed to rebuild prompt after channel reference cleanup", "channel", channelType, "account_id", accountID, "err", err.Error())
		}
	}
	return nil
}

func teamHasChannelBinding(settings config.Settings, team string) bool {
	for _, bot := range settings.QQBots {
		if bot.BindAgent == team {
			return true
		}
	}
	for _, bot := range settings.WechatBots {
		if bot.BindAgent == team {
			return true
		}
	}
	for _, bot := range settings.TelegramBots {
		if bot.BindAgent == team {
			return true
		}
	}
	return false
}
