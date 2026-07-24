package server

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/session"
)

func TestListSessions_UsesDistinctPersistedCtxwinSnapshotsOnColdStart(t *testing.T) {
	workDir := t.TempDir()
	snapshots := map[string]struct {
		used  int
		limit int
	}{
		"session-a": {used: 1200, limit: 131072},
		"session-b": {used: 9800, limit: 262144},
	}
	for id, snapshot := range snapshots {
		if err := session.SaveMeta(workDir, id, &session.SessionMeta{
			Group:       "dev",
			CtxwinUsed:  snapshot.used,
			CtxwinLimit: snapshot.limit,
		}); err != nil {
			t.Fatalf("SaveMeta(%s): %v", id, err)
		}
	}

	mux := &Mux{workDir: workDir}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/sessions", nil)
	mux.handleListSessions(rec, req)

	var response struct {
		Sessions []struct {
			ID          string `json:"id"`
			CtxwinUsed  int    `json:"ctxwin_used"`
			CtxwinLimit int    `json:"ctxwin_limit"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	got := make(map[string][2]int, len(response.Sessions))
	for _, item := range response.Sessions {
		got[item.ID] = [2]int{item.CtxwinUsed, item.CtxwinLimit}
	}
	for id, snapshot := range snapshots {
		want := [2]int{snapshot.used, snapshot.limit}
		if got["l2:"+id] != want {
			t.Errorf("%s ctxwin = %v, want %v", id, got["l2:"+id], want)
		}
	}
}
