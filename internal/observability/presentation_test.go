package observability

import (
	"fmt"
	"testing"
	"time"
)

func TestPromptChangesRespectAgentStreamAndMissingBoundaries(t *testing.T) {
	s := ForWorkDir(t.TempDir())
	seq := []struct{ agent, dispatch, text, representation string }{
		{"a", "", "A", "messages"}, {"a", "", "A", "messages"}, {"a", "", "B", "messages"}, {"a", "", "B", "messages"}, {"a", "", "A", "messages"},
		{"b", "", "A", "messages"}, {"a", "", "", ""}, {"a", "", "A", "messages"}, {"a", "child", "A", "messages"}, {"a", "", "A", "instructions"},
	}
	now := time.Now()
	for i, v := range seq {
		var p *Prompt
		if v.text != "" {
			p = &Prompt{RecordingStatus: "complete", Representation: v.representation, Messages: []Instruction{{Role: "system", Content: v.text}}}
			if v.representation == "instructions" {
				p.Messages = nil
				p.Instructions = v.text
			}
		}
		if err := s.SaveCall(Call{CallID: fmt.Sprint(i), SessionID: "l1", RequestID: "r", AgentID: v.agent, DispatchID: v.dispatch, StartedAt: now.Add(time.Duration(i) * time.Second)}, p); err != nil {
			t.Fatal(err)
		}
	}
	r, err := s.Request("l1", "r")
	if err != nil {
		t.Fatal(err)
	}
	Summarize(&r)
	for i, c := range r.Calls {
		want := ""
		if i == 1 {
			want = "0"
		}
		if i == 3 {
			want = "2"
		}
		if c.PromptSourceCallID != want {
			t.Errorf("call%d source=%q want%q", i, c.PromptSourceCallID, want)
		}
	}
}
func TestSummaryTimingAndCountProvenance(t *testing.T) {
	start := time.Now()
	end := start.Add(time.Second)
	longEnd := start.Add(4 * time.Second)
	for _, tc := range []struct {
		name               string
		r                  Request
		source             string
		duration           int64
		complete, attempts bool
	}{
		{"unknown", Request{}, "unknown", 0, false, false},
		{"zero recorded", Request{StartedAt: start, FinishedAt: &start}, "request", 0, false, false},
		{"parallel", Request{Calls: []Call{{StartedAt: start, FinishedAt: &end, AttemptsReported: true}, {StartedAt: start, FinishedAt: &longEnd, AttemptsReported: true}}}, "call_span", 4000, true, true},
		{"missing", Request{CallCount: 3, Calls: []Call{{StartedAt: start, FinishedAt: &end, AttemptsReported: true}, {StartedAt: start}}}, "call_span", 1000, false, false},
		{"request wins", Request{StartedAt: start, FinishedAt: &longEnd, DurationMS: 4000, Calls: []Call{{StartedAt: start, FinishedAt: &end}}}, "request", 4000, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			Summarize(&tc.r)
			if tc.r.DurationSource != tc.source || tc.r.DurationMS != tc.duration || tc.r.TimingComplete != tc.complete || tc.r.AttemptsReported != tc.attempts {
				t.Fatalf("%+v", tc.r)
			}
		})
	}
}

func TestPromptContinuityUsesInstructionsStatusNotOutputStatus(t *testing.T) {
	s := ForWorkDir(t.TempDir())
	now := time.Now()
	for i := 0; i < 4; i++ {
		p := &Prompt{RecordingStatus: "complete", Representation: "instructions", Instructions: "same actual instructions"}
		if i == 2 {
			p.RecordingStatus = "partial"
		}
		c := Call{CallID: fmt.Sprint(i), SessionID: "l1", RequestID: "r", AgentID: "agent", StartedAt: now.Add(time.Duration(i) * time.Second), RecordingStatus: "partial"}
		if err := s.SaveCall(c, p); err != nil {
			t.Fatal(err)
		}
		// Final output updates preserve the independent completeness of instructions.
		if err := s.SaveCall(c, nil); err != nil {
			t.Fatal(err)
		}
	}
	r, err := s.Request("l1", "r")
	if err != nil {
		t.Fatal(err)
	}
	Summarize(&r)
	for i, c := range r.Calls {
		want := ""
		if i == 1 {
			want = "0"
		}
		if c.PromptSourceCallID != want {
			t.Errorf("call%d source=%q want%q", i, c.PromptSourceCallID, want)
		}
	}
}

func TestSummaryRetainsExpiredEssentialCounts(t *testing.T) {
	r := Request{RecordingStatus: "expired", CallCount: 5, AttemptCount: 7}
	Summarize(&r)
	if r.CallCount != 5 || r.AttemptCount != 7 || r.AttemptsReported {
		t.Fatalf("expired summaries lost or coverage invented: %+v", r)
	}
}

func TestSummaryRetainsAttemptsWithPartialCallCoverage(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		count, retained, want int
		reported              bool
	}{
		{"retained partial", 5, 7, 7, false},
		{"observed lower bound", 5, 1, 2, false},
		{"complete recalculates", 2, 7, 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := Request{RecordingStatus: "expired", CallCount: tc.count, AttemptCount: tc.retained, Calls: []Call{{AttemptCount: 1, AttemptsReported: true}, {AttemptCount: 1, AttemptsReported: true}}}
			Summarize(&r)
			if r.CallCount != tc.count || r.AttemptCount != tc.want || r.AttemptsReported != tc.reported {
				t.Fatalf("retained total or coverage changed: %+v", r)
			}
			Summarize(&r)
			if r.AttemptCount != tc.want || r.AttemptsReported != tc.reported {
				t.Fatalf("summary is not stable: %+v", r)
			}
		})
	}
}

func TestSummaryUsesIndependentTimestampBounds(t *testing.T) {
	start := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	later := start.Add(10 * time.Second)
	end := start.Add(20 * time.Second)
	invalidEnd := start.Add(-time.Second)
	zero := time.Time{}
	for _, tc := range []struct {
		name     string
		calls    []Call
		source   string
		duration int64
	}{
		{"missing finish", []Call{{StartedAt: start}, {StartedAt: later, FinishedAt: &end}}, "call_span", 20000},
		{"missing start", []Call{{StartedAt: start, FinishedAt: &later}, {FinishedAt: &end}}, "call_span", 20000},
		{"invalid finish retains start", []Call{{StartedAt: start, FinishedAt: &invalidEnd}, {StartedAt: later, FinishedAt: &end}}, "call_span", 20000},
		{"zero finish retains start", []Call{{StartedAt: start, FinishedAt: &zero}, {StartedAt: later, FinishedAt: &end}}, "call_span", 20000},
		{"no valid finish", []Call{{StartedAt: start}}, "unknown", 0},
		{"no valid start", []Call{{FinishedAt: &end}}, "unknown", 0},
		{"inverted available bounds", []Call{{FinishedAt: &start}, {StartedAt: end}}, "unknown", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := Request{Calls: tc.calls}
			Summarize(&r)
			if r.DurationSource != tc.source || r.DurationMS != tc.duration || r.TimingComplete || !r.StartedAt.IsZero() || r.FinishedAt != nil || r.Status != "" {
				t.Fatalf("incorrect partial timestamp span or fabricated request metadata: %+v", r)
			}
		})
	}
}
