package agent

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

// Registry maps InstanceID (UUID) → Agent with a secondary index by template ID (Def.ID).
// Multiple agents with the same template can coexist for parallel scheduling.
// Lifecycle (Start/Stop) is explicit — Register/Unregister only manage the map.
type Registry struct {
	mu           sync.RWMutex
	agents       map[string]*Agent   // InstanceID → Agent
	byTemplate   map[string][]string // templateID (Def.ID) → []InstanceID
	log          *logger.Logger
	onChange     func()       // optional callback invoked after Register/Unregister
	onRegister   func(*Agent) // optional callback invoked after Register (under write lock)
	onUnregister func(string) // optional callback invoked after Unregister (under write lock)

	// Test-only barrier for deterministic scheduling-publication coverage.
	beforeSchedulingPublish func()
}

// NewRegistry constructs an empty registry. log can be nil.
func NewRegistry(log *logger.Logger) *Registry {
	return &Registry{
		agents:     make(map[string]*Agent),
		byTemplate: make(map[string][]string),
		log:        log,
	}
}

// SetOnChange sets a callback invoked (under write lock) after Register/Unregister.
// Must not call back into the Registry (deadlock).
func (r *Registry) SetOnChange(fn func()) {
	r.mu.Lock()
	r.onChange = fn
	r.mu.Unlock()
}

func (r *Registry) SetOnRegister(fn func(*Agent)) {
	r.mu.Lock()
	r.onRegister = fn
	r.mu.Unlock()
}

func (r *Registry) SetOnUnregister(fn func(string)) {
	r.mu.Lock()
	r.onUnregister = fn
	r.mu.Unlock()
}

// Register adds an agent by InstanceID. Does not call Start.
func (r *Registry) Register(a *Agent) error {
	if a == nil {
		return ErrAgentNil
	}
	id := a.InstanceID
	if id == "" {
		return ErrEmptyID
	}

	r.mu.Lock()
	if _, exists := r.agents[id]; exists {
		r.mu.Unlock()
		return nil
	}
	r.agents[id] = a
	tmplID := a.Def.ID
	if tmplID != "" {
		r.byTemplate[tmplID] = append(r.byTemplate[tmplID], id)
	}
	size := len(r.agents)
	if r.onRegister != nil {
		r.onRegister(a)
	}
	if r.onChange != nil {
		r.onChange()
	}
	r.mu.Unlock()

	r.logInfo(logger.CatActor, "registry register",
		"instance_id", id,
		"template_id", tmplID,
		"kind", string(a.Def.Kind),
		"role", string(a.Def.Role),
		"size", size,
	)
	return nil
}

// Unregister removes an agent by InstanceID. Does not call Stop.
func (r *Registry) Unregister(id string) bool {
	r.mu.Lock()
	a, exists := r.agents[id]
	if !exists {
		r.mu.Unlock()
		return false
	}
	delete(r.agents, id)

	// Clean up secondary index
	tmplID := a.Def.ID
	if tmplID != "" {
		instances := r.byTemplate[tmplID]
		for i, instID := range instances {
			if instID == id {
				r.byTemplate[tmplID] = append(instances[:i], instances[i+1:]...)
				if len(r.byTemplate[tmplID]) == 0 {
					delete(r.byTemplate, tmplID)
				}
				break
			}
		}
	}

	size := len(r.agents)
	if r.onUnregister != nil {
		r.onUnregister(id)
	}
	if r.onChange != nil {
		r.onChange()
	}
	r.mu.Unlock()

	r.logInfo(logger.CatActor, "registry unregister",
		"instance_id", id,
		"template_id", tmplID,
		"size", size,
	)
	return true
}

