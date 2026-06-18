package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/xiaobaitu/soloqueue/internal/sqlitedb"
)

// GlobalService is the global configuration service, embedding Loader[Settings]
// Automatically inherits all Loader methods: Load / Save / Get / Set.
// Provides business-related convenience query interfaces on top of that
type GlobalService struct {
	*Loader[Settings]
	workDir    string
	db         *sqlitedb.DB
	dbSettings Settings
	dbMu       sync.RWMutex
	hasDB      bool
}

// New creates a GlobalService
// workDir is typically ~/.soloqueue
func New(workDir string) (*GlobalService, error) {
	mainPath := filepath.Join(workDir, "settings.toml")
	localPath := filepath.Join(workDir, "settings.local.toml")

	loader, err := NewLoader(DefaultSettings(), mainPath, localPath)
	if err != nil {
		return nil, fmt.Errorf("config.New: %w", err)
	}

	return &GlobalService{Loader: loader, workDir: workDir}, nil
}

// Get returns the current config snapshot (DB-backed values override file values if DB is ready)
func (s *GlobalService) Get() Settings {
	s.dbMu.RLock()
	if s.hasDB {
		fileSettings := s.Loader.Get()
		fileSettings.Providers = s.dbSettings.Providers
		fileSettings.Models = s.dbSettings.Models
		fileSettings.DefaultModels = s.dbSettings.DefaultModels
		fileSettings.Tools = s.dbSettings.Tools
		fileSettings.QQBot = s.dbSettings.QQBot
		fileSettings.LSPMCP = s.dbSettings.LSPMCP
		fileSettings.Embedding = s.dbSettings.Embedding
		fileSettings.Session = s.dbSettings.Session
		fileSettings.Simulation = s.dbSettings.Simulation
		s.dbMu.RUnlock()
		return fileSettings
	}
	s.dbMu.RUnlock()
	return s.Loader.Get()
}

// SetDB sets the SQLite connection and syncs/loads configurations
func (s *GlobalService) SetDB(db *sqlitedb.DB) error {
	s.dbMu.Lock()
	s.db = db
	s.dbMu.Unlock()

	if err := s.seedDatabaseIfNeeded(); err != nil {
		return err
	}

	// Save configuration back to settings.toml to clean up/strip migrated keys
	if err := s.Loader.Save(); err != nil {
		// Non-fatal: don't fail startup if file write fails, but we should proceed.
	}

	return s.ReloadFromDB()
}

// GetDB returns the database connection of the config service.
func (s *GlobalService) GetDB() *sqlitedb.DB {
	s.dbMu.RLock()
	defer s.dbMu.RUnlock()
	return s.db
}

