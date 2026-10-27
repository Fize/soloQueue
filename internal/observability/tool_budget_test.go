package observability

import (
	"strings"
	"testing"
	"time"
)

func TestToolDetailsShareLimitsAndGenerations(t *testing.T) {
	s := ForWorkDir(t.TempDir())
	s.SetPolicy(func() Policy { return Policy{MaxFileBytes: 2048, MaxTurnBytes: 4096, MaxBytes: 8192} })
	if err := s.Begin("l1", "r"); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	c := Call{Kind: "tool", ToolName: "Bash", CallID: "tool", ToolCallID: "original", SessionID: "l1", RequestID: "r", Status: "success", StartedAt: now, Arguments: strings.Repeat("A", 10000), Content: strings.Repeat("B", 10000)}
	if err := s.SaveCall(c, nil); err != nil {
		t.Fatal(err)
	}
	r, err := s.Request("l1", "r")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Tools) != 1 || r.Tools[0].RecordingStatus != "partial" || r.Tools[0].Arguments != "" || r.Tools[0].Content != "" {
		t.Fatalf("unbounded tool: %+v", r.Tools)
	}
	if err = s.Finish("l1", "r", "completed"); err != nil {
		t.Fatal(err)
	}
	if err = s.Begin("l1", "r"); err != nil {
		t.Fatal(err)
	}
	r, err = s.Request("l1", "r")
	if err != nil || len(r.Tools) != 0 {
		t.Fatalf("old tools leaked into new generation: %+v %v", r, err)
	}
}
