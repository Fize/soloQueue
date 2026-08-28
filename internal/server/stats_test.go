package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/infra/db"
)

func TestStatsOverviewUsesRequestedTimezoneAndEnvelope(t *testing.T) {
	database := openStatsTestDB(t)
	insertStatsMetric(t, database, db.LLMCallMetric{
		CallID: "call-1", TeamID: "team-a", Origin: "desktop", UsageType: "chat",
		TaskType: "engineering", ProviderID: "provider-a", ModelID: "model-a",
		StartedAt:  time.Date(2026, 8, 11, 16, 29, 59, 0, time.UTC),
		FinishedAt: time.Date(2026, 8, 11, 16, 30, 0, 0, time.UTC),
		Status:     "success", DurationMS: 1000, PromptTokens: 100,
		CompletionTokens: 50, ReasoningTokens: 20, TotalTokens: 150,
		CacheHitTokens: 80, CacheMissTokens: 20,
		ReasoningDetailsReported: true, CacheDetailsReported: true,
	})

	mux := NewMux(t.TempDir(), nil, WithSharedDB(database))
	defer mux.Close()
	query := url.Values{
		"from":     {"2026-08-11T16:00:00Z"},
		"to":       {"2026-08-11T18:00:00Z"},
		"timezone": {"Asia/Shanghai"},
		"team_id":  {"team-a"},
	}
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, newStatsRequest("/api/stats/overview?"+query.Encode()))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	assertNoCostFields(t, recorder.Body.String())
	var response struct {
		Data struct {
			Meta struct {
				Timezone   string `json:"timezone"`
				BucketSize string `json:"bucket_size"`
			} `json:"meta"`
			Summary struct {
				TotalTokens     int     `json:"total_tokens"`
				ReasoningTokens int     `json:"reasoning_tokens"`
				RequestCount    int     `json:"request_count"`
				SuccessRate     float64 `json:"success_rate"`
			} `json:"summary"`
			Series []struct {
				Start string `json:"start"`
			} `json:"series"`
		} `json:"data"`
		Error *string `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Error != nil {
		t.Fatalf("error = %q", *response.Error)
	}
	if response.Data.Meta.Timezone != "Asia/Shanghai" || response.Data.Meta.BucketSize != "hour" {
		t.Fatalf("meta = %+v", response.Data.Meta)
	}
	if response.Data.Summary.TotalTokens != 150 || response.Data.Summary.ReasoningTokens != 20 || response.Data.Summary.RequestCount != 1 || response.Data.Summary.SuccessRate != 1 {
		t.Fatalf("summary = %+v", response.Data.Summary)
	}
	if len(response.Data.Series) != 2 || response.Data.Series[0].Start != "2026-08-11T16:00:00Z" {
		t.Fatalf("series = %+v", response.Data.Series)
	}
}

func TestStatsCoverageDistinguishesLegacyMissingAndReportedDetails(t *testing.T) {
	database := openStatsTestDB(t)
	now := time.Now().UTC().Truncate(time.Second)
	insertStatsMetric(t, database, db.LLMCallMetric{
		CallID: "call-known", Origin: "desktop", UsageType: "chat", TaskType: "engineering",
		ProviderID: "provider-a", ModelID: "model-a", StartedAt: now, FinishedAt: now.Add(time.Second),
		Status: "success", DurationMS: 1000, TotalTokens: 10, CacheDetailsReported: true,
	})
	insertStatsMetric(t, database, db.LLMCallMetric{
		CallID: "call-missing", UsageType: "chat", ProviderID: "provider-a", ModelID: "model-a",
		StartedAt: now, FinishedAt: now.Add(2 * time.Second), Status: "success", DurationMS: 500, TotalTokens: 20,
	})
	if err := database.InsertTokenUsage(context.Background(), "chat", "", "provider-a/model-a", 1, 1, 2, 0, 0); err != nil {
		t.Fatalf("InsertTokenUsage: %v", err)
	}

	mux := NewMux(t.TempDir(), nil, WithSharedDB(database))
	defer mux.Close()
	from := now.Add(-time.Hour).Format(time.RFC3339)
	to := now.Add(time.Hour).Format(time.RFC3339)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, newStatsRequest("/api/stats/overview?timezone=UTC&from="+url.QueryEscape(from)+"&to="+url.QueryEscape(to)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Data struct {
			Meta struct {
				Coverage struct {
					TotalRows  int64              `json:"total_rows"`
					LegacyRows int64              `json:"legacy_rows"`
					Origin     statsCoverageCount `json:"origin"`
					TaskType   statsCoverageCount `json:"task_type"`
					Cache      statsCoverageCount `json:"cache_detail"`
					Reasoning  statsCoverageCount `json:"reasoning_detail"`
				} `json:"coverage"`
			} `json:"meta"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	coverage := response.Data.Meta.Coverage
	if coverage.TotalRows != 3 || coverage.LegacyRows != 1 || coverage.Origin.KnownRows != 1 || coverage.Origin.ApplicableRows != 2 {
		t.Fatalf("coverage = %+v", coverage)
	}
	if coverage.TaskType.KnownRows != 1 || coverage.TaskType.ApplicableRows != 1 {
		t.Fatalf("task coverage = %+v", coverage.TaskType)
	}
	if coverage.Cache.KnownRows != 1 || coverage.Cache.ApplicableRows != 2 || coverage.Reasoning.KnownRows != 0 {
		t.Fatalf("provider coverage = cache %+v reasoning %+v", coverage.Cache, coverage.Reasoning)
	}
	if strings.Contains(recorder.Body.String(), "retry_count") {
		t.Fatal("events contract must not expose retry_count")
	}
}

