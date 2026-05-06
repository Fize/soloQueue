package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/prompt"
)

// ─── fakeFactoryForTUI ─────────────────────────────────────────────────────

// fakeFactoryForTUI creates agents without starting them, for sidebar tests.
type fakeFactoryForTUI struct {
	reg *agent.Registry
	log *logger.Logger
}

func (f *fakeFactoryForTUI) Create(_ context.Context, tmpl agent.AgentTemplate) (*agent.Agent, *ctxwin.ContextWindow, error) {
	a := agent.NewAgent(agent.Definition{
		ID:   tmpl.ID,
		Name: tmpl.Name,
		Kind: agent.KindCustom,
	}, &fakeLLMForTUI{}, f.log)
	if f.reg != nil {
		f.reg.Register(a)
	}
	cw := ctxwin.NewContextWindow(8000, 2000, 0, ctxwin.NewTokenizer())
	return a, cw, nil
}

func (f *fakeFactoryForTUI) Registry() *agent.Registry { return f.reg }

// supervisorWithChildren creates a Supervisor with L3 children already tracked.
func supervisorWithChildren(l2 *agent.Agent, reg *agent.Registry, log *logger.Logger, childTmpls ...agent.AgentTemplate) *agent.Supervisor {
	sv := agent.NewSupervisor(l2, &fakeFactoryForTUI{reg: reg, log: log}, log)
	for _, tmpl := range childTmpls {
		sv.SpawnChild(context.Background(), tmpl)
	}
	return sv
}

// ─── counts ─────────────────────────────────────────────────────────────────

func TestCounts_NoRegistry(t *testing.T) {
	s := newSidebar(nil, nil, nil, nil, "")
	c := s.counts()
	if c.a1 != 0 || c.a2 != 0 || c.a3 != 0 {
		t.Errorf("nil registry should have zero counts, got a1=%d a2=%d a3=%d", c.a1, c.a2, c.a3)
	}
	if c.run != 0 || c.idle != 0 || c.off != 0 || c.stop != 0 {
		t.Errorf("nil registry should have zero state counts, got run=%d idle=%d off=%d stop=%d", c.run, c.idle, c.off, c.stop)
	}
}

func TestCounts_L1Only(t *testing.T) {
	log, _ := logger.System("/tmp", logger.WithConsole(false), logger.WithFile(false))
	reg := agent.NewRegistry(log)
	a := agent.NewAgent(agent.Definition{ID: "session-1", Name: "MainSession", ModelID: "ds-v4"}, &fakeLLMForTUI{}, log)
	reg.Register(a)

	s := newSidebar(reg, nil, nil, nil, "")
	c := s.counts()
	if c.a1 != 1 {
		t.Errorf("expected a1=1, got %d", c.a1)
	}
	// Agent not started → StateStopped → counted as off
	if c.off != 1 {
		t.Errorf("expected off=1 (not started), got %d", c.off)
	}
}

func TestCounts_L2WithL3Children(t *testing.T) {
	log, _ := logger.System("/tmp", logger.WithConsole(false), logger.WithFile(false))
	reg := agent.NewRegistry(log)

	l2 := agent.NewAgent(agent.Definition{ID: "dev", Name: "DevLead", ModelID: "ds-v4-pro"}, &fakeLLMForTUI{}, log)
	reg.Register(l2)

	sv := supervisorWithChildren(l2, reg, log,
		agent.AgentTemplate{ID: "coder", Name: "Coder"},
	)

	supervisors := []*agent.Supervisor{sv}
	s := newSidebar(reg, func() []*agent.Supervisor { return supervisors }, nil, nil, "")
	c := s.counts()
	if c.a2 != 1 {
		t.Errorf("expected a2=1, got %d", c.a2)
	}
	if c.a3 != 1 {
		t.Errorf("expected a3=1, got %d", c.a3)
	}
	// Agents not started → StateStopped → counted as off
	if c.off != 2 {
		t.Errorf("expected off=2 (a2+a3 not started), got %d", c.off)
	}
}

func TestCounts_NilSupervisor(t *testing.T) {
	s := newSidebar(nil, func() []*agent.Supervisor { return []*agent.Supervisor{nil} }, nil, nil, "")
	c := s.counts()
	if c.a2 != 0 {
		t.Errorf("nil supervisor should not count as a2, got %d", c.a2)
	}
}

func TestCounts_NilSupervisorAgent(t *testing.T) {
	sv := agent.NewSupervisor(nil, nil, nil)
	svs := []*agent.Supervisor{sv}
	s := newSidebar(nil, func() []*agent.Supervisor { return svs }, nil, nil, "")
	c := s.counts()
	if c.a2 != 0 {
		t.Errorf("supervisor with nil agent should not count as a2, got %d", c.a2)
	}
}

