// channel.go routes messaging channels (QQ, WeChat) to session providers.
// L1 channels share the global session; L2 channels get an isolated session
// with a dedicated agent and per-channel memory context.
package cli

import (
	"os"
	"path/filepath"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/channel"
	"github.com/xiaobaitu/soloqueue/internal/memory/conversation"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/runtime"
	"github.com/xiaobaitu/soloqueue/internal/session"
)

type channelBinding struct {
	channelID string
	accountID string
	bindType  string
	bindAgent string
}

func newChannelSessionProvider(binding channelBinding, mgr *session.SessionManager, l2Store *session.L2SessionStore, rt *runtime.Stack, workDir string, log *logger.Logger, supervisorsFn func() []*agent.Supervisor, registry *agent.Registry) channel.SessionProvider {
	if binding.bindType != "l2" {
		adapter := session.NewChannelAdapter(mgr, log)
		adapter.SetSupervisorsFn(supervisorsFn)
		adapter.SetRegistry(registry)
		return adapter
	}
	if binding.bindAgent == "" {
		log.Error(logger.CatApp, "channel configured as L2 but missing bind_agent", "channel", binding.channelID, "account_id", binding.accountID)
		return session.NewErrorChannelAdapter("Configuration error: channel is bound to L2 but missing target agent.")
	}

	var mm *conversation.Manager
	if rt.LLMClient != nil {
		memoryDir := filepath.Join(workDir, "memory", binding.bindAgent)
		if err := os.MkdirAll(memoryDir, 0o755); err != nil {
			log.Warn(logger.CatApp, "failed to create channel L2 memory dir", "dir", memoryDir, "err", err)
		}
		model := rt.ReadDefaultModel()
		mm = conversation.NewManager(memoryDir, rt.LLMClient, model.ProviderID, model.ID, log)
	}
	adapter := session.NewL2ChannelAdapter(l2Store, binding.channelID, binding.accountID, binding.bindAgent, log, mm)
	adapter.SetSupervisorsFn(supervisorsFn)
	adapter.SetRegistry(registry)
	return adapter
}