// PublishReplacement atomically transfers scheduling ownership from old to
// fresh under the same Registry lock used by every Locate path. publish runs
// while locators are excluded and must not call back into this Registry.
func (r *Registry) PublishReplacement(old, fresh *Agent, publish func()) error {
	if old == nil || fresh == nil {
		return ErrAgentNil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if registered, ok := r.agents[old.InstanceID]; !ok || registered != old {
		return fmt.Errorf("registry: old replacement generation %q is not registered", old.InstanceID)
	}
	if registered, ok := r.agents[fresh.InstanceID]; !ok || registered != fresh {
		return fmt.Errorf("registry: fresh replacement generation %q is not registered", fresh.InstanceID)
	}
	if fresh.Schedulable() {
		return fmt.Errorf("registry: fresh replacement generation %q is already schedulable", fresh.InstanceID)
	}
	if publish != nil {
		publish()
	}
	old.DeactivateScheduling()
	if r.beforeSchedulingPublish != nil {
		r.beforeSchedulingPublish()
	}
	fresh.ActivateScheduling()
	if r.onChange != nil {
		r.onChange()
	}
	return nil
}

func (r *Registry) Get(id string) (*Agent, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.agents[id]
	return a, ok
}

func (r *Registry) GetByTemplate(templateID string) []*Agent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := r.byTemplate[templateID]
	if len(ids) == 0 {
		return nil
	}
	out := make([]*Agent, 0, len(ids))
	for _, id := range ids {
		if a, ok := r.agents[id]; ok {
			out = append(out, a)
		}
	}
	return out
}

// LocateIdle finds an idle agent instance for the given template ID.
// Preferred for SpawnFn — reuses idle instances before creating new ones.
func (r *Registry) LocateIdle(templateID string) (iface.Locatable, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := r.byTemplate[templateID]
	for _, id := range ids {
		if a, ok := r.agents[id]; ok && a.Schedulable() && a.State() == StateIdle {
			return &LocatableAdapter{Agent: a}, true
		}
	}
	return nil, false
}

// LocateIdleInWorkDir finds an idle instance matching both template and workDir.
// Prevents cross-project reuse of leaders with the same template ID.
func (r *Registry) LocateIdleInWorkDir(templateID, workDir string) (iface.Locatable, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := r.byTemplate[templateID]
	for _, id := range ids {
		if a, ok := r.agents[id]; ok && a.Schedulable() && a.State() == StateIdle && a.WorkDir == workDir {
			return &LocatableAdapter{Agent: a}, true
		}
	}
	return nil, false
}

// List returns a snapshot of all agents, sorted by name.
func (r *Registry) List() []*Agent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Agent, 0, len(r.agents))
	for _, a := range r.agents {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		ni, nj := out[i].Def.Name, out[j].Def.Name
		if ni != nj {
			return ni < nj
		}
		return out[i].Def.ID < out[j].Def.ID
	})
	return out
}

// Len returns the number of agents currently in the registry
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.agents)
}

// ─── Batch lifecycle ────────────────────────────────────────────────────────

// StartAll calls Start on all registered agents. Returns all errors.
func (r *Registry) StartAll(parent context.Context) []error {
	agents := r.List()
	start := time.Now()
	r.logInfo(logger.CatActor, "registry start all",
		"count", len(agents),
	)

	var errs []error
	for _, a := range agents {
		if err := a.Start(parent); err != nil {
			errs = append(errs, fmt.Errorf("agent %q (template %q): %w", a.InstanceID, a.Def.ID, err))
		}
	}

	r.logInfo(logger.CatActor, "registry start all done",
		"count", len(agents),
		"errors", len(errs),
		"duration_ms", time.Since(start).Milliseconds(),
	)
	return errs
}

// StopAll calls Stop on all agents. timeout is per-agent, not total.
func (r *Registry) StopAll(timeout time.Duration) []error {
	agents := r.List()
	start := time.Now()
	r.logInfo(logger.CatActor, "registry stop all",
		"count", len(agents),
		"timeout_ms", timeout.Milliseconds(),
	)

	var errs []error
	for _, a := range agents {
		if err := a.Stop(timeout); err != nil {
			// An agent that was not started is not considered an error.
			if errors.Is(err, ErrNotStarted) {
				continue
			}
			errs = append(errs, fmt.Errorf("agent %q (template %q): %w", a.InstanceID, a.Def.ID, err))
		}
	}

	r.logInfo(logger.CatActor, "registry stop all done",
		"count", len(agents),
		"errors", len(errs),
		"duration_ms", time.Since(start).Milliseconds(),
	)
	return errs
}

