package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/xiaobaitu/soloqueue/internal/agent"
)

// ─── Agent sidebar data model ────────────────────────────────────────────────

type sidebar struct {
	visible       bool
	registry      *agent.Registry
	supervisorsFn func() []*agent.Supervisor
	spinner       spinner
}

// agentTickMsg is sent periodically to refresh agent state snapshots.
type agentTickMsg time.Time

func agentTickCmd(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(t time.Time) tea.Msg { return agentTickMsg(t) })
}

// agentTickInterval returns the polling interval based on generation state.
func agentTickInterval(isGenerating bool) time.Duration {
	if isGenerating {
		return 500 * time.Millisecond
	}
	return 2 * time.Second
}

func newSidebar(registry *agent.Registry, supervisorsFn func() []*agent.Supervisor) sidebar {
	return sidebar{
		visible:       true, // default visible; Ctrl+A or /agents collapses it
		registry:      registry,
		supervisorsFn: supervisorsFn,
		spinner:       newSpinner(),
	}
}

// getSupervisors returns the current supervisor list via the live function, or nil.
func (s sidebar) getSupervisors() []*agent.Supervisor {
	if s.supervisorsFn == nil {
		return nil
	}
	return s.supervisorsFn()
}

// Toggle switches sidebar visibility.
func (s *sidebar) Toggle() {
	s.visible = !s.visible
}

// Width returns the sidebar width (0 when hidden).
func (s sidebar) Width() int {
	if s.visible {
		return 26
	}
	return 0
}

// stateLabel returns the short display label for an agent state.
func stateLabel(s agent.State) string {
	switch s {
	case agent.StateIdle:
		return "IDLE"
	case agent.StateProcessing:
		return "RUN"
	case agent.StateStopping:
		return "STOP"
	case agent.StateStopped:
		return "OFF"
	default:
		return "UNK"
	}
}

