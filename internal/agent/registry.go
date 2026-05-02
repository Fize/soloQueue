package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// Registry is a concurrent-safe mapping of agent ID → Agent
//
// Design principle: Registry only manages the map; does **not** implicitly trigger Start/Stop.
// Lifecycle control uses explicit APIs: StartAll / StopAll / Shutdown.
type Registry struct {
	mu     sync.RWMutex
	agents map[string]*Agent
	log    *logger.Logger
}

// NewRegistry constructs an empty registry
//
// log can be nil (log calls are skipped). After passing a logger, Register / Unregister /
// StartAll / StopAll / Shutdown produce structured logs for tracking batch lifecycle events.
func NewRegistry(log *logger.Logger) *Registry {
	return &Registry{
		agents: make(map[string]*Agent),
		log:    log,
	}
}

// Register adds an agent; returns ErrAgentAlreadyExists if ID already exists
// Returns ErrAgentNil if agent is nil; ErrEmptyID if Def.ID is empty
//
// Does not start the agent — caller must explicitly call Start or use StartAll.
func (r *Registry) Register(a *Agent) error {
	if a == nil {
		return ErrAgentNil
	}
	id := a.Def.ID
	if id == "" {
		return ErrEmptyID
	}
	r.mu.Lock()
	if _, exists := r.agents[id]; exists {
		r.mu.Unlock()
		return ErrAgentAlreadyExists
	}
	r.agents[id] = a
	size := len(r.agents)
	r.mu.Unlock()

	r.logInfo(logger.CatActor, "registry register",
		slog.String("actor_id", id),
		slog.String("kind", string(a.Def.Kind)),
		slog.String("role", string(a.Def.Role)),
		slog.Int("size", size),
	)
	return nil
}

// Unregister removes an ID from registry; returns true if it existed and was removed
//
// Does not stop the agent — caller must explicitly call Stop or use Shutdown.
func (r *Registry) Unregister(id string) bool {
	r.mu.Lock()
	if _, exists := r.agents[id]; !exists {
		r.mu.Unlock()
		return false
	}
	delete(r.agents, id)
	size := len(r.agents)
	r.mu.Unlock()

	r.logInfo(logger.CatActor, "registry unregister",
		slog.String("actor_id", id),
		slog.Int("size", size),
	)
	return true
}

// Get looks up an agent; returns (nil, false) if not found
func (r *Registry) Get(id string) (*Agent, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.agents[id]
	return a, ok
}

// List returns a snapshot slice of all current agents
//
// The returned slice is independent of the internal map; modifying it doesn't affect registry;
// slice elements are still *Agent pointers.
func (r *Registry) List() []*Agent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Agent, 0, len(r.agents))
	for _, a := range r.agents {
		out = append(out, a)
	}
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
			errs = append(errs, fmt.Errorf("agent %q: %w", a.Def.ID, err))
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
			errs = append(errs, fmt.Errorf("agent %q: %w", a.Def.ID, err))
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

	// 清空 map
	r.mu.Lock()
	r.agents = make(map[string]*Agent)
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

// ─── Logging helpers ─────────────────────────────────────────────────────────

func (r *Registry) logInfo(cat logger.Category, msg string, args ...any) {
	if r.log == nil {
		return
	}
	r.log.Info(cat, msg, args...)
}

// Locate implements iface.AgentLocator.
//
// Returns an iface.Locatable wrapper around the Agent, used by
// DelegateTool to find target agents without importing this package.
func (r *Registry) Locate(id string) (iface.Locatable, bool) {
	agent, ok := r.Get(id)
	if !ok {
		return nil, false
	}
	return &LocatableAdapter{Agent: agent}, true
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
		for ev := range eventCh {
			select {
			case out <- ev:
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
	})
}