func TestStatsEndpointsApplyFiltersAndPaginate(t *testing.T) {
	database := openStatsTestDB(t)
	now := time.Now().UTC().Truncate(time.Second)
	for index, model := range []string{"model-a", "model-b"} {
		insertStatsMetric(t, database, db.LLMCallMetric{
			CallID: "call-" + model, TeamID: "team-a", Origin: "desktop", UsageType: "chat",
			TaskType: "engineering", ProviderID: "provider-a", ModelID: model,
			StartedAt:  now.Add(time.Duration(index) * time.Second),
			FinishedAt: now.Add(time.Duration(index+1) * time.Second),
			Status:     "success", DurationMS: 1000, TotalTokens: (index + 1) * 100,
		})
	}
	mux := NewMux(t.TempDir(), nil, WithSharedDB(database))
	defer mux.Close()
	from := now.Add(-time.Hour).Format(time.RFC3339)
	to := now.Add(time.Hour).Format(time.RFC3339)
	base := "from=" + url.QueryEscape(from) + "&to=" + url.QueryEscape(to) + "&timezone=UTC"

	for _, path := range []string{
		"/api/stats/breakdowns?" + base + "&dimension=model",
		"/api/stats/filters?" + base,
		"/api/stats/activity?timezone=UTC&days=7",
	} {
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, newStatsRequest(path))
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s status = %d, body = %s", path, recorder.Code, recorder.Body.String())
		}
		assertStatsEnvelope(t, recorder.Body.Bytes())
		assertNoCostFields(t, recorder.Body.String())
	}

	first := httptest.NewRecorder()
	mux.ServeHTTP(first, newStatsRequest("/api/stats/events?"+base+"&limit=1"))
	if first.Code != http.StatusOK {
		t.Fatalf("first page status = %d, body = %s", first.Code, first.Body.String())
	}
	assertNoCostFields(t, first.Body.String())
	if strings.Contains(first.Body.String(), "retry_count") {
		t.Fatal("events contract must not expose retry_count")
	}
	var page struct {
		Data struct {
			Items []struct {
				CallID string `json:"call_id"`
			} `json:"items"`
			Next *string `json:"next_cursor"`
		} `json:"data"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode first page: %v", err)
	}
	if len(page.Data.Items) != 1 || page.Data.Next == nil {
		t.Fatalf("first page = %+v", page.Data)
	}
	second := httptest.NewRecorder()
	mux.ServeHTTP(second, newStatsRequest("/api/stats/events?"+base+"&limit=1&cursor="+url.QueryEscape(*page.Data.Next)))
	if second.Code != http.StatusOK {
		t.Fatalf("second page status = %d, body = %s", second.Code, second.Body.String())
	}
	assertNoCostFields(t, second.Body.String())
	var secondPage struct {
		Data struct {
			Items []struct {
				CallID string `json:"call_id"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(second.Body.Bytes(), &secondPage); err != nil {
		t.Fatalf("decode second page: %v", err)
	}
	if len(secondPage.Data.Items) != 1 || secondPage.Data.Items[0].CallID == page.Data.Items[0].CallID {
		t.Fatalf("second page = %+v", secondPage.Data)
	}
}

func TestStatsVersionedRoutesAreNotRegistered(t *testing.T) {
	database := openStatsTestDB(t)
	mux := NewMux(t.TempDir(), nil, WithSharedDB(database))
	defer mux.Close()

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, newStatsRequest("/api/stats/v2/overview?timezone=Not/AZone"))
	if recorder.Code == http.StatusBadRequest || strings.Contains(recorder.Body.String(), "invalid_timezone") {
		t.Fatalf("versioned statistics handler is still registered: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestStatsRejectsInvalidTimezone(t *testing.T) {
	database := openStatsTestDB(t)
	mux := NewMux(t.TempDir(), nil, WithSharedDB(database))
	defer mux.Close()
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, newStatsRequest("/api/stats/overview?timezone=Not/AZone"))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Data  any    `json:"data"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Data != nil || response.Error == "" {
		t.Fatalf("response = %+v", response)
	}
}

func TestStatsRejectsRemovedWorkflowOrigin(t *testing.T) {
	database := openStatsTestDB(t)
	mux := NewMux(t.TempDir(), nil, WithSharedDB(database))
	defer mux.Close()

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, newStatsRequest("/api/stats/overview?origin=workflow"))
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "invalid_origin") {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func openStatsTestDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "stats.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func insertStatsMetric(t *testing.T, database *db.DB, metric db.LLMCallMetric) {
	t.Helper()
	if err := database.InsertLLMCallMetric(context.Background(), metric); err != nil {
		t.Fatalf("InsertLLMCallMetric: %v", err)
	}
}

func assertStatsEnvelope(t *testing.T, body []byte) {
	t.Helper()
	var response map[string]json.RawMessage
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if _, ok := response["data"]; !ok {
		t.Fatalf("missing data: %s", body)
	}
	if _, ok := response["error"]; !ok {
		t.Fatalf("missing error: %s", body)
	}
}

func assertNoCostFields(t *testing.T, body string) {
	t.Helper()
	for _, field := range []string{"estimated_cost", "pricing_coverage_pct"} {
		if strings.Contains(body, field) {
			t.Fatalf("unexpected cost field %q in response: %s", field, body)
		}
	}
}

func newStatsRequest(path string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Host = "localhost:8765"
	request.RemoteAddr = "127.0.0.1:12345"
	return request
}
