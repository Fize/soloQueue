package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/agent/agenttest"
	"github.com/xiaobaitu/soloqueue/internal/agenttools/skill"
	"github.com/xiaobaitu/soloqueue/internal/config"
	sqlitedb "github.com/xiaobaitu/soloqueue/internal/infra/db"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/memory/engine"
	"github.com/xiaobaitu/soloqueue/internal/prompt"
	"github.com/xiaobaitu/soloqueue/internal/runtime"
	"github.com/xiaobaitu/soloqueue/internal/team/store"
)

func TestBuilderBuild_RegistersExpectedL1Tools(t *testing.T) {
	workDir := t.TempDir()
	cfg, err := config.New(workDir)
	if err != nil {
		t.Fatalf("config.New: %v", err)
	}
	shared, err := sqlitedb.Open(filepath.Join(workDir, "soloqueue.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer shared.Close()
	memoryEngine := engine.New(shared.DB, &shared.WMu, nil, nil, nil)

	rt := &runtime.Stack{
		LLMClient:     &agenttest.FakeLLM{},
		DefaultModel:  &config.LLMModel{ID: "test", ContextWindow: 8192},
		AgentRegistry: agent.NewRegistry(nil),
		SkillRegistry: skill.NewSkillRegistry(),
		Tokenizer:     ctxwin.NewTokenizer(),
		PromptCfg:     &prompt.PromptConfig{RolesDir: workDir, GlobalDir: workDir},
		TeamStore:     store.NewStore(filepath.Join(workDir, "groups"), filepath.Join(workDir, "agents"), nil),
		MemoryEngine:  memoryEngine,
	}

	a, _, timelineWriter, err := NewBuilder(rt, workDir, cfg, false).Build(context.Background(), "default")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(func() {
		_ = a.Stop(time.Second)
		_ = timelineWriter.Close()
	})

	if !hasToolSpec(a, "resolve_project") {
		t.Fatal("L1 tools do not include resolve_project")
	}
	if !hasToolSpec(a, "Remember") || !hasToolSpec(a, "RecallMemory") {
		t.Fatal("L1 tools do not include L1-bound memory capabilities")
	}
	if !hasToolSpec(a, "Skill") {
		t.Fatal("L1 tools must include Skill even when no skills are installed yet")
	}
	for _, removed := range []string{"workflow_list", "workflow_run", "workflow_get", "workflow_wait"} {
		if hasToolSpec(a, removed) {
			t.Fatalf("L1 tools still include removed tool %q", removed)
		}
	}
}

func TestBuilderBuild_PropagatesWorkDirToL1AgentAndTools(t *testing.T) {
	workDir, cfg, rt := newBuilderRegistryTestHarness(t)

	a, _, timelineWriter, err := NewBuilder(rt, workDir, cfg, false).Build(context.Background(), "default")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(func() {
		_ = a.Stop(time.Second)
		_ = timelineWriter.Close()
	})

	if a.WorkDir != workDir {
		t.Fatalf("L1 Agent.WorkDir = %q, want %q", a.WorkDir, workDir)
	}
}

func TestBuilderL1RebuildReusesGenerationLogger(t *testing.T) {
	workDir := t.TempDir()
	cfg, err := config.New(workDir)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := sqlitedb.Open(filepath.Join(workDir, "soloqueue.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer shared.Close()
	rt := &runtime.Stack{
		LLMClient:     &agenttest.FakeLLM{},
		DefaultModel:  &config.LLMModel{ID: "test", ContextWindow: 8192},
		AgentRegistry: agent.NewRegistry(nil),
		SkillRegistry: skill.NewSkillRegistry(),
		Tokenizer:     ctxwin.NewTokenizer(),
		PromptCfg:     &prompt.PromptConfig{RolesDir: workDir, GlobalDir: workDir},
		TeamStore:     store.NewStore(filepath.Join(workDir, "groups"), filepath.Join(workDir, "agents"), nil),
		MemoryEngine:  engine.New(shared.DB, &shared.WMu, nil, nil, nil),
	}
	b := NewBuilder(rt, workDir, cfg, false)
	first, _, firstTimeline, err := b.Build(context.Background(), "default")
	if err != nil {
		t.Fatal(err)
	}
	second, _, secondTimeline, err := b.Build(context.Background(), "default")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = first.Stop(time.Second)
		_ = second.Stop(time.Second)
		_ = firstTimeline.Close()
		_ = secondTimeline.Close()
	})
	if first.Log == nil || second.Log == nil {
		t.Fatal("builder returned Agent without generation logger")
	}
	if first.Log != second.Log {
		t.Fatal("L1 rebuild opened a fresh logger instead of reusing the Session generation logger")
	}
}

func TestBuilderBuild_TimelineFailureUnregistersPendingAgent(t *testing.T) {
	workDir, cfg, rt := newBuilderRegistryTestHarness(t)
	preexisting := agent.NewAgent(agent.Definition{ID: "preexisting"}, &agenttest.FakeLLM{}, nil)
	if err := rt.AgentRegistry.Register(preexisting); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workDir, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workDir, "logs", "timelines"), []byte("block timeline directory"), 0o644); err != nil {
		t.Fatal(err)
	}

	var registered *agent.Agent
	rt.AgentRegistry.SetOnRegister(func(a *agent.Agent) { registered = a })
	_, _, _, err := NewBuilder(rt, workDir, cfg, false).Build(context.Background(), "default")
	if err == nil {
		t.Fatal("Build succeeded despite an invalid timeline parent")
	}
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) {
		t.Fatalf("Build error did not preserve the timeline path error: %v", err)
	}
	if registered == nil {
		t.Fatal("Build did not register the pending L1 Agent before timeline creation")
	}
	if _, ok := rt.AgentRegistry.Get(registered.InstanceID); ok {
		t.Fatalf("timeline failure left pending Agent %q registered", registered.InstanceID)
	}
	if got, ok := rt.AgentRegistry.Get(preexisting.InstanceID); !ok || got != preexisting {
		t.Fatal("timeline failure cleanup removed an unrelated Registry entry")
	}
}

