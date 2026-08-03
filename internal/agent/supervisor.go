package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/agenttools/tools"
)

// ─── Supervisor ────────────────────────────────────────────────────────────

// Supervisor is the L2 domain manager. Composes an Agent + Factory for L3 child lifecycle.
// children[templateID] supports multiple L3 instances per template for parallel work.
type Supervisor struct {
	agent    *Agent
	factory  AgentFactory
	children map[string][]*childSlot // templateID → instances
	childMu  sync.RWMutex
	group    string // team group name for matching workers during auto-reload
	log      *logger.Logger
}

// childSlot tracks an L3 child Agent
type childSlot struct {
	agent     *Agent
	cw        *ctxwin.ContextWindow
	createdAt time.Time
}


func NewSupervisor(agent *Agent, factory AgentFactory, log *logger.Logger) *Supervisor {
	return &Supervisor{
		agent:    agent,
		factory:  factory,
		children: make(map[string][]*childSlot),
		log:      log,
	}
}

// SpawnChild creates a new L3 child with a unique InstanceID.
// workDir passes through from L2 to L3.
func (s *Supervisor) SpawnChild(ctx context.Context, tmpl AgentTemplate, workDir string) (*Agent, error) {
	if s.factory == nil {
		return nil, fmt.Errorf("supervisor: no factory configured")
	}

	child, cw, err := s.factory.Create(ctx, tmpl, workDir)
	if err != nil {
		return nil, fmt.Errorf("supervisor: spawn child %q: %w", tmpl.ID, err)
	}

	// Share/inherit parent's confirmStore
	child.SetConfirmStore(s.agent.ConfirmStore())

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
			"work_dir", child.WorkDir,
		)
	}

	return child, nil
}

// ReapChild stops a child, unregisters it, and releases references.
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
		// Continue cleanup even on Stop timeout.
	}

	// 2. Unregister from Registry
	if s.factory != nil && s.factory.Registry() != nil {
		s.factory.Registry().Unregister(instanceID)
	}

	// 3. Explicitly release references
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

// ReapAll reaps all children. Best-effort: continues even if some fail.
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

// AdoptChild tracks an existing agent without going through SpawnChild.
// Used by auto-reload for hot-instantiated workers.
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


func (s *Supervisor) Agent() *Agent { return s.agent }


func (s *Supervisor) ChildCount() int {
	s.childMu.RLock()
	defer s.childMu.RUnlock()
	count := 0
	for _, slots := range s.children {
		count += len(slots)
	}
	return count
}

// UpdateLeaderPrompt replaces the system prompt and re-wires delegate tools.
// Caller must also update the session-level ContextWindow.
func (s *Supervisor) UpdateLeaderPrompt(newPrompt string, allTemplates []AgentTemplate) {
	s.agent.SetSystemPrompt(newPrompt)
	s.WireSpawnFns(allTemplates)
}

// ─── WireSpawnFns ──────────────────────────────────────────────────────────

// WireSpawnFns rewires delegate tools to use Supervisor.SpawnChild so L3
// children are tracked. Must be called after the Supervisor is constructed.
func (s *Supervisor) WireSpawnFns(allTemplates []AgentTemplate) {
	l2 := s.Agent()
	if l2.tools == nil {
		return
	}
	for _, tmpl := range allTemplates {
		if tmpl.IsLeader || tmpl.Group == "" {
			continue
		}
		tmpl := tmpl // capture loop variable
		l2.SetDelegateSpawnFn(tmpl.ID, func(ctx context.Context, task string, wd string) (iface.Locatable, error) {
			freshTmpl, ok := s.factory.ResolveTemplate(ctx, tmpl.ID)
			if !ok {
				freshTmpl = tmpl
			}
			child, err := s.spawnInheritedChild(ctx, freshTmpl)
			if err != nil {
				return nil, err
			}
			return &reapableAdapter{
				LocatableAdapter: &LocatableAdapter{Agent: child},
				supervisor:       s,
			}, nil
		})
	}

	// Wire the generic delegate_agent tool if it exists on the leader
	if t, ok := l2.tools.Get("delegate_agent"); ok {
		if dat, ok2 := t.(*tools.DelegateAgentTool); ok2 {
			dat.SpawnFn = func(ctx context.Context, name, systemPrompt, modelID, task, workDir string, baseAgentName string, skillDir string) (iface.Locatable, error) {
				var tmpl AgentTemplate
				var ok bool

				if skillDir != "" {
					tmpl, ok = LoadSkillAgentTemplate(skillDir, name)
					if !ok && baseAgentName != "" {
						tmpl, ok = LoadSkillAgentTemplate(skillDir, baseAgentName)
					}
				}

				if !ok && baseAgentName != "" {
					tmpl, ok = s.factory.ResolveTemplate(ctx, baseAgentName)
				}

				if !ok {
					tmpl, ok = s.factory.ResolveTemplate(ctx, name)
				}

				tmpl.ID = strings.ToLower(name)
				tmpl.Name = name
				tmpl.IsLeader = false // All dynamically delegated agents are L3 workers

				if ok {
					if systemPrompt != "" {
						if tmpl.SystemPrompt != "" {
							tmpl.SystemPrompt = tmpl.SystemPrompt + "\n\n# Skill/Custom execution logic:\n" + systemPrompt
						} else {
							tmpl.SystemPrompt = systemPrompt
						}
					}
				} else {
					tmpl.Description = "Dynamic skill agent"
					tmpl.SystemPrompt = systemPrompt
				}

				if modelID != "" {
					tmpl.ModelID = modelID
				}

				child, err := s.spawnInheritedChild(ctx, tmpl)
				if err != nil {
					return nil, err
				}
				return &reapableAdapter{
					LocatableAdapter: &LocatableAdapter{Agent: child},
					supervisor:       s,
				}, nil
			}
		}
	}
}

