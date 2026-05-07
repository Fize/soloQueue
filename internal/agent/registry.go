package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// Registry is a concurrent-safe mapping of instance ID → Agent, with a secondary
// index by template ID for multi-instance lookup.
//
// Keys:
//   - Primary:   InstanceID (UUID, unique per agent instance)
//   - Secondary: Def.ID   (template/role identifier, shared by all instances of
//     the same template)
//
// This separation allows multiple agents with the same template ID to coexist,
// enabling parallel scheduling (e.g., two "dev" L2 agents working concurrently
// on different tasks).
//
// Design principle: Registry only manages the map; does **not** implicitly
// trigger Start/Stop. Lifecycle control uses explicit APIs: StartAll / StopAll /
// Shutdown.
type Registry struct {
	mu         sync.RWMutex
	agents     map[string]*Agent   // InstanceID → Agent
	byTemplate map[string][]string // templateID (Def.ID) → []InstanceID
	log        *logger.Logger
}

// NewRegistry constructs an empty registry
//
// log can be nil (log calls are skipped). After passing a logger, Register /
// Unregister / StartAll / StopAll / Shutdown produce structured logs for
// tracking batch lifecycle events.
func NewRegistry(log *logger.Logger) *Registry {
	return &Registry{
		agents:     make(map[string]*Agent),
		byTemplate: make(map[string][]string),
		log:        log,
	}
}

// Register adds an agent keyed by InstanceID (never Def.ID).
// Multiple agents with the same Def.ID (template) can coexist.
//
// Returns ErrAgentNil if agent is nil; ErrEmptyID if InstanceID is empty.
//
// Does not start the agent — caller must explicitly call Start or use StartAll.
func (r *Registry) Register(a *Agent) error {
	if a == nil {
		return ErrAgentNil
	}
	id := a.InstanceID
	if id == "" {
		return ErrEmptyID
	}

	r.mu.Lock()
	r.agents[id] = a
	tmplID := a.Def.ID
	if tmplID != "" {
		r.byTemplate[tmplID] = append(r.byTemplate[tmplID], id)
	}
	size := len(r.agents)
	r.mu.Unlock()

	r.logInfo(logger.CatActor, "registry register",
		slog.String("instance_id", id),
		slog.String("template_id", tmplID),
		slog.String("kind", string(a.Def.Kind)),
		slog.String("role", string(a.Def.Role)),
		slog.Int("size", size),
	)
	return nil
}

// Unregister removes an agent by InstanceID; returns true if it existed and
// was removed.
//
// Does not stop the agent — caller must explicitly call Stop or use Shutdown.
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
	r.mu.Unlock()

	r.logInfo(logger.CatActor, "registry unregister",
		slog.String("instance_id", id),
		slog.String("template_id", tmplID),
		slog.Int("size", size),
	)
	return true
}

// Get looks up an agent by InstanceID; returns (nil, false) if not found.
func (r *Registry) Get(id string) (*Agent, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.agents[id]
	return a, ok
}

// GetByTemplate returns all agent instances for a given template ID.
// Returns nil if no instances exist.
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
// Returns (nil, false) if no idle instance is available.
// This is the preferred method for SpawnFn — it reuses idle instances before
// creating new ones.
func (r *Registry) LocateIdle(templateID string) (iface.Locatable, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := r.byTemplate[templateID]
	for _, id := range ids {
		if a, ok := r.agents[id]; ok && a.State() == StateIdle {
			return &LocatableAdapter{Agent: a}, true
		}
	}
	return nil, false
}

// List returns a snapshot slice of all current agents, sorted by name for
// stable display.
//
// The returned slice is independent of the internal map; modifying it doesn't
// affect registry; slice elements are still *Agent pointers.
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

// StartAll calls Start on all registered agents
//
// 返回所有 Start 报错（每个 agent 最多一条）；nil slice 表示全部成功。
// 已在运行的 agent 返回 ErrAlreadyStarted 会被跳过收集？不 —— 如实收集，
// 让 caller 自己决定是否视为错误。
func (r *Registry) StartAll(parent context.Context) []error {
	agents := r.List()
	start := time.Now()
	r.logInfo(logger.CatActor, "registry start all",
		slog.Int("count", len(agents)),
	)

	var errs []error
	for _, a := range agents {
		if err := a.Start(parent); err != nil {
			errs = append(errs, fmt.Errorf("agent %q (template %q): %w", a.InstanceID, a.Def.ID, err))
		}
	}

	r.logInfo(logger.CatActor, "registry start all done",
		slog.Int("count", len(agents)),
		slog.Int("errors", len(errs)),
		slog.Int64("duration_ms", time.Since(start).Milliseconds()),
	)
	return errs
}

