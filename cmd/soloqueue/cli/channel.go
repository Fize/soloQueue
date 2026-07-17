package cli

import (
	"os"
	"path/filepath"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/channel"
	"github.com/xiaobaitu/soloqueue/internal/conversationlog"
	"github.com/xiaobaitu/soloqueue/internal/logger"
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
		return session.NewErrorChannelAdapter("配置错误：该渠道被绑定到 L2，但尚未指定目标 Agent。")
	}

	var mm *conversationlog.Manager
	if rt.LLMClient != nil {
		memoryDir := filepath.Join(workDir, "memory", binding.bindAgent)
		if err := os.MkdirAll(memoryDir, 0o755); err != nil {
			log.Warn(logger.CatApp, "failed to create channel L2 memory dir", "dir", memoryDir, "err", err)
		}
		model := rt.ReadDefaultModel()
		mm = conversationlog.NewManager(memoryDir, rt.LLMClient, model.ProviderID, model.ID, log)
	}
	adapter := session.NewL2ChannelAdapter(l2Store, binding.channelID, binding.accountID, binding.bindAgent, log, mm)
	adapter.SetSupervisorsFn(supervisorsFn)
	adapter.SetRegistry(registry)
	return adapter
}
