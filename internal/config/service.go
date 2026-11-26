package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

// GlobalService manages application configuration persisted in settings.yaml.
// It embeds Loader[Settings] for thread-safe hot-reload and atomic updates.
type GlobalService struct {
	*Loader[Settings]
	workDir string
	mu      sync.RWMutex
	log     *logger.Logger
}

// New initializes GlobalService targeting settings.yaml within workDir.
func New(workDir string) (*GlobalService, error) {
	path := filepath.Join(workDir, "settings.yaml")

	loader, err := NewLoader(DefaultSettings(), path)
	if err != nil {
		return nil, fmt.Errorf("config.New: %w", err)
	}

	return &GlobalService{Loader: loader, workDir: workDir}, nil
}

// Get returns an atomic read-only snapshot of current settings.
func (s *GlobalService) Get() Settings {
	return s.Loader.Get()
}

// SetLogger attaches a logger for config diagnostics and hot-reload error reporting.
func (s *GlobalService) SetLogger(log *logger.Logger) {
	s.mu.Lock()
	s.log = log
	s.mu.Unlock()
	if log == nil {
		s.Loader.SetOnError(nil)
		return
	}
	s.Loader.SetOnError(func(err error) {
		log.Error(logger.CatConfig, "config hot-reload failed", "err", err.Error())
	})
}

// Set applies a mutation, normalizes the resulting snapshot, and persists it.
// Normalization runs on the save path so no caller can write a thinking
// configuration the loader would reject or rewrite on the next load.
func (s *GlobalService) Set(mutate func(*Settings)) (Settings, error) {
	return s.Loader.Set(func(st *Settings) {
		mutate(st)
		normalizeThinkingConfigs(st)
	})
}

// LoadFromDisk reads settings from disk without modifying the loader cache.
func (s *GlobalService) LoadFromDisk() (Settings, error) {
	return s.Loader.ReadFromDisk()
}

