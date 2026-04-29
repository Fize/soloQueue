package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// ─── Supervisor ────────────────────────────────────────────────────────────

// Supervisor 是 L2 领域主管
//
// 组合 Agent（而非嵌入），持有 L2 Agent 实例和 AgentFactory，
// 负责 L3 子 Agent 的生命周期管理（Spawn/Reap）。
//
// L2 的 Fan-out/Fan-in 复用现有 execTools 并行机制：
//   - L2 LLM 返回多个 delegate_* tool_calls
//   - execTools 并行执行每个 DelegateTool
//   - 每个 DelegateTool 同步阻塞调用 L3.Ask()
//   - 全部完成后结果注入 L2 上下文
//
// Supervisor 的核心价值是生命周期管理，而非并发调度。
type Supervisor struct {
	agent    *Agent
	factory  AgentFactory
	children map[string]*childSlot
	childMu  sync.RWMutex
	log      *logger.Logger
}

// childSlot 追踪一个 L3 子 Agent
type childSlot struct {
	agent     *Agent
	cw        *ctxwin.ContextWindow
	createdAt time.Time
}

// NewSupervisor 创建 Supervisor
func NewSupervisor(agent *Agent, factory AgentFactory, log *logger.Logger) *Supervisor {
	return &Supervisor{
		agent:    agent,
		factory:  factory,
		children: make(map[string]*childSlot),
		log:      log,
	}
}

// SpawnChild instantiates an L3 child Agent from the given template.
//
// Flow: factory.Create → register in children map → return.
//
// KNOWN OVERHEAD: Each spawn pays the full Agent initialization cost
// (Definition build, tool build, skill load, registry register,
// ContextWindow create, agent Start goroutine). This is acceptable for
// the current L2→L3 pattern where spawns are infrequent (triggered by
// user tasks, not per-token). For high-frequency spawning, consider:
//   - Agent pooling (pre-spawn N agents, reuse via Reset)
//   - Template caching (pre-build tools once, clone per instance)
func (s *Supervisor) SpawnChild(ctx context.Context, tmpl AgentTemplate) (*Agent, error) {
	if s.factory == nil {
		return nil, fmt.Errorf("supervisor: no factory configured")
	}

	child, cw, err := s.factory.Create(ctx, tmpl)
	if err != nil {
		return nil, fmt.Errorf("supervisor: spawn child %q: %w", tmpl.ID, err)
	}

	s.childMu.Lock()
	s.children[tmpl.ID] = &childSlot{
		agent:     child,
		cw:        cw,
		createdAt: time.Now(),
	}
	s.childMu.Unlock()

	if s.log != nil {
		s.log.InfoContext(ctx, logger.CatActor, "supervisor spawned child",
			"child_id", tmpl.ID,
			"child_name", tmpl.Name,
		)
	}

	return child, nil
}

// ReapChild 回收一个子 Agent
//
// 彻底清理：
//  1. Stop Agent（关闭 mailbox + cancel ctx + 等 goroutine 退出）
//  2. Unregister from Registry（断开 Locator 引用）
//  3. 显式释放引用（帮助 GC）
func (s *Supervisor) ReapChild(childID string, timeout time.Duration) error {
	s.childMu.Lock()
	slot, ok := s.children[childID]
	delete(s.children, childID)
	s.childMu.Unlock()

	if !ok {
		return fmt.Errorf("supervisor: child %q not found", childID)
	}

	// 1. Stop Agent
	if err := slot.agent.Stop(timeout); err != nil {
		if s.log != nil {
			s.log.ErrorContext(context.Background(), logger.CatActor, "supervisor stop child failed", err,
				"child_id", childID,
			)
		}
		// 继续清理，不返回错误（Stop 超时不是致命错误）
	}

	// 2. Unregister from Registry
	if s.factory != nil && s.factory.Registry() != nil {
		s.factory.Registry().Unregister(childID)
	}

	// 3. 显式释放引用
	slot.cw = nil
	slot.agent = nil

	if s.log != nil {
		s.log.InfoContext(context.Background(), logger.CatActor, "supervisor reaped child",
			"child_id", childID,
		)
	}

	return nil
}

// ReapAll 回收所有子 Agent
//
// 返回每个子 Agent 的回收错误（如有）。即使部分失败，也会尝试回收所有。
func (s *Supervisor) ReapAll(timeout time.Duration) []error {
	s.childMu.Lock()
	ids := make([]string, 0, len(s.children))
	for id := range s.children {
		ids = append(ids, id)
	}
	s.childMu.Unlock()

	var errs []error
	for _, id := range ids {
		if err := s.ReapChild(id, timeout); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// Children 返回当前所有子 Agent 的快照
func (s *Supervisor) Children() []*Agent {
	s.childMu.RLock()
	defer s.childMu.RUnlock()

	agents := make([]*Agent, 0, len(s.children))
	for _, slot := range s.children {
		agents = append(agents, slot.agent)
	}
	return agents
}

// Agent 返回 Supervisor 管理的 L2 Agent
func (s *Supervisor) Agent() *Agent { return s.agent }

// ChildCount 返回当前子 Agent 数量
func (s *Supervisor) ChildCount() int {
	s.childMu.RLock()
	defer s.childMu.RUnlock()
	return len(s.children)
}

// ─── SpawnFn 闭包注入 ──────────────────────────────────────────────────────

// SpawnFnFor 为指定 SubAgent 模板创建 SpawnFn 闭包
//
// 注入到 L2 的 DelegateTool.SpawnFn 中，使 DelegateTool 可以动态孵化 L3。
// DelegateTool 不感知 Supervisor/Factory 的存在，只调用注入的闭包。
func (s *Supervisor) SpawnFnFor(tmpl AgentTemplate) func(ctx context.Context, task string) (iface.Locatable, error) {
	return func(ctx context.Context, task string) (iface.Locatable, error) {
		child, err := s.SpawnChild(ctx, tmpl)
		if err != nil {
			return nil, err
		}
		return &LocatableAdapter{Agent: child}, nil
	}
}

// SpawnFnForID 根据 child ID 查找模板并创建 SpawnFn
//
// 如果找不到对应模板，返回错误。
func (s *Supervisor) SpawnFnForID(childID string, allTemplates []AgentTemplate) func(ctx context.Context, task string) (iface.Locatable, error) {
	var tmpl *AgentTemplate
	for i := range allTemplates {
		if allTemplates[i].ID == childID {
			tmpl = &allTemplates[i]
			break
		}
	}
	if tmpl == nil {
		return func(ctx context.Context, task string) (iface.Locatable, error) {
			return nil, fmt.Errorf("supervisor: no template for child %q", childID)
		}
	}
	return s.SpawnFnFor(*tmpl)
}
