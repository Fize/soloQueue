package session

import (
	"fmt"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/config"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/runtime"
)

// ModelSelection is the model a user chose for one session. An empty ModelID
// means "use the configured default model". ReasoningEffort is optional and
// only applies when the chosen model thinks.
type ModelSelection struct {
	ProviderID      string `json:"provider_id,omitempty"`
	ModelID         string `json:"model_id,omitempty"`
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
}

// IsAuto reports that no explicit model was chosen.
func (m ModelSelection) IsAuto() bool { return m.ModelID == "" }

// ModelSpec is the resolved shape of a selectable model. It keeps the session
// layer free of the settings schema.
type ModelSpec struct {
	ProviderID       string
	APIModel         string
	Name             string
	ContextWindow    int
	Vision           bool
	ThinkingEnabled  bool
	ReasoningEfforts []string
	ThinkingType     string
}

// ModelLookup resolves an enabled model. It reports false when the provider or
// model is missing or disabled.
type ModelLookup func(providerID, modelID string) (ModelSpec, bool)

// ModelLookupForConfig resolves models from the live configuration.
func ModelLookupForConfig(cfg *config.GlobalService) ModelLookup {
	if cfg == nil {
		return nil
	}
	return func(providerID, modelID string) (ModelSpec, bool) {
		model := cfg.EnabledModel(providerID, modelID)
		if model == nil {
			return ModelSpec{}, false
		}
		apiModel := model.APIModel
		if apiModel == "" {
			apiModel = model.ID
		}
		return ModelSpec{
			ProviderID:       model.ProviderID,
			APIModel:         apiModel,
			Name:             model.Name,
			ContextWindow:    model.ContextWindow,
			Vision:           model.Vision,
			ThinkingEnabled:  model.Thinking.Enabled,
			ReasoningEfforts: append([]string(nil), model.Thinking.ReasoningEfforts...),
			ThinkingType:     model.Thinking.ThinkingType,
		}, true
	}
}

// ModelLookupForStack resolves models from the runtime stack configuration.
func ModelLookupForStack(rt *runtime.Stack) ModelLookup {
	if rt == nil {
		return nil
	}
	return ModelLookupForConfig(rt.Settings)
}

// NormalizeModelSelection validates and canonicalizes a selection. A nil lookup
// skips the enabled-model check and the capability check. An empty ModelID
// clears the selection. A non-empty ReasoningEffort must be declared by the
// selected model; the error lists the efforts it does accept.
func NormalizeModelSelection(sel ModelSelection, lookup ModelLookup) (ModelSelection, error) {
	sel.ProviderID = strings.TrimSpace(sel.ProviderID)
	sel.ModelID = strings.TrimSpace(sel.ModelID)
	sel.ReasoningEffort = strings.ToLower(strings.TrimSpace(sel.ReasoningEffort))
	if sel.ModelID == "" {
		return ModelSelection{}, nil
	}
	var spec ModelSpec
	if lookup != nil {
		var ok bool
		spec, ok = lookup(sel.ProviderID, sel.ModelID)
		if !ok {
			return ModelSelection{}, fmt.Errorf("model %s:%s is not enabled", sel.ProviderID, sel.ModelID)
		}
	}
	if sel.ReasoningEffort == "" {
		return sel, nil
	}
	if lookup != nil && !supportsReasoningEffort(spec.ReasoningEfforts, sel.ReasoningEffort) {
		return ModelSelection{}, fmt.Errorf("unsupported reasoning effort %q for model %s:%s (available: %s)",
			sel.ReasoningEffort, sel.ProviderID, sel.ModelID, describeReasoningEfforts(spec.ReasoningEfforts))
	}
	return sel, nil
}

// supportsReasoningEffort reports whether the model declares effort.
func supportsReasoningEffort(available []string, effort string) bool {
	for _, candidate := range available {
		if candidate == effort {
			return true
		}
	}
	return false
}

// describeReasoningEfforts renders the declared efforts for error messages.
func describeReasoningEfforts(available []string) string {
	if len(available) == 0 {
		return "none"
	}
	return strings.Join(available, ", ")
}

// ModelSelection returns the session's chosen model override.
func (s *Session) ModelSelection() ModelSelection {
	s.modelSelMu.RLock()
	defer s.modelSelMu.RUnlock()
	return s.modelSelection
}

// SetModelSelection stores and persists the override. An empty ModelID resets
// the session to the configured default model.
func (s *Session) SetModelSelection(sel ModelSelection) error {
	s.modelSelMu.RLock()
	lookup := s.modelLookup
	s.modelSelMu.RUnlock()

	normalized, err := NormalizeModelSelection(sel, lookup)
	if err != nil {
		return err
	}

	s.modelSelMu.Lock()
	s.modelSelection = normalized
	s.modelSelMu.Unlock()

	return s.persistModelSelection(normalized)
}

