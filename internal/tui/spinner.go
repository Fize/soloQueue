package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// ─── Spinner animation ──────────────────────────────────────────────────────

// brailleSpinnerFrames are 8 braille-dot spinner frames for smooth animation.
var brailleSpinnerFrames = []string{"⣷", "⣯", "⣟", "⡿", "⢿", "⣻", "⣽", "⣾"}

type spinner struct {
	frame  int
	frames []string
}

func newSpinner() spinner {
	return spinner{
		frame:  0,
		frames: brailleSpinnerFrames,
	}
}

// Next advances the spinner and returns the current frame character.
func (s *spinner) Next() string {
	f := s.frames[s.frame]
	s.frame = (s.frame + 1) % len(s.frames)
	return f
}

// Current returns the current frame character without advancing.
func (s *spinner) Current() string {
	return s.frames[s.frame]
}

// spinnerInterval is the time between spinner frame advances during generation.
const spinnerInterval = 100 * time.Millisecond

// spinnerMsg is sent on each spinner tick to advance the animation frame.
type spinnerMsg time.Time

func spinnerCmd() tea.Cmd {
	return tea.Tick(spinnerInterval, func(t time.Time) tea.Msg { return spinnerMsg(t) })
}