// ─── countState ─────────────────────────────────────────────────────────────

func TestCountState_AllStates(t *testing.T) {
	tests := []struct {
		state    agent.State
		wantRun  int
		wantIdle int
		wantOff  int
		wantStop int
	}{
		{agent.StateProcessing, 1, 0, 0, 0},
		{agent.StateIdle, 0, 1, 0, 0},
		{agent.StateStopping, 0, 0, 0, 1},
		{agent.StateStopped, 0, 0, 1, 0},
	}
	for _, tt := range tests {
		t.Run(tt.state.String(), func(t *testing.T) {
			c := agentCounts{}
			countState(&c, tt.state)
			if c.run != tt.wantRun {
				t.Errorf("run = %d, want %d", c.run, tt.wantRun)
			}
			if c.idle != tt.wantIdle {
				t.Errorf("idle = %d, want %d", c.idle, tt.wantIdle)
			}
			if c.off != tt.wantOff {
				t.Errorf("off = %d, want %d", c.off, tt.wantOff)
			}
			if c.stop != tt.wantStop {
				t.Errorf("stop = %d, want %d", c.stop, tt.wantStop)
			}
		})
	}
}

func TestCountState_UnknownFallsToOff(t *testing.T) {
	c := agentCounts{}
	countState(&c, agent.State(99))
	if c.off != 1 {
		t.Errorf("unknown state should count as off, got %d", c.off)
	}
}

// ─── AgentSummary ───────────────────────────────────────────────────────────

func TestAgentSummary_NoAgents(t *testing.T) {
	s := newSidebar(nil, nil, nil, nil, "")
	got := s.AgentSummary(40)
	if !strings.Contains(got, "Teams:0") {
		t.Error("AgentSummary should show Teams:0 for nil registry")
	}
}

func TestAgentSummary_WithAgents(t *testing.T) {
	log, _ := logger.System("/tmp", logger.WithConsole(false), logger.WithFile(false))
	reg := agent.NewRegistry(log)
	a := agent.NewAgent(agent.Definition{ID: "s1", Name: "Session", ModelID: "m1"}, &fakeLLMForTUI{}, log)
	reg.Register(a)

	s := newSidebar(reg, nil, nil, nil, "")
	got := s.AgentSummary(60)
	if !strings.Contains(got, "Agents:1") {
		t.Error("AgentSummary should show Agents:1")
	}
}

// ─── AgentRail ──────────────────────────────────────────────────────────────

func TestAgentRail_NoAgents(t *testing.T) {
	s := newSidebar(nil, nil, nil, nil, "")
	got := s.AgentRail(40, 10)
	if !strings.Contains(got, "TEAM") {
		t.Error("AgentRail should contain TEAM header")
	}
}

// ─── AgentInspector ─────────────────────────────────────────────────────────

func TestAgentInspector_ShowAgents(t *testing.T) {
	log, _ := logger.System("/tmp", logger.WithConsole(false), logger.WithFile(false))
	reg := agent.NewRegistry(log)
	a := agent.NewAgent(agent.Definition{ID: "s1", Name: "Session", ModelID: "m1"}, &fakeLLMForTUI{}, log)
	reg.Register(a)

	s := newSidebar(reg, nil, nil, nil, "")
	m := newTestModel()
	m.sidebar = s

	got := s.AgentInspector(40, 20, m, true)
	if !strings.Contains(got, "AGENTS") {
		t.Error("AgentInspector with showAgents should contain AGENTS section")
	}
	if !strings.Contains(got, "RUNTIME") {
		t.Error("AgentInspector should contain RUNTIME section")
	}
	if !strings.Contains(got, "ready") {
		t.Error("AgentInspector should show phase=ready when idle")
	}
}

func TestAgentInspector_HideAgents(t *testing.T) {
	s := newSidebar(nil, nil, nil, nil, "")
	m := newTestModel()
	m.sidebar = s

	got := s.AgentInspector(40, 20, m, false)
	if strings.Contains(got, "AGENTS") {
		t.Error("AgentInspector with showAgents=false should not contain AGENTS section")
	}
	if !strings.Contains(got, "RUNTIME") {
		t.Error("AgentInspector should always contain RUNTIME section")
	}
}

func TestAgentInspector_GeneratingPhase(t *testing.T) {
	s := newSidebar(nil, nil, nil, nil, "")
	m := newTestModel()
	m.sidebar = s
	m.isGenerating = true
	m.genPhase = phaseGenerating

	got := s.AgentInspector(40, 20, m, true)
	if !strings.Contains(got, "generating") {
		t.Error("AgentInspector should show generating phase")
	}
}

