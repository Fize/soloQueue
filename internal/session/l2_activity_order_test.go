package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newActivityStore builds a store whose sessions all exist on disk with a
// timeline directory, so List() returns them instead of filtering them out as
// never-activated phantoms.
func newActivityStore(t *testing.T, ids ...string) (*L2SessionStore, string) {
	t.Helper()
	dir := t.TempDir()
	store := newTestStore(t, dir)
	for _, id := range ids {
		if _, err := store.Create(context.Background(), id, "dev", "", ""); err != nil {
			t.Fatalf("Create(%s): %v", id, err)
		}
		tlDir := filepath.Join(dir, "logs", "timelines", "l2-"+id)
		if err := os.MkdirAll(tlDir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", tlDir, err)
		}
	}
	return store, dir
}

// asColdStart drops the in-memory activity marks so only disk truth remains,
// which is the state List() sees for sessions restored after a restart.
func asColdStart(store *L2SessionStore) {
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, entry := range store.sessions {
		entry.ActivityAt = time.Time{}
	}
}

func listIDs(store *L2SessionStore) []string {
	list := store.List()
	ids := make([]string, 0, len(list))
	for _, info := range list {
		ids = append(ids, info.ID)
	}
	return ids
}

func assertOrder(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("order length = %d (%v), want %d (%v)", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

func TestL2SessionStore_ListOrdersByLastActivity(t *testing.T) {
	store, dir := newActivityStore(t, "old", "mid", "recent")
	now := time.Now().UTC()

	// Creation order is old → mid → recent, activity order is the reverse, so a
	// creation-ordered list would fail here.
	writeTimelineFile(t, filepath.Join(dir, "logs", "timelines", "l2-old"), "timeline-2026-01-01.jsonl", now.Add(-8*time.Hour).Format(time.RFC3339Nano), now.Add(-8*time.Hour))
	writeTimelineFile(t, filepath.Join(dir, "logs", "timelines", "l2-mid"), "timeline-2026-01-01.jsonl", now.Add(-2*time.Hour).Format(time.RFC3339Nano), now.Add(-2*time.Hour))
	writeTimelineFile(t, filepath.Join(dir, "logs", "timelines", "l2-recent"), "timeline-2026-01-01.jsonl", now.Add(-time.Minute).Format(time.RFC3339Nano), now.Add(-time.Minute))
	asColdStart(store)

	assertOrder(t, listIDs(store), []string{"recent", "mid", "old"})
}

func TestL2SessionStore_ListIgnoresMetadataWrites(t *testing.T) {
	store, dir := newActivityStore(t, "older", "newer")
	now := time.Now().UTC()
	writeTimelineFile(t, filepath.Join(dir, "logs", "timelines", "l2-older"), "timeline-2026-01-01.jsonl", now.Add(-3*time.Hour).Format(time.RFC3339Nano), now.Add(-3*time.Hour))
	writeTimelineFile(t, filepath.Join(dir, "logs", "timelines", "l2-newer"), "timeline-2026-01-01.jsonl", now.Add(-time.Hour).Format(time.RFC3339Nano), now.Add(-time.Hour))
	asColdStart(store)

	assertOrder(t, listIDs(store), []string{"newer", "older"})

	// Renaming writes meta.json inside the timeline directory, which bumps the
	// directory mtime. That must not reorder the list.
	store.SetName("older", "renamed session")

	assertOrder(t, listIDs(store), []string{"newer", "older"})
}

func TestL2SessionStore_ListIsDeterministic(t *testing.T) {
	store, _ := newActivityStore(t, "s1", "s2", "s3", "s4", "s5")

	// Force a total tie: identical activity and identical creation time. Only
	// the id tie-break can order these, and it must do so identically on every
	// call (the entries come from a Go map with randomized iteration).
	fixed := time.Date(2026, 2, 2, 2, 2, 2, 0, time.UTC)
	store.mu.Lock()
	for _, entry := range store.sessions {
		entry.CreatedAt = fixed
		entry.ActivityAt = fixed
	}
	store.mu.Unlock()

	want := listIDs(store)
	for i := 0; i < 50; i++ {
		assertOrder(t, listIDs(store), want)
	}
	if want[0] != "s1" || want[4] != "s5" {
		t.Fatalf("tie-break should order by id, got %v", want)
	}
}

func TestL2SessionStore_TouchActivityReorders(t *testing.T) {
	store, dir := newActivityStore(t, "a", "b")
	now := time.Now().UTC()
	writeTimelineFile(t, filepath.Join(dir, "logs", "timelines", "l2-a"), "timeline-2026-01-01.jsonl", now.Add(-time.Hour).Format(time.RFC3339Nano), now.Add(-time.Hour))
	writeTimelineFile(t, filepath.Join(dir, "logs", "timelines", "l2-b"), "timeline-2026-01-01.jsonl", now.Add(-2*time.Hour).Format(time.RFC3339Nano), now.Add(-2*time.Hour))
	asColdStart(store)

	assertOrder(t, listIDs(store), []string{"a", "b"})

	// An accepted prompt moves the session to the top before the turn's first
	// timeline write reaches disk.
	store.TouchActivity("b", now)
	assertOrder(t, listIDs(store), []string{"b", "a"})

	if got := store.LastActive("b"); !got.Equal(now) {
		t.Fatalf("LastActive(b) = %v, want %v", got, now)
	}
}

func TestL2SessionStore_RestoreKeepsCreationTime(t *testing.T) {
	dir := t.TempDir()
	id := "restore-created"
	tlDir := filepath.Join(dir, "logs", "timelines", "l2-"+id)
	firstEvent := time.Date(2026, 1, 9, 8, 7, 6, 0, time.UTC)
	writeTimelineFile(t, tlDir, "timeline-2026-01-09.jsonl", firstEvent.Format(time.RFC3339Nano), firstEvent)
	if err := MergeAndSave(dir, id, func(m *SessionMeta) { m.Group = "dev" }); err != nil {
		t.Fatalf("MergeAndSave: %v", err)
	}

	store := newTestStore(t, dir)
	if err := store.restoreFromDisk(context.Background(), id); err != nil {
		t.Fatalf("restoreFromDisk: %v", err)
	}

	list := store.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 session, got %d", len(list))
	}
	// A restore is not a creation: stamping "now" here is what made an untouched
	// session jump to the top of the list every time it was opened.
	if !list[0].CreatedAt.Equal(firstEvent) {
		t.Fatalf("CreatedAt = %v, want first event time %v", list[0].CreatedAt, firstEvent)
	}
	if !list[0].LastActiveAt.Equal(firstEvent) {
		t.Fatalf("LastActiveAt = %v, want %v", list[0].LastActiveAt, firstEvent)
	}

	// The derived creation time is backfilled so later restores are exact.
	meta, err := LoadMeta(dir, id)
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if !meta.CreatedAt.Equal(firstEvent) {
		t.Fatalf("persisted CreatedAt = %v, want %v", meta.CreatedAt, firstEvent)
	}
}

func TestL2SessionStore_CreatePersistsCreationTime(t *testing.T) {
	dir := t.TempDir()
	store := newTestStore(t, dir)
	info, err := store.Create(context.Background(), "persist-created", "dev", "", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	store.SetName(info.ID, "titled")

	meta, err := LoadMeta(dir, info.ID)
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if meta.CreatedAt.IsZero() {
		t.Fatal("meta.json should record the creation time on its first write")
	}
	if !meta.CreatedAt.Equal(info.CreatedAt) {
		t.Fatalf("persisted CreatedAt = %v, want %v", meta.CreatedAt, info.CreatedAt)
	}
}
