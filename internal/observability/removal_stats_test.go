package observability

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRemovalStatsSharedPromptsAndNetBytes(t *testing.T) {
	s := ForWorkDir(t.TempDir())
	prompt := &Prompt{RecordingStatus: "complete", Messages: []Instruction{{Role: "system", Content: strings.Repeat("shared", 40000)}}}
	for _, req := range []string{"a", "b"} {
		if err := s.Start("l1", req); err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{"one", "two"} {
			if err := s.SaveCall(Call{CallID: id, SessionID: "l1", RequestID: req, StartedAt: time.Now(), Content: "body"}, prompt); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.Finish("l1", req, "completed"); err != nil {
			t.Fatal(err)
		}
	}
	before := bytesIn(s.Root())
	stats, err := s.ExpireWithStats(key("l1", "a"))
	if err != nil || stats.DeletedFiles != 2 {
		t.Fatalf("%+v %v", stats, err)
	}
	gc, err := s.PrunePromptsWithStats()
	if err != nil || gc.DeletedFiles != 0 || gc.FreedBytes != 0 {
		t.Fatalf("shared prompt credited early %+v %v", gc, err)
	}
	if stats.FreedBytes != before-bytesIn(s.Root()) {
		t.Fatal("net expiry bytes differ")
	}
	before = bytesIn(s.Root())
	stats, err = s.ExpireWithStats(key("l1", "b"))
	if err != nil {
		t.Fatal(err)
	}
	gc, err = s.PrunePromptsWithStats()
	if err != nil || gc.DeletedFiles != 1 || gc.FreedBytes < 200000 {
		t.Fatalf("prompt not credited %+v %v", gc, err)
	}
	if stats.FreedBytes+gc.FreedBytes != before-bytesIn(s.Root()) {
		t.Fatal("combined actual byte delta differs")
	}
}
func TestRemovalStatsActiveAndUnsafeFailuresNotCredited(t *testing.T) {
	s := ForWorkDir(t.TempDir())
	if err := s.Start("l1", "active"); err != nil {
		t.Fatal(err)
	}
	if stats, err := s.ExpireWithStats(key("l1", "active")); err != ErrActive || stats.DeletedFiles != 0 || stats.FreedBytes != 0 {
		t.Fatalf("active %+v %v", stats, err)
	}
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "keep"), []byte("safe"), 0600)
	os.MkdirAll(filepath.Join(s.Root(), "prompts"), 0700)
	name := strings.Repeat("a", 64) + ".json"
	if err := os.Symlink(filepath.Join(outside, "keep"), filepath.Join(s.Root(), "prompts", name)); err != nil {
		t.Fatal(err)
	}
	stats, err := s.PrunePromptsWithStats()
	if err != nil || stats.DeletedFiles != 0 || stats.FreedBytes != 0 {
		t.Fatalf("unsafe credited %+v %v", stats, err)
	}
	if b, err := os.ReadFile(filepath.Join(outside, "keep")); err != nil || string(b) != "safe" {
		t.Fatal("outside changed")
	}
}

func TestExpiryPartialFailureOnlyCreditsSuccessfulRemovals(t *testing.T) {
	s := ForWorkDir(t.TempDir())
	if err := s.Start("l1", "partial"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCall(Call{CallID: "one", SessionID: "l1", RequestID: "partial", StartedAt: time.Now(), Content: "body"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Finish("l1", "partial", "completed"); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(s.Root(), "requests", key("l1", "partial"), "calls")
	outside := filepath.Join(t.TempDir(), "keep")
	os.WriteFile(outside, []byte("safe"), 0600)
	// An unowned entry is invisible to call JSON reads but must not be removed as a call.
	if err := os.Symlink(outside, filepath.Join(dir, "zz-unowned")); err != nil {
		t.Fatal(err)
	}
	before := bytesIn(s.Root())
	stats, err := s.ExpireWithStats(key("l1", "partial"))
	if err == nil || stats.DeletedFiles != 1 || stats.FreedBytes != before-bytesIn(s.Root()) {
		t.Fatalf("partial removal credited incorrectly: %+v %v", stats, err)
	}
	if _, err = os.Lstat(filepath.Join(dir, "zz-unowned")); err != nil {
		t.Fatal("failed removal credited/deleted", err)
	}
}
