package session

import (
	"context"
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
	for _, removed := range []string{"workflow_list", "workflow_run", "workflow_get", "workflow_wait"} {
		if hasToolSpec(a, removed) {
			t.Fatalf("L1 tools still include removed tool %q", removed)
		}
	}
}

func hasToolSpec(a *agent.Agent, name string) bool {
	for _, spec := range a.ToolSpecs() {
		if spec.Function.Name == name {
			return true
		}
	}
	return false
}
