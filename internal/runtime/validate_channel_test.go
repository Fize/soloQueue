package runtime

import (
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

func newTestLoggerForValidation(t *testing.T) *logger.Logger {
	t.Helper()
	l, err := logger.System(t.TempDir(), logger.WithConsole(false), logger.WithFile(false))
	if err != nil {
		t.Fatalf("create test logger: %v", err)
	}
	t.Cleanup(func() { l.Close() })
	return l
}

func TestValidateChannelBindings_AllValid(t *testing.T) {
	log := newTestLoggerForValidation(t)
	templates := []agent.AgentTemplate{
		{
			ID:            "engineering",
			Channels:      map[string]string{"qq": "bot-a"},
			NotifyChannel: "qq",
		},
	}
	settings := config.Settings{
		QQBots: []config.QQBotConfig{
			{ID: "bot-a", BindAgent: "engineering"},
		},
	}

	// Should not panic.
	validateAndWarnChannelBindings(log, templates, settings)
}

func TestValidateChannelBindings_NotifyChannelNotInChannels(t *testing.T) {
	log := newTestLoggerForValidation(t)
	templates := []agent.AgentTemplate{
		{
			ID:            "engineering",
			Channels:      map[string]string{"qq": "bot-a"},
			NotifyChannel: "wechat", // not in channels
		},
	}
	settings := config.Settings{
		QQBots: []config.QQBotConfig{
			{ID: "bot-a", BindAgent: "engineering"},
		},
	}

	// Should not panic, logs warning.
	validateAndWarnChannelBindings(log, templates, settings)
}

func TestValidateChannelBindings_AgentChannelMismatchConfig(t *testing.T) {
	log := newTestLoggerForValidation(t)
	templates := []agent.AgentTemplate{
		{
			ID:       "engineering",
			Channels: map[string]string{"qq": "bot-a"},
		},
	}
	settings := config.Settings{
		QQBots: []config.QQBotConfig{
			{ID: "bot-a", BindAgent: "other-team"}, // mismatch
		},
	}

	// Should not panic, logs warning.
	validateAndWarnChannelBindings(log, templates, settings)
}

func TestValidateChannelBindings_ConfigBindsButAgentNoChannel(t *testing.T) {
	log := newTestLoggerForValidation(t)
	templates := []agent.AgentTemplate{
		{
			ID:       "engineering",
			Channels: map[string]string{}, // no channels
		},
	}
	settings := config.Settings{
		QQBots: []config.QQBotConfig{
			{ID: "bot-a", BindAgent: "engineering"},
		},
	}

	// No channels declared, validation is skipped early.
	validateAndWarnChannelBindings(log, templates, settings)
}

func TestValidateChannelBindings_NoChannels(t *testing.T) {
	log := newTestLoggerForValidation(t)
	templates := []agent.AgentTemplate{
		{ID: "simple-agent"}, // no Channels field
	}
	settings := config.Settings{}

	// Should not panic.
	validateAndWarnChannelBindings(log, templates, settings)
}

func TestValidateChannelBindings_WechatMismatch(t *testing.T) {
	log := newTestLoggerForValidation(t)
	templates := []agent.AgentTemplate{
		{
			ID:       "engineering",
			Channels: map[string]string{"wechat": "wx1"},
		},
	}
	settings := config.Settings{
		WechatBots: []config.WechatBotConfig{
			{ID: "wx1", BindAgent: "different-agent"},
		},
	}

	// Should not panic.
	validateAndWarnChannelBindings(log, templates, settings)
}

func TestValidateChannelBindings_MultipleAgents(t *testing.T) {
	log := newTestLoggerForValidation(t)
	templates := []agent.AgentTemplate{
		{
			ID:            "team-a",
			Channels:      map[string]string{"qq": "bot-a"},
			NotifyChannel: "qq",
		},
		{
			ID:       "team-b",
			Channels: map[string]string{"qq": "bot-b"},
		},
	}
	settings := config.Settings{
		QQBots: []config.QQBotConfig{
			{ID: "bot-a", BindAgent: "team-a"},
			{ID: "bot-b", BindAgent: "team-b"},
		},
	}

	// All valid, should not panic.
	validateAndWarnChannelBindings(log, templates, settings)
}

func TestValidateChannelBindings_EmptyChannels(t *testing.T) {
	log := newTestLoggerForValidation(t)
	templates := []agent.AgentTemplate{
		{
			ID:       "agent",
			Channels: map[string]string{},
		},
	}
	settings := config.Settings{}

	// Skipped because len(channels) == 0 && notifyChannel == "".
	validateAndWarnChannelBindings(log, templates, settings)
}
