package observability

import (
	"sort"
	"time"
)

// Summarize must run on the complete call list, before the API slices a page.
// Call span measures observed provider activity, not the complete user turn.
func Summarize(r *Request) {
	sort.SliceStable(r.Calls, func(i, j int) bool {
		if r.Calls[i].StartedAt.Equal(r.Calls[j].StartedAt) {
			return r.Calls[i].CallID < r.Calls[j].CallID
		}
		return r.Calls[i].StartedAt.Before(r.Calls[j].StartedAt)
	})
	expected := r.CallCount
	r.CallCount = max(expected, len(r.Calls))
	retainedAttempts := r.AttemptCount
	observedAttempts := 0
	r.AttemptsReported = len(r.Calls) > 0 && len(r.Calls) >= expected
	r.MetricsOnly = len(r.Calls) > 0
	r.TimingComplete = len(r.Calls) > 0 && len(r.Calls) >= expected
	var start, end time.Time
	type previous struct{ hash, source string }
	streams := map[string]previous{}
	for i := range r.Calls {
		c := &r.Calls[i]
		r.MetricsOnly = r.MetricsOnly && c.MetricsOnly
		observedAttempts += c.AttemptCount
		r.AttemptsReported = r.AttemptsReported && c.AttemptsReported
		if c.StartedAt.IsZero() || c.FinishedAt == nil || c.FinishedAt.Before(c.StartedAt) {
			r.TimingComplete = false
		}
		// An incomplete call can still supply either observed timestamp bound.
		if !c.StartedAt.IsZero() && (start.IsZero() || c.StartedAt.Before(start)) {
			start = c.StartedAt
		}
		if c.FinishedAt != nil && !c.FinishedAt.IsZero() && (c.StartedAt.IsZero() || !c.FinishedAt.Before(c.StartedAt)) && (end.IsZero() || c.FinishedAt.After(end)) {
			end = *c.FinishedAt
		}
		c.PromptSourceCallID = ""
		key := c.SessionID + "\x00" + c.RequestID + "\x00" + c.AgentID + "\x00" + c.DispatchID
		prior := streams[key]
		// Empty identities and incomplete instructions cannot establish a stable stream.
		if c.AgentID == "" {
			clear(streams)
			continue
		}
		if !c.PromptAvailable || c.PromptHash == "" || c.PromptRecordingStatus != "complete" {
			delete(streams, key)
			continue
		}
		source := c.CallID
		if prior.hash == c.PromptHash {
			source = prior.source
			c.PromptSourceCallID = source
		}
		streams[key] = previous{c.PromptHash, source}
	}
	if len(r.Calls) > 0 && len(r.Calls) >= expected {
		r.AttemptCount = observedAttempts
	} else {
		// Surviving metrics must not erase essential counts retained at expiry.
		r.AttemptCount = max(retainedAttempts, observedAttempts)
	}
	r.DurationSource = "unknown"
	if !r.StartedAt.IsZero() && r.DurationMS >= 0 && (r.FinishedAt != nil && !r.FinishedAt.Before(r.StartedAt) || r.Status == "running" || r.Status == "queued") {
		r.DurationSource = "request"
	} else if !start.IsZero() && !end.IsZero() && !end.Before(start) {
		r.DurationMS = end.Sub(start).Milliseconds()
		r.DurationSource = "call_span"
	}
}
