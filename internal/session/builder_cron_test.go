package session

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
)

func TestBuildL1ForCronCreatesIsolatedEquivalentSessions(t *testing.T) {
	workDir, cfg, rt := newBuilderRegistryTestHarness(t)
	rt.SystemPrompt = "L1 cron system prompt"
	builder := NewBuilder(rt, workDir, cfg, false)

	permanentAgent, permanentCW, permanentTimeline, err := builder.Build(context.Background(), "default")
	if err != nil {
		t.Fatalf("Build permanent L1: %v", err)
	}
	permanentCW.Push(ctxwin.RoleUser, "permanent conversation must not be replayed")
	permanentAgent.ActivateScheduling()
	t.Cleanup(func() {
		_ = permanentAgent.Stop(time.Second)
		rt.AgentRegistry.Unregister(permanentAgent.InstanceID)
		_ = permanentTimeline.Close()
	})

	first, err := builder.BuildL1ForCron(context.Background(), "task-1", filepath.Join(workDir, "logs", "cron", "task-1"))
	if err != nil {
		t.Fatalf("BuildL1ForCron first: %v", err)
	}
	second, err := builder.BuildL1ForCron(context.Background(), "task-2", filepath.Join(workDir, "logs", "cron", "task-2"))
	if err != nil {
		t.Fatalf("BuildL1ForCron second: %v", err)
	}
	for _, cronSession := range []*Session{first, second} {
		t.Cleanup(func() {
			if a := cronSession.CurrentAgent(); a != nil {
				_ = a.Stop(time.Second)
				rt.AgentRegistry.Unregister(a.InstanceID)
			}
			cronSession.Close()
		})
	}

	if first == second || first.CurrentAgent() == second.CurrentAgent() || first.CurrentAgent().InstanceID == second.CurrentAgent().InstanceID {
		t.Fatal("BuildL1ForCron did not create distinct temporary sessions and agents")
	}
	if got := rt.AgentRegistry.Len(); got != 1 {
		t.Fatalf("registry Len with two temporary L1 sessions = %d, want only permanent L1", got)
	}
	for _, cronSession := range []*Session{first, second} {
		if _, ok := rt.AgentRegistry.Get(cronSession.CurrentAgent().InstanceID); ok {
			t.Fatalf("temporary L1 %q is visible in the general registry", cronSession.CurrentAgent().InstanceID)
		}
	}
	located, ok := rt.AgentRegistry.LocateIdleInWorkDir("l1-agent", workDir)
	locatedAgent, locatedIsAgent := located.(*agent.LocatableAdapter)
	if !ok || !locatedIsAgent || locatedAgent.Agent != permanentAgent {
		t.Fatalf("general locator returned %#v, want permanent L1 %q", located, permanentAgent.InstanceID)
	}
	for _, cronSession := range []*Session{first, second} {
		a := cronSession.CurrentAgent()
		if a.Def.SystemPrompt != rt.SystemPrompt {
			t.Fatalf("cron system prompt = %q, want %q", a.Def.SystemPrompt, rt.SystemPrompt)
		}
		for _, toolName := range []string{"Skill", "delegate", "Remember", "RecallMemory"} {
			if !hasToolSpec(a, toolName) {
				t.Fatalf("temporary L1 missing %s", toolName)
			}
		}
		payload := cronSession.ContextWindow().BuildPayload()
		if len(payload) != 1 || payload[0].Role != string(ctxwin.RoleSystem) || payload[0].Content != rt.SystemPrompt {
			t.Fatalf("temporary L1 context replayed permanent state: %+v", payload)
		}
	}
}