// DefaultProvider returns the preferred enabled provider, falling back to the
// first enabled provider when the default marker is absent or disabled.
func (s *GlobalService) DefaultProvider() *LLMProvider {
	settings := s.Get()
	for i := range settings.Providers {
		if settings.Providers[i].Enabled && settings.Providers[i].IsDefault {
			p := settings.Providers[i]
			return &p
		}
	}
	for i := range settings.Providers {
		if settings.Providers[i].Enabled {
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

// DefaultModel resolves the single model used for every conversation turn.
// The configured route is a preference: when it is empty or no longer
// resolves, the first enabled model of the first enabled provider is used.
func (s *GlobalService) DefaultModel() *LLMModel {
	return ResolveDefaultModel(s.Get())
}

// ResolveDefaultModel resolves the effective default model from a settings
// snapshot. It is shared by the config service and runtime hot reload so both
// paths apply the same fallback rules.
func ResolveDefaultModel(settings Settings) *LLMModel {
	if model := enabledModelByRef(settings, settings.ModelRoutes.Ref()); model != nil {
		return model
	}
	for i := range settings.Providers {
		provider := settings.Providers[i]
		if !provider.Enabled || provider.ID == "" {
			continue
		}
		for j := range settings.Models {
			model := settings.Models[j]
			if model.ProviderID == provider.ID && model.ID != "" && model.Enabled {
				return &model
			}
		}
	}
	return nil
}

// EnabledModel returns the model for a provider/model pair when both the
// provider and the model are enabled; nil otherwise.
func (s *GlobalService) EnabledModel(providerID, modelID string) *LLMModel {
	if providerID == "" || modelID == "" {
		return nil
	}
	return enabledModelByRef(s.Get(), providerID+":"+modelID)
}

// VisionModel returns the first enabled model that can read images. It is only
// used to transcribe attachments when the active model cannot see them.
func (s *GlobalService) VisionModel() *LLMModel {
	settings := s.Get()
	for i := range settings.Models {
		model := settings.Models[i]
		if !model.Enabled || !model.Vision {
			continue
		}
		if enabledModelByRef(settings, model.ProviderID+":"+model.ID) != nil {
			return &model
		}
	}
	return nil
}

func enabledModelByRef(settings Settings, ref string) *LLMModel {
	providerID, modelID, ok := parseProviderModelID(ref)
	if !ok {
		return nil
	}
	providerEnabled := false
	for i := range settings.Providers {
		if settings.Providers[i].ID == providerID && settings.Providers[i].Enabled {
			providerEnabled = true
			break
		}
	}
	if !providerEnabled {
		return nil
	}
	for i := range settings.Models {
		model := settings.Models[i]
		if model.ProviderID == providerID && model.ID == modelID && model.Enabled {
			return &model
		}
	}
	return nil
}

// ReasoningEffortVocabulary is the closed set of reasoning efforts a model may
// declare. The order is the render order used by the UI.
var ReasoningEffortVocabulary = []string{"low", "medium", "high", "xhigh", "max"}

// IsKnownReasoningEffort reports whether effort belongs to the vocabulary.
func IsKnownReasoningEffort(effort string) bool {
	for _, known := range ReasoningEffortVocabulary {
		if effort == known {
			return true
		}
	}
	return false
}

// normalizeThinking canonicalizes one thinking configuration in place:
// lowercase/trim, drop unknown values, dedupe while keeping order, migrate the
// legacy single-value field to the full vocabulary, and clear the legacy field.
// A disabled model keeps no declared capability.
func normalizeThinking(t *ThinkingConfig) {
	if t == nil {
		return
	}
	if !t.Enabled {
		t.ReasoningEfforts = nil
		t.ReasoningEffort = ""
		return
	}
	if len(t.ReasoningEfforts) == 0 && t.ReasoningEffort != "" {
		// Legacy config declared one effort; the closed vocabulary is the
		// widest set those models can be asked for.
		t.ReasoningEfforts = append([]string(nil), ReasoningEffortVocabulary...)
	}
	t.ReasoningEffort = ""
	canonical := make([]string, 0, len(t.ReasoningEfforts))
	seen := make(map[string]bool, len(t.ReasoningEfforts))
	for _, effort := range t.ReasoningEfforts {
		effort = strings.ToLower(strings.TrimSpace(effort))
		if !IsKnownReasoningEffort(effort) || seen[effort] {
			continue
		}
		seen[effort] = true
		canonical = append(canonical, effort)
	}
	if len(canonical) == 0 {
		t.ReasoningEfforts = nil
		return
	}
	t.ReasoningEfforts = canonical
}

// normalizeThinkingConfigs normalizes the thinking block of every model.
func normalizeThinkingConfigs(st *Settings) {
	for i := range st.Models {
		normalizeThinking(&st.Models[i].Thinking)
	}
}

// ValidateThinking rejects thinking configurations that cannot be stored:
// values outside the vocabulary and capabilities declared by a model that does
// not think. Case and surrounding spaces are accepted (they are normalized).
func ValidateThinking(t ThinkingConfig) error {
	if !t.Enabled {
		if len(t.ReasoningEfforts) > 0 {
			return fmt.Errorf("thinking.reasoning_efforts requires thinking.enabled")
		}
		return nil
	}
	for _, effort := range t.ReasoningEfforts {
		if !IsKnownReasoningEffort(strings.ToLower(strings.TrimSpace(effort))) {
			return fmt.Errorf("unsupported reasoning effort %q (use %s)",
				effort, strings.Join(ReasoningEffortVocabulary, ", "))
		}
	}
	return nil
}

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

	settingsPath := filepath.Join(workDir, "settings.yaml")
	if _, statErr := os.Stat(settingsPath); os.IsNotExist(statErr) {
		if err := cfg.Save(); err != nil {
			return nil, fmt.Errorf("save default config: %w", err)
		}
	} else if statErr != nil {
		return nil, fmt.Errorf("stat config: %w", statErr)
	}

	return cfg, nil
}
