package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ─── L2SessionStore tests ──────────────────────────────────────────────────

func TestL2SessionStore_Create(t *testing.T) {
	dir := t.TempDir()
	store := newTestStore(t, dir)

	info, err := store.Create(context.Background(), "test-id-1", "dev", "", "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if info.ID != "test-id-1" {
		t.Errorf("ID = %q, want %q", info.ID, "test-id-1")
	}
	if info.Group != "dev" {
		t.Errorf("Group = %q, want %q", info.Group, "dev")
	}
	if info.Name != "" {
		t.Errorf("Name should be empty initially, got %q", info.Name)
	}
}

func TestL2SessionStore_CreateWithWorkDir(t *testing.T) {
	dir := t.TempDir()
	store := newTestStore(t, dir)

	info, err := store.Create(context.Background(), "wd-test", "dev", "proj1", "/path/to/proj1")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if info.ProjectID != "proj1" {
		t.Errorf("ProjectID = %q, want %q", info.ProjectID, "proj1")
	}
}

func TestL2SessionStore_Create_Duplicate(t *testing.T) {
	dir := t.TempDir()
	store := newTestStore(t, dir)

	_, err := store.Create(context.Background(), "dup-id", "dev", "", "")
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}

	_, err = store.Create(context.Background(), "dup-id", "dev", "", "")
	if err == nil {
		t.Fatal("expected error for duplicate ID")
	}
}

func TestL2SessionStore_SetName(t *testing.T) {
	dir := t.TempDir()
	store := newTestStore(t, dir)

	_, _ = store.Create(context.Background(), "name-test", "dev", "", "")
	store.SetName("name-test", "Fix login bug")

	list := store.List()
	for _, s := range list {
		if s.ID == "name-test" {
			if s.Name != "Fix login bug" {
				t.Errorf("Name = %q, want %q", s.Name, "Fix login bug")
			}
			return
		}
	}
	t.Fatal("session not found in list")
}

func TestL2SessionStore_Remove(t *testing.T) {
	dir := t.TempDir()
	store := newTestStore(t, dir)

	_, _ = store.Create(context.Background(), "remove-test", "dev", "", "")

	err := store.Remove(context.Background(), "remove-test")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}

	if len(store.List()) != 0 {
		t.Errorf("expected empty list after remove, got %d", len(store.List()))
	}
}

func TestL2SessionStore_Remove_NotFound(t *testing.T) {
	dir := t.TempDir()
	store := newTestStore(t, dir)

	err := store.Remove(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
}

func TestL2SessionStore_List(t *testing.T) {
	dir := t.TempDir()
	store := newTestStore(t, dir)

	_, _ = store.Create(context.Background(), "a", "dev", "", "")
	time.Sleep(1 * time.Millisecond)
	_, _ = store.Create(context.Background(), "b", "ops", "", "")
	time.Sleep(1 * time.Millisecond)
	_, _ = store.Create(context.Background(), "c", "dev", "", "")

	// Simulate activation (BuildL2 creates the timeline directory on first use).
	// Without the directory, List() treats the session as a phantom and skips it.
	for _, id := range []string{"a", "b", "c"} {
		tlDir := filepath.Join(dir, "logs", "timelines", "l2-"+id)
		if err := os.MkdirAll(tlDir, 0755); err != nil {
			t.Fatalf("failed to create timeline dir for %q: %v", id, err)
		}
	}

	list := store.List()
	if len(list) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(list))
	}

	// Should be sorted newest first.
	if list[0].ID != "c" {
		t.Errorf("first should be newest (c), got %q", list[0].ID)
	}
}

func TestL2SessionStore_Shutdown(t *testing.T) {
	dir := t.TempDir()
	store := newTestStore(t, dir)

	_, _ = store.Create(context.Background(), "s1", "dev", "", "")
	_, _ = store.Create(context.Background(), "s2", "ops", "", "")

	store.Shutdown()
	if len(store.List()) != 0 {
		t.Errorf("expected empty list after shutdown, got %d", len(store.List()))
	}
}

// ─── Helpers ───────────────────────────────────────────────────────────────

func newTestStore(t *testing.T, dir string) *L2SessionStore {
	t.Helper()
	builder := &Builder{
		WorkDir: dir,
	}
	return NewL2SessionStore(builder, dir, nil)
}

