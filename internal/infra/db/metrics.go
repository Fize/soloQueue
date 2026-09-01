package db

import (
	"context"
	"fmt"
)

// MetricCategory constants
const (
	MetricCategoryTokenUsage           = "token_usage"
	MetricCategoryRouterClassification = "router_classification"
)

// InsertTokenUsage inserts a record for token usage.
func (db *DB) InsertTokenUsage(
	ctx context.Context,
	usageType string,
	teamID string,
	modelName string,
	promptTokens int,
	completionTokens int,
	totalTokens int,
	cacheHitTokens int,
	cacheMissTokens int,
) error {
	db.WMu.Lock()
	defer db.WMu.Unlock()

	query := `
		INSERT INTO usage_metrics (
			metric_category, usage_type, team_id, model_name,
			prompt_tokens, completion_tokens, total_tokens,
			cache_hit_tokens, cache_miss_tokens
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := db.ExecContext(ctx, query,
		MetricCategoryTokenUsage,
		usageType,
		teamID,
		modelName,
		promptTokens,
		completionTokens,
		totalTokens,
		cacheHitTokens,
		cacheMissTokens,
	)
	if err != nil {
		return fmt.Errorf("insert token usage: %w", err)
	}
	return nil
}

// InsertRouterClassification inserts a record for router classification.
func (db *DB) InsertRouterClassification(
	ctx context.Context,
	usageType string,
	teamID string,
	classificationLevel string,
	classificationSource string,
) error {
	db.WMu.Lock()
	defer db.WMu.Unlock()

	query := `
		INSERT INTO usage_metrics (
			metric_category, usage_type, team_id,
			classification_level, classification_source
		) VALUES (?, ?, ?, ?, ?)
	`
	_, err := db.ExecContext(ctx, query,
		MetricCategoryRouterClassification,
		usageType,
		teamID,
		classificationLevel,
		classificationSource,
	)
	if err != nil {
		return fmt.Errorf("insert router classification: %w", err)
	}
	return nil
}

// AggregatedTokenUsage represents grouped token usage over a time period.
type AggregatedTokenUsage struct {
	Period           string `json:"period"`
	UsageType        string `json:"usage_type"`
	TeamID           string `json:"team_id"`
	ModelName        string `json:"model_name"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
	CacheHitTokens   int    `json:"cache_hit_tokens"`
	CacheMissTokens  int    `json:"cache_miss_tokens"`
}

