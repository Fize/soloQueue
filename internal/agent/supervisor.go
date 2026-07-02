package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// ─── Supervisor ────────────────────────────────────────────────────────────

// Supervisor is the L2 domain manager
//
// It composes an Agent (rather than embedding), holding the L2 Agent instance and AgentFactory,
// responsible for L3 sub-Agent lifecycle management (Spawn/Reap).
//
// 'children' is a map from templateID to []childSlot, supporting multiple L3 instances of the same template
// working in parallel (each instance having its own context window and mailbox).
//
// L2's Fan-out/Fan-in reuses the existing execTools parallel mechanism:
//   - The L2 LLM returns multiple delegate_* tool_calls
//   - execTools executes each DelegateTool in parallel
//   - Each DelegateTool synchronously blocks on L3.Ask()
//   - After all are completed, the results are injected into the L2 context
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

// NewSupervisor creates a Supervisor
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
// workDir is passed through from the parent agent (L2→L3 passthrough).
//
// Flow: factory.Create → append to children[tmpl.ID] → return.
func (s *Supervisor) SpawnChild(ctx context.Context, tmpl AgentTemplate, workDir string) (*Agent, error) {
	if s.factory == nil {
		return nil, fmt.Errorf("supervisor: no factory configured")
	}

	child, cw, err := s.factory.Create(ctx, tmpl, workDir)
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

// ReapChild reaps a child Agent (by InstanceID)
//
// Full cleanup:
//  1. Stop Agent (close mailbox + cancel ctx + wait for goroutines to exit)
//  2. Unregister from Registry (break Locator reference)
//  3. Explicitly release references (aid GC)
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
		// Continue cleanup, do not return an error (Stop timeout is not a fatal error)
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

// ReapAll reaps all child Agents
//
// Returns reaping errors for each child Agent (if any). Even if some fail, it attempts to reap all.
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

// Children returns a snapshot of all current child Agents, sorted by name for stable display.
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

// Agent returns the L2 Agent managed by the Supervisor
func (s *Supervisor) Agent() *Agent { return s.agent }

// ChildCount returns the current number of child Agents
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
		l2.SetDelegateSpawnFn(tmpl.ID, func(ctx context.Context, task string, wd string) (iface.Locatable, error) {
			child, err := s.SpawnChild(ctx, tmpl, wd)
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
					for i := range allTemplates {
						if strings.EqualFold(allTemplates[i].ID, baseAgentName) {
							tmpl = allTemplates[i]
							ok = true
							break
						}
					}
				}

				if !ok {
					for i := range allTemplates {
						if strings.EqualFold(allTemplates[i].ID, name) {
							tmpl = allTemplates[i]
							ok = true
							break
						}
					}
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

				child, err := s.SpawnChild(ctx, tmpl, workDir)
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

// SpawnFnFor creates a SpawnFn closure for the given SubAgent template
//
// Injects into the L2's DelegateTool.SpawnFn, allowing the DelegateTool to dynamically spawn L3 agents.
// The DelegateTool is unaware of the Supervisor/Factory's existence; it only calls the injected closure.
// workDir is passed through from the delegate tool.
func (s *Supervisor) SpawnFnFor(tmpl AgentTemplate) func(ctx context.Context, task string, workDir string) (iface.Locatable, error) {
	return func(ctx context.Context, task string, wd string) (iface.Locatable, error) {
		child, err := s.SpawnChild(ctx, tmpl, wd)
		if err != nil {
			return nil, err
		}
		return &reapableAdapter{
			LocatableAdapter: &LocatableAdapter{Agent: child},
			supervisor:       s,
		}, nil
	}
}

// SpawnFnForID finds a template by child ID and creates a SpawnFn
//
// Returns an error if the corresponding template is not found.
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