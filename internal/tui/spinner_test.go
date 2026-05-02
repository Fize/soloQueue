package tui

import (
	"testing"
)

func TestSpinner_Next(t *testing.T) {
	s := newSpinner()
	// Verify it cycles through all frames
	seen := make(map[string]bool)
	for i := 0; i < len(brailleSpinnerFrames)*2; i++ {
		frame := s.Next()
		seen[frame] = true
	}
	for _, f := range brailleSpinnerFrames {
		if !seen[f] {
			t.Errorf("frame %q never seen after 2 full cycles", f)
		}
	}
}

func TestSpinner_Current(t *testing.T) {
	s := newSpinner()
	first := s.Current()
	if first != brailleSpinnerFrames[0] {
		t.Errorf("Current() = %q, want %q", first, brailleSpinnerFrames[0])
	}
}

func TestNewSpinner(t *testing.T) {
	s := newSpinner()
	if s.frame != 0 {
		t.Errorf("initial frame = %d, want 0", s.frame)
	}
	if len(s.frames) != len(brailleSpinnerFrames) {
		t.Errorf("frames count = %d, want %d", len(s.frames), len(brailleSpinnerFrames))
	}
}