func TestBuilderBuild_StartFailureUnregistersPendingAgent(t *testing.T) {
	workDir, cfg, rt := newBuilderRegistryTestHarness(t)

	var registered *agent.Agent
	rt.AgentRegistry.SetOnRegister(func(a *agent.Agent) {
		registered = a
		a.Quarantine(agent.ErrQuarantined)
	})
	_, _, _, err := NewBuilder(rt, workDir, cfg, false).Build(context.Background(), "default")
	if !errors.Is(err, agent.ErrQuarantined) {
		t.Fatalf("Build error = %v, want %v", err, agent.ErrQuarantined)
	}
	if registered == nil {
		t.Fatal("Build did not register the pending L1 Agent before starting it")
	}
	if _, ok := rt.AgentRegistry.Get(registered.InstanceID); ok {
		t.Fatalf("start failure left pending Agent %q registered", registered.InstanceID)
	}
}

func TestBuilderBuild_SuccessKeepsRegisteredAgent(t *testing.T) {
	workDir, cfg, rt := newBuilderRegistryTestHarness(t)

	a, _, timelineWriter, err := NewBuilder(rt, workDir, cfg, false).Build(context.Background(), "default")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(func() {
		_ = a.Stop(time.Second)
		_ = timelineWriter.Close()
	})
	registered, ok := rt.AgentRegistry.Get(a.InstanceID)
	if !ok || registered != a {
		t.Fatalf("successful Build did not retain returned Agent %q in Registry", a.InstanceID)
	}
	if a.Schedulable() {
		t.Fatal("successful Build unexpectedly published scheduling before Session ownership transfer")
	}
}

func newBuilderRegistryTestHarness(t *testing.T) (string, *config.GlobalService, *runtime.Stack) {
	t.Helper()
	workDir := t.TempDir()
	cfg, err := config.New(workDir)
	if err != nil {
		t.Fatalf("config.New: %v", err)
	}
	shared, err := sqlitedb.Open(filepath.Join(workDir, "soloqueue.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = shared.Close() })
	rt := &runtime.Stack{
		LLMClient:     &agenttest.FakeLLM{},
		DefaultModel:  &config.LLMModel{ID: "test", ContextWindow: 8192},
		AgentRegistry: agent.NewRegistry(nil),
		SkillRegistry: skill.NewSkillRegistry(),
		Tokenizer:     ctxwin.NewTokenizer(),
		PromptCfg:     &prompt.PromptConfig{RolesDir: workDir, GlobalDir: workDir},
		TeamStore:     store.NewStore(filepath.Join(workDir, "groups"), filepath.Join(workDir, "agents"), nil),
		MemoryEngine:  engine.New(shared.DB, &shared.WMu, nil, nil, nil),
	}
	return workDir, cfg, rt
}

func hasToolSpec(a *agent.Agent, name string) bool {
	for _, spec := range a.ToolSpecs() {
		if spec.Function.Name == name {
			return true
		}
	}
	return false
}
