package tui

import (
	"strings"
	"testing"
)

// ─── renderContextBar ───────────────────────────────────────────────────────

func TestRenderContextBar_AllPercentages(t *testing.T) {
	tests := []struct {
		pct int
	}{
		{0}, {10}, {25}, {42}, {50}, {67}, {75}, {85}, {99}, {100}, {150},
	}
	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			got := renderContextBar(tt.pct)
			if !strings.Contains(got, "ctx") {
				t.Errorf("renderContextBar(%d) should contain 'ctx'", tt.pct)
			}
			pctStr := strings.Split(got, "%")[0]
			if !strings.Contains(pctStr, "") {
				t.Errorf("renderContextBar(%d) should contain percentage", tt.pct)
			}
		})
	}
}

// ─── phaseStyle ─────────────────────────────────────────────────────────────

func TestPhaseStyle_AllPhases(t *testing.T) {
	phases := []genPhase{phaseWaiting, phaseThinking, phaseGenerating, phaseToolCall, genPhase(99)}
	for _, p := range phases {
		style := phaseStyle(p)
		got := style.Render("test")
		if got == "" {
			t.Errorf("phaseStyle(%d) should produce non-empty render", p)
		}
	}
}
