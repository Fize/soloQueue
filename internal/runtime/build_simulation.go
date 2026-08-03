package runtime

import (
	"context"
	"path/filepath"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/infra/telemetry"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/simulation"
)

type simChatClient struct {
	inner agent.LLMClient
}

func (s *simChatClient) Chat(ctx context.Context, req agent.LLMRequest) (*agent.LLMResponse, error) {
	simCtx := telemetry.WithTelemetryContext(ctx, "unknown", telemetry.UsageSimulation)
	return s.inner.Chat(simCtx, req)
}

func (s *simChatClient) ChatStream(ctx context.Context, req agent.LLMRequest) (<-chan llm.Event, error) {
	simCtx := telemetry.WithTelemetryContext(ctx, "unknown", telemetry.UsageSimulation)
	return s.inner.ChatStream(simCtx, req)
}

func (bc *buildContext) buildSimulationEngine() error {
	if bc.agentFactory == nil || bc.agentRegistry == nil {
		return nil
	}

	defaultModelID := bc.settings.Simulation.DefaultModelID
	if defaultModelID == "" {
		defaultModelID = bc.fastModelID
	}
	if defaultModelID == "" && bc.defaultModel != nil {
		defaultModelID = bc.defaultModel.ID
	}

	defaultProviderID := bc.settings.Simulation.DefaultProviderID
	if defaultProviderID == "" {
		defaultProviderID = bc.fastModelProviderID
	}
	if defaultProviderID == "" && bc.defaultModel != nil {
		defaultProviderID = bc.defaultModel.ProviderID
	}

	dbPath := bc.settings.Simulation.DBPath
	if dbPath == "" {
		dbPath = filepath.Join(bc.workDir, "simulation.db")
	}

	simCfg := bc.settings.Simulation
	simCfg.DefaultModelID = defaultModelID
	simCfg.DefaultProviderID = defaultProviderID
	simCfg.DBPath = dbPath

	bc.simEngine = simulation.NewSimulationEngine(
		bc.agentFactory,
		bc.agentRegistry,
		&simChatClient{inner: bc.llmClient},
		bc.toolsCfg,
		simCfg,
		bc.log,
	)

	if bc.modelResolver != nil {
		bc.simEngine.WithModelResolver(bc.modelResolver)
	}

	return nil
}