// hydrateModelSelection restores the persisted selection. A persisted effort the
// model no longer declares degrades to "not specified" instead of making the
// session unusable; the provider then picks its own default.
func (s *Session) hydrateModelSelection(meta *SessionMeta) {
	if meta == nil {
		return
	}
	sel := meta.Model
	s.modelSelMu.RLock()
	lookup := s.modelLookup
	s.modelSelMu.RUnlock()
	if sel.ReasoningEffort != "" && lookup != nil {
		if spec, ok := lookup(sel.ProviderID, sel.ModelID); ok && !supportsReasoningEffort(spec.ReasoningEfforts, sel.ReasoningEffort) {
			if s.logger != nil {
				s.logger.Warn(logger.CatApp, "session reasoning effort no longer supported, dropping it",
					"target_id", s.TargetID,
					"provider_id", sel.ProviderID,
					"model_id", sel.ModelID,
					"reasoning_effort", sel.ReasoningEffort,
					"available", describeReasoningEfforts(spec.ReasoningEfforts),
				)
			}
			sel.ReasoningEffort = ""
		}
	}
	s.modelSelMu.Lock()
	s.modelSelection = sel
	s.modelSelMu.Unlock()
}

// persistModelSelection writes the selection to meta.json. Sessions without a
// metadata file keep the selection in memory only.
func (s *Session) persistModelSelection(sel ModelSelection) error {
	path := s.usageMetaPath
	if path == "" {
		return nil
	}
	return MergeAndSavePath(path, func(m *SessionMeta) {
		m.Model = sel
	})
}

// modelOverrideParams builds the model override for the next ask. ok=false
// means the configured default model applies; a selection that no longer
// resolves degrades to the default instead of failing the turn.
func (s *Session) modelOverrideParams() (iface.ModelOverrideParams, bool) {
	sel := s.ModelSelection()
	if sel.IsAuto() {
		return iface.ModelOverrideParams{}, false
	}

	s.modelSelMu.RLock()
	lookup := s.modelLookup
	s.modelSelMu.RUnlock()
	if lookup == nil {
		return iface.ModelOverrideParams{}, false
	}

	spec, ok := lookup(sel.ProviderID, sel.ModelID)
	if !ok {
		if s.logger != nil {
			s.logger.Warn(logger.CatApp, "session model selection unavailable, using default model",
				"target_id", s.TargetID,
				"provider_id", sel.ProviderID,
				"model_id", sel.ModelID,
			)
		}
		return iface.ModelOverrideParams{}, false
	}

	params := iface.ModelOverrideParams{
		ProviderID:    spec.ProviderID,
		ModelID:       spec.APIModel,
		ContextWindow: spec.ContextWindow,
		Vision:        spec.Vision,
	}
	if spec.ThinkingEnabled {
		params.ThinkingEnabled = true
		params.ThinkingType = spec.ThinkingType
		// The effort comes from the session selection only. Without a selection
		// nothing is sent and the provider decides.
		if supportsReasoningEffort(spec.ReasoningEfforts, sel.ReasoningEffort) {
			params.ReasoningEffort = sel.ReasoningEffort
		}
	}
	return params, true
}

// ModelCommand applies a /model argument and returns the reply text. It is
// shared by the Web Console and every channel bridge.
func (s *Session) ModelCommand(arg string) string {
	arg = strings.TrimSpace(arg)
	if arg == "" || strings.EqualFold(arg, "show") {
		return s.describeModelSelection()
	}
	if strings.EqualFold(arg, "auto") || strings.EqualFold(arg, "reset") {
		if err := s.SetModelSelection(ModelSelection{}); err != nil {
			return "Model was not reset: " + err.Error()
		}
		return "Model: auto (configured default)"
	}

	fields := strings.Fields(arg)
	providerID, modelID, ok := strings.Cut(fields[0], ":")
	if !ok || providerID == "" || modelID == "" {
		return fmt.Sprintf("Usage: /model <provider:model> [%s] (model-declared), /model auto. Unrecognized reference %q.",
			strings.Join(config.ReasoningEffortVocabulary, "|"), fields[0])
	}
	sel := ModelSelection{ProviderID: providerID, ModelID: modelID}
	if len(fields) > 1 {
		sel.ReasoningEffort = fields[1]
	}
	if err := s.SetModelSelection(sel); err != nil {
		return "Model not changed: " + err.Error()
	}
	if !sel.IsAuto() {
		return "Model: " + sel.ProviderID + ":" + sel.ModelID + effortSuffix(sel.ReasoningEffort)
	}
	return s.describeModelSelection()
}

func (s *Session) describeModelSelection() string {
	sel := s.ModelSelection()
	if sel.IsAuto() {
		return "Model: auto (configured default)"
	}
	return "Model: " + sel.ProviderID + ":" + sel.ModelID + effortSuffix(sel.ReasoningEffort)
}

func effortSuffix(effort string) string {
	if effort == "" {
		return ""
	}
	return " (" + effort + ")"
}
