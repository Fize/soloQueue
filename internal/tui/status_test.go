package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/llm"
)

// ─── renderStatus ─────────────────────────────────────────────────────────

func TestRenderStatus_NilRegistry(t *testing.T) {
	text := renderStatus(nil, nil)
	if !strings.Contains(text, "No agents registered") {
		t.Errorf("expected 'No agents registered', got:\n%s", text)
	}
}

func TestRenderStatus_EmptyRegistry(t *testing.T) {
	r := agent.NewRegistry(nil)
	text := renderStatus(r, nil)
	if !strings.Contains(text, "No agents registered") {
		t.Errorf("expected 'No agents registered', got:\n%s", text)
	}
}

func TestRenderStatus_L1Only(t *testing.T) {
	r := agent.NewRegistry(nil)
	a := agent.NewAgent(agent.Definition{ID: "agent-1", Name: "main", ModelID: "deepseek-chat"}, &fakeLLMForTUI{}, nil)
	_ = r.Register(a)

	text := renderStatus(r, nil)

	if !strings.Contains(text, "Agent Status") {
		t.Error("missing 'Agent Status' header")
	}
	if !strings.Contains(text, "A1 Session Agents") {
		t.Error("missing 'L1 Session Agents' section")
	}
	if !strings.Contains(text, "main") {
		t.Error("missing agent name 'main'")
	}
	if !strings.Contains(text, "deepseek-chat") {
		t.Error("missing model ID")
	}
	if !strings.Contains(text, "Total: 1 agents") {
		t.Error("missing total count")
	}
	// Should not show L2 section when no supervisors
	if strings.Contains(text, "A2 Domain Leaders") {
		t.Error("should not show L2 section with no supervisors")
	}
}

func TestRenderStatus_L2WithNoChildren(t *testing.T) {
	r := agent.NewRegistry(nil)
	l2Agent := agent.NewAgent(agent.Definition{ID: "dev", Name: "DevLead", ModelID: "deepseek-chat"}, &fakeLLMForTUI{}, nil)
	_ = r.Register(l2Agent)

	sv := agent.NewSupervisor(l2Agent, nil, nil)
	supervisors := []*agent.Supervisor{sv}

	text := renderStatus(r, func() []*agent.Supervisor { return supervisors })

	if !strings.Contains(text, "A2 Domain Leaders") {
		t.Error("missing 'A2 Domain Leaders' section")
	}
	if !strings.Contains(text, "DevLead") {
		t.Error("missing L2 agent name 'DevLead'")
	}
	if !strings.Contains(text, "A3 Workers") {
		t.Error("missing 'A3 Workers' section")
	}
	if !strings.Contains(text, "(none)") {
		t.Error("A3 section with no children should show '(none)'")
	}
	// L2 should NOT appear in L1 section
	if strings.Contains(text, "A1 Session Agents") && strings.Contains(text, "DevLead") {
		lines := strings.Split(text, "\n")
		inL1Section := false
		for _, line := range lines {
			if strings.Contains(line, "A1 Session Agents") {
				inL1Section = true
			}
			if strings.Contains(line, "A2 Domain Leaders") {
				inL1Section = false
			}
			if inL1Section && strings.Contains(line, "DevLead") {
				t.Error("L2 agent 'DevLead' should not appear in L1 section")
			}
		}
	}
}
func TestRenderStatus_L1L2Mixed(t *testing.T) {
	r := agent.NewRegistry(nil)
	l1Agent := agent.NewAgent(agent.Definition{ID: "session-1", Name: "UserSession"}, &fakeLLMForTUI{}, nil)
	l2Agent := agent.NewAgent(agent.Definition{ID: "dev", Name: "DevLead"}, &fakeLLMForTUI{}, nil)
	_ = r.Register(l1Agent)
	_ = r.Register(l2Agent)

	sv := agent.NewSupervisor(l2Agent, nil, nil)
	supervisors := []*agent.Supervisor{sv}

	text := renderStatus(r, func() []*agent.Supervisor { return supervisors })

	if !strings.Contains(text, "A1 Session Agents") {
		t.Error("missing A1 section")
	}
	if !strings.Contains(text, "A2 Domain Leaders") {
		t.Error("missing A2 section")
	}
	if !strings.Contains(text, "A3 Workers") {
		t.Error("missing A3 section")
	}
	if !strings.Contains(text, "Total: 2 agents") {
		t.Error("missing correct total count")
	}
}

