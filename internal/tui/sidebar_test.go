package tui

import (
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/agent"
)

// ─── sidebar basics ─────────────────────────────────────────────────────────

func TestSidebar_DefaultVisible(t *testing.T) {
	s := newSidebar(nil, nil)
	if !s.visible {
		t.Error("sidebar should be visible by default")
	}
	if s.Width() != 26 {
		t.Errorf("visible sidebar width = %d, want 38", s.Width())
	}
}

func TestSidebar_Toggle(t *testing.T) {
	s := newSidebar(nil, nil)
	s.Toggle()
	if s.visible {
		t.Error("sidebar should be hidden after one toggle")
	}
	if s.Width() != 0 {
		t.Error("hidden sidebar should have width 0")
	}
	s.Toggle()
	if !s.visible {
		t.Error("sidebar should be visible after two toggles")
	}
	if s.Width() != 26 {
		t.Errorf("re-shown sidebar width = %d, want 38", s.Width())
	}
}

// ─── stateIcon all states ───────────────────────────────────────────────────

func TestStateIcon_AllStates(t *testing.T) {
	tests := []struct {
		state agent.State
		icon  string
	}{
		{agent.StateIdle, "◉"},
		{agent.StateProcessing, "◌"},
		{agent.StateStopping, "⊘"},
		{agent.StateStopped, "○"},
	}
	for _, tt := range tests {
		t.Run(tt.state.String(), func(t *testing.T) {
			got := stateIcon(tt.state)
			if got != tt.icon {
				t.Errorf("stateIcon(%s) = %q, want %q", tt.state, got, tt.icon)
			}
		})
	}
}

func TestStateIcon_UnknownState(t *testing.T) {
	got := stateIcon(agent.State(99))
	if got != "○" {
		t.Errorf("stateIcon(unknown) = %q, want ○", got)
	}
}

// ─── stateLabel ─────────────────────────────────────────────────────────────

func TestStateLabel_AllStates(t *testing.T) {
	tests := []struct {
		state agent.State
		label string
	}{
		{agent.StateIdle, "IDLE"},
		{agent.StateProcessing, "RUN"},
		{agent.StateStopping, "STOP"},
		{agent.StateStopped, "OFF"},
	}
	for _, tt := range tests {
		t.Run(tt.state.String(), func(t *testing.T) {
			got := stateLabel(tt.state)
			if got != tt.label {
				t.Errorf("stateLabel(%s) = %q, want %q", tt.state, got, tt.label)
			}
		})
	}
}

func TestStateLabel_UnknownState(t *testing.T) {
	got := stateLabel(agent.State(99))
	if got != "UNK" {
		t.Errorf("stateLabel(unknown) = %q, want UNK", got)
	}
}
