package runtime

import (
	"github.com/xiaobaitu/soloqueue/internal/simulation"
)

func (bc *buildContext) buildSimulationEngine() error {
	if bc.agentFactory == nil || bc.agentRegistry == nil {
		return nil
	}

	simCfg := simulation.SimulationConfigFile{
		DefaultModelID:        bc.settings.Simulation.DefaultModelID,
		DefaultProviderID:     bc.settings.Simulation.DefaultProviderID,
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
