package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/infra/db"
)

// Exercise the existing endpoint used by the Web Laya statistics panel against SQLite.
func TestRouterStatsLayaSourcesTimeAndTeam(t *testing.T) {
	database := openStatsTestDB(t)
	for _, item := range []struct{ team, source, task, timestamp string }{
		{"team-a", "laya", "engineering", "2026-10-26 00:00:00"},
		{"team-a", "laya", "engineering", "2026-10-26 01:00:00"},
		{"team-a", "laya", "research", "2026-10-26 02:00:00"},
		{"team-a", "laya_unavailable", "general", "2026-10-26 03:00:00"},
		{"team-a", "local", "engineering", "2026-10-26 03:00:00"},
		{"team-b", "laya", "general", "2026-10-26 03:00:00"},
		{"", "laya_unavailable", "general", "2026-10-26 03:00:00"},
		{"team-a", "laya", "general", "2026-10-25 23:59:59"},
		{"team-a", "laya", "general", "2026-10-27 00:00:01"},
	} {
		if err := database.InsertRouterClassification(context.Background(), "router", item.team, item.task, item.source); err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec(`UPDATE usage_metrics SET timestamp = ? WHERE id = (SELECT MAX(id) FROM usage_metrics)`, item.timestamp); err != nil {
			t.Fatal(err)
		}
	}
	mux := NewMux(t.TempDir(), nil, WithSharedDB(database))
	defer mux.Close()
	for _, tc := range []struct {
		team string
		want map[string]int
	}{
		{"team-a", map[string]int{"laya/engineering": 2, "laya/research": 1, "laya_unavailable/general": 1, "local/engineering": 1}},
		{"__solo__", map[string]int{"laya_unavailable/general": 1}},
		{"missing", map[string]int{}},
	} {
		t.Run(tc.team, func(t *testing.T) {
			query := url.Values{"timeframe": {"daily"}, "team_id": {tc.team}, "from": {"2026-10-26 00:00:00"}, "to": {"2026-10-27 00:00:00"}}
			recorder := httptest.NewRecorder()
			mux.ServeHTTP(recorder, newStatsRequest("/api/stats/router?"+query.Encode()))
			if recorder.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			var rows []db.AggregatedRouterStats
			if err := json.Unmarshal(recorder.Body.Bytes(), &rows); err != nil {
				t.Fatal(err)
			}
			got := map[string]int{}
			for _, row := range rows {
				got[row.ClassificationSource+"/"+row.ClassificationLevel] += row.Count
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
			for key, want := range tc.want {
				if got[key] != want {
					t.Errorf("%s=%d want %d", key, got[key], want)
				}
			}
		})
	}
}