func (s *GlobalService) seedDatabaseIfNeeded() error {
	s.dbMu.RLock()
	db := s.db
	s.dbMu.RUnlock()
	if db == nil {
		return nil
	}

	ctx := context.Background()
	defaultSettings := DefaultSettings()

	// Check if providers are empty
	var count int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM llm_providers`).Scan(&count)
	if err != nil {
		return fmt.Errorf("seed check: %w", err)
	}

	if count == 0 {
		// 1. Seed providers
		for _, p := range defaultSettings.Providers {
			if err := SaveProvider(ctx, db, p); err != nil {
				return fmt.Errorf("seed provider %s: %w", p.ID, err)
			}
		}

		// 2. Seed models
		for _, m := range defaultSettings.Models {
			if err := SaveModel(ctx, db, m); err != nil {
				return fmt.Errorf("seed model %s: %w", m.ID, err)
			}
		}

		// 3. Seed default models config
		if err := SaveDefaultModels(ctx, db, defaultSettings.DefaultModels); err != nil {
			return fmt.Errorf("seed default models: %w", err)
		}
	}

	// 4. Ensure system_settings table exists (may have been missed by migration)
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS system_settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`); err != nil {
		return fmt.Errorf("seed check: ensure system_settings table: %w", err)
	}

	// 5. Seed system settings individually
	hasSetting := func(key string) bool {
		var cnt int
		_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM system_settings WHERE key = ?`, key).Scan(&cnt)
		return cnt > 0
	}

	if !hasSetting("tools") {
		if err := SaveSystemSetting(ctx, db, "tools", defaultSettings.Tools); err != nil {
			return fmt.Errorf("seed tools: %w", err)
		}
	}
	if !hasSetting("qqbot") {
		if err := SaveSystemSetting(ctx, db, "qqbot", defaultSettings.QQBot); err != nil {
			return fmt.Errorf("seed qqbot: %w", err)
		}
	}
	if !hasSetting("lspmcp") {
		if err := SaveSystemSetting(ctx, db, "lspmcp", defaultSettings.LSPMCP); err != nil {
			return fmt.Errorf("seed lspmcp: %w", err)
		}
	}
	if !hasSetting("embedding") {
		if err := SaveSystemSetting(ctx, db, "embedding", defaultSettings.Embedding); err != nil {
			return fmt.Errorf("seed embedding: %w", err)
		}
	}
	if !hasSetting("session") {
		if err := SaveSystemSetting(ctx, db, "session", defaultSettings.Session); err != nil {
			return fmt.Errorf("seed session: %w", err)
		}
	}
	if !hasSetting("simulation") {
		simCfg := defaultSettings.Simulation
		if simCfg.DBPath == "" {
			simCfg.DBPath = filepath.Join(s.workDir, "simulation.db")
		}
		if err := SaveSystemSetting(ctx, db, "simulation", simCfg); err != nil {
			return fmt.Errorf("seed simulation: %w", err)
		}
	}

	return nil
}

// ReloadFromDB loads configuration from SQLite into memory cache
func (s *GlobalService) ReloadFromDB() error {
	s.dbMu.RLock()
	db := s.db
	s.dbMu.RUnlock()
	if db == nil {
		return nil
	}

	ctx := context.Background()

	providers, err := LoadProviders(ctx, db)
	if err != nil {
		return err
	}

	models, err := LoadModels(ctx, db)
	if err != nil {
		return err
	}

	defaultModels, err := LoadDefaultModels(ctx, db)
	if err != nil {
		return err
	}

	var tools ToolsConfig
	if ok, err := LoadSystemSetting(ctx, db, "tools", &tools); err != nil {
		return err
	} else if !ok {
		tools = DefaultSettings().Tools
	}

	var qqbot QQBotConfig
	if ok, err := LoadSystemSetting(ctx, db, "qqbot", &qqbot); err != nil {
		return err
	} else if !ok {
		qqbot = DefaultSettings().QQBot
	}

	var lspmcp LSPMCPConfig
	if ok, err := LoadSystemSetting(ctx, db, "lspmcp", &lspmcp); err != nil {
		return err
	} else if !ok {
		lspmcp = DefaultSettings().LSPMCP
	}

	var embedding EmbeddingConfig
	if ok, err := LoadSystemSetting(ctx, db, "embedding", &embedding); err != nil {
		return err
	} else if !ok {
		embedding = DefaultSettings().Embedding
	}

	var session SessionConfig
	if ok, err := LoadSystemSetting(ctx, db, "session", &session); err != nil {
		return err
	} else if !ok {
		session = DefaultSettings().Session
	}

	var simulation SimulationConfig
	if ok, err := LoadSystemSetting(ctx, db, "simulation", &simulation); err != nil {
		return err
	} else if !ok {
		simulation = DefaultSettings().Simulation
	}
	if simulation.DBPath == "" {
		simulation.DBPath = filepath.Join(s.workDir, "simulation.db")
	}

	s.dbMu.Lock()
	s.dbSettings.Providers = providers
	s.dbSettings.Models = models
	s.dbSettings.DefaultModels = defaultModels
	s.dbSettings.Tools = tools
	s.dbSettings.QQBot = qqbot
	s.dbSettings.LSPMCP = lspmcp
	s.dbSettings.Embedding = embedding
	s.dbSettings.Session = session
	s.dbSettings.Simulation = simulation
	s.hasDB = true
	s.dbMu.Unlock()

	return nil
}

// LoadFromDisk reads settings from disk without modifying the loader cache.
func (s *GlobalService) LoadFromDisk() (Settings, error) {
	settings, err := s.Loader.ReadFromDisk()
	if err != nil {
		return settings, err
	}
	s.dbMu.RLock()
	if s.hasDB {
		settings.Providers = s.dbSettings.Providers
		settings.Models = s.dbSettings.Models
		settings.DefaultModels = s.dbSettings.DefaultModels
		settings.Tools = s.dbSettings.Tools
		settings.QQBot = s.dbSettings.QQBot
		settings.LSPMCP = s.dbSettings.LSPMCP
		settings.Embedding = s.dbSettings.Embedding
		settings.Session = s.dbSettings.Session
		settings.Simulation = s.dbSettings.Simulation
	}
	s.dbMu.RUnlock()
	return settings, nil
}

// ─── Convenience Queries ──────────────────────────────────────────────────────

// DefaultProvider returns the LLM Provider with isDefault=true, or nil if not found
func (s *GlobalService) DefaultProvider() *LLMProvider {
	settings := s.Get()
	for i := range settings.Providers {
		if settings.Providers[i].IsDefault {
			p := settings.Providers[i]
			return &p
		}
	}
	return nil
}

// DefaultEmbeddingModel returns the Embedding model with isDefault=true
func (s *GlobalService) DefaultEmbeddingModel() *EmbeddingModel {
	settings := s.Get()
	for i := range settings.Embedding.Models {
		m := settings.Embedding.Models[i]
		if m.IsDefault {
			return &m
		}
	}
	return nil
}

// EmbeddingProviderByID looks up an Embedding Provider by ID.
func (s *GlobalService) EmbeddingProviderByID(id string) *EmbeddingProvider {
	settings := s.Get()
	for i := range settings.Embedding.Providers {
		p := settings.Embedding.Providers[i]
		if p.ID == id {
			return &p
		}
	}
	return nil
}

// ProviderByID looks up an LLM Provider by ID
func (s *GlobalService) ProviderByID(id string) *LLMProvider {
	settings := s.Get()
	for i := range settings.Providers {
		if settings.Providers[i].ID == id {
			p := settings.Providers[i]
			return &p
		}
	}
	return nil
}

// ModelByID looks up an LLM Model by ID
func (s *GlobalService) ModelByID(id string) *LLMModel {
	settings := s.Get()
	for i := range settings.Models {
		if settings.Models[i].ID == id {
			m := settings.Models[i]
			return &m
		}
	}
	return nil
}

// ModelByProviderID looks up an LLM Model by dual keys: providerID + modelID
func (s *GlobalService) ModelByProviderID(providerID, modelID string) *LLMModel {
	settings := s.Get()
	for i := range settings.Models {
		m := settings.Models[i]
		if m.ProviderID == providerID && m.ID == modelID {
			return &m
		}
	}
	return nil
}

// DefaultModelByRole resolves the default model by role
//
// role supports: "expert", "superior", "universal", "fast".
//
// Resolution priority: role config value → Fallback → hardcoded default value.
// Config value format is "provider:id"; both provider and id must exist in the config file.
// Returns nil if the corresponding model is not found.
func (s *GlobalService) DefaultModelByRole(role string) *LLMModel {
	settings := s.Get()

	// 1. Get role config value
	ref := roleField(settings.DefaultModels, role)

	// 2. Role not configured → try Fallback
	if ref == "" {
		ref = settings.DefaultModels.Fallback
	}

	// 3. Fallback is empty → use hardcoded default value
	if ref == "" {
		ref = roleDefault(role)
	}

	if ref == "" {
		return nil
	}

	// 4. Parse "provider:id" and look up
	providerID, modelID, ok := parseProviderModelID(ref)
	if !ok {
		return nil
	}
	return s.ModelByProviderID(providerID, modelID)
}

// roleField returns the config value corresponding to the role
func roleField(dm DefaultModelsConfig, role string) string {
	switch role {
	case "expert":
		return dm.Expert
	case "superior":
		return dm.Superior
	case "universal":
		return dm.Universal
	case "fast":
		return dm.Fast
	default:
		return ""
	}
}

// roleDefault returns the hardcoded default value for the role
func roleDefault(role string) string {
	defaults := map[string]string{
		"expert":    "deepseek:deepseek-v4-pro-max",
		"superior":  "deepseek:deepseek-v4-flash-thinking-max",
		"universal": "deepseek:deepseek-v4-flash-thinking",
		"fast":      "deepseek:deepseek-v4-flash",
	}
	return defaults[role]
}

// ─── Init & DefaultWorkDir ────────────────────────────────────────────────────

// DefaultWorkDir returns the working directory for soloqueue.
// It checks the SOLOQUEUE_WORK_DIR env var first, then falls back to ~/.soloqueue.
// It also creates the plan/ subdirectory for design documents.
func DefaultWorkDir() (string, error) {
	// 1. Check env var first (for dev/test isolation)
	if envDir := os.Getenv("SOLOQUEUE_WORK_DIR"); envDir != "" {
		if err := os.MkdirAll(envDir, 0o755); err != nil {
			return "", fmt.Errorf("create work dir from env %s: %w", envDir, err)
		}
		planDir := filepath.Join(envDir, "plan")
		if err := os.MkdirAll(planDir, 0o755); err != nil {
			return "", fmt.Errorf("create plan dir from env %s: %w", envDir, err)
		}
		workspaceDir := filepath.Join(envDir, "workspace")
		if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
			return "", fmt.Errorf("create workspace dir from env %s: %w", envDir, err)
		}
		return envDir, nil
	}

	// 2. Fall back to ~/.soloqueue
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	dir := filepath.Join(home, ".soloqueue")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create work dir %s: %w", dir, err)
	}
	planDir := filepath.Join(dir, "plan")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		return "", fmt.Errorf("create plan dir %s: %w", planDir, err)
	}
	workspaceDir := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		return "", fmt.Errorf("create workspace dir %s: %w", workspaceDir, err)
	}
	return dir, nil
}

// PlanDir returns the absolute path to ~/.soloqueue/plan/.
// It creates the directory if it doesn't exist.
// This is used by L1 which has no team concept.
func PlanDir() (string, error) {
	workDir, err := DefaultWorkDir()
	if err != nil {
		return "", err
	}
	planDir := filepath.Join(workDir, "plan")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		return "", fmt.Errorf("create plan dir %s: %w", planDir, err)
	}
	return planDir, nil
}

// TeamPlanDir returns the absolute path to ~/.soloqueue/plan/<team>/.
// It creates the directory if it doesn't exist.
// Each team has its own plan directory for isolation.
func TeamPlanDir(team string) (string, error) {
	workDir, err := DefaultWorkDir()
	if err != nil {
		return "", err
	}
	if team == "" {
		team = "default"
	}
	planDir := filepath.Join(workDir, "plan", team)
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		return "", fmt.Errorf("create team plan dir %s: %w", planDir, err)
	}
	return planDir, nil
}

// Init creates a GlobalService, loads config from disk, and saves defaults if needed.
func Init(workDir string) (*GlobalService, error) {
	cfg, err := New(workDir)
	if err != nil {
		return nil, err
	}

	if err := cfg.Load(); err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	settingsPath := filepath.Join(workDir, "settings.toml")
	if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
		if err := cfg.Save(); err != nil {
			// Non-fatal: don't pollute terminal before logger is ready.
		}
	}

	return cfg, nil
}
