package db

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// LLMCallMetric is one completed LLM request. Empty correlation fields are
// stored without inventing identifiers, so coverage remains explicit.
type LLMCallMetric struct {
	CallID                   string
	RequestID                string
	SessionID                string
	RunID                    string
	AgentID                  string
	TeamID                   string
	Origin                   string
	UsageType                string
	TaskType                 string
	ProviderID               string
	ModelID                  string
	StartedAt                time.Time
	FinishedAt               time.Time
	Status                   string
	FinishReason             string
	ErrorCode                string
	RetryCount               int
	DurationMS               int64
	PromptTokens             int
	CompletionTokens         int
	ReasoningTokens          int
	TotalTokens              int
	CacheHitTokens           int
	CacheMissTokens          int
	ReasoningDetailsReported bool
	CacheDetailsReported     bool
	Legacy                   bool
}

// RouteDecisionMetric is one task-routing decision.
type RouteDecisionMetric struct {
	DecisionID           string
	RequestID            string
	SessionID            string
	RunID                string
	TeamID               string
	TaskType             string
	ProviderID           string
	ModelID              string
	ClassificationSource string
	Status               string
	DecidedAt            time.Time
}

// InsertLLMCallMetric persists request-level telemetry.
func (db *DB) InsertLLMCallMetric(ctx context.Context, metric LLMCallMetric) error {
	db.WMu.Lock()
	defer db.WMu.Unlock()

	_, err := db.ExecContext(ctx, `
		INSERT INTO llm_call_metrics (
			call_id, request_id, session_id, run_id, agent_id, team_id,
			origin, usage_type, task_type, provider_id, model_id,
			started_at, finished_at, status, finish_reason, error_code,
			retry_count, duration_ms, prompt_tokens, completion_tokens,
			reasoning_tokens, total_tokens, cache_hit_tokens, cache_miss_tokens,
			reasoning_details_reported, cache_details_reported
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		metric.CallID, metric.RequestID, metric.SessionID, metric.RunID,
		metric.AgentID, metric.TeamID, metric.Origin,
		metric.UsageType, metric.TaskType,
		metric.ProviderID, metric.ModelID,
		metric.StartedAt.UTC().Format(time.RFC3339Nano),
		metric.FinishedAt.UTC().Format(time.RFC3339Nano),
		valueOrUnknown(metric.Status), metric.FinishReason, metric.ErrorCode,
		metric.RetryCount, metric.DurationMS, metric.PromptTokens,
		metric.CompletionTokens, metric.ReasoningTokens, metric.TotalTokens,
		metric.CacheHitTokens, metric.CacheMissTokens,
		metric.ReasoningDetailsReported, metric.CacheDetailsReported,
	)
	if err != nil {
		return fmt.Errorf("insert llm call metric: %w", err)
	}
	return nil
}

// InsertRouteDecisionMetric persists routing diagnostics independently of LLM calls.
func (db *DB) InsertRouteDecisionMetric(ctx context.Context, metric RouteDecisionMetric) error {
	db.WMu.Lock()
	defer db.WMu.Unlock()
	_, err := db.ExecContext(ctx, `
		INSERT INTO route_decisions_v2 (
			decision_id, request_id, session_id, run_id, team_id, task_type,
			provider_id, model_id, classification_source, status, decided_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, metric.DecisionID, metric.RequestID, metric.SessionID, metric.RunID,
		metric.TeamID, valueOrUnknown(metric.TaskType), metric.ProviderID,
		metric.ModelID, metric.ClassificationSource, valueOrUnknown(metric.Status),
		metric.DecidedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("insert route decision metric: %w", err)
	}
	return nil
}

// ListLLMCallMetrics returns current and legacy events in descending completion order.
func (db *DB) ListLLMCallMetrics(ctx context.Context, from, to time.Time) ([]LLMCallMetric, error) {
	rows, err := db.QueryContext(ctx, `
		WITH events AS (
			SELECT
				call_id, request_id, session_id, run_id, agent_id, team_id,
				origin, usage_type, task_type, provider_id, model_id,
				started_at, finished_at, status, finish_reason, error_code,
				retry_count, duration_ms, prompt_tokens, completion_tokens,
				reasoning_tokens, total_tokens, cache_hit_tokens, cache_miss_tokens,
				reasoning_details_reported, cache_details_reported,
				0 AS legacy, unixepoch(finished_at) AS finished_epoch
			FROM llm_call_metrics
			UNION ALL
			SELECT
				'legacy:' || id, '', '', '', '', team_id,
				'unknown', usage_type, 'unknown', '', model_name,
				timestamp, timestamp, 'unknown', '', '',
				0, 0, prompt_tokens, completion_tokens,
				0, total_tokens, cache_hit_tokens, cache_miss_tokens,
				0, CASE WHEN cache_hit_tokens + cache_miss_tokens > 0 THEN 1 ELSE 0 END,
				1, unixepoch(timestamp)
			FROM usage_metrics
			WHERE metric_category = ?
		)
		SELECT
			call_id, request_id, session_id, run_id, agent_id, team_id,
			origin, usage_type, task_type, provider_id, model_id,
			started_at, finished_at, status, finish_reason, error_code,
			retry_count, duration_ms, prompt_tokens, completion_tokens,
			reasoning_tokens, total_tokens, cache_hit_tokens, cache_miss_tokens,
			reasoning_details_reported, cache_details_reported,
			legacy
		FROM events
		WHERE finished_epoch >= ? AND finished_epoch < ?
		ORDER BY finished_epoch DESC, call_id DESC
	`, MetricCategoryTokenUsage, from.UTC().Unix(), to.UTC().Unix())
	if err != nil {
		return nil, fmt.Errorf("query llm call metrics: %w", err)
	}
	defer rows.Close()

	var metrics []LLMCallMetric
	for rows.Next() {
		var metric LLMCallMetric
		var startedAt, finishedAt string
		var legacy int
		if err := rows.Scan(
			&metric.CallID, &metric.RequestID, &metric.SessionID, &metric.RunID,
			&metric.AgentID, &metric.TeamID, &metric.Origin, &metric.UsageType,
			&metric.TaskType, &metric.ProviderID, &metric.ModelID,
			&startedAt, &finishedAt, &metric.Status, &metric.FinishReason,
			&metric.ErrorCode, &metric.RetryCount, &metric.DurationMS,
			&metric.PromptTokens, &metric.CompletionTokens, &metric.ReasoningTokens,
			&metric.TotalTokens, &metric.CacheHitTokens, &metric.CacheMissTokens,
			&metric.ReasoningDetailsReported, &metric.CacheDetailsReported,
			&legacy,
		); err != nil {
			return nil, fmt.Errorf("scan llm call metric: %w", err)
		}
		metric.StartedAt, err = parseMetricTime(startedAt)
		if err != nil {
			return nil, fmt.Errorf("parse metric start time: %w", err)
		}
		metric.FinishedAt, err = parseMetricTime(finishedAt)
		if err != nil {
			return nil, fmt.Errorf("parse metric finish time: %w", err)
		}
		metric.Legacy = legacy != 0
		if metric.Legacy {
			metric.ProviderID, metric.ModelID = splitLegacyModel(metric.ModelID)
		}
		metrics = append(metrics, metric)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate llm call metrics: %w", err)
	}
	return metrics, nil
}

func splitLegacyModel(value string) (string, string) {
	provider, model, found := strings.Cut(value, "/")
	if !found {
		return "", value
	}
	return provider, model
}

func parseMetricTime(value string) (time.Time, error) {
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed.UTC(), nil
	}
	return time.ParseInLocation("2006-01-02 15:04:05", value, time.UTC)
}

func valueOrUnknown(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}