func TestAgentInspector_WithTokens(t *testing.T) {
	s := newSidebar(nil, nil, nil, nil, "")
	m := newTestModel()
	m.sidebar = s
	m.promptTokens = 1000
	m.outputTokens = 500

	got := s.AgentInspector(40, 20, m, true)
	if !strings.Contains(got, "tokens") {
		t.Error("AgentInspector should show tokens section when tokens > 0")
	}
}

// ─── renderAgentTreeContent ─────────────────────────────────────────────────

func TestRenderAgentTreeContent_L1Only(t *testing.T) {
	log, _ := logger.System("/tmp", logger.WithConsole(false), logger.WithFile(false))
	reg := agent.NewRegistry(log)
	a := agent.NewAgent(agent.Definition{ID: "s1", Name: "Main", ModelID: "ds-v4"}, &fakeLLMForTUI{}, log)
	reg.Register(a)

	s := newSidebar(reg, nil, nil, nil, "")
	got := s.renderAgentTreeContent(40, 20, true)
	if !strings.Contains(got, "A1 Session") {
		t.Error("should show A1 Session section header")
	}
	if !strings.Contains(got, "Main") {
		t.Error("should show L1 agent name")
	}
}

func TestRenderAgentTreeContent_L2WithChildren(t *testing.T) {
	log, _ := logger.System("/tmp", logger.WithConsole(false), logger.WithFile(false))
	reg := agent.NewRegistry(log)

	l2 := agent.NewAgent(agent.Definition{ID: "dev", Name: "DevLead"}, &fakeLLMForTUI{}, log)
	reg.Register(l2)

	sv := supervisorWithChildren(l2, reg, log,
		agent.AgentTemplate{ID: "coder", Name: "Coder"},
	)
	supervisors := []*agent.Supervisor{sv}
	s := newSidebar(reg, func() []*agent.Supervisor { return supervisors }, nil, nil, "")

	got := s.renderAgentTreeContent(40, 20, true)
	if !strings.Contains(got, "Teams") {
		t.Error("should show Teams section header")
	}
	if !strings.Contains(got, "DevLead") {
		t.Error("should show A2 agent name")
	}
	if !strings.Contains(got, "Coder") {
		t.Error("should show A3 child name")
	}
}

func TestRenderAgentTreeContent_NoL1Agents(t *testing.T) {
	log, _ := logger.System("/tmp", logger.WithConsole(false), logger.WithFile(false))
	reg := agent.NewRegistry(log)
	l2 := agent.NewAgent(agent.Definition{ID: "dev", Name: "DevLead"}, &fakeLLMForTUI{}, log)
	reg.Register(l2)
	sv := agent.NewSupervisor(l2, nil, nil)

	supervisors := []*agent.Supervisor{sv}
	s := newSidebar(reg, func() []*agent.Supervisor { return supervisors }, nil, nil, "MyAssistant")
	got := s.renderAgentTreeContent(40, 20, true)
	if !strings.Contains(got, "MyAssistant") {
		t.Error("L1 section with no L1 agents should show assistant name")
	}
}

func TestRenderAgentTreeContent_NoL2Children(t *testing.T) {
	log, _ := logger.System("/tmp", logger.WithConsole(false), logger.WithFile(false))
	reg := agent.NewRegistry(log)
	l2 := agent.NewAgent(agent.Definition{ID: "dev", Name: "DevLead"}, &fakeLLMForTUI{}, log)
	reg.Register(l2)
	sv := agent.NewSupervisor(l2, nil, nil)

	supervisors := []*agent.Supervisor{sv}
	s := newSidebar(reg, func() []*agent.Supervisor { return supervisors }, nil, nil, "")
	got := s.renderAgentTreeContent(40, 20, true)
	if !strings.Contains(got, "Teams") {
		t.Error("should show Teams section")
	}
}

// ─── agentTreeLine ──────────────────────────────────────────────────────────

func TestAgentTreeLine_BasicAgent(t *testing.T) {
	log, _ := logger.System("/tmp", logger.WithConsole(false), logger.WithFile(false))
	a := agent.NewAgent(agent.Definition{ID: "a1", Name: "TestAgent", ModelID: "ds-v4"}, &fakeLLMForTUI{}, log)

	got := agentTreeLine(a, "  ", 40)
	if !strings.Contains(got, "TestAgent") {
		t.Error("agentTreeLine should contain agent name")
	}
	// Agent not started → StateStopped → label "OFF"
	if !strings.Contains(got, "OFF") {
		t.Errorf("agentTreeLine should contain state label for stopped agent, got %q", got)
	}
}

func TestAgentTreeLine_FallbackToID(t *testing.T) {
	log, _ := logger.System("/tmp", logger.WithConsole(false), logger.WithFile(false))
	a := agent.NewAgent(agent.Definition{ID: "agent-fallback", Name: ""}, &fakeLLMForTUI{}, log)

	got := agentTreeLine(a, "  ", 40)
	if !strings.Contains(got, "agent-fallback") {
		t.Error("agentTreeLine should fall back to ID when Name is empty")
	}
}

