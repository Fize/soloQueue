package team

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/agent/agenttest"
	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
)

type reloadTestFactory struct {
	registry *agent.Registry
	created  *agent.Agent
}

func (f *reloadTestFactory) Create(ctx context.Context, tmpl agent.AgentTemplate, _ string) (*agent.Agent, *ctxwin.ContextWindow, error) {
	a := agent.NewAgent(agent.Definition{
		ID:   tmpl.ID,
		Name: tmpl.Name,
	}, &agenttest.FakeLLM{}, nil)
	if err := f.registry.Register(a); err != nil {
		return nil, nil, err
	}
	if err := a.Start(ctx); err != nil {
		f.registry.Unregister(a.InstanceID)
		return nil, nil, err
	}
	f.created = a
	return a, nil, nil
}

func (f *reloadTestFactory) CreateWithOptions(
	ctx context.Context,
	tmpl agent.AgentTemplate,
	workDir string,
	_ agent.CreateOptions,
) (*agent.Agent, *ctxwin.ContextWindow, error) {
	return f.Create(ctx, tmpl, workDir)
}

func (f *reloadTestFactory) Registry() *agent.Registry {
	return f.registry
}

func (f *reloadTestFactory) RebuildLeaderPrompt(tmpl agent.AgentTemplate, _ string) (string, error) {
	return tmpl.SystemPrompt, nil
}

func (f *reloadTestFactory) ResolveTemplate(_ context.Context, _ string) (agent.AgentTemplate, bool) {
	return agent.AgentTemplate{}, false
}

func TestReloadAgentOutlivesToolContext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "persistent-leader.md")
	content := `---
name: Persistent Leader
description: Reload lifecycle test
group: engineering
is_leader: true
---
Remain available after the edit tool returns.
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write agent file: %v", err)
	}

	factory := &reloadTestFactory{registry: agent.NewRegistry(nil)}
	wrapper := &reloadWrapper{
		cfg: &AutoReloadConfig{
			AgentFactory: factory,
		},
	}

	toolCtx, cancelTool := context.WithCancel(context.Background())
	note := wrapper.reloadAgent(toolCtx, path)
	if note == "" {
		t.Fatal("reloadAgent returned no activation note")
	}
	if factory.created == nil {
		t.Fatal("reloadAgent did not create an agent")
	}

	cancelTool()
	select {
	case <-factory.created.Done():
		t.Fatal("auto-reloaded agent stopped when the tool context was cancelled")
	case <-time.After(50 * time.Millisecond):
	}

	if got := factory.created.State(); got != agent.StateIdle {
		t.Fatalf("agent state = %s, want idle", got)
	}

	if err := factory.created.Stop(time.Second); err != nil {
		t.Fatalf("stop agent: %v", err)
	}
	factory.registry.Unregister(factory.created.InstanceID)
}
