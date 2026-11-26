package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/session"
)

// listSessionsResponse is the subset of /session/list this file asserts on.
type listSessionsResponse struct {
	Sessions []struct {
		ID           string `json:"id"`
		CreatedAt    string `json:"created_at"`
		LastActiveAt string `json:"last_active_at"`
	} `json:"sessions"`
}

func callListSessions(t *testing.T, workDir string) listSessionsResponse {
	t.Helper()
	mux := &Mux{workDir: workDir}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/sessions", nil)
	mux.handleListSessions(rec, req)

	var response listSessionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return response
}

func writeSessionTimeline(t *testing.T, workDir, id, ts string, mtime time.Time) {
	t.Helper()
	dir := filepath.Join(workDir, "logs", "timelines", "l2-"+id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	path := filepath.Join(dir, "timeline-2026-01-01.jsonl")
	if err := os.WriteFile(path, []byte(`{"ts":"`+ts+`","type":"msg"}`+"\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

func sessionIDs(response listSessionsResponse) []string {
	ids := make([]string, 0, len(response.Sessions))
	for _, s := range response.Sessions {
		ids = append(ids, s.ID)
	}
	return ids
}

func TestListSessions_OrdersByLastActivity(t *testing.T) {
	workDir := t.TempDir()
	now := time.Now().UTC()

	for _, id := range []string{"dormant", "busy", "recent"} {
		if err := session.SaveMeta(workDir, id, &session.SessionMeta{Group: "engineering"}); err != nil {
			t.Fatalf("SaveMeta(%s): %v", id, err)
		}
	}
	writeSessionTimeline(t, workDir, "dormant", now.Add(-30*24*time.Hour).Format(time.RFC3339Nano), now.Add(-30*24*time.Hour))
	writeSessionTimeline(t, workDir, "busy", now.Add(-3*time.Hour).Format(time.RFC3339Nano), now.Add(-3*time.Hour))
	writeSessionTimeline(t, workDir, "recent", now.Add(-time.Minute).Format(time.RFC3339Nano), now.Add(-time.Minute))

	response := callListSessions(t, workDir)
	got := sessionIDs(response)
	want := []string{"l2:recent", "l2:busy", "l2:dormant"}
	if len(got) != len(want) {
		t.Fatalf("sessions = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sessions = %v, want %v", got, want)
		}
	}

	parsed, err := time.Parse(time.RFC3339Nano, response.Sessions[0].LastActiveAt)
	if err != nil {
		t.Fatalf("parse last_active_at: %v", err)
	}
	if !parsed.Equal(now.Add(-time.Minute).Truncate(time.Second)) && !parsed.Equal(now.Add(-time.Minute)) {
		if delta := parsed.Sub(now.Add(-time.Minute)); delta > time.Second || delta < -time.Second {
			t.Fatalf("last_active_at = %v, want ≈ %v", parsed, now.Add(-time.Minute))
		}
	}
}

func TestListSessions_IgnoresDirectoryMtime(t *testing.T) {
	workDir := t.TempDir()
	now := time.Now().UTC()

	for _, id := range []string{"older", "newer"} {
		if err := session.SaveMeta(workDir, id, &session.SessionMeta{Group: "engineering"}); err != nil {
			t.Fatalf("SaveMeta(%s): %v", id, err)
		}
	}
	writeSessionTimeline(t, workDir, "older", now.Add(-4*time.Hour).Format(time.RFC3339Nano), now.Add(-4*time.Hour))
	writeSessionTimeline(t, workDir, "newer", now.Add(-time.Hour).Format(time.RFC3339Nano), now.Add(-time.Hour))

	if got, want := sessionIDs(callListSessions(t, workDir)), []string{"l2:newer", "l2:older"}; got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("sessions = %v, want %v", got, want)
	}

	// Any metadata write renames a temp file inside the timeline directory and
	// therefore bumps the directory mtime. Ordering must not follow it.
	if err := session.MergeAndSave(workDir, "older", func(m *session.SessionMeta) { m.Name = "renamed" }); err != nil {
		t.Fatalf("MergeAndSave: %v", err)
	}

	response := callListSessions(t, workDir)
	got := sessionIDs(response)
	if len(got) != 2 || got[0] != "l2:newer" || got[1] != "l2:older" {
		t.Fatalf("order changed after a metadata write: %v", got)
	}
}

func TestListSessions_CreatedAtComesFromFirstEvent(t *testing.T) {
	workDir := t.TempDir()
	firstEvent := time.Date(2026, 4, 5, 6, 7, 8, 0, time.UTC)

	if err := session.SaveMeta(workDir, "legacy", &session.SessionMeta{Group: "engineering"}); err != nil {
		t.Fatalf("SaveMeta: %v", err)
	}
	writeSessionTimeline(t, workDir, "legacy", firstEvent.Format(time.RFC3339Nano), firstEvent.Add(2*time.Hour))

	response := callListSessions(t, workDir)
	if len(response.Sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(response.Sessions))
	}
	created, err := time.Parse(time.RFC3339Nano, response.Sessions[0].CreatedAt)
	if err != nil {
		t.Fatalf("parse created_at: %v", err)
	}
	if !created.Equal(firstEvent) {
		t.Fatalf("created_at = %v, want first event %v (directory mtime was %v)",
			created, firstEvent, firstEvent.Add(2*time.Hour))
	}
}

func TestListSessions_StableAcrossCalls(t *testing.T) {
	workDir := t.TempDir()
	now := time.Now().UTC()
	ids := []string{"s1", "s2", "s3", "s4", "s5", "s6"}
	for _, id := range ids {
		if err := session.SaveMeta(workDir, id, &session.SessionMeta{Group: "engineering"}); err != nil {
			t.Fatalf("SaveMeta(%s): %v", id, err)
		}
	}
	// Identical activity makes the id tie-break the only discriminator; the
	// handler must still return a deterministic order on every call.
	for _, id := range ids {
		writeSessionTimeline(t, workDir, id, now.Add(-time.Hour).Format(time.RFC3339Nano), now.Add(-time.Hour))
	}

	want := sessionIDs(callListSessions(t, workDir))
	for i := 0; i < 10; i++ {
		got := sessionIDs(callListSessions(t, workDir))
		if len(got) != len(want) {
			t.Fatalf("call %d returned %d sessions, want %d", i, len(got), len(want))
		}
		for j := range want {
			if got[j] != want[j] {
				t.Fatalf("call %d order = %v, want %v", i, got, want)
			}
		}
	}
}

func TestListSessions_UsesMetadataWriteTimeWhenTimelineIsEmpty(t *testing.T) {
	workDir := t.TempDir()
	if err := session.SaveMeta(workDir, "empty", &session.SessionMeta{Group: "engineering"}); err != nil {
		t.Fatalf("SaveMeta: %v", err)
	}
	metaPath := filepath.Join(workDir, "logs", "timelines", "l2-empty", session.MetaFileName)
	when := time.Now().UTC().Add(-90 * time.Minute).Truncate(time.Second)
	if err := os.Chtimes(metaPath, when, when); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	response := callListSessions(t, workDir)
	if len(response.Sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(response.Sessions))
	}
	lastActive, err := time.Parse(time.RFC3339Nano, response.Sessions[0].LastActiveAt)
	if err != nil {
		t.Fatalf("parse last_active_at: %v", err)
	}
	if !lastActive.Equal(when) {
		t.Fatalf("last_active_at = %v, want the metadata write time %v", lastActive, when)
	}
	if response.Sessions[0].CreatedAt == "" {
		t.Fatal("created_at must never be empty for a listed session")
	}
}