// StopAll 对所有已注册 agent 调用 Stop
//
// timeout 是每个 agent 的单独超时（不是总超时）。
// 返回所有 Stop 报错；nil 表示全部成功。
func (r *Registry) StopAll(timeout time.Duration) []error {
	agents := r.List()
	start := time.Now()
	r.logInfo(logger.CatActor, "registry stop all",
		slog.Int("count", len(agents)),
		slog.Int64("timeout_ms", timeout.Milliseconds()),
	)

	var errs []error
	for _, a := range agents {
		if err := a.Stop(timeout); err != nil {
			// 未 Start 的 agent 不算错误
			if errors.Is(err, ErrNotStarted) {
				continue
			}
			errs = append(errs, fmt.Errorf("agent %q (template %q): %w", a.InstanceID, a.Def.ID, err))
		}
	}

	r.logInfo(logger.CatActor, "registry stop all done",
		slog.Int("count", len(agents)),
		slog.Int("errors", len(errs)),
		slog.Int64("duration_ms", time.Since(start).Milliseconds()),
	)
	return errs
}

// Shutdown 依次 Stop 所有 agent 然后清空 registry
//
// timeout 是每个 agent 的单独超时。
// 所有 agent 都 Stop 完成后 Unregister；即使有 Stop 超时也会继续。
// 返回 joined error（所有 Stop 错误的合并），nil 表示全部成功。
func (r *Registry) Shutdown(timeout time.Duration) error {
	start := time.Now()
	r.logInfo(logger.CatActor, "registry shutdown",
		slog.Int("count", r.Len()),
		slog.Int64("timeout_ms", timeout.Milliseconds()),
	)

	stopErrs := r.StopAll(timeout)

	// 清空 maps
	r.mu.Lock()
	r.agents = make(map[string]*Agent)
	r.byTemplate = make(map[string][]string)
	r.mu.Unlock()

	r.logInfo(logger.CatActor, "registry shutdown done",
		slog.Int("errors", len(stopErrs)),
		slog.Int64("duration_ms", time.Since(start).Milliseconds()),
	)

	if len(stopErrs) == 0 {
		return nil
	}
	return errors.Join(stopErrs...)
}

// ─── AgentLocator ───────────────────────────────────────────────────────────

// Locate implements iface.AgentLocator.
//
// Finds an idle agent instance by template ID. If no idle instance exists,
// returns the first instance regardless of state (the caller can still use it
// — the agent's mailbox will queue the job).
//
// For SpawnFn callers that want to create a new instance when none are idle,
// use LocateIdle instead.
func (r *Registry) Locate(id string) (iface.Locatable, bool) {
	// First try to find an idle instance
	if loc, ok := r.LocateIdle(id); ok {
		return loc, true
	}
	// Fall back to any instance
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := r.byTemplate[id]
	if len(ids) == 0 {
		return nil, false
	}
	a, ok := r.agents[ids[0]]
	if !ok {
		return nil, false
	}
	return &LocatableAdapter{Agent: a}, true
}

// ─── Logging helpers ─────────────────────────────────────────────────────────

func (r *Registry) logInfo(cat logger.Category, msg string, args ...any) {
	if r.log == nil {
		return
	}
	r.log.Info(cat, msg, args...)
}

// --- LocatableAdapter ---

// LocatableAdapter wraps *Agent to satisfy the iface.Locatable interface.
//
// The primary adaptation is AskStream: Agent returns <-chan AgentEvent,
// but iface.Locatable requires <-chan iface.AgentEvent. Since Go channels
// are not covariant, a thin relay goroutine converts the channel type.
type LocatableAdapter struct {
	*Agent
}

// AskStream implements iface.Locatable.AskStream.
// Relays typed AgentEvent values through an iface.AgentEvent channel.
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
// Propagates task-level model override from parent to child during delegation.
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
		Level:           params.Level,
	})
}
