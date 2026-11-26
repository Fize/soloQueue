package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeTimelineFile writes one timeline event and stamps the file's mtime so a
// test can control the activity timestamp without sleeping.
func writeTimelineFile(t *testing.T, dir, name, ts string, mtime time.Time) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(`{"ts":"`+ts+`","type":"msg"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
	return path
}

func TestLatestTimelineWriteIgnoresEmptyFilesAndDirMtime(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().UTC()
	writeTimelineFile(t, dir, "timeline-2026-01-01.jsonl", now.Add(-48*time.Hour).Format(time.RFC3339Nano), now.Add(-48*time.Hour))

	// A rotated-but-unwritten file must not count as activity.
	empty := filepath.Join(dir, "timeline-2026-01-02.jsonl")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatalf("write empty file: %v", err)
	}
	if err := os.Chtimes(empty, now, now); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	if got, want := LatestTimelineWrite(dir), now.Add(-48*time.Hour); !got.Equal(want) {
		t.Fatalf("LatestTimelineWrite = %v, want %v", got, want)
	}

	// Writing metadata bumps the directory mtime (temp file + rename). The
	// activity timestamp must not move with it.
	if err := MergeAndSavePath(filepath.Join(dir, MetaFileName), func(m *SessionMeta) { m.Group = "dev" }); err != nil {
		t.Fatalf("MergeAndSavePath: %v", err)
	}
	if got, want := LatestTimelineWrite(dir), now.Add(-48*time.Hour); !got.Equal(want) {
		t.Fatalf("LatestTimelineWrite after meta write = %v, want %v", got, want)
	}
}

func TestLatestTimelineWriteEmptyDir(t *testing.T) {
	if got := LatestTimelineWrite(t.TempDir()); !got.IsZero() {
		t.Fatalf("expected zero activity for a session without timeline content, got %v", got)
	}
}

func TestFirstEventTimeReadsOldestFile(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	writeTimelineFile(t, dir, "timeline-2026-03-04.jsonl", base.Add(time.Hour).Format(time.RFC3339Nano), base.Add(time.Hour))
	writeTimelineFile(t, dir, "timeline-2026-03-05.jsonl", base.Add(-72*time.Hour).Format(time.RFC3339Nano), base.Add(2*time.Hour))

	// The earliest event lives in the file written first, not in the file whose
	// name sorts last.
	if got, want := FirstEventTime(dir), base.Add(time.Hour); !got.Equal(want) {
		t.Fatalf("FirstEventTime = %v, want %v", got, want)
	}
}

func TestFirstEventTimeFallsBackToFileModTime(t *testing.T) {
	dir := t.TempDir()
	when := time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(dir, "timeline-2026-05-06.jsonl")
	if err := os.WriteFile(path, []byte("not-json\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	if got := FirstEventTime(dir); !got.Equal(when) {
		t.Fatalf("FirstEventTime fallback = %v, want %v", got, when)
	}
}

func TestFirstEventTimeEmptyDir(t *testing.T) {
	if got := FirstEventTime(t.TempDir()); !got.IsZero() {
		t.Fatalf("expected zero creation time for an empty session dir, got %v", got)
	}
}
