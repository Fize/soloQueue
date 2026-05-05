package agent

import (
	"context"
	"fmt"
	"sort"
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
// children 是 templateID → []childSlot 的映射，支持同一模板的多个 L3 实例
// 并行工作（每个实例拥有独立的 context window 和 mailbox）。
//
// L2 的 Fan-out/Fan-in 复用现有 execTools 并行机制：
//   - L2 LLM 返回多个 delegate_* tool_calls
//   - execTools 并行执行每个 DelegateTool
//   - 每个 DelegateTool 同步阻塞调用 L3.Ask()
//   - 全部完成后结果注入 L2 上下文
type Supervisor struct {
	agent    *Agent
	factory  AgentFactory
	children map[string][]*childSlot // templateID → instances
	childMu  sync.RWMutex
	group    string // team group name for matching workers during auto-reload
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
		children: make(map[string][]*childSlot),
		log:      log,
	}
}

// SpawnChild instantiates an L3 child Agent from the given template.
// Each call creates a new Agent instance with a unique InstanceID,
// allowing multiple children of the same template to run concurrently.
//
// Flow: factory.Create → append to children[tmpl.ID] → return.
func (s *Supervisor) SpawnChild(ctx context.Context, tmpl AgentTemplate) (*Agent, error) {
	if s.factory == nil {
		return nil, fmt.Errorf("supervisor: no factory configured")
	}

	child, cw, err := s.factory.Create(ctx, tmpl)
	if err != nil {
		return nil, fmt.Errorf("supervisor: spawn child %q: %w", tmpl.ID, err)
	}

	slot := &childSlot{
		agent:     child,
		cw:        cw,
		createdAt: time.Now(),
	}

	s.childMu.Lock()
	s.children[tmpl.ID] = append(s.children[tmpl.ID], slot)
	s.childMu.Unlock()

	if s.log != nil {
		s.log.InfoContext(ctx, logger.CatActor, "supervisor spawned child",
			"instance_id", child.InstanceID,
			"template_id", tmpl.ID,
			"child_name", tmpl.Name,
		)
	}

	return child, nil
}

