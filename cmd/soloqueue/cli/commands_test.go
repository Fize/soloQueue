package cli

import (
	"context"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/agent/agenttest"
	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/session"
)

func TestCronSessionCleanupStopsAndUnregistersAgents(t *testing.T) {
	registry := agent.NewRegistry(nil)
	leader := agent.NewAgent(
		agent.Definition{ID: "cron-leader", Name: "Cron Leader"},
		&agenttest.FakeLLM{},
		nil,
	)
	child := agent.NewAgent(
		agent.Definition{ID: "cron-worker", Name: "Cron Worker"},
		&agenttest.FakeLLM{},
		nil,
	)
	for _, a := range []*agent.Agent{leader, child} {
		if err := registry.Register(a); err != nil {
			t.Fatalf("register %s: %v", a.Def.ID, err)
		}
		if err := a.Start(context.Background()); err != nil {
			t.Fatalf("start %s: %v", a.Def.ID, err)
		}
	}

	supervisor := agent.NewSupervisor(leader, &cronCleanupTestFactory{registry: registry}, nil)
	supervisor.AdoptChild(child)
	cronSession := session.NewSession("cron-task", "cron-team", leader, nil, nil, nil)
	cronSession.Supervisor = supervisor

	cleanup := newCronSessionCleanup(cronSession, registry)
	cleanup()
	cleanup()

	if got := registry.Len(); got != 0 {
		t.Fatalf("registry Len = %d, want 0", got)
	}
	if got := leader.State(); got != agent.StateStopped {
		t.Fatalf("leader state = %s, want stopped", got)
	}
	if got := child.State(); got != agent.StateStopped {
		t.Fatalf("child state = %s, want stopped", got)
	}
	if got := supervisor.ChildCount(); got != 0 {
		t.Fatalf("supervisor child count = %d, want 0", got)
	}
}

type cronCleanupTestFactory struct {
	registry *agent.Registry
}

func (f *cronCleanupTestFactory) Create(context.Context, agent.AgentTemplate, string) (*agent.Agent, *ctxwin.ContextWindow, error) {
	panic("unexpected Create call")
}

func (f *cronCleanupTestFactory) CreateWithOptions(
	context.Context,
	agent.AgentTemplate,
	string,
	agent.CreateOptions,
) (*agent.Agent, *ctxwin.ContextWindow, error) {
	panic("unexpected CreateWithOptions call")
}

func (f *cronCleanupTestFactory) Registry() *agent.Registry {
	return f.registry
}

func (f *cronCleanupTestFactory) RebuildLeaderPrompt(tmpl agent.AgentTemplate, _ string) (string, error) {
	return tmpl.SystemPrompt, nil
}

func (f *cronCleanupTestFactory) ResolveTemplate(_ context.Context, _ string) (agent.AgentTemplate, bool) {
	return agent.AgentTemplate{}, false
}
