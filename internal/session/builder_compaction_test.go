package session

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/agent/agenttest"
	"github.com/xiaobaitu/soloqueue/internal/agenttools/tools"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
)

type countingSessionCompactor struct{ calls atomic.Int32 }

func (c *countingSessionCompactor) Compact(context.Context, []ctxwin.Message) (string, error) {
	c.calls.Add(1)
	return "Preserved conversation summary.", nil
}

func TestBuildL2DoesNotCompactIdleConversation(t *testing.T) {
	workDir, cfg, rt := newBuilderRegistryTestHarness(t)
	fake := &agenttest.FakeLLM{Responses: []string{"reply"}}
	rt.LLMClient = fake
	rt.AllTemplates = []agent.AgentTemplate{{ID: "leader", Group: "dev", ModelID: "test", IsLeader: true, SystemPrompt: "System"}}
	rt.AgentFactory = agent.NewDefaultFactory(rt.AgentRegistry, fake, tools.Config{}, nil,
		agent.WithWorkDir(workDir), agent.WithModelResolver(func(string) (agent.ModelInfo, error) {
			return agent.ModelInfo{ContextWindow: 1048576}, nil
		}))
	compactor := &countingSessionCompactor{}
	rt.Compactor = compactor
	s, err := NewBuilder(rt, workDir, cfg, false).BuildL2(t.Context(), "idle-test", "dev", workDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.DisposeGeneration(time.Second); s.Close() })
	s.cw.Push(ctxwin.RoleUser, strings.Repeat("x ", 210000), ctxwin.WithSuppressAutoCompact())
	if tokens := s.cw.CurrentTokens(); tokens < 200000 {
		t.Fatalf("test context has %d tokens, needs at least 200000", tokens)
	}
	s.lastActive.Store(time.Now().Add(-time.Hour).UnixNano())
	stream, err := s.AskStream(t.Context(), "continue")
	if err != nil {
		t.Fatal(err)
	}
	for event := range stream {
		if failure, ok := event.(agent.ErrorEvent); ok {
			t.Fatal(failure.Err)
		}
	}
	if got := compactor.calls.Load(); got != 0 {
		t.Fatalf("idle L2 compacted %d times, want 0", got)
	}
	stream, err = s.AskStream(t.Context(), "/compact")
	if err != nil {
		t.Fatal(err)
	}
	for event := range stream {
		if failure, ok := event.(agent.ErrorEvent); ok {
			t.Fatal(failure.Err)
		}
	}
	if got := compactor.calls.Load(); got == 0 {
		t.Fatal("manual L2 compact did not invoke the compactor")
	}
	before := compactor.calls.Load()
	s.cw.Resize(10000, 1000, 100)
	s.cw.Push(ctxwin.RoleUser, strings.Repeat("waterline ", 100))
	deadline := time.After(2 * time.Second)
	for compactor.calls.Load() == before || s.cw.ActiveCompactionID() != "" {
		select {
		case <-deadline:
			t.Fatal("L2 waterline compact did not complete")
		case <-time.After(time.Millisecond):
		}
	}
}

func TestL1RetainsIdleCompression(t *testing.T) {
	workDir, cfg, rt := newBuilderRegistryTestHarness(t)
	rt.SystemPrompt = "System"
	rt.DefaultModel.ContextWindow = 1048576
	rt.LLMClient = &agenttest.FakeLLM{Responses: []string{"reply"}}
	compactor := &countingSessionCompactor{}
	rt.Compactor = compactor
	mgr := NewSessionManager(NewBuilder(rt, workDir, cfg, false).Build, nil)
	mgr.SetIdleReaper(30*time.Minute, 200000)
	s, err := mgr.Init(t.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { mgr.Shutdown(time.Second) })
	s.cw.Push(ctxwin.RoleUser, strings.Repeat("x ", 210000), ctxwin.WithSuppressAutoCompact())
	if s.cw.CurrentTokens() < 200000 {
		t.Fatal("test context is below the idle compression threshold")
	}
	s.lastActive.Store(time.Now().Add(-time.Hour).UnixNano())
	stream, err := s.AskStream(t.Context(), "continue")
	if err != nil {
		t.Fatal(err)
	}
	for event := range stream {
		if failure, ok := event.(agent.ErrorEvent); ok {
			t.Fatal(failure.Err)
		}
	}
	if got := compactor.calls.Load(); got != 1 {
		t.Fatalf("idle L1 compacted %d times, want 1", got)
	}
}
