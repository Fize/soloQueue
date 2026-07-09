package sqlitedb

import (
	"context"
	"fmt"
)

// MetricCategory constants
const (
	MetricCategoryTokenUsage         = "token_usage"
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

// GetTokenUsageAggregated returns aggregated token stats grouped by the specified timeframe (daily, weekly, monthly).
// If teamID is non-empty, it filters by that team. If usageType is non-empty, it filters by usage type.
func (db *DB) GetTokenUsageAggregated(ctx context.Context, timeframe string, teamID string, usageType string) ([]AggregatedTokenUsage, error) {
	var dateModifier string
	switch timeframe {
	case "daily":
		dateModifier = "start of day"
	case "weekly":
		dateModifier = "weekday 1" // Start of week (Monday)
	case "monthly":
		dateModifier = "start of month"
	default:
		dateModifier = "start of day"
	}

	// SQLite datetime grouping
	periodExpr := fmt.Sprintf("datetime(timestamp, '%s')", dateModifier)

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

// GetRouterStatsAggregated returns aggregated router stats grouped by timeframe.
// If teamID is non-empty, it filters by that team.
func (db *DB) GetRouterStatsAggregated(ctx context.Context, timeframe string, teamID string) ([]AggregatedRouterStats, error) {
	var dateModifier string
	switch timeframe {
	case "daily":
		dateModifier = "start of day"
	case "weekly":
		dateModifier = "weekday 1" // Start of week (Monday)
	case "monthly":
		dateModifier = "start of month"
	default:
		dateModifier = "start of day"
	}

	periodExpr := fmt.Sprintf("datetime(timestamp, '%s')", dateModifier)

	whereClause := "WHERE metric_category = ?"
	args := []any{MetricCategoryRouterClassification}

	if teamID == "__solo__" {
		whereClause += " AND team_id = ''"
	} else if teamID != "" {
		whereClause += " AND team_id = ?"
		args = append(args, teamID)
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