// ReapChild 回收一个子 Agent（按 InstanceID 查找）
//
// 彻底清理：
//  1. Stop Agent（关闭 mailbox + cancel ctx + 等 goroutine 退出）
//  2. Unregister from Registry（断开 Locator 引用）
//  3. 显式释放引用（帮助 GC）
func (s *Supervisor) ReapChild(instanceID string, timeout time.Duration) error {
	s.childMu.Lock()
	var tmplID string
	var slot *childSlot
	var idx int
	found := false
	for tid, slots := range s.children {
		for i, sl := range slots {
			if sl.agent.InstanceID == instanceID {
				tmplID = tid
				slot = sl
				idx = i
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if found {
		slots := s.children[tmplID]
		s.children[tmplID] = append(slots[:idx], slots[idx+1:]...)
		if len(s.children[tmplID]) == 0 {
			delete(s.children, tmplID)
		}
	}
	s.childMu.Unlock()

	if !found {
		return fmt.Errorf("supervisor: child %q not found", instanceID)
	}

	// 1. Stop Agent
	if err := slot.agent.Stop(timeout); err != nil {
		if s.log != nil {
			s.log.ErrorContext(context.Background(), logger.CatActor, "supervisor stop child failed", err,
				"instance_id", instanceID,
			)
		}
		// 继续清理，不返回错误（Stop 超时不是致命错误）
	}

	// 2. Unregister from Registry
	if s.factory != nil && s.factory.Registry() != nil {
		s.factory.Registry().Unregister(instanceID)
	}

	// 3. 显式释放引用
	slot.cw = nil
	slot.agent = nil

	if s.log != nil {
		s.log.InfoContext(context.Background(), logger.CatActor, "supervisor reaped child",
			"instance_id", instanceID,
			"template_id", tmplID,
		)
	}

	return nil
}

// ReapAll 回收所有子 Agent
//
// 返回每个子 Agent 的回收错误（如有）。即使部分失败，也会尝试回收所有。
func (s *Supervisor) ReapAll(timeout time.Duration) []error {
	s.childMu.Lock()
	type reapTarget struct {
		instanceID string
	}
	var targets []reapTarget
	for _, slots := range s.children {
		for _, slot := range slots {
			targets = append(targets, reapTarget{instanceID: slot.agent.InstanceID})
		}
	}
	s.childMu.Unlock()

	var errs []error
	for _, t := range targets {
		if err := s.ReapChild(t.instanceID, timeout); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// Children 返回当前所有子 Agent 的快照，按名称排序以保证稳定显示。
func (s *Supervisor) Children() []*Agent {
	s.childMu.RLock()
	defer s.childMu.RUnlock()

	var agents []*Agent
	for _, slots := range s.children {
		for _, slot := range slots {
			agents = append(agents, slot.agent)
		}
	}
	sort.Slice(agents, func(i, j int) bool {
		ni, nj := agents[i].Def.Name, agents[j].Def.Name
		if ni != nj {
			return ni < nj
		}
		return agents[i].Def.ID < agents[j].Def.ID
	})
	return agents
}

// AdoptChild adds an already-created agent to the supervisor's children map
// under its template ID. Used by auto-reload to track hot-instantiated workers
// without going through SpawnChild.
func (s *Supervisor) AdoptChild(child *Agent) {
	s.childMu.Lock()
	tmplID := child.Def.ID
	s.children[tmplID] = append(s.children[tmplID], &childSlot{
		agent:     child,
		createdAt: time.Now(),
	})
	s.childMu.Unlock()
}

// SetGroup sets the team group name, used to match workers to their leader during auto-reload.
func (s *Supervisor) SetGroup(g string) { s.group = g }

// Group returns the team group name.
func (s *Supervisor) Group() string { return s.group }

// Agent 返回 Supervisor 管理的 L2 Agent
func (s *Supervisor) Agent() *Agent { return s.agent }

// ChildCount 返回当前子 Agent 数量
func (s *Supervisor) ChildCount() int {
	s.childMu.RLock()
	defer s.childMu.RUnlock()
	count := 0
	for _, slots := range s.children {
		count += len(slots)
	}
	return count
}

// ─── WireSpawnFns ──────────────────────────────────────────────────────────

// WireSpawnFns rewires the L2 agent's DelegateTools to use Supervisor.SpawnChild
// instead of direct factory.Create. This ensures L3 children spawned via
// delegation are tracked in the Supervisor's children map.
//
// Must be called after the L2 agent is created and the Supervisor is constructed.
// allTemplates is the full template list used to resolve worker templates by ID.
func (s *Supervisor) WireSpawnFns(allTemplates []AgentTemplate) {
	l2 := s.Agent()
	for _, tmpl := range allTemplates {
		if tmpl.IsLeader || tmpl.Group == "" {
			continue
		}
		tmpl := tmpl // capture loop variable
		l2.SetDelegateSpawnFn(tmpl.ID, func(ctx context.Context, task string) (iface.Locatable, error) {
			child, err := s.SpawnChild(ctx, tmpl)
			if err != nil {
				return nil, err
			}
			return &reapableAdapter{
				LocatableAdapter: &LocatableAdapter{Agent: child},
				supervisor:       s,
			}, nil
		})
	}
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
		return &reapableAdapter{
			LocatableAdapter: &LocatableAdapter{Agent: child},
			supervisor:       s,
		}, nil
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

// ─── Reapable adapters ─────────────────────────────────────────────────────

// reapableAdapter wraps a LocatableAdapter with a DoneNotifier that reaps the
// child from the supervisor when delegation completes. Used for L3 workers.
type reapableAdapter struct {
	*LocatableAdapter
	supervisor *Supervisor
}

func (ra *reapableAdapter) OnDelegationDone() {
	ra.supervisor.ReapChild(ra.Agent.InstanceID, 10*time.Second)
}

// Compile-time interface checks.
var _ iface.Locatable = (*reapableAdapter)(nil)
var _ iface.ModelOverridable = (*reapableAdapter)(nil)
var _ iface.DoneNotifier = (*reapableAdapter)(nil)

// NewSelfReapableAdapter creates a SelfReapableAdapter that reaps the entire
// supervisor (L2 + all children) when delegation completes.
func NewSelfReapableAdapter(agent *Agent, sv *Supervisor) *SelfReapableAdapter {
	return &SelfReapableAdapter{
		LocatableAdapter: &LocatableAdapter{Agent: agent},
		supervisor:       sv,
	}
}

// SelfReapableAdapter wraps a LocatableAdapter with a DoneNotifier that reaps
// the entire supervisor (L2 + all children) when delegation completes.
// Used for dynamically created L2 agents (SpawnFn in main.go).
type SelfReapableAdapter struct {
	*LocatableAdapter
	supervisor *Supervisor
}

func (ra *SelfReapableAdapter) OnDelegationDone() {
	ra.supervisor.ReapAll(10 * time.Second)
	ra.supervisor.Agent().Stop(10 * time.Second)
	if ra.supervisor.factory != nil && ra.supervisor.factory.Registry() != nil {
		ra.supervisor.factory.Registry().Unregister(ra.Agent.InstanceID)
	}
}

// Compile-time interface checks.
var _ iface.Locatable = (*SelfReapableAdapter)(nil)
var _ iface.ModelOverridable = (*SelfReapableAdapter)(nil)
var _ iface.DoneNotifier = (*SelfReapableAdapter)(nil)
