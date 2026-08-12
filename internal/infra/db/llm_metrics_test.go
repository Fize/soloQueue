package db

import (
	"context"
	"testing"
	"time"
)

func TestInsertAndListLLMCallMetrics(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	started := time.Date(2026, 8, 11, 8, 0, 0, 0, time.UTC)
	finished := started.Add(1250 * time.Millisecond)
	metric := LLMCallMetric{
		CallID:                   "call-1",
		RequestID:                "request-1",
		SessionID:                "session-1",
		RunID:                    "run-1",
		AgentID:                  "agent-1",
		TeamID:                   "team-a",
		Origin:                   "desktop",
		UsageType:                "chat",
		TaskType:                 "engineering",
		ProviderID:               "provider-a",
		ModelID:                  "model-a",
		StartedAt:                started,
		FinishedAt:               finished,
		Status:                   "success",
		FinishReason:             "stop",
		DurationMS:               1250,
		PromptTokens:             100,
		CompletionTokens:         50,
		ReasoningTokens:          20,
		TotalTokens:              150,
		CacheHitTokens:           80,
		CacheMissTokens:          20,
		ReasoningDetailsReported: true,
		CacheDetailsReported:     true,
	}
	if err := database.InsertLLMCallMetric(context.Background(), metric); err != nil {
		t.Fatalf("InsertLLMCallMetric: %v", err)
	}

	rows, err := database.ListLLMCallMetrics(context.Background(), started.Add(-time.Minute), finished.Add(time.Minute))
	if err != nil {
		t.Fatalf("ListLLMCallMetrics: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("row count = %d, want 1", len(rows))
	}
	got := rows[0]
	if got.CallID != metric.CallID || got.ReasoningTokens != 20 || got.Status != "success" || !got.ReasoningDetailsReported || !got.CacheDetailsReported {
		t.Fatalf("metric = %+v", got)
	}
	if got.Legacy {
		t.Fatal("new metric must not be marked legacy")
	}
}

func TestMigrationRemovesEstimatedCostColumn(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	if _, err := database.Exec(`ALTER TABLE llm_call_metrics ADD COLUMN estimated_cost_microusd INTEGER`); err != nil {
		t.Fatalf("add legacy estimated cost column: %v", err)
	}
	metric := LLMCallMetric{
		CallID: "call-v16", StartedAt: time.Now().UTC(), FinishedAt: time.Now().UTC(),
		Status: "success", UsageType: "chat", TotalTokens: 10,
	}
	if err := database.InsertLLMCallMetric(context.Background(), metric); err != nil {
		t.Fatalf("insert legacy metric: %v", err)
	}
	if _, err := database.Exec(`UPDATE llm_call_metrics SET estimated_cost_microusd = 1250 WHERE call_id = ?`, metric.CallID); err != nil {
		t.Fatalf("seed legacy estimated cost: %v", err)
	}

	if err := database.migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	var costColumns int
	if err := database.QueryRow(`SELECT count(*) FROM pragma_table_info('llm_call_metrics') WHERE name = 'estimated_cost_microusd'`).Scan(&costColumns); err != nil {
		t.Fatalf("inspect migrated schema: %v", err)
	}
	if costColumns != 0 {
		t.Fatalf("estimated cost columns = %d, want 0", costColumns)
	}
	var totalTokens int
	if err := database.QueryRow(`SELECT total_tokens FROM llm_call_metrics WHERE call_id = ?`, metric.CallID).Scan(&totalTokens); err != nil {
		t.Fatalf("read migrated metric: %v", err)
	}
	if totalTokens != metric.TotalTokens {
		t.Fatalf("total tokens = %d, want %d", totalTokens, metric.TotalTokens)
	}
}

func TestListLLMCallMetricsIncludesLegacyRows(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	if err := database.InsertTokenUsage(context.Background(), "chat", "team-a", "provider-a/model-a", 10, 5, 15, 4, 6); err != nil {
		t.Fatalf("InsertTokenUsage: %v", err)
	}

	rows, err := database.ListLLMCallMetrics(context.Background(), time.Now().UTC().Add(-time.Hour), time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatalf("ListLLMCallMetrics: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("row count = %d, want 1", len(rows))
	}
	got := rows[0]
	if !got.Legacy || got.ProviderID != "provider-a" || got.ModelID != "model-a" {
		t.Fatalf("legacy metric = %+v", got)
	}
	if got.Status != "unknown" || got.Origin != "unknown" || got.TaskType != "unknown" || !got.CacheDetailsReported {
		t.Fatalf("legacy dimensions = %+v", got)
	}
}

func TestMigrationAddsProviderDetailCoverageColumns(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	for _, column := range []string{"reasoning_details_reported", "cache_details_reported"} {
		var count int
		if err := database.QueryRow(`SELECT count(*) FROM pragma_table_info('llm_call_metrics') WHERE name = ?`, column).Scan(&count); err != nil {
			t.Fatalf("inspect %s: %v", column, err)
		}
		if count != 1 {
			t.Fatalf("column %s count = %d, want 1", column, count)
		}
	}
}

func TestLLMCallMetricsMigrationIsIdempotent(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	if err := database.migrate(); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	var version int
	if err := database.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if version != schemaVersion {
		t.Fatalf("schema version = %d, want %d", version, schemaVersion)
	}
}

func TestInsertRouteDecisionMetric(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	decision := RouteDecisionMetric{
		DecisionID: "decision-1", RequestID: "request-1", TeamID: "team-a",
		TaskType: "engineering", ProviderID: "provider-a", ModelID: "model-a",
		ClassificationSource: "local", Status: "success",
		DecidedAt: time.Date(2026, 8, 11, 8, 0, 0, 0, time.UTC),
	}
	if err := database.InsertRouteDecisionMetric(context.Background(), decision); err != nil {
		t.Fatalf("InsertRouteDecisionMetric: %v", err)
	}
	var taskType, source string
	if err := database.QueryRow(`SELECT task_type, classification_source FROM route_decisions_v2 WHERE decision_id = ?`, decision.DecisionID).Scan(&taskType, &source); err != nil {
		t.Fatalf("query route decision: %v", err)
	}
	if taskType != "engineering" || source != "local" {
		t.Fatalf("route decision = %s/%s", taskType, source)
	}
}
