package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// GlobalService is the global configuration service, embedding Loader[Settings]
// Automatically inherits all Loader methods: Load / Save / Get / Set / OnChange / Watch / Close, etc.
// Provides business-related convenience query interfaces on top of that
type GlobalService struct {
	*Loader[Settings]
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

	return &GlobalService{Loader: loader}, nil
}

// LoadFromDisk reads settings from disk without modifying the loader cache or triggering OnChange.
func (s *GlobalService) LoadFromDisk() (Settings, error) {
	return s.Loader.ReadFromDisk()
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

// DefaultWorkDir returns ~/.soloqueue, creating it if it doesn't exist.
// It also creates the ~/.soloqueue/plan/ subdirectory for design documents.
func DefaultWorkDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}
	dir := filepath.Join(home, ".soloqueue")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create work dir %s: %w", dir, err)
	}
	// Create plan subdirectory for design documents
	planDir := filepath.Join(dir, "plan")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		return "", fmt.Errorf("create plan dir %s: %w", planDir, err)
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

// Init creates a GlobalService and completes loading, hot-reloading, and initial save
func Init(workDir string) (*GlobalService, error) {
	cfg, err := New(workDir)
	if err != nil {
		return nil, err
	}

	if err := cfg.Load(); err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	if err := cfg.Watch(); err != nil {
		// Non-fatal: config changes will require restart.
	}

	settingsPath := filepath.Join(workDir, "settings.toml")
	if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
		if err := cfg.Save(); err != nil {
			// Non-fatal: don't pollute terminal before logger is ready.
		}
	}

	return cfg, nil
}
