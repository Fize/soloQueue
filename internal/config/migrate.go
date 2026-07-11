package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
	"github.com/xiaobaitu/soloqueue/internal/sqlitedb"
)

// LegacySettings is the old TOML-based settings shape used only for one-time
// migration to settings.yaml.
type LegacySettings struct {
	Session       SessionConfig       `toml:"session"`
	Auth          AuthConfig          `toml:"auth"`
	Log           LogConfig           `toml:"log"`
	Tools         ToolsConfig         `toml:"tools"`
	Providers     []LLMProvider       `toml:"providers"`
	Models        []LLMModel          `toml:"models"`
	Embedding     EmbeddingConfig     `toml:"embedding"`
	DefaultModels DefaultModelsConfig `toml:"default_models"`
	QQBots        []QQBotConfig       `toml:"qqbots"`
	Agent         AgentConfig         `toml:"agent"`
	LSPMCP        LSPMCPConfig        `toml:"lspmcp"`
	Simulation    SimulationConfig    `toml:"simulation"`
}

// ToSettings converts LegacySettings to the current Settings shape.
func (l LegacySettings) ToSettings() Settings {
	return Settings{
		Session:       l.Session,
		Auth:          l.Auth,
		Log:           l.Log,
		Tools:         l.Tools,
		Providers:     l.Providers,
		Models:        l.Models,
		Embedding:     l.Embedding,
		DefaultModels: l.DefaultModels,
		QQBots:        l.QQBots,
		Agent:         l.Agent,
		LSPMCP:        l.LSPMCP,
		Simulation:    l.Simulation,
	}
}

// MigrateIfNeeded migrates legacy configuration (settings.toml and SQLite
// configuration tables) into settings.yaml. It is a no-op if settings.yaml
// already exists. It uses the current in-memory Settings as a base and returns
// the resulting snapshot.
func (s *GlobalService) MigrateIfNeeded() (Settings, error) {
	yamlPath := filepath.Join(s.workDir, "settings.yaml")
	if _, err := os.Stat(yamlPath); err == nil {
		return s.Get(), nil
	} else if !os.IsNotExist(err) {
		return Settings{}, fmt.Errorf("check settings.yaml: %w", err)
	}

	merged, err := buildMergedSettings(s.workDir, s.GetDB())
	if err != nil {
		return Settings{}, err
	}

	if err := s.normalize(&merged); err != nil {
		return Settings{}, err
	}

	snapshot, err := s.Set(func(st *Settings) {
		*st = merged
	})
	return snapshot, err
}

// normalize fills in derived defaults for settings that may be empty after load
// or migration.
func (s *GlobalService) normalize(st *Settings) error {
	if st.Simulation.DBPath == "" {
		st.Simulation.DBPath = filepath.Join(s.workDir, "simulation.db")
	}
	return nil
}

// buildMergedSettings constructs a Settings value from defaults, legacy TOML
// and the SQLite configuration tables. DB-backed values take precedence over
// legacy TOML values for the migrated configuration fields.
func buildMergedSettings(workDir string, db *sqlitedb.DB) (Settings, error) {
	settings := DefaultSettings()

	tomlPath := filepath.Join(workDir, "settings.toml")
	tomlData, err := os.ReadFile(tomlPath)
	if err != nil && !os.IsNotExist(err) {
		return Settings{}, fmt.Errorf("read legacy settings.toml: %w", err)
	}
	if err == nil {
		var legacy LegacySettings
		if err := toml.Unmarshal(tomlData, &legacy); err != nil {
			return Settings{}, fmt.Errorf("parse legacy settings.toml: %w", err)
		}
		legacySettings := legacy.ToSettings()
		settings.Auth = legacySettings.Auth
		settings.Log = legacySettings.Log
		settings.Agent = legacySettings.Agent
		settings.Session = legacySettings.Session
		settings.Tools = legacySettings.Tools
		settings.Providers = legacySettings.Providers
		settings.Models = legacySettings.Models
		settings.Embedding = legacySettings.Embedding
		settings.DefaultModels = legacySettings.DefaultModels
		settings.QQBots = legacySettings.QQBots
		settings.LSPMCP = legacySettings.LSPMCP
		settings.Simulation = legacySettings.Simulation
	}

	if db != nil {
		ctx := context.Background()
		if providers, err := LoadProviders(ctx, db); err == nil && len(providers) > 0 {
			settings.Providers = providers
		}
		if models, err := LoadModels(ctx, db); err == nil && len(models) > 0 {
			settings.Models = models
		}
		if defaultModels, err := LoadDefaultModels(ctx, db); err == nil {
			settings.DefaultModels = defaultModels
		}
		var tools ToolsConfig
		if ok, err := LoadSystemSetting(ctx, db, "tools", &tools); err == nil && ok {
			settings.Tools = tools
		}
		var qqbots []QQBotConfig
		if ok, err := LoadSystemSetting(ctx, db, "qqbots", &qqbots); err == nil && ok {
			settings.QQBots = qqbots
		}
		var lspmcp LSPMCPConfig
		if ok, err := LoadSystemSetting(ctx, db, "lspmcp", &lspmcp); err == nil && ok {
			settings.LSPMCP = lspmcp
		}
		var embedding EmbeddingConfig
		if ok, err := LoadSystemSetting(ctx, db, "embedding", &embedding); err == nil && ok {
			settings.Embedding = embedding
		}
		var session SessionConfig
		if ok, err := LoadSystemSetting(ctx, db, "session", &session); err == nil && ok {
			settings.Session = session
		}
		var simulation SimulationConfig
		if ok, err := LoadSystemSetting(ctx, db, "simulation", &simulation); err == nil && ok {
			settings.Simulation = simulation
		}
	}

	return settings, nil
}
