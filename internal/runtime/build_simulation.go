package runtime

import (
	"github.com/xiaobaitu/soloqueue/internal/simulation"
)

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

	simCfg := simulation.SimulationConfigFile{
		DefaultModelID:        defaultModelID,
		DefaultProviderID:     defaultProviderID,
		DBPath:                bc.settings.Simulation.DBPath,
		DefaultMaxActions:     bc.settings.Simulation.DefaultMaxActions,
		DefaultMaxWallClockMs: bc.settings.Simulation.DefaultMaxWallClockMs,
	}

	bc.simEngine = simulation.NewSimulationEngine(
		bc.agentFactory,
		bc.agentRegistry,
		bc.llmClient,
		bc.toolsCfg,
		simCfg,
		bc.log,
	)

	if bc.memoryEngine != nil {
		bc.simEngine.SetMemoryEngine(bc.memoryEngine)
	}

	return nil
}