// GetTokenUsageAggregated returns aggregated token stats grouped by the specified timeframe (minutely, hourly, daily, weekly, monthly).
// If teamID is non-empty, it filters by that team. If usageType is non-empty, it filters by usage type.
// If fromDate or toDate is non-empty, it filters the timestamp column accordingly (format: "YYYY-MM-DD HH:MM:SS").
func (db *DB) GetTokenUsageAggregated(ctx context.Context, timeframe string, teamID string, usageType string, fromDate string, toDate string) ([]AggregatedTokenUsage, error) {
	var periodExpr string
	switch timeframe {
	case "minutely":
		periodExpr = "strftime('%Y-%m-%d %H:%M:00', timestamp)"
	case "hourly":
		periodExpr = "strftime('%Y-%m-%d %H:00:00', timestamp)"
	case "daily":
		periodExpr = "datetime(timestamp, 'start of day')"
	case "weekly":
		periodExpr = "datetime(timestamp, 'weekday 1')" // Start of week (Monday)
	case "monthly":
		periodExpr = "datetime(timestamp, 'start of month')"
	default:
		periodExpr = "datetime(timestamp, 'start of day')"
	}

	query := fmt.Sprintf(`
		SELECT 
			%s as period,
			usage_type,
			team_id,
			model_name,
			SUM(prompt_tokens) as prompt_tokens,
			SUM(completion_tokens) as completion_tokens,
			SUM(total_tokens) as total_tokens,
			SUM(cache_hit_tokens) as cache_hit_tokens,
			SUM(cache_miss_tokens) as cache_miss_tokens
		FROM usage_metrics
		WHERE metric_category = ?
	`, periodExpr)

	args := []interface{}{MetricCategoryTokenUsage}

	if teamID == "__solo__" {
		query += " AND team_id = ''"
	} else if teamID != "" {
		query += " AND team_id = ?"
		args = append(args, teamID)
	}
	if usageType != "" {
		query += " AND usage_type = ?"
		args = append(args, usageType)
	}
	if fromDate != "" {
		query += " AND timestamp >= ?"
		args = append(args, fromDate)
	}
	if toDate != "" {
		query += " AND timestamp <= ?"
		args = append(args, toDate)
	}

	query += ` GROUP BY period, usage_type, team_id, model_name ORDER BY period DESC`

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query token usage: %w", err)
	}
	defer rows.Close()

	var results []AggregatedTokenUsage
	for rows.Next() {
		var item AggregatedTokenUsage
		if err := rows.Scan(
			&item.Period,
			&item.UsageType,
			&item.TeamID,
			&item.ModelName,
			&item.PromptTokens,
			&item.CompletionTokens,
			&item.TotalTokens,
			&item.CacheHitTokens,
			&item.CacheMissTokens,
		); err != nil {
			return nil, fmt.Errorf("scan token usage: %w", err)
		}
		results = append(results, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return results, nil
}

// AggregatedRouterStats represents grouped router classifications over a time period.
type AggregatedRouterStats struct {
	Period               string `json:"period"`
	ClassificationSource string `json:"classification_source"`
	ClassificationLevel  string `json:"classification_level"`
	Count                int    `json:"count"`
}

// GetRouterStatsAggregated returns aggregated router stats grouped by timeframe (minutely, hourly, daily, weekly, monthly).
// If teamID is non-empty, it filters by that team.
// If fromDate or toDate is non-empty, it filters the timestamp column accordingly (format: "YYYY-MM-DD HH:MM:SS").
func (db *DB) GetRouterStatsAggregated(ctx context.Context, timeframe string, teamID string, fromDate string, toDate string) ([]AggregatedRouterStats, error) {
	var periodExpr string
	switch timeframe {
	case "minutely":
		periodExpr = "strftime('%Y-%m-%d %H:%M:00', timestamp)"
	case "hourly":
		periodExpr = "strftime('%Y-%m-%d %H:00:00', timestamp)"
	case "daily":
		periodExpr = "datetime(timestamp, 'start of day')"
	case "weekly":
		periodExpr = "datetime(timestamp, 'weekday 1')" // Start of week (Monday)
	case "monthly":
		periodExpr = "datetime(timestamp, 'start of month')"
	default:
		periodExpr = "datetime(timestamp, 'start of day')"
	}

	whereClause := "WHERE metric_category = ?"
	args := []any{MetricCategoryRouterClassification}

	if teamID == "__solo__" {
		whereClause += " AND team_id = ''"
	} else if teamID != "" {
		whereClause += " AND team_id = ?"
		args = append(args, teamID)
	}
	if fromDate != "" {
		whereClause += " AND timestamp >= ?"
		args = append(args, fromDate)
	}
	if toDate != "" {
		whereClause += " AND timestamp <= ?"
		args = append(args, toDate)
	}

	query := fmt.Sprintf(`
		SELECT 
			%s as period,
			classification_source,
			classification_level,
			COUNT(*) as count
		FROM usage_metrics
		%s
		GROUP BY period, classification_source, classification_level
		ORDER BY period DESC
	`, periodExpr, whereClause)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query router stats: %w", err)
	}
	defer rows.Close()

	var results []AggregatedRouterStats
	for rows.Next() {
		var item AggregatedRouterStats
		if err := rows.Scan(
			&item.Period,
			&item.ClassificationSource,
			&item.ClassificationLevel,
			&item.Count,
		); err != nil {
			return nil, fmt.Errorf("scan router stats: %w", err)
		}
		results = append(results, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return results, nil
}

// GetDistinctTeams returns all distinct non-empty team_id values from usage_metrics.
func (db *DB) GetDistinctTeams(ctx context.Context) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT team_id FROM usage_metrics WHERE team_id != '' AND team_id IS NOT NULL ORDER BY team_id`)
	if err != nil {
		return nil, fmt.Errorf("query distinct teams: %w", err)
	}
	defer rows.Close()

	var teams []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, fmt.Errorf("scan team: %w", err)
		}
		teams = append(teams, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return teams, nil
}

// ClassifierDecision is a single classification decision record for optimization analysis.
type ClassifierDecision struct {
	PromptTrunc   string
	FTLevel       string
	FTConfidence  int
	LLMInvoked    int
	LLMLevel      string
	LLMConfidence int
	LLMError      string
	FinalLevel    string
	FinalSource   string
	HybridApplied int
	PriorLevel    string
}

// InsertClassifierDecision records a classification decision for optimization analysis.
func (db *DB) InsertClassifierDecision(ctx context.Context, d ClassifierDecision) error {
	db.WMu.Lock()
	defer db.WMu.Unlock()

	query := `
		INSERT INTO classifier_decisions (
			prompt_trunc, ft_level, ft_confidence,
			llm_invoked, llm_level, llm_confidence, llm_error,
			final_level, final_source, hybrid_applied, prior_level
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := db.ExecContext(ctx, query,
		d.PromptTrunc, d.FTLevel, d.FTConfidence,
		d.LLMInvoked, d.LLMLevel, d.LLMConfidence, d.LLMError,
		d.FinalLevel, d.FinalSource, d.HybridApplied, d.PriorLevel,
	)
	if err != nil {
		return fmt.Errorf("insert classifier decision: %w", err)
	}
	return nil
}

// AggregatedClassifierStats represents aggregated classifier decision stats over a time period.
type AggregatedClassifierStats struct {
	Period        string  `json:"period"`
	FTCount       int     `json:"ft_count"`
	LLMCount      int     `json:"llm_count"`
	LLMErrorCount int     `json:"llm_error_count"`
	AgreedCount   int     `json:"agreed_count"`
	AvgFTConf     float64 `json:"avg_ft_conf"`
	AvgLLMConf    float64 `json:"avg_llm_conf"`
	TotalCount    int     `json:"total_count"`
}

// GetClassifierStatsAggregated returns aggregated classifier decision stats grouped by timeframe.
func (db *DB) GetClassifierStatsAggregated(ctx context.Context, timeframe string, fromDate string, toDate string) ([]AggregatedClassifierStats, error) {
	var periodExpr string
	switch timeframe {
	case "minutely":
		periodExpr = "strftime('%Y-%m-%d %H:%M:00', timestamp)"
	case "hourly":
		periodExpr = "strftime('%Y-%m-%d %H:00:00', timestamp)"
	case "daily":
		periodExpr = "datetime(timestamp, 'start of day')"
	case "weekly":
		periodExpr = "datetime(timestamp, 'weekday 1')"
	case "monthly":
		periodExpr = "datetime(timestamp, 'start of month')"
	default:
		periodExpr = "datetime(timestamp, 'start of day')"
	}

	whereClause := "WHERE 1=1"
	args := []any{}
	if fromDate != "" {
		whereClause += " AND timestamp >= ?"
		args = append(args, fromDate)
	}
	if toDate != "" {
		whereClause += " AND timestamp <= ?"
		args = append(args, toDate)
	}

	query := fmt.Sprintf(`
		SELECT
			%s as period,
			SUM(CASE WHEN final_source = 'fast-track' THEN 1 ELSE 0 END) as ft_count,
			SUM(CASE WHEN llm_invoked = 1 AND llm_error = '' THEN 1 ELSE 0 END) as llm_count,
			SUM(CASE WHEN llm_error != '' THEN 1 ELSE 0 END) as llm_error_count,
			SUM(CASE WHEN llm_invoked = 1 AND ft_level = llm_level THEN 1 ELSE 0 END) as agreed_count,
			COALESCE(AVG(ft_confidence), 0) as avg_ft_conf,
			COALESCE(AVG(CASE WHEN llm_invoked = 1 THEN llm_confidence ELSE NULL END), 0) as avg_llm_conf,
			COUNT(*) as total_count
		FROM classifier_decisions
		%s
		GROUP BY period
		ORDER BY period DESC
	`, periodExpr, whereClause)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query classifier stats: %w", err)
	}
	defer rows.Close()

	var results []AggregatedClassifierStats
	for rows.Next() {
		var item AggregatedClassifierStats
		if err := rows.Scan(
			&item.Period, &item.FTCount, &item.LLMCount, &item.LLMErrorCount,
			&item.AgreedCount, &item.AvgFTConf, &item.AvgLLMConf, &item.TotalCount,
		); err != nil {
			return nil, fmt.Errorf("scan classifier stats: %w", err)
		}
		results = append(results, item)
	}
	return results, rows.Err()
}