// Shutdown stops all agents and clears the registry. Continues even if some Stop calls fail.
func (r *Registry) Shutdown(timeout time.Duration) error {
	start := time.Now()
	r.logInfo(logger.CatActor, "registry shutdown",
		"count", r.Len(),
		"timeout_ms", timeout.Milliseconds(),
	)

	stopErrs := r.StopAll(timeout)

	// Clear maps
	r.mu.Lock()
	r.agents = make(map[string]*Agent)
	r.byTemplate = make(map[string][]string)
	r.mu.Unlock()

	r.logInfo(logger.CatActor, "registry shutdown done",
		"errors", len(stopErrs),
		"duration_ms", time.Since(start).Milliseconds(),
	)

	if len(stopErrs) == 0 {
		return nil
	}
	return errors.Join(stopErrs...)
}

// ─── AgentLocator ───────────────────────────────────────────────────────────

// Locate implements iface.AgentLocator. Prefers idle instances; falls back
// to any active (non-stopping) instance for queueing.
func (r *Registry) Locate(id string) (iface.Locatable, bool) {
	// First try to find an idle instance
	if loc, ok := r.LocateIdle(id); ok {
		return loc, true
	}
	// Fall back to any active instance.
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := r.byTemplate[id]
	for _, instanceID := range ids {
		a, ok := r.agents[instanceID]
		if !ok || !a.Schedulable() {
			continue
		}
		state := a.State()
		if state == StateStopping || state == StateStopped {
			continue
		}
		return &LocatableAdapter{Agent: a}, true
	}
	return nil, false
}

// ─── Logging helpers ─────────────────────────────────────────────────────────

func (r *Registry) logInfo(cat logger.Category, msg string, args ...any) {
	if r.log == nil {
		return
	}
	r.log.Info(cat, msg, args...)
}

// --- LocatableAdapter ---

// LocatableAdapter wraps *Agent to satisfy iface.Locatable.
// AskStream uses a relay goroutine because Go channels are not covariant.
type LocatableAdapter struct {
	*Agent
}

func (la *LocatableAdapter) InstanceID() string {
	if la.Agent != nil {
		return la.Agent.InstanceID
	}
	return ""
}

func (la *LocatableAdapter) Name() string {
	if la.Agent != nil {
		return la.Agent.Def.Name
	}
	return ""
}

func (la *LocatableAdapter) AskStream(ctx context.Context, prompt string) (<-chan iface.AgentEvent, error) {
	eventCh, err := la.Agent.AskStream(ctx, prompt)
	if err != nil {
		return nil, err
	}

	out := make(chan iface.AgentEvent, 64)
	go func() {
		defer close(out)
		defer func() {
			if r := recover(); r != nil {
				if la.Agent.Log != nil {
					la.Agent.Log.ErrorContext(ctx, logger.CatTool, "registry relay goroutine panic recovered",
						"agent_id", la.Agent.Def.ID,
						"panic", fmt.Sprintf("%v", r),
					)
				}
			}
		}()
		for ev := range eventCh {
			select {
			case out <- ev:
				if la.Agent.Log != nil {
					la.Agent.Log.InfoContext(ctx, logger.CatTool, "locatable-adapter: relayed event",
						"agent_id", la.Agent.Def.ID,
						"event_type", fmt.Sprintf("%T", ev),
					)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	return out, nil
}

// Compile-time interface assertions.
var _ iface.Locatable = (*LocatableAdapter)(nil)
var _ iface.ModelOverridable = (*LocatableAdapter)(nil)
var _ iface.AgentLocator = (*Registry)(nil)

// SetModelOverride implements iface.ModelOverridable.
// Propagates model override from parent to child during delegation.
func (la *LocatableAdapter) SetModelOverride(params *iface.ModelOverrideParams) {
	if params == nil {
		la.Agent.ClearModelOverride()
		return
	}
	la.Agent.SetModelOverride(&ModelParams{
		ProviderID:      params.ProviderID,
		ModelID:         params.ModelID,
		ThinkingEnabled: params.ThinkingEnabled,
		ReasoningEffort: params.ReasoningEffort,
		ThinkingType:    params.ThinkingType,
		TaskType:        params.TaskType,
		ContextWindow:   params.ContextWindow,
		Vision:          params.Vision,
	})
}
