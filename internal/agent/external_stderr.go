package agent

import (
	"io"
	"strings"
	"sync"
)

// agentStderrTailBytes bounds the stderr tail captured for inclusion in
// error messages when an agent CLI exits before emitting a structured
// error.
const agentStderrTailBytes = 2048

// stderrTail forwards writes to an inner writer (typically the logger)
// while also retaining a bounded tail of the bytes written.
type stderrTail struct {
	inner io.Writer
	max   int

	mu  sync.Mutex
	buf []byte
}

func newStderrTail(inner io.Writer, max int) *stderrTail {
	if max <= 0 {
		max = agentStderrTailBytes
	}
	return &stderrTail{inner: inner, max: max}
}

func (s *stderrTail) Write(p []byte) (int, error) {
	if _, err := s.inner.Write(p); err != nil {
		return 0, err
	}
	s.mu.Lock()
	s.buf = append(s.buf, p...)
	if len(s.buf) > s.max {
		s.buf = s.buf[len(s.buf)-s.max:]
	}
	s.mu.Unlock()
	return len(p), nil
}

// Tail returns the captured stderr with leading/trailing whitespace trimmed.
func (s *stderrTail) Tail() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.TrimSpace(string(s.buf))
}

// withAgentStderr appends a stderr tail hint to an error message when non-empty.
func withAgentStderr(msg, label, tail string) string {
	if tail == "" {
		return msg
	}
	return msg + "; " + label + " stderr: " + tail
}