func TestAgentTreeLine_LongName(t *testing.T) {
	log, _ := logger.System("/tmp", logger.WithConsole(false), logger.WithFile(false))
	a := agent.NewAgent(agent.Definition{
		ID:      "a1",
		Name:    "ThisIsAVeryLongAgentNameThatShouldBeTruncated",
		ModelID: "ds-v4-pro-max-ultra",
	}, &fakeLLMForTUI{}, log)

	got := agentTreeLine(a, "  ", 40)
	if strings.Contains(got, "ThisIsAVeryLongAgentNameThatShouldBeTruncated") {
		t.Error("long agent name should be truncated")
	}
}

// ─── fitLines ───────────────────────────────────────────────────────────────

func TestFitLines_ZeroMaxLines(t *testing.T) {
	got := fitLines("hello", 0)
	if got != "" {
		t.Errorf("fitLines with maxLines=0 should return empty, got %q", got)
	}
}

func TestFitLines_Truncation(t *testing.T) {
	input := "line1\nline2\nline3\nline4\nline5"
	got := fitLines(input, 3)
	lines := strings.Split(got, "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d", len(lines))
	}
}

func TestFitLines_Padding(t *testing.T) {
	input := "line1\nline2"
	got := fitLines(input, 5)
	lines := strings.Split(got, "\n")
	if len(lines) != 5 {
		t.Errorf("expected 5 lines (padded), got %d", len(lines))
	}
}

// ─── Static template tree rendering ────────────────────────────────────────

func TestRenderAgentTreeContent_StaticTemplatesNoRuntime(t *testing.T) {
	// When no agents are running, the tree should still show template names from static config.
	templates := []agent.AgentTemplate{
		{ID: "lead", Name: "TeamLead", Group: "dev", IsLeader: true},
		{ID: "coder", Name: "Coder", Group: "dev"},
		{ID: "tester", Name: "Tester", Group: "dev"},
	}
	s := newSidebar(nil, nil, templates, map[string]prompt.GroupFile{"dev": {}}, "MyBot")
	got := s.renderAgentTreeContent(40, 20, true)
	if !strings.Contains(got, "MyBot") {
		t.Error("should show assistant name for A1")
	}
	if !strings.Contains(got, "TeamLead") {
		t.Error("should show leader template name")
	}
	if !strings.Contains(got, "Coder") {
		t.Error("should show worker template name")
	}
	if !strings.Contains(got, "Tester") {
		t.Error("should show worker template name")
	}
}

func TestRenderAgentTreeContent_StaticTemplatesWithRuntime(t *testing.T) {
	// When agents are running, runtime state should be overlaid on template tree.
	log, _ := logger.System("/tmp", logger.WithConsole(false), logger.WithFile(false))
	reg := agent.NewRegistry(log)

	templates := []agent.AgentTemplate{
		{ID: "lead", Name: "TeamLead", Group: "dev", IsLeader: true},
		{ID: "coder", Name: "Coder", Group: "dev"},
	}

	l2 := agent.NewAgent(agent.Definition{ID: "lead", Name: "TeamLead"}, &fakeLLMForTUI{}, log)
	reg.Register(l2)

	sv := supervisorWithChildren(l2, reg, log,
		agent.AgentTemplate{ID: "coder", Name: "Coder"},
	)
	supervisors := []*agent.Supervisor{sv}

	s := newSidebar(reg, func() []*agent.Supervisor { return supervisors }, templates, map[string]prompt.GroupFile{"dev": {}}, "")
	got := s.renderAgentTreeContent(40, 20, true)
	if !strings.Contains(got, "TeamLead") {
		t.Error("should show leader name with runtime state")
	}
	if !strings.Contains(got, "Coder") {
		t.Error("should show worker name with runtime state")
	}
}

// ─── Assistant name display ────────────────────────────────────────────────

func TestRenderAgentTreeContent_AssistantName(t *testing.T) {
	// A1 section should display the assistant name when no L1 agents are running.
	s := newSidebar(nil, nil, nil, nil, "SuperBot")
	got := s.renderAgentTreeContent(40, 20, true)
	if !strings.Contains(got, "SuperBot") {
		t.Error("A1 section should show assistant name 'SuperBot'")
	}
}

func TestRenderAgentTreeContent_DefaultAssistantName(t *testing.T) {
	// When no assistant name is provided, should show "Assistant".
	s := newSidebar(nil, nil, nil, nil, "")
	got := s.renderAgentTreeContent(40, 20, true)
	if !strings.Contains(got, "Assistant") {
		t.Error("A1 section should show default name 'Assistant'")
	}
}
