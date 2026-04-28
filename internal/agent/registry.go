package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// Registry 是 agent ID → Agent 的并发安全映射
//
// 设计原则：Registry 只管 map；**不隐式**触发 Start/Stop。
// 生命周期触发用显式 API：StartAll / StopAll / Shutdown。
type Registry struct {
	mu     sync.RWMutex
	agents map[string]*Agent
	log    *logger.Logger
}

// NewRegistry 构造空 registry
//
// log 可为 nil（日志调用被跳过）。传入 logger 后 Register / Unregister /
// StartAll / StopAll / Shutdown 会产生结构化日志，便于追踪批量生命周期事件。
func NewRegistry(log *logger.Logger) *Registry {
	return &Registry{
		agents: make(map[string]*Agent),
		log:    log,
	}
}

// Register 添加 agent；ID 已存在返回 ErrAgentAlreadyExists
// agent 为 nil 返回 ErrAgentNil；Def.ID 为空返回 ErrEmptyID
//
// 不启动 agent —— 调用方需要显式 Start 或使用 StartAll。
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

// Unregister 从 registry 移除 ID；返回 true 表示确实存在并被移除
//
// 不停止 agent —— 调用方需要显式 Stop 或使用 Shutdown。
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

// Get 查找 agent；不存在返回 (nil, false)
func (r *Registry) Get(id string) (*Agent, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.agents[id]
	return a, ok
}

// List 返回当前所有 agent 的快照切片
//
// 返回的切片与内部 map 独立，修改切片不影响 registry；
// 切片元素仍是 *Agent 指针。
func (r *Registry) List() []*Agent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Agent, 0, len(r.agents))
	for _, a := range r.agents {
		out = append(out, a)
	}
	return out
}

// Len 当前 registry 中 agent 的数量
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.agents)
}

// ─── Batch lifecycle ────────────────────────────────────────────────────────

// StartAll 对所有已注册 agent 调用 Start
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

// Locate 实现 tools.AgentLocator 接口
//
// 返回 tools.Locatable（Agent 的最小抽象），供 DelegateTool 查找目标 Agent。
func (r *Registry) Locate(id string) (tools.Locatable, bool) {
	agent, ok := r.Get(id)
	if !ok {
		return nil, false
	}
	return &LocatableAdapter{agent}, true
}


// ─── locatableAdapter ──────────────────────────────────────────────────────

// locatableAdapter 包装 *Agent 以适配 tools.Locatable 接口
//
// 主要区别是 AskStream 返回值从 <-chan AgentEvent 转换为 <-chan interface{}。
// 这个适配层避免 tools 包对 agent 包的循环导入。
type LocatableAdapter struct {
	*Agent
}

// AskStream 实现 tools.Locatable.AskStream
// 通过调用 Agent.AskStreamInterface 来返回 interface{} 通道
func (la *LocatableAdapter) AskStream(ctx context.Context, prompt string) (<-chan interface{}, error) {
	return la.Agent.AskStreamInterface(ctx, prompt)
}

// 编译时验证 locatableAdapter 实现 tools.Locatable
var _ tools.Locatable = (*LocatableAdapter)(nil)

// 编译时断言：Registry 实现 tools.AgentLocator
var _ tools.AgentLocator = (*Registry)(nil)
