package observability

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func budgetPrompt(kib int) *Prompt {
	return &Prompt{RecordingStatus: "complete", Representation: "messages", Messages: []Instruction{{Role: "system", Content: strings.Repeat("p", kib<<10)}}}
}
func budgetCall(request, id string, kib int) Call {
	return Call{CallID: id, SessionID: "l1", RequestID: request, StartedAt: time.Now().UTC(), Status: "success", Content: strings.Repeat("b", kib<<10)}
}
func assertFinalTurnBudget(t *testing.T, s *Store, request string, maxBytes int64) {
	t.Helper()
	diskMu.Lock()
	_, turn, known := s.sharedUsage("l1", request)
	diskMu.Unlock()
	if !known || turn > maxBytes {
		t.Fatalf("final turn usage=%d, limit=%d, known=%v", turn, maxBytes, known)
	}
}
func TestFinalStateCombinedPromptAndBodyBudget(t *testing.T) {
	s := ForWorkDir(t.TempDir())
	s.SetPolicy(func() Policy { return Policy{MaxBytes: 4 << 20, MaxTurnBytes: 1 << 20, MaxFileBytes: 1 << 20} })
	if e := s.SaveCall(budgetCall("r", "call", 650), budgetPrompt(650)); e != nil {
		t.Fatal(e)
	}
	assertFinalTurnBudget(t, s, "r", 1<<20)
	r, _ := s.Request("l1", "r")
	if r.RecordingStatus != "partial" {
		t.Fatal("over-limit capture must be partial")
	}
}
func TestFinalStateExistingPromptFirstUseAndTightenedPolicy(t *testing.T) {
	for _, tighten := range []bool{false, true} {
		t.Run(map[bool]string{false: "existing", true: "tightened"}[tighten], func(t *testing.T) {
			s := ForWorkDir(t.TempDir())
			limit := int64(1 << 20)
			s.SetPolicy(func() Policy { return Policy{MaxBytes: 4 << 20, MaxTurnBytes: limit, MaxFileBytes: 1 << 20} })
			if e := s.SaveCall(budgetCall("origin", "origin", 0), budgetPrompt(650)); e != nil {
				t.Fatal(e)
			}
			c := budgetCall("target", "call", 650)
			if e := s.SaveCall(c, nil); e != nil {
				t.Fatal(e)
			}
			if tighten {
				limit = 500 << 10
			}
			if e := s.SaveCall(c, budgetPrompt(650)); e != nil {
				t.Fatal(e)
			}
			assertFinalTurnBudget(t, s, "target", limit)
			if tighten {
				p, e := s.CallPrompt("l1", "target", "call")
				if e != nil || p.RecordingStatus == "complete" {
					t.Fatalf("oversized reused prompt %#v %v", p, e)
				}
			}
		})
	}
}
func TestFinalStateSameTurnDedupAndReplacementNoDoubleCharge(t *testing.T) {
	s := ForWorkDir(t.TempDir())
	s.SetPolicy(func() Policy { return Policy{MaxBytes: 1 << 20, MaxTurnBytes: 550 << 10, MaxFileBytes: 1 << 20} })
	for _, id := range []string{"first", "second"} {
		if e := s.SaveCall(budgetCall("r", id, 100), budgetPrompt(300)); e != nil {
			t.Fatal(e)
		}
	}
	for i := 0; i < 10; i++ {
		if e := s.SaveCall(budgetCall("r", "second", 100), budgetPrompt(300)); e != nil {
			t.Fatal(e)
		}
		r, e := s.Request("l1", "r")
		if e != nil {
			t.Fatal(e)
		}
		for _, call := range r.Calls {
			if call.CallID == "second" && (len(call.Content) != 100<<10 || call.RecordingStatus != "complete") {
				t.Fatalf("replacement %d charged twice: content bytes=%d status=%s", i, len(call.Content), call.RecordingStatus)
			}
		}
	}
	r, _ := s.Request("l1", "r")
	for _, c := range r.Calls {
		if len(c.Content) != 100<<10 || !c.PromptAvailable || c.RecordingStatus != "complete" {
			t.Fatalf("same reference/replacement double charged: %#v", c)
		}
	}
	assertFinalTurnBudget(t, s, "r", 550<<10)
	files, _ := filepath.Glob(filepath.Join(s.Root(), "prompts", "*.json"))
	if len(files) != 1 {
		t.Fatal("snapshot not deduplicated", files)
	}
}
func TestFinalStateGlobalReusedPromptAndReplacementCost(t *testing.T) {
	s := ForWorkDir(t.TempDir())
	s.SetPolicy(func() Policy { return Policy{MaxBytes: 1500 << 10, MaxTurnBytes: 2 << 20, MaxFileBytes: 1 << 20} })
	if e := s.SaveCall(budgetCall("origin", "origin", 0), budgetPrompt(650)); e != nil {
		t.Fatal(e)
	}
	c := budgetCall("target", "call", 650)
	if e := s.SaveCall(c, nil); e != nil {
		t.Fatal(e)
	}
	if e := s.SaveCall(c, budgetPrompt(650)); e != nil {
		t.Fatal(e)
	}
	r, _ := s.Request("l1", "target")
	if len(r.Calls) != 1 || len(r.Calls[0].Content) != 650<<10 || !r.Calls[0].PromptAvailable || r.Calls[0].RecordingStatus != "complete" {
		t.Fatalf("global cost was not incremental %#v", r)
	}
	files, _ := filepath.Glob(filepath.Join(s.Root(), "prompts", "*.json"))
	if len(files) != 1 {
		t.Fatal(files)
	}
}

