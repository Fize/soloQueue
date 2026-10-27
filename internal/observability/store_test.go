package observability

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSnapshotsLifecycleAndExpiry(t *testing.T) {
	s := ForWorkDir(t.TempDir())
	if e := s.Start("l1", "req"); e != nil {
		t.Fatal(e)
	}
	p := &Prompt{RecordingStatus: "complete", Representation: "messages", Messages: []Instruction{{Role: "system", Content: "original"}}}
	c := Call{CallID: "call-1", SessionID: "l1", RequestID: "req", Status: "running", StartedAt: time.Now()}
	if e := s.SaveCall(c, p); e != nil {
		t.Fatal(e)
	}
	if e := s.Expire(key("l1", "req")); e != ErrActive {
		t.Fatalf("active expiry %v", e)
	}
	now := time.Now()
	c.Status = "success"
	c.FinishedAt = &now
	c.Content = "complete intermediate text"
	c.AttemptCount = 2
	c.AttemptsReported = true
	if e := s.SaveCall(c, nil); e != nil {
		t.Fatal(e)
	}
	if e := s.Finish("l1", "req", "completed"); e != nil {
		t.Fatal(e)
	}
	r, e := s.Request("l1", "req")
	if e != nil || r.CallCount != 1 || r.Calls[0].Content != c.Content || r.AttemptCount != 2 {
		t.Fatalf("request %#v %v", r, e)
	}
	got, e := s.CallPrompt("l1", "req", "call-1")
	if e != nil || got.Messages[0].Content != "original" {
		t.Fatalf("prompt %#v %v", got, e)
	}
	if e = s.Expire(key("l1", "req")); e != nil {
		t.Fatal(e)
	}
	r, e = s.Request("l1", "req")
	if e != nil || r.CallCount != 1 || r.RecordingStatus != "expired" {
		t.Fatalf("expired %#v %v", r, e)
	}
	if e = s.PrunePrompts(); e != nil {
		t.Fatal(e)
	}
	files, _ := filepath.Glob(filepath.Join(s.Root(), "prompts", "*"))
	if len(files) != 0 {
		t.Fatal(files)
	}
}
func TestPromptDedupAndReplacement(t *testing.T) {
	s := ForWorkDir(t.TempDir())
	p := &Prompt{RecordingStatus: "complete", Representation: "messages", Messages: []Instruction{{Role: "system", Content: "same"}}}
	for _, id := range []string{"a", "b"} {
		if e := s.SaveCall(Call{CallID: id, SessionID: "l1", RequestID: "r", StartedAt: time.Now()}, p); e != nil {
			t.Fatal(e)
		}
	}
	files, _ := filepath.Glob(filepath.Join(s.Root(), "prompts", "*"))
	if len(files) != 1 {
		t.Fatal(files)
	}
	p.Representation = "instructions"
	p.Messages = nil
	p.Instructions = "effective"
	_ = s.SaveCall(Call{CallID: "a", SessionID: "l1", RequestID: "r"}, p)
	got, e := s.CallPrompt("l1", "r", "a")
	if e != nil || got.Instructions != "effective" {
		t.Fatalf("%#v %v", got, e)
	}
}
func TestBudgetAndConcurrentCapture(t *testing.T) {
	s := ForWorkDir(t.TempDir())
	s.SetPolicy(func() Policy { return Policy{MaxTurnBytes: 4096, MaxBytes: 16384, MaxFileBytes: 1024} })
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = s.SaveCall(Call{CallID: string(rune('a' + i)), SessionID: "l1", RequestID: "r", Content: strings.Repeat("large", 4096)}, nil)
		}(i)
	}
	wg.Wait()
	r, e := s.Request("l1", "r")
	if e != nil || len(r.Calls) != 8 {
		t.Fatalf("%#v %v", r, e)
	}
	for _, c := range r.Calls {
		if c.Content != "" || c.RecordingStatus != "partial" {
			t.Fatalf("unbounded %#v", c)
		}
	}
}
func TestTraversalSymlinkAndCorruption(t *testing.T) {
	dir := t.TempDir()
	s := ForWorkDir(dir)
	if _, e := s.Request("l1", "../escape"); e == nil {
		t.Fatal("accepted traversal")
	}
	outside := t.TempDir()
	if e := os.MkdirAll(s.Root(), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(outside, filepath.Join(s.Root(), "requests")); e != nil {
		t.Fatal(e)
	}
	if e := s.Start("l1", "r"); e == nil {
		t.Fatal("accepted parent symlink")
	}
	_ = os.Remove(filepath.Join(s.Root(), "requests"))
	p := filepath.Join(s.requestDir("l1", "r"), "meta.json")
	_ = os.MkdirAll(filepath.Dir(p), 0700)
	_ = os.WriteFile(p, []byte("{bad"), 0600)
	r, e := s.Request("l1", "r")
	if e != nil || r.RecordingStatus != "partial" || len(r.Errors) == 0 {
		t.Fatalf("corruption hidden %#v %v", r, e)
	}
}
func TestSharedRequestAttribution(t *testing.T) {
	s := ForWorkDir(t.TempDir())
	_ = s.Start("l1", "a")
	_ = s.Start("l1", "b")
	end := time.Now()
	c := Call{CallID: "one-shared-call", SessionID: "l1", RequestID: "b", RequestIDs: []string{"a", "b"}, StartedAt: end, FinishedAt: &end, Status: "success"}
	if e := s.SaveCall(c, nil); e != nil {
		t.Fatal(e)
	}
	for _, id := range []string{"a", "b"} {
		r, e := s.Request("l1", id)
		if e != nil || len(r.Calls) != 1 || r.Calls[0].CallID != c.CallID {
			t.Fatalf("%s %#v %v", id, r, e)
		}
	}
}

func TestSharedRequestExpiryPropagates(t *testing.T) {
	s := ForWorkDir(t.TempDir())
	_ = s.Start("l1", "a")
	_ = s.Start("l1", "b")
	_ = s.SaveCall(Call{CallID: "shared", SessionID: "l1", RequestID: "b", RequestIDs: []string{"a", "b"}, StartedAt: time.Now()}, nil)
	_ = s.Finish("l1", "a", "completed")
	_ = s.Finish("l1", "b", "completed")
	if e := s.Expire(key("l1", "b")); e != nil {
		t.Fatal(e)
	}
	r, e := s.Request("l1", "a")
	if e != nil || r.RecordingStatus != "expired" || r.CallCount != 1 {
		t.Fatalf("%#v %v", r, e)
	}
	p, e := s.CallPrompt("l1", "a", "shared")
	if e != nil || p.RecordingStatus != "expired" {
		t.Fatalf("%#v %v", p, e)
	}
}

func TestOversizedPromptAndUnknownJournalFailClosed(t *testing.T) {
	root := t.TempDir()
	s := ForWorkDir(root)
	release := RegisterPolicy(root, func() Policy { return Policy{MaxBytes: 1 << 20, MaxTurnBytes: 32 << 10, MaxFileBytes: 1024} })
	defer release()
	c := Call{CallID: "c", SessionID: "l1", RequestID: "r", StartedAt: time.Now()}
	if e := s.SaveCall(c, &Prompt{RecordingStatus: "complete", Messages: []Instruction{{Role: "system", Content: strings.Repeat("s", 4096)}}}); e != nil {
		t.Fatal(e)
	}
	files, _ := filepath.Glob(filepath.Join(s.Root(), "prompts", "*.json"))
	if len(files) != 0 {
		t.Fatal("oversized prompt persisted")
	}
	r, _ := s.Request("l1", "r")
	if r.RecordingStatus != "partial" || r.Calls[0].PromptAvailable {
		t.Fatal(r)
	}
	dir := filepath.Join(root, "logs", "timelines", "default", "delegations", "dlg_corrupt")
	_ = os.MkdirAll(dir, 0700)
	_ = os.WriteFile(filepath.Join(dir, "meta.json"), []byte("{broken"), 0600)
	_ = os.WriteFile(filepath.Join(dir, "stream-2026-10-26.jsonl"), []byte(strings.Repeat("x", 4096)), 0600)
	c.CallID = "next"
	c.Content = "must not be persisted while accounting incomplete"
	if e := s.SaveCall(c, nil); e != nil {
		t.Fatal(e)
	}
	r, _ = s.Request("l1", "r")
	for _, call := range r.Calls {
		if call.CallID == "next" && (call.Content != "" || call.RecordingStatus != "partial") {
			t.Fatalf("failed-open %#v", call)
		}
	}
}

func TestPrunePromptsConfinedAndKnownFilesOnly(t *testing.T) {
	for _, symlink := range []bool{true, false} {
		t.Run(fmt.Sprint(symlink), func(t *testing.T) {
			s := ForWorkDir(t.TempDir())
			outside := t.TempDir()
			if err := os.MkdirAll(s.Root(), 0700); err != nil {
				t.Fatal(err)
			}
			prompts := filepath.Join(s.Root(), "prompts")
			target := prompts
			if symlink {
				target = outside
				if err := os.Symlink(outside, prompts); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Mkdir(prompts, 0700); err != nil {
					t.Fatal(err)
				}
			}
			known := strings.Repeat("a", 64) + ".json"
			for _, name := range []string{known, "unknown.json", strings.Repeat("z", 64) + ".json"} {
				if err := os.WriteFile(filepath.Join(target, name), []byte("{}"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			err := s.PrunePrompts()
			if symlink && err == nil {
				t.Fatal("unsafe prompt directory accepted")
			}
			if !symlink && err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"unknown.json", strings.Repeat("z", 64) + ".json"} {
				if _, err := os.Stat(filepath.Join(target, name)); err != nil {
					t.Fatalf("unknown file deleted: %s", name)
				}
			}
			_, err = os.Stat(filepath.Join(target, known))
			if symlink && err != nil {
				t.Fatal("external snapshot deleted")
			}
			if !symlink && !os.IsNotExist(err) {
				t.Fatal("orphan snapshot retained", err)
			}
		})
	}
}

func TestAdmissionGenerationPinsAliasRejectsLateWritesAndScopesBudget(t *testing.T) {
	s := ForWorkDir(t.TempDir())
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.Begin("l1", "root"))
	must(s.Begin("l1", "alias"))
	old := Call{CallID: "old", SessionID: "l1", RequestID: "root", RequestIDs: []string{"alias"}, StartedAt: time.Now(), Status: "success", AttemptCount: 2, Content: strings.Repeat("o", 3000)}
	prompt := &Prompt{RecordingStatus: "complete", Messages: []Instruction{{Role: "system", Content: "old instructions"}}}
	must(s.SaveCall(old, prompt))
	must(s.Finish("l1", "root", "completed"))
	must(s.Finish("l1", "alias", "completed"))
	must(s.Begin("l1", "root"))
	current, err := s.Request("l1", "root")
	must(err)
	must(s.Start("l1", "root"))
	again, err := s.Request("l1", "root")
	must(err)
	if again.Generation != current.Generation || !again.StartedAt.Equal(current.StartedAt) {
		t.Fatal("per-call Start changed generation")
	}
	if err := s.SaveCall(old, prompt); err == nil {
		t.Fatal("late old generation write accepted")
	}
	s.SetPolicy(func() Policy { return Policy{MaxTurnBytes: 2000, MaxBytes: 20000, MaxFileBytes: 2000} })
	fresh := Call{CallID: "new", SessionID: "l1", RequestID: "root", StartedAt: time.Now(), Status: "success", Content: strings.Repeat("n", 500)}
	must(s.SaveCall(fresh, nil))
	latest, err := s.Request("l1", "root")
	must(err)
	if len(latest.Calls) != 1 || latest.Calls[0].CallID != "new" || latest.Calls[0].Content != fresh.Content {
		t.Fatalf("latest generation/budget mixed: %#v", latest)
	}
	alias, err := s.Request("l1", "alias")
	must(err)
	if len(alias.Calls) != 1 || alias.Calls[0].CallID != "old" || alias.AttemptCount != 2 {
		t.Fatalf("alias moved generation: %#v", alias)
	}
	got, err := s.CallPrompt("l1", "alias", "old")
	must(err)
	if len(got.Messages) != 1 || got.Messages[0].Content != "old instructions" {
		t.Fatalf("alias prompt lost: %#v", got)
	}
	must(s.PrunePrompts())
	got, err = s.CallPrompt("l1", "alias", "old")
	must(err)
	if got.RecordingStatus == "expired" {
		t.Fatal("GC collected archived generation reference")
	}
	must(s.Finish("l1", "root", "completed"))
	must(s.Expire(key("l1", "root")))
	alias, err = s.Request("l1", "alias")
	must(err)
	if alias.RecordingStatus != "expired" || alias.CallCount != 1 || alias.AttemptCount != 2 {
		t.Fatalf("archived alias expiry lost: %#v", alias)
	}
}
