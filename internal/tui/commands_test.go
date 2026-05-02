package tui

import (
	"strings"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/skill"
)

// ─── handleBuiltin ──────────────────────────────────────────────────────────

func TestHandleBuiltin_Quit(t *testing.T) {
	m := newTestModel()
	quit, cmd := m.handleBuiltin("/quit")
	if !quit || cmd != nil {
		t.Error("/quit should return quit=true, cmd=nil")
	}
}

func TestHandleBuiltin_Exit(t *testing.T) {
	m := newTestModel()
	quit, _ := m.handleBuiltin("/exit")
	if !quit {
		t.Error("/exit should return quit=true")
	}
}

func TestHandleBuiltin_Q(t *testing.T) {
	m := newTestModel()
	quit, _ := m.handleBuiltin("/q")
	if !quit {
		t.Error("/q should return quit=true")
	}
}

func TestHandleBuiltin_Help(t *testing.T) {
	m := newTestModel()
	quit, cmd := m.handleBuiltin("/help")
	if quit || cmd != nil {
		t.Error("/help should not quit, should return nil cmd")
	}
	// Should have user message + agent response
	if len(m.messages) < 2 || !strings.Contains(m.messages[1].content, "Commands:") {
		t.Error("/help should add user message and agent response with commands list")
	}
}

func TestHandleBuiltin_HelpWithSkills(t *testing.T) {
	m := newTestModel()
	sr := skill.NewSkillRegistry()
	sr.Register(&skill.Skill{ID: "deploy", Description: "Deploy app", UserInvocable: true})
	sr.Register(&skill.Skill{ID: "internal", Description: "Internal", UserInvocable: false, DisableModelInvocation: true})
	m.cfg.Skills = sr
	m.handleBuiltin("/help")
	if len(m.messages) < 2 {
		t.Fatal("/help should add messages")
	}
	if !strings.Contains(m.messages[1].content, "deploy") {
		t.Error("/help should list user-invocable skills")
	}
}

func TestHandleBuiltin_Clear(t *testing.T) {
	m := newTestModel()
	m.messages = []message{{role: "user", content: "old"}}
	quit, _ := m.handleBuiltin("/clear")
	if quit {
		t.Error("/clear should not quit")
	}
	if len(m.messages) != 2 || m.messages[1].content != "◆  context cleared" {
		t.Error("/clear should reset messages to user + clear notice")
	}
}

func TestHandleBuiltin_ClearDuringGeneration(t *testing.T) {
	m := newTestModel()
	m.isGenerating = true
	m.current = &streamState{toolExecMap: make(map[string]*toolExecInfo)}
	m.handleBuiltin("/clear")
	if m.isGenerating || m.current != nil {
		t.Error("/clear during generation should reset state")
	}
}

func TestHandleBuiltin_Version(t *testing.T) {
	m := newTestModel()
	quit, _ := m.handleBuiltin("/version")
	if quit {
		t.Error("/version should not quit")
	}
	if len(m.messages) < 2 || !strings.Contains(m.messages[1].content, "v0.1.0") {
		t.Error("/version should show version")
	}
}

func TestHandleBuiltin_Status(t *testing.T) {
	log, _ := logger.System("/tmp", logger.WithConsole(false), logger.WithFile(false))
	reg := agent.NewRegistry(log)
	a := agent.NewAgent(agent.Definition{ID: "a1", Name: "test"}, &fakeLLMForTUI{}, log)
	reg.Register(a)
	m := newTestModel()
	m.cfg.Registry = reg
	quit, _ := m.handleBuiltin("/status")
	if quit {
		t.Error("/status should not quit")
	}
	if len(m.messages) < 2 || !strings.Contains(m.messages[1].content, "Agent Status") {
		t.Error("/status should show agent status")
	}
}

func TestHandleBuiltin_Agents(t *testing.T) {
	m := newTestModel()
	if !m.showAgents {
		t.Error("agents pane should start visible")
	}
	quit, _ := m.handleBuiltin("/agents")
	if quit || m.showAgents {
		t.Error("/agents should toggle agents pane hidden without quitting")
	}
}

func TestHandleBuiltin_UnknownCommand(t *testing.T) {
	m := newTestModel()
	quit, _ := m.handleBuiltin("/foobar")
	if quit {
		t.Error("unknown command should not quit")
	}
	if len(m.messages) < 2 || !strings.Contains(m.messages[1].content, "Unknown command") {
		t.Error("unknown command should show error")
	}
}

func TestHandleBuiltin_SkillCommand(t *testing.T) {
	m := newTestModel()
	sr := skill.NewSkillRegistry()
	sr.Register(&skill.Skill{ID: "deploy", Description: "Deploy app", UserInvocable: true})
	m.cfg.Skills = sr
	quit, cmd := m.handleBuiltin("/deploy staging")
	if quit {
		t.Error("/deploy should not quit")
	}
	if cmd == nil || !m.isGenerating {
		t.Error("/deploy should start generation")
	}
}

func TestHandleBuiltin_NonCommandInput(t *testing.T) {
	m := newTestModel()
	quit, cmd := m.handleBuiltin("hello world")
	if quit || cmd != nil {
		t.Error("non-command input should not quit, should return nil cmd")
	}
}

// ─── buildSkillPrompt ───────────────────────────────────────────────────────

func TestBuildSkillPrompt_WithArgs(t *testing.T) {
	s := &skill.Skill{ID: "deploy"}
	got := buildSkillPrompt(s, "staging")
	if !strings.Contains(got, "deploy") || !strings.Contains(got, "staging") {
		t.Errorf("prompt should mention skill ID and args, got %q", got)
	}
}

func TestBuildSkillPrompt_NoArgs(t *testing.T) {
	s := &skill.Skill{ID: "commit"}
	got := buildSkillPrompt(s, "")
	if !strings.Contains(got, "commit") {
		t.Errorf("prompt should mention skill ID, got %q", got)
	}
	if strings.Contains(got, "arguments") {
		t.Error("prompt without args should not mention arguments")
	}
}

// ─── startStreamFromInput ──────────────────────────────────────────────────

func TestStartStreamFromInput(t *testing.T) {
	m := newTestModel()
	cmd := m.startStreamFromInput("/skill", "test prompt")
	if cmd == nil {
		t.Error("startStreamFromInput should return a tea.Cmd")
	}
	if !m.isGenerating || m.genPhase != phaseWaiting || m.current == nil {
		t.Error("startStreamFromInput should set up generation state")
	}
	if len(m.messages) == 0 {
		t.Error("startStreamFromInput should add an agent message placeholder")
	}
}