// ─── SpawnFn closure injection ──────────────────────────────────────────────────────

// SpawnFnFor creates a SpawnFn closure for the given template.
// The DelegateTool calls this closure without knowing about the Supervisor.
// L2 children always inherit the supervisor's parent work directory.
func (s *Supervisor) SpawnFnFor(tmpl AgentTemplate) func(ctx context.Context, task string, workDir string) (iface.Locatable, error) {
	return func(ctx context.Context, task string, wd string) (iface.Locatable, error) {
		child, err := s.spawnInheritedChild(ctx, tmpl)
		if err != nil {
			return nil, err
		}
		return &reapableAdapter{
			LocatableAdapter: &LocatableAdapter{Agent: child},
			supervisor:       s,
		}, nil
	}
}

func (s *Supervisor) spawnInheritedChild(ctx context.Context, tmpl AgentTemplate) (*Agent, error) {
	if s.agent == nil || s.agent.WorkDir == "" {
		return nil, fmt.Errorf("supervisor: parent work directory is not configured")
	}
	return s.SpawnChild(ctx, tmpl, s.agent.WorkDir)
}

// SpawnFnForID resolves a template by ID and creates a SpawnFn.
func (s *Supervisor) SpawnFnForID(childID string, allTemplates []AgentTemplate) func(ctx context.Context, task string, workDir string) (iface.Locatable, error) {
	var tmpl *AgentTemplate
	for i := range allTemplates {
		if allTemplates[i].ID == childID {
			tmpl = &allTemplates[i]
			break
		}
	}
	if tmpl == nil {
		return func(ctx context.Context, task string, workDir string) (iface.Locatable, error) {
			return nil, fmt.Errorf("supervisor: no template for child %q", childID)
		}
	}
	return s.SpawnFnFor(*tmpl)
}

// ─── Reapable adapters ─────────────────────────────────────────────────────

// reapableAdapter wraps LocatableAdapter with auto-reap on delegation completion.
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
	return NewSelfReapableAdapterWithCleanup(agent, sv, nil)
}

// NewSelfReapableAdapterWithCleanup creates a self-reaping adapter with an optional
// post-reap callback. The callback breaks the runtime→supervisor reference without
// introducing a package dependency.
func NewSelfReapableAdapterWithCleanup(agent *Agent, sv *Supervisor, onReaped func()) *SelfReapableAdapter {
	return &SelfReapableAdapter{
		LocatableAdapter: &LocatableAdapter{Agent: agent},
		supervisor:       sv,
		onReaped:         onReaped,
	}
}

// SelfReapableAdapter reaps the entire supervisor (L2 + all L3 children) on delegation done.
// Used for dynamically created L2 agents.
type SelfReapableAdapter struct {
	*LocatableAdapter
	supervisor *Supervisor
	onReaped   func()
	reapOnce   sync.Once
}

func (ra *SelfReapableAdapter) OnDelegationDone() {
	ra.reapOnce.Do(func() {
		// Stop L2 first so it cannot submit new work to children during cleanup.
		ra.supervisor.Agent().Stop(10 * time.Second)
		// Then reap all children (each child has its own reapableAdapter that
		// already called ReapChild on completion; this is defensive cleanup).
		ra.supervisor.ReapAll(10 * time.Second)
		if ra.supervisor.factory != nil && ra.supervisor.factory.Registry() != nil {
			ra.supervisor.factory.Registry().Unregister(ra.Agent.InstanceID)
		}
		if ra.onReaped != nil {
			ra.onReaped()
		}
	})
}

// Compile-time interface checks.
var _ iface.Locatable = (*SelfReapableAdapter)(nil)
var _ iface.ModelOverridable = (*SelfReapableAdapter)(nil)
var _ iface.DoneNotifier = (*SelfReapableAdapter)(nil)
