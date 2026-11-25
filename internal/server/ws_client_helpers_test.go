package server

import (
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/session"
	"github.com/xiaobaitu/soloqueue/internal/transcript"
)

// readClient returns the next message the client's write goroutine would
// write: a queued message, or the newest coalesced runtime state. It returns
// nil on timeout or when the send queue is closed.
func readClient(c *Client, timeout time.Duration) []byte {
	deadline := time.After(timeout)
	for {
		select {
		case data, ok := <-c.send:
			if !ok {
				return nil
			}
			return data
		case <-c.signal:
			if state := c.takeLatestState(); state != nil {
				return state
			}
		case <-deadline:
			return nil
		}
	}
}

// transcriptMark returns the head of the session's transcript so a test only
// observes frames published after this point.
func transcriptMark(t *testing.T, sess *session.Session) uint64 {
	t.Helper()
	j := sess.Transcript()
	if j == nil {
		t.Fatal("session has no transcript journal")
	}
	return j.Head()
}

// waitSessionFrame waits for a frame of kind owned by requestID that was
// published after mark.
func waitSessionFrame(t *testing.T, sess *session.Session, mark uint64, kind, requestID string, timeout time.Duration) transcript.Frame {
	t.Helper()
	j := sess.Transcript()
	if j == nil {
		t.Fatal("session has no transcript journal")
	}
	deadline := time.Now().Add(timeout)
	for {
		wake := j.Wait()
		frames, err := j.ReadAfter(j.Epoch(), mark, 0)
		if err != nil {
			t.Fatalf("read transcript: %v", err)
		}
		for _, f := range frames {
			if f.Kind == kind && f.RequestID == requestID {
				return f
			}
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			t.Fatalf("no %s frame for %s", kind, requestID)
		}
		select {
		case <-wake:
		case <-time.After(remaining):
		}
	}
}
