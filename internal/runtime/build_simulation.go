package runtime

import (
	"path/filepath"

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

	dbPath := bc.settings.Simulation.DBPath
	if dbPath == "" {
		dbPath = filepath.Join(bc.workDir, "simulation.db")
	}

	simCfg := simulation.SimulationConfigFile{
		DefaultModelID:        defaultModelID,
		DefaultProviderID:     defaultProviderID,
		DBPath:                dbPath,
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

	if bc.modelResolver != nil {
		bc.simEngine.WithModelResolver(bc.modelResolver)
	}

	if bc.memoryEngine != nil {
		bc.simEngine.SetMemoryEngine(bc.memoryEngine)
	}

	return nil
}
