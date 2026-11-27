package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/memory/timeline"
	"github.com/xiaobaitu/soloqueue/internal/transcript"
)

// attachmentSession builds a session whose only purpose is attachment pruning:
// a context window with stamped messages and a timeline directory on disk.
func attachmentSession(t *testing.T) (*Session, *ctxwin.ContextWindow) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "default")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	w, err := timeline.NewWriter(dir, "timeline", 1<<20, 30)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	cw := ctxwin.NewContextWindow(1<<20, 2000, 0, ctxwin.NewTokenizer())
	return &Session{cw: cw, tl: w}, cw
}

func seedAttachment(t *testing.T, s *Session, itemID string) string {
	t.Helper()
	dir := transcript.ItemAttachmentsDir(s.timelineDir(), itemID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "image.png"), []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return dir
}

func TestRewindDropsAttachmentsAfterBoundaryOnly(t *testing.T) {
	s, cw := attachmentSession(t)
	base := time.Now().Add(-time.Hour)
	boundary := base.Add(10 * time.Minute)
	later := base.Add(20 * time.Minute)

	cw.Push(ctxwin.RoleUser, "boundary", ctxwin.WithTimestamp(boundary), ctxwin.WithItem("in:req-boundary", 1))
	cw.Push(ctxwin.RoleUser, "later", ctxwin.WithTimestamp(later), ctxwin.WithItem("in:req-later", 2))
	boundaryDir := seedAttachment(t, s, "in:req-boundary")
	laterDir := seedAttachment(t, s, "in:req-later")

	dropped := s.itemIDsDroppedByRewind(boundary)
	if len(dropped) != 1 || dropped[0] != "in:req-later" {
		t.Fatalf("dropped = %v, want [in:req-later]", dropped)
	}
	s.dropAttachments(dropped)

	if _, err := os.Stat(laterDir); !os.IsNotExist(err) {
		t.Errorf("attachments after the rewind boundary must be removed, err=%v", err)
	}
	if _, err := os.Stat(boundaryDir); err != nil {
		t.Errorf("boundary attachments are kept for the re-send: %v", err)
	}
}

func TestDeleteMessagesDropsThoseAttachments(t *testing.T) {
	s, cw := attachmentSession(t)
	base := time.Now().Add(-time.Hour)
	keepTs := base
	dropTs := base.Add(10 * time.Minute)

	cw.Push(ctxwin.RoleUser, "keep", ctxwin.WithTimestamp(keepTs), ctxwin.WithItem("in:req-keep", 1))
	cw.Push(ctxwin.RoleUser, "drop", ctxwin.WithTimestamp(dropTs), ctxwin.WithItem("in:req-drop", 2))
	keepDir := seedAttachment(t, s, "in:req-keep")
	dropDir := seedAttachment(t, s, "in:req-drop")

	ids := s.itemIDsAt([]time.Time{dropTs})
	if len(ids) != 1 || ids[0] != "in:req-drop" {
		t.Fatalf("itemIDsAt = %v, want [in:req-drop]", ids)
	}
	s.dropAttachments(ids)

	if _, err := os.Stat(dropDir); !os.IsNotExist(err) {
		t.Errorf("deleted message attachments must be removed, err=%v", err)
	}
	if _, err := os.Stat(keepDir); err != nil {
		t.Errorf("surviving message attachments must stay: %v", err)
	}
}

func TestClearAttachmentsRemovesSessionAttachments(t *testing.T) {
	s, _ := attachmentSession(t)
	seedAttachment(t, s, "in:req-1")
	seedAttachment(t, s, "in:req-2")

	s.clearAttachments()

	if _, err := os.Stat(transcript.AttachmentsDir(s.timelineDir())); !os.IsNotExist(err) {
		t.Fatalf("attachments survived clear, err=%v", err)
	}
}

func TestAttachmentHelpersTolerateUnpersistedSession(t *testing.T) {
	s := &Session{}
	if got := s.itemIDsDroppedByRewind(time.Now()); got != nil {
		t.Errorf("expected nil for a session without a context window, got %v", got)
	}
	s.dropAttachments([]string{"in:req-1"})
	s.clearAttachments()
}
