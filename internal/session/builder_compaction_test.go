package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/agent/agenttest"
	"github.com/xiaobaitu/soloqueue/internal/agent/llmtypes"
	"github.com/xiaobaitu/soloqueue/internal/agenttools/tools"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
)

type countingSessionCompactor struct{ calls atomic.Int32 }

func TestBuildL2ProjectInstructionsAreInitialUserInputAcrossReplay(t *testing.T) {
	workDir, cfg, rt := newBuilderRegistryTestHarness(t)
	projectDir := t.TempDir()
	const rules = "Project rule: keep changes within the requested scope."
	if err := os.WriteFile(filepath.Join(projectDir, "AGENTS.md"), []byte(rules), 0644); err != nil {
		t.Fatal(err)
	}
	requests := make(chan llmtypes.LLMRequest, 3)
	fake := &agenttest.FakeLLM{Responses: []string{"reply"}, Hook: func(req llmtypes.LLMRequest) { requests <- req }}
	rt.AllTemplates = []agent.AgentTemplate{{ID: "leader", Group: "dev", IsLeader: true, SystemPrompt: "System"}}
	rt.AgentFactory = agent.NewDefaultFactory(rt.AgentRegistry, fake, tools.Config{}, nil, agent.WithWorkDir(workDir))
	rt.Compactor = &countingSessionCompactor{}
	b := NewBuilder(rt, workDir, cfg, false)
	build := func() *Session {
		s, err := b.BuildL2(t.Context(), "project-input", "dev", projectDir)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.DisposeGeneration(time.Second); s.Close() })
		return s
	}
	assertInstructions := func(t *testing.T, s *Session, want int) {
		t.Helper()
		count := 0
		for i := 0; i < s.cw.Len(); i++ {
			m, _ := s.cw.MessageAt(i)
			if strings.Contains(m.Content, rules) {
				if m.Role != ctxwin.RoleUser {
					t.Fatalf("project instructions have role %s, want user", m.Role)
				}
				count++
			}
		}
		if count != want {
			t.Fatalf("project instructions appear %d times, want %d", count, want)
		}
	}
	s := build()
	assertInstructions(t, s, 1)
	initial, _ := s.cw.MessageAt(1)
	if !strings.Contains(initial.Content, rules) {
		t.Fatal("project instructions must precede the first task")
	}
	fake.Err = errors.New("temporary model failure")
	stream, err := s.AskStream(t.Context(), "failed task")
	if err != nil {
		t.Fatal(err)
	}
	failed := false
	for ev := range stream {
		if _, ok := ev.(agent.ErrorEvent); ok {
			failed = true
		}
	}
	if !failed {
		t.Fatal("expected the model failure to reach the caller")
	}
	<-requests
	assertInstructions(t, s, 1)
	fake.Err = nil
	for _, task := range []string{"first task", "follow-up"} {
		stream, err := s.AskStream(t.Context(), task)
		if err != nil {
			t.Fatal(err)
		}
		for ev := range stream {
			if failure, ok := ev.(agent.ErrorEvent); ok {
				t.Fatal(failure.Err)
			}
		}
		req := <-requests
		count := 0
		for _, m := range req.Messages {
			if strings.Contains(m.Content, rules) {
				if m.Role != "user" {
					t.Fatalf("outgoing project instructions have role %s, want user", m.Role)
				}
				count++
			}
		}
		if count != 1 {
			t.Fatalf("outgoing request contains %d project inputs, want 1", count)
		}
		assertInstructions(t, s, 1)
	}
	s.DisposeGeneration(time.Second)
	s.Close()
	s = build()
	assertInstructions(t, s, 1)
	if _, err := s.cw.CompactAndReplace(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertInstructions(t, s, 0)
	s.DisposeGeneration(time.Second)
	s.Close()
	s = build()
	assertInstructions(t, s, 0)
	summary, ok := s.cw.MessageAt(1)
	if !ok || summary.Role != ctxwin.RoleUser || !summary.IsSummary {
		t.Fatalf("restored context must start with user summary, got %+v", summary)
	}
	for _, clear := range []struct {
		name string
		run  func() error
	}{{"clear", s.Clear}, {"silent clear", s.ClearSilent}} {
		t.Run(clear.name, func(t *testing.T) {
			if err := clear.run(); err != nil {
				t.Fatal(err)
			}
			assertInstructions(t, s, 1)
			stream, err := s.AskStream(t.Context(), "task after clear")
			if err != nil {
				t.Fatal(err)
			}
			for ev := range stream {
				if failure, ok := ev.(agent.ErrorEvent); ok {
					t.Fatal(failure.Err)
				}
			}
			req := <-requests
			if len(req.Messages) != 3 || req.Messages[1].Role != "user" || !strings.Contains(req.Messages[1].Content, rules) {
				t.Fatal("new conversation must send project instructions before its task")
			}
		})
	}
	s.DisposeGeneration(time.Second)
	s.Close()
	s = build()
	assertInstructions(t, s, 1)
}

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
		agent.WithWorkDir(workDir), agent.WithDefaultModelID("test"), agent.WithModelResolver(func(string) (agent.ModelInfo, error) {
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
