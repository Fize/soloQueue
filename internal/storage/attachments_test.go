package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/transcript"
)

func TestSweepSessionAttachmentsConvergesLeftovers(t *testing.T) {
	root := t.TempDir()
	sessionDir := filepath.Join(root, "logs", "timelines", "l2-s1")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	row := `{"ts":"2026-11-26T10:00:00+08:00","type":"message","msg":{"role":"user","content":"hi","timestamp":"2026-11-26T10:00:00+08:00","item_id":"in:req-1","files":[{"name":"image.png","path":"/kept/image.png"}]}}` + "\n"
	if err := os.WriteFile(filepath.Join(sessionDir, "timeline-2026-11-26.jsonl"), []byte(row), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	kept := transcript.ItemAttachmentsDir(sessionDir, "in:req-1")
	orphan := transcript.ItemAttachmentsDir(sessionDir, "in:req-gone")
	young := transcript.ItemAttachmentsDir(sessionDir, "in:req-young")
	for _, dir := range []string{kept, orphan, young} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "image.png"), []byte("x"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	old := time.Now().Add(-2 * time.Hour)
	for _, dir := range []string{kept, orphan} {
		if err := os.Chtimes(dir, old, old); err != nil {
			t.Fatalf("Chtimes: %v", err)
		}
	}

	pendingDir := transcript.AttachmentsDir(filepath.Join(root, "logs", "timelines", pendingTimelineDirName))
	if err := os.MkdirAll(pendingDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	stalePending := filepath.Join(pendingDir, "stale.png")
	freshPending := filepath.Join(pendingDir, "fresh.png")
	for _, path := range []string{stalePending, freshPending} {
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	if err := os.Chtimes(stalePending, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	stale := time.Now().Add(-25 * time.Hour)
	if err := os.Chtimes(stalePending, stale, stale); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	m := &Manager{root: root}
	m.sweepSessionAttachments(time.Now())

	if _, err := os.Stat(kept); err != nil {
		t.Errorf("attachments referenced by history must survive: %v", err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Errorf("unreferenced attachments must be removed, err=%v", err)
	}
	if _, err := os.Stat(young); err != nil {
		t.Errorf("attachments inside the grace window must survive: %v", err)
	}
	if _, err := os.Stat(stalePending); !os.IsNotExist(err) {
		t.Errorf("unsent uploads expire, err=%v", err)
	}
	if _, err := os.Stat(freshPending); err != nil {
		t.Errorf("fresh uploads must survive: %v", err)
	}
}

func TestSweepSessionAttachmentsClearsSessionWithoutTimeline(t *testing.T) {
	root := t.TempDir()
	sessionDir := filepath.Join(root, "logs", "timelines", "l2-gone")
	itemDir := transcript.ItemAttachmentsDir(sessionDir, "in:req-1")
	if err := os.MkdirAll(itemDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(itemDir, "image.png"), []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(itemDir, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	m := &Manager{root: root}
	m.sweepSessionAttachments(time.Now())

	if _, err := os.Stat(itemDir); !os.IsNotExist(err) {
		t.Errorf("a session without timeline rows keeps no attachments, err=%v", err)
	}
}