func TestReplacingSnapshotsReclaimsOrphansWithinBudget(t *testing.T) {
	s := ForWorkDir(t.TempDir())
	s.SetPolicy(func() Policy { return Policy{MaxBytes: 900 << 10, MaxTurnBytes: 900 << 10, MaxFileBytes: 700 << 10} })
	for i := 0; i < 8; i++ {
		p := budgetPrompt(300)
		p.Messages[0].Content += strings.Repeat("x", i)
		if err := s.SaveCall(budgetCall("r", "call", 20), p); err != nil {
			t.Fatal(err)
		}
	}
	r, err := s.Request("l1", "r")
	if err != nil || len(r.Calls) != 1 || !r.Calls[0].PromptAvailable || r.RecordingStatus != "complete" {
		t.Fatalf("request=%+v err=%v", r, err)
	}
	files, _ := filepath.Glob(filepath.Join(s.Root(), "prompts", "*.json"))
	if len(files) != 1 {
		t.Fatalf("orphan snapshots: %v", files)
	}
}

func TestReplacingSnapshotPreservesAnotherCallReference(t *testing.T) {
	s := ForWorkDir(t.TempDir())
	shared := budgetPrompt(50)
	for _, id := range []string{"a", "b"} {
		if err := s.SaveCall(budgetCall("r", id, 0), shared); err != nil {
			t.Fatal(err)
		}
	}
	next := budgetPrompt(60)
	if err := s.SaveCall(budgetCall("r", "a", 0), next); err != nil {
		t.Fatal(err)
	}
	p, err := s.CallPrompt("l1", "r", "b")
	if err != nil || len(p.Messages) != 1 || len(p.Messages[0].Content) != 50<<10 {
		t.Fatalf("shared reference lost: err=%v", err)
	}
}

func TestOverBudgetReplacementRetainsPreviousSnapshotAsPartial(t *testing.T) {
	s := ForWorkDir(t.TempDir())
	s.SetPolicy(func() Policy { return Policy{MaxBytes: 600 << 10, MaxTurnBytes: 900 << 10, MaxFileBytes: 700 << 10} })
	c := budgetCall("r", "call", 0)
	old := budgetPrompt(350)
	if err := s.SaveCall(c, old); err != nil {
		t.Fatal(err)
	}
	newer := budgetPrompt(360)
	if err := s.SaveCall(c, newer); err != nil {
		t.Fatal(err)
	}
	p, err := s.CallPrompt("l1", "r", "call")
	if err != nil || p.RecordingStatus != "partial" || len(p.Messages) != 1 || len(p.Messages[0].Content) != 350<<10 {
		t.Fatalf("status=%s messages=%d err=%v", p.RecordingStatus, len(p.Messages), err)
	}
}
