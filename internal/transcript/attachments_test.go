package transcript

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/memory/timeline"
)

func TestSanitizeItemID(t *testing.T) {
	cases := map[string]string{
		"in:req-1795694743016-lcl3qp": "in:req-1795694743016-lcl3qp",
		"":                            "",
		".":                           "",
		"..":                          "",
		"in:../../etc":                "in:.._.._etc",
		"in:req/1":                    "in:req_1",
	}
	for in, want := range cases {
		if got := SanitizeItemID(in); got != want {
			t.Errorf("SanitizeItemID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestItemAttachmentsDir(t *testing.T) {
	dir := ItemAttachmentsDir("/tmp/session", "in:req-1")
	want := filepath.Join("/tmp/session", AttachmentsDirName, "in:req-1")
	if dir != want {
		t.Fatalf("ItemAttachmentsDir = %q, want %q", dir, want)
	}
	if got := ItemAttachmentsDir("/tmp/session", ".."); got != "" {
		t.Fatalf("expected empty dir for unusable id, got %q", got)
	}
}

func TestAttachmentItemIDs(t *testing.T) {
	events := []timeline.Event{
		{Message: &timeline.MessagePayload{ItemID: "in:req-1", Files: []timeline.FileAttachment{{Name: "a.png", Path: "/a"}}}},
		{Message: &timeline.MessagePayload{ItemID: "in:req-2"}},
		{Control: &timeline.ControlPayload{Action: "clear"}},
	}
	ids := AttachmentItemIDs(events)
	if len(ids) != 1 || ids[0] != "in:req-1" {
		t.Fatalf("AttachmentItemIDs = %v, want [in:req-1]", ids)
	}
}

func TestPruneAttachments(t *testing.T) {
	dir := t.TempDir()
	keepDir := ItemAttachmentsDir(dir, "in:req-keep")
	dropDir := ItemAttachmentsDir(dir, "in:req-drop")
	youngDir := ItemAttachmentsDir(dir, "in:req-young")
	for _, d := range []string{keepDir, dropDir, youngDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.WriteFile(filepath.Join(d, "image.png"), []byte("x"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}

	now := time.Now()
	old := now.Add(-2 * time.Hour)
	if err := os.Chtimes(dropDir, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	removed := PruneAttachments(dir, map[string]bool{"in:req-keep": true}, 30*time.Minute, now)
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if _, err := os.Stat(keepDir); err != nil {
		t.Errorf("referenced attachments must survive: %v", err)
	}
	if _, err := os.Stat(dropDir); !os.IsNotExist(err) {
		t.Errorf("unreferenced attachments must be removed, err=%v", err)
	}
	if _, err := os.Stat(youngDir); err != nil {
		t.Errorf("young directories stay inside the grace window: %v", err)
	}
}

func TestClearAndRemoveItemAttachments(t *testing.T) {
	dir := t.TempDir()
	item := ItemAttachmentsDir(dir, "in:req-1")
	if err := os.MkdirAll(item, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if !RemoveItemAttachments(dir, "in:req-1") {
		t.Fatal("RemoveItemAttachments reported failure")
	}
	if _, err := os.Stat(item); !os.IsNotExist(err) {
		t.Fatalf("item directory survived removal, err=%v", err)
	}

	if err := os.MkdirAll(ItemAttachmentsDir(dir, "in:req-2"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if !ClearAttachments(dir) {
		t.Fatal("ClearAttachments reported failure")
	}
	if _, err := os.Stat(AttachmentsDir(dir)); !os.IsNotExist(err) {
		t.Fatalf("attachments root survived clear, err=%v", err)
	}
}