func TestRenderStatus_FallbackToID(t *testing.T) {
	r := agent.NewRegistry(nil)
	// Agent with empty Name — should fall back to ID
	a := agent.NewAgent(agent.Definition{ID: "agent-12345", Name: ""}, &fakeLLMForTUI{}, nil)
	_ = r.Register(a)

	text := renderStatus(r, nil)

	if !strings.Contains(text, "agent-12345") {
		t.Error("should fall back to ID when Name is empty")
	}
}

// ─── renderAgentLine ──────────────────────────────────────────────────────

func TestRenderAgentLine_BasicOutput(t *testing.T) {
	a := agent.NewAgent(agent.Definition{
		ID:      "a1",
		Name:    "TestAgent",
		ModelID: "deepseek-chat",
	}, &fakeLLMForTUI{}, nil)

	line := renderAgentLine(a, "  ")

	if !strings.Contains(line, "TestAgent") {
		t.Error("missing agent name")
	}
	if !strings.Contains(line, "deepseek-chat") {
		t.Error("missing model ID")
	}
	if !strings.Contains(line, "stopped") {
		t.Error("new agent should show 'stopped' state")
	}
	if !strings.HasPrefix(line, "  ") {
		t.Error("missing indent prefix")
	}
	if !strings.HasSuffix(line, "\n") {
		t.Error("missing trailing newline")
	}
}

func TestRenderAgentLine_TreePrefix(t *testing.T) {
	a := agent.NewAgent(agent.Definition{ID: "c1", Name: "Child"}, &fakeLLMForTUI{}, nil)

	line1 := renderAgentLine(a, "    ├─ ")
	if !strings.HasPrefix(line1, "    ├─ ") {
		t.Errorf("tree prefix not applied: %q", line1)
	}

	line2 := renderAgentLine(a, "    └─ ")
	if !strings.HasPrefix(line2, "    └─ ") {
		t.Errorf("last-child prefix not applied: %q", line2)
	}
}

func TestRenderAgentLine_NoModelID(t *testing.T) {
	a := agent.NewAgent(agent.Definition{ID: "a1", Name: "NoModel"}, &fakeLLMForTUI{}, nil)

	line := renderAgentLine(a, "")

	// Should not have orphan separators
	if strings.Contains(line, "· ·") {
		t.Error("should not have empty separator between fields")
	}
}

// ─── stateStyle ───────────────────────────────────────────────────────────

func TestStateStyle_AllStates(t *testing.T) {
	// Verify stateStyle does not panic for all known states
	states := []agent.State{
		agent.StateIdle,
		agent.StateProcessing,
		agent.StateStopping,
		agent.StateStopped,
		agent.State(99), // unknown
	}

	for _, s := range states {
		style := stateStyle(s)
		rendered := style.Render(s.String())
		if rendered == "" {
			t.Errorf("stateStyle(%s).Render() returned empty", s)
		}
	}
}

// ─── sectionHeader ────────────────────────────────────────────────────────

func TestSectionHeader(t *testing.T) {
	h := sectionHeader("Test Section")
	if !strings.Contains(h, "Test Section") {
		t.Errorf("sectionHeader missing title: %q", h)
	}
	if !strings.Contains(h, "▸") {
		t.Errorf("sectionHeader missing marker: %q", h)
	}
	if !strings.HasSuffix(h, "\n") {
		t.Error("sectionHeader missing trailing newline")
	}
}

// ─── fakeLLMForTUI ────────────────────────────────────────────────────────

// fakeLLMForTUI 最小 LLM mock，仅满足 agent.LLMClient 接口以构造 Agent
type fakeLLMForTUI struct{}

func (f *fakeLLMForTUI) Chat(_ context.Context, _ agent.LLMRequest) (*agent.LLMResponse, error) {
	return &agent.LLMResponse{Content: "ok"}, nil
}

func (f *fakeLLMForTUI) ChatStream(_ context.Context, _ agent.LLMRequest) (<-chan llm.Event, error) {
	ch := make(chan llm.Event)
	close(ch)
	return ch, nil
}
