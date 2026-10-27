package db

import (
	"context"
	"time"
)

// ObservationCalls uses the request identity rather than scanning all historical metrics.
func (d *DB) ObservationCalls(ctx context.Context, sessionID, requestID string) ([]LLMCallMetric, error) {
	rows, e := d.QueryContext(ctx, `SELECT call_id,run_id,agent_id,provider_id,model_id,usage_type,status,started_at,finished_at,duration_ms,retry_count,prompt_tokens,completion_tokens,total_tokens,attempt_count,attempts_reported FROM llm_call_metrics WHERE session_id=? AND request_id=? ORDER BY started_at,call_id`, sessionID, requestID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []LLMCallMetric{}
	for rows.Next() {
		var m LLMCallMetric
		var start, end string
		if e = rows.Scan(&m.CallID, &m.RunID, &m.AgentID, &m.ProviderID, &m.ModelID, &m.UsageType, &m.Status, &start, &end, &m.DurationMS, &m.RetryCount, &m.PromptTokens, &m.CompletionTokens, &m.TotalTokens, &m.AttemptCount, &m.AttemptsReported); e != nil {
			return nil, e
		}
		m.StartedAt, _ = time.Parse(time.RFC3339Nano, start)
		m.FinishedAt, _ = time.Parse(time.RFC3339Nano, end)
		out = append(out, m)
	}
	return out, rows.Err()
}