func TestCleanupTimelineDir(t *testing.T) {
	dir := t.TempDir()
	tlDir := filepath.Join(dir, "logs", "timelines", "l2-test-cleanup")
	if err := os.MkdirAll(tlDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	store := newTestStore(t, dir)
	_, _ = store.Create(context.Background(), "test-cleanup", "dev", "", "")

	if err := os.WriteFile(filepath.Join(tlDir, "timeline.jsonl"), []byte(`{"ts":"2026-01-01T00:00:00Z","type":"message","msg":{"role":"user","content":"hi"}}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_ = store.Remove(context.Background(), "test-cleanup")

	if _, err := os.Stat(tlDir); !os.IsNotExist(err) {
		t.Errorf("timeline directory should be removed after session deletion")
	}
}


func TestL2SessionStore_RestoreFromDisk(t *testing.T) {
	dir := t.TempDir()
	store := newTestStore(t, dir)

	// Create timeline directory with meta.json simulating a past session.
	id := "test-restore-id"
	tlDir := filepath.Join(dir, "logs", "timelines", "l2-"+id)
	if err := os.MkdirAll(tlDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	meta := `{"schema_version":1,"group":"dev","work_dir":"/path/to/project"}`
	if err := os.WriteFile(filepath.Join(tlDir, MetaFileName), []byte(meta), 0o644); err != nil {
		t.Fatalf("write %s: %v", MetaFileName, err)
	}

	// Session should not be in memory yet.
	for _, s := range store.List() {
		if s.ID == id {
			t.Fatal("session should not exist before restore")
		}
	}

	// Call restoreFromDisk directly (private method, same package).
	if err := store.restoreFromDisk(context.Background(), id); err != nil {
		t.Fatalf("restoreFromDisk: %v", err)
	}

	// Verify the session was restored into the in-memory map.
	found := false
	for _, s := range store.List() {
		if s.ID == id {
			found = true
			if s.Group != "dev" {
				t.Errorf("Group = %q, want %q", s.Group, "dev")
			}
			break
		}
	}
	if !found {
		t.Error("session should be in the store after disk restoration")
	}
}

func TestL2SessionStore_RestoreFromDisk_MissingMeta(t *testing.T) {
	dir := t.TempDir()
	store := newTestStore(t, dir)

	id := "test-no-meta"
	tlDir := filepath.Join(dir, "logs", "timelines", "l2-"+id)
	if err := os.MkdirAll(tlDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// No meta or group file written.

	if err := store.restoreFromDisk(context.Background(), id); err == nil {
		t.Fatal("expected error for session with no metadata on disk")
	}

	// Session should not be in the store.
	for _, s := range store.List() {
		if s.ID == id {
			t.Error("session should not appear in store when metadata is missing")
		}
	}
}

func TestL2SessionStore_RestoreFromDisk_NoDiskDir(t *testing.T) {
	dir := t.TempDir()
	store := newTestStore(t, dir)

	if err := store.restoreFromDisk(context.Background(), "nonexistent-session"); err == nil {
		t.Fatal("expected error for session with no disk directory")
	}
}

func TestL2SessionStore_SetName_PersistAndRestore(t *testing.T) {
	dir := t.TempDir()
	store := newTestStore(t, dir)

	id := "test-persist-id"
	_, err := store.Create(context.Background(), id, "dev", "", "/project/path")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	tlDir := filepath.Join(dir, "logs", "timelines", "l2-"+id)
	if err := os.MkdirAll(tlDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	expectedName := "Concise title generation"
	store.SetName(id, expectedName)

	metaFile := filepath.Join(tlDir, MetaFileName)
	if _, err := os.Stat(metaFile); os.IsNotExist(err) {
		t.Fatalf("expected %s to be written to disk, but it does not exist", MetaFileName)
	}

	newStore := newTestStore(t, dir)
	err = newStore.restoreFromDisk(context.Background(), id)
	if err != nil {
		t.Fatalf("restoreFromDisk: %v", err)
	}

	found := false
	for _, s := range newStore.List() {
		if s.ID == id {
			found = true
			if s.Name != expectedName {
				t.Errorf("Name = %q, want %q", s.Name, expectedName)
			}
			if s.Group != "dev" {
				t.Errorf("Group = %q, want %q", s.Group, "dev")
			}
			if s.WorkDir != "/project/path" {
				t.Errorf("WorkDir = %q, want %q", s.WorkDir, "/project/path")
			}
			break
		}
	}
	if !found {
		t.Error("session was not restored in the new store")
	}
}

func TestL2SessionStore_UpdatePlanStatus_PersistAndRestore(t *testing.T) {
	dir := t.TempDir()
	store := newTestStore(t, dir)

	id := "test-plan-id"
	_, err := store.Create(context.Background(), id, "dev", "", "/project/path")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	tlDir := filepath.Join(dir, "logs", "timelines", "l2-"+id)
	if err := os.MkdirAll(tlDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// 1. Add some plan paths
	plan1 := "/project/path/plan/dev/task1.md"
	plan2 := "/project/path/plan/dev/task2.md"

	store.UpdatePlanStatus(id, plan1)
	store.UpdatePlanStatus(id, plan2)
	// Deduplicate check
	store.UpdatePlanStatus(id, plan1)

	// Check in-memory state
	list := store.List()
	var currentPlans []string
	for _, s := range list {
		if s.ID == id {
			currentPlans = s.Plans
		}
	}
	if len(currentPlans) != 2 {
		t.Errorf("expected 2 plans, got %d: %v", len(currentPlans), currentPlans)
	} else {
		if currentPlans[0] != plan1 || currentPlans[1] != plan2 {
			t.Errorf("plans mismatch: %v", currentPlans)
		}
	}

	// 2. Restore in a new store and check persistence
	newStore := newTestStore(t, dir)
	err = newStore.restoreFromDisk(context.Background(), id)
	if err != nil {
		t.Fatalf("restoreFromDisk: %v", err)
	}

	restoredList := newStore.List()
	var restoredPlans []string
	found := false
	for _, s := range restoredList {
		if s.ID == id {
			found = true
			restoredPlans = s.Plans
		}
	}
	if !found {
		t.Fatal("session not found after restore")
	}
	if len(restoredPlans) != 2 {
		t.Errorf("expected 2 restored plans, got %d: %v", len(restoredPlans), restoredPlans)
	} else {
		if restoredPlans[0] != plan1 || restoredPlans[1] != plan2 {
			t.Errorf("restored plans mismatch: %v", restoredPlans)
		}
	}
}
