package tui

// Thinking block rendering is now handled declaratively in View().
// The stream.go handleAgentEvent appends reasoning deltas to current.thoughts.
// The View() method renders thinking as fold/unfold based on showThinking state.
