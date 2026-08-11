package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/infra/db"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

type statsV2Envelope struct {
	Data  any     `json:"data"`
	Error *string `json:"error"`
}

type statsV2Query struct {
	From       time.Time
	To         time.Time
	Location   *time.Location
	Timezone   string
	TeamID     string
	Origin     string
	UsageType  string
	TaskType   string
	ProviderID string
	ModelID    string
	Status     string
}

type statsV2Coverage struct {
	TotalRows            int64   `json:"total_rows"`
	LegacyRows           int64   `json:"legacy_rows"`
	CacheCoveragePct     float64 `json:"cache_coverage_pct"`
	ReasoningCoveragePct float64 `json:"reasoning_coverage_pct"`
}

type statsV2Meta struct {
	GeneratedAt time.Time       `json:"generated_at"`
	DataFrom    time.Time       `json:"data_from"`
	DataTo      time.Time       `json:"data_to"`
	Timezone    string          `json:"timezone"`
	BucketSize  string          `json:"bucket_size"`
	Coverage    statsV2Coverage `json:"coverage"`
}

type statsV2Metrics struct {
	TotalTokens      int64   `json:"total_tokens"`
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	ReasoningTokens  int64   `json:"reasoning_tokens"`
	CacheHitTokens   int64   `json:"cache_hit_tokens"`
	CacheMissTokens  int64   `json:"cache_miss_tokens"`
	RequestCount     int64   `json:"request_count"`
	SuccessCount     int64   `json:"success_count"`
	ErrorCount       int64   `json:"error_count"`
	CancelledCount   int64   `json:"cancelled_count"`
	TimeoutCount     int64   `json:"timeout_count"`
	SuccessRate      float64 `json:"success_rate"`
	CacheHitRate     float64 `json:"cache_hit_rate"`
	P95DurationMS    *int64  `json:"p95_duration_ms"`
}

type statsV2Delta struct {
	Current   float64  `json:"current"`
	Previous  float64  `json:"previous"`
	ChangePct *float64 `json:"change_pct"`
}

type statsV2SeriesPoint struct {
	Start   time.Time      `json:"start"`
	End     time.Time      `json:"end"`
	Metrics statsV2Metrics `json:"metrics"`
}

type statsV2Insight struct {
	ID        string   `json:"id"`
	Severity  string   `json:"severity"`
	Title     string   `json:"title"`
	Detail    string   `json:"detail"`
	Metric    *string  `json:"metric"`
	ChangePct *float64 `json:"change_pct"`
}

type statsV2Overview struct {
	Meta       statsV2Meta             `json:"meta"`
	Summary    statsV2Metrics          `json:"summary"`
	Comparison map[string]statsV2Delta `json:"comparison"`
	Series     []statsV2SeriesPoint    `json:"series"`
	Insights   []statsV2Insight        `json:"insights"`
}

type statsV2BreakdownItem struct {
	Key     string         `json:"key"`
	Label   string         `json:"label"`
	Metrics statsV2Metrics `json:"metrics"`
}

type statsV2Breakdown struct {
	Meta      statsV2Meta            `json:"meta"`
	Dimension string                 `json:"dimension"`
	Items     []statsV2BreakdownItem `json:"items"`
}

type statsV2Event struct {
	CallID           string    `json:"call_id"`
	RequestID        *string   `json:"request_id"`
	SessionID        *string   `json:"session_id"`
	RunID            *string   `json:"run_id"`
	AgentID          *string   `json:"agent_id"`
	TeamID           *string   `json:"team_id"`
	Origin           string    `json:"origin"`
	UsageType        string    `json:"usage_type"`
	TaskType         string    `json:"task_type"`
	ProviderID       string    `json:"provider_id"`
	ModelID          string    `json:"model_id"`
	StartedAt        time.Time `json:"started_at"`
	FinishedAt       time.Time `json:"finished_at"`
	Status           string    `json:"status"`
	FinishReason     *string   `json:"finish_reason"`
	ErrorCode        *string   `json:"error_code"`
	RetryCount       int       `json:"retry_count"`
	DurationMS       int64     `json:"duration_ms"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	ReasoningTokens  int       `json:"reasoning_tokens"`
	TotalTokens      int       `json:"total_tokens"`
	CacheHitTokens   int       `json:"cache_hit_tokens"`
	CacheMissTokens  int       `json:"cache_miss_tokens"`
	Legacy           bool      `json:"legacy"`
}

type statsV2Events struct {
	Meta       statsV2Meta    `json:"meta"`
	Items      []statsV2Event `json:"items"`
	NextCursor *string        `json:"next_cursor"`
}

type statsV2FilterOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type statsV2Filters struct {
	Meta       statsV2Meta           `json:"meta"`
	Teams      []statsV2FilterOption `json:"teams"`
	Origins    []statsV2FilterOption `json:"origins"`
	UsageTypes []statsV2FilterOption `json:"usage_types"`
	TaskTypes  []statsV2FilterOption `json:"task_types"`
	Providers  []statsV2FilterOption `json:"providers"`
	Models     []statsV2FilterOption `json:"models"`
	Statuses   []statsV2FilterOption `json:"statuses"`
}

type statsV2ActivityPoint struct {
	Date         string `json:"date"`
	TotalTokens  int64  `json:"total_tokens"`
	RequestCount int64  `json:"request_count"`
	Level        int    `json:"level"`
}

type statsV2Activity struct {
	Meta        statsV2Meta            `json:"meta"`
	ActiveDays  int                    `json:"active_days"`
	TotalTokens int64                  `json:"total_tokens"`
	Points      []statsV2ActivityPoint `json:"points"`
}

func (m *Mux) handleGetStatsV2Overview(w http.ResponseWriter, r *http.Request) {
	query, ok := m.parseStatsV2Query(w, r, true)
	if !ok {
		return
	}
	period := query.To.Sub(query.From)
	rows, ok := m.loadStatsV2Rows(w, r, query.From.Add(-period), query.To)
	if !ok {
		return
	}
	current := filterStatsV2Rows(rows, query, query.From, query.To)
	previous := filterStatsV2Rows(rows, query, query.From.Add(-period), query.From)
	bucketSize := statsV2BucketSize(period)
	summary := aggregateStatsV2Metrics(current)
	previousSummary := aggregateStatsV2Metrics(previous)
	data := statsV2Overview{
		Meta:       buildStatsV2Meta(query, bucketSize, current),
		Summary:    summary,
		Comparison: compareStatsV2Metrics(summary, previousSummary),
		Series:     buildStatsV2Series(current, query, bucketSize),
		Insights:   buildStatsV2Insights(summary, previousSummary),
	}
	m.writeStatsV2Data(w, data)
}

func (m *Mux) handleGetStatsV2Breakdowns(w http.ResponseWriter, r *http.Request) {
	query, ok := m.parseStatsV2Query(w, r, true)
	if !ok {
		return
	}
	dimension := r.URL.Query().Get("dimension")
	if !containsString([]string{"usage_type", "model", "task_type", "origin", "team", "status"}, dimension) {
		m.writeStatsV2Error(w, http.StatusBadRequest, "invalid_dimension: dimension is required and must be supported")
		return
	}
	rows, ok := m.loadStatsV2Rows(w, r, query.From, query.To)
	if !ok {
		return
	}
	rows = filterStatsV2Rows(rows, query, query.From, query.To)
	groups := make(map[string][]db.LLMCallMetric)
	for _, row := range rows {
		key := statsV2DimensionKey(row, dimension)
		groups[key] = append(groups[key], row)
	}
	items := make([]statsV2BreakdownItem, 0, len(groups))
	for key, group := range groups {
		label := key
		if dimension == "team" && key == "__solo__" {
			label = "Solo"
		}
		items = append(items, statsV2BreakdownItem{Key: key, Label: label, Metrics: aggregateStatsV2Metrics(group)})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Metrics.TotalTokens == items[j].Metrics.TotalTokens {
			return items[i].Key < items[j].Key
		}
		return items[i].Metrics.TotalTokens > items[j].Metrics.TotalTokens
	})
	m.writeStatsV2Data(w, statsV2Breakdown{
		Meta: buildStatsV2Meta(query, "none", rows), Dimension: dimension, Items: items,
	})
}

func (m *Mux) handleGetStatsV2Events(w http.ResponseWriter, r *http.Request) {
	query, ok := m.parseStatsV2Query(w, r, true)
	if !ok {
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 100 {
			m.writeStatsV2Error(w, http.StatusBadRequest, "invalid_limit: limit must be between 1 and 100")
			return
		}
		limit = value
	}
	rows, ok := m.loadStatsV2Rows(w, r, query.From, query.To)
	if !ok {
		return
	}
	rows = filterStatsV2Rows(rows, query, query.From, query.To)
	start := 0
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		cursor, err := decodeStatsV2Cursor(raw)
		if err != nil {
			m.writeStatsV2Error(w, http.StatusBadRequest, "invalid_cursor: cursor is malformed")
			return
		}
		found := false
		for index, row := range rows {
			if row.CallID == cursor.CallID && row.FinishedAt.Equal(cursor.FinishedAt) {
				start = index + 1
				found = true
				break
			}
		}
		if !found {
			m.writeStatsV2Error(w, http.StatusBadRequest, "invalid_cursor: cursor is no longer in the result set")
			return
		}
	}
	end := start + limit
	if end > len(rows) {
		end = len(rows)
	}
	items := make([]statsV2Event, 0, end-start)
	for _, row := range rows[start:end] {
		items = append(items, statsV2EventFromMetric(row))
	}
	var next *string
	if end < len(rows) && end > start {
		cursor := encodeStatsV2Cursor(rows[end-1])
		next = &cursor
	}
	m.writeStatsV2Data(w, statsV2Events{
		Meta: buildStatsV2Meta(query, "none", rows), Items: items, NextCursor: next,
	})
}

func (m *Mux) handleGetStatsV2Filters(w http.ResponseWriter, r *http.Request) {
	query, ok := m.parseStatsV2Query(w, r, true)
	if !ok {
		return
	}
	rows, ok := m.loadStatsV2Rows(w, r, query.From, query.To)
	if !ok {
		return
	}
	rows = filterStatsV2Rows(rows, statsV2Query{Location: query.Location, Timezone: query.Timezone}, query.From, query.To)
	m.writeStatsV2Data(w, statsV2Filters{
		Meta:       buildStatsV2Meta(query, "none", rows),
		Teams:      statsV2Options(rows, func(row db.LLMCallMetric) string { return teamDimension(row.TeamID) }),
		Origins:    statsV2Options(rows, func(row db.LLMCallMetric) string { return row.Origin }),
		UsageTypes: statsV2Options(rows, func(row db.LLMCallMetric) string { return row.UsageType }),
		TaskTypes:  statsV2Options(rows, func(row db.LLMCallMetric) string { return row.TaskType }),
		Providers:  statsV2Options(rows, func(row db.LLMCallMetric) string { return row.ProviderID }),
		Models:     statsV2Options(rows, func(row db.LLMCallMetric) string { return modelDimension(row) }),
		Statuses:   statsV2Options(rows, func(row db.LLMCallMetric) string { return row.Status }),
	})
}

func (m *Mux) handleGetStatsV2Activity(w http.ResponseWriter, r *http.Request) {
	query, ok := m.parseStatsV2Query(w, r, false)
	if !ok {
		return
	}
	days := 365
	if raw := r.URL.Query().Get("days"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 730 {
			m.writeStatsV2Error(w, http.StatusBadRequest, "invalid_days: days must be between 1 and 730")
			return
		}
		days = value
	}
	now := time.Now().In(query.Location)
	localEnd := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, query.Location)
	localStart := localEnd.AddDate(0, 0, -days)
	query.From, query.To = localStart.UTC(), localEnd.UTC()
	rows, ok := m.loadStatsV2Rows(w, r, query.From, query.To)
	if !ok {
		return
	}
	rows = filterStatsV2Rows(rows, query, query.From, query.To)
	byDate := make(map[string]*statsV2ActivityPoint, days)
	points := make([]statsV2ActivityPoint, 0, days)
	for index := 0; index < days; index++ {
		date := localStart.AddDate(0, 0, index).Format("2006-01-02")
		points = append(points, statsV2ActivityPoint{Date: date})
		byDate[date] = &points[len(points)-1]
	}
	var total int64
	for _, row := range rows {
		point := byDate[row.FinishedAt.In(query.Location).Format("2006-01-02")]
		if point == nil {
			continue
		}
		point.TotalTokens += int64(row.TotalTokens)
		point.RequestCount++
		total += int64(row.TotalTokens)
	}
	levels := statsV2ActivityLevels(points)
	activeDays := 0
	for index := range points {
		if points[index].RequestCount > 0 {
			activeDays++
		}
		points[index].Level = levels[points[index].TotalTokens]
	}
	m.writeStatsV2Data(w, statsV2Activity{
		Meta: buildStatsV2Meta(query, "day", rows), ActiveDays: activeDays, TotalTokens: total, Points: points,
	})
}

func (m *Mux) parseStatsV2Query(w http.ResponseWriter, r *http.Request, withRange bool) (statsV2Query, bool) {
	query := statsV2Query{
		Timezone:   r.URL.Query().Get("timezone"),
		TeamID:     r.URL.Query().Get("team_id"),
		Origin:     r.URL.Query().Get("origin"),
		UsageType:  r.URL.Query().Get("usage_type"),
		TaskType:   r.URL.Query().Get("task_type"),
		ProviderID: r.URL.Query().Get("provider_id"),
		ModelID:    r.URL.Query().Get("model_id"),
		Status:     r.URL.Query().Get("status"),
	}
	if query.Timezone == "" {
		query.Timezone = "UTC"
	}
	location, err := time.LoadLocation(query.Timezone)
	if err != nil {
		m.writeStatsV2Error(w, http.StatusBadRequest, "invalid_timezone: timezone must be a valid IANA timezone")
		return statsV2Query{}, false
	}
	query.Location = location
	validEnums := []struct {
		name  string
		value string
		valid []string
	}{
		{"origin", query.Origin, []string{"desktop", "portal", "api", "qq", "wechat", "cron", "workflow", "simulation", "unknown"}},
		{"usage_type", query.UsageType, []string{"chat", "router", "compactor", "memory", "simulation", "unknown"}},
		{"task_type", query.TaskType, []string{"general", "engineering", "research", "unknown"}},
		{"status", query.Status, []string{"success", "error", "cancelled", "timeout", "unknown"}},
	}
	for _, item := range validEnums {
		if item.value != "" && !containsString(item.valid, item.value) {
			m.writeStatsV2Error(w, http.StatusBadRequest, "invalid_"+item.name+": unsupported value")
			return statsV2Query{}, false
		}
	}
	if !withRange {
		return query, true
	}
	now := time.Now().UTC()
	query.To = now
	query.From = now.AddDate(0, 0, -30)
	if raw := r.URL.Query().Get("to"); raw != "" {
		query.To, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			m.writeStatsV2Error(w, http.StatusBadRequest, "invalid_to: to must be an RFC 3339 instant")
			return statsV2Query{}, false
		}
	}
	if raw := r.URL.Query().Get("from"); raw != "" {
		query.From, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			m.writeStatsV2Error(w, http.StatusBadRequest, "invalid_from: from must be an RFC 3339 instant")
			return statsV2Query{}, false
		}
	}
	query.From, query.To = query.From.UTC(), query.To.UTC()
	if !query.From.Before(query.To) {
		m.writeStatsV2Error(w, http.StatusBadRequest, "invalid_time_range: from must be earlier than to")
		return statsV2Query{}, false
	}
	return query, true
}

func (m *Mux) loadStatsV2Rows(w http.ResponseWriter, r *http.Request, from, to time.Time) ([]db.LLMCallMetric, bool) {
	if m.sharedDB == nil {
		m.writeStatsV2Error(w, http.StatusServiceUnavailable, "stats_unavailable: usage metrics storage is not configured")
		return nil, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rows, err := m.sharedDB.ListLLMCallMetrics(ctx, from, to)
	if err != nil {
		if m.log != nil {
			m.log.Error(logger.CatApp, "failed to query usage metrics", "err", err)
		}
		m.writeStatsV2Error(w, http.StatusInternalServerError, "stats_query_failed: failed to query usage metrics")
		return nil, false
	}
	return rows, true
}

func (m *Mux) writeStatsV2Data(w http.ResponseWriter, data any) {
	m.writeJSON(w, http.StatusOK, statsV2Envelope{Data: data, Error: nil})
}

func (m *Mux) writeStatsV2Error(w http.ResponseWriter, status int, message string) {
	m.writeJSON(w, status, statsV2Envelope{Data: nil, Error: &message})
}

func filterStatsV2Rows(rows []db.LLMCallMetric, query statsV2Query, from, to time.Time) []db.LLMCallMetric {
	result := make([]db.LLMCallMetric, 0, len(rows))
	for _, row := range rows {
		if row.FinishedAt.Before(from) || !row.FinishedAt.Before(to) {
			continue
		}
		if query.TeamID == "__solo__" && row.TeamID != "" {
			continue
		}
		if query.TeamID != "" && query.TeamID != "__solo__" && row.TeamID != query.TeamID {
			continue
		}
		if query.Origin != "" && row.Origin != query.Origin {
			continue
		}
		if query.UsageType != "" && row.UsageType != query.UsageType {
			continue
		}
		if query.TaskType != "" && row.TaskType != query.TaskType {
			continue
		}
		if query.ProviderID != "" && row.ProviderID != query.ProviderID {
			continue
		}
		if query.ModelID != "" && row.ModelID != query.ModelID && modelDimension(row) != query.ModelID {
			continue
		}
		if query.Status != "" && row.Status != query.Status {
			continue
		}
		result = append(result, row)
	}
	return result
}

func aggregateStatsV2Metrics(rows []db.LLMCallMetric) statsV2Metrics {
	var metrics statsV2Metrics
	durations := make([]int64, 0, len(rows))
	for _, row := range rows {
		metrics.TotalTokens += int64(row.TotalTokens)
		metrics.PromptTokens += int64(row.PromptTokens)
		metrics.CompletionTokens += int64(row.CompletionTokens)
		metrics.ReasoningTokens += int64(row.ReasoningTokens)
		metrics.CacheHitTokens += int64(row.CacheHitTokens)
		metrics.CacheMissTokens += int64(row.CacheMissTokens)
		metrics.RequestCount++
		switch row.Status {
		case "success":
			metrics.SuccessCount++
		case "error":
			metrics.ErrorCount++
		case "cancelled":
			metrics.CancelledCount++
		case "timeout":
			metrics.TimeoutCount++
		}
		if !row.Legacy {
			durations = append(durations, row.DurationMS)
		}
	}
	knownStatus := metrics.SuccessCount + metrics.ErrorCount + metrics.CancelledCount + metrics.TimeoutCount
	if knownStatus > 0 {
		metrics.SuccessRate = float64(metrics.SuccessCount) / float64(knownStatus)
	}
	cacheTotal := metrics.CacheHitTokens + metrics.CacheMissTokens
	if cacheTotal > 0 {
		metrics.CacheHitRate = float64(metrics.CacheHitTokens) / float64(cacheTotal)
	}
	if len(durations) > 0 {
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		value := durations[int(math.Ceil(float64(len(durations))*0.95))-1]
		metrics.P95DurationMS = &value
	}
	return metrics
}

func buildStatsV2Meta(query statsV2Query, bucketSize string, rows []db.LLMCallMetric) statsV2Meta {
	coverage := statsV2Coverage{TotalRows: int64(len(rows))}
	var cacheKnown, reasoningKnown int64
	for _, row := range rows {
		if row.Legacy {
			coverage.LegacyRows++
		} else {
			reasoningKnown++
		}
		if row.CacheHitTokens+row.CacheMissTokens > 0 {
			cacheKnown++
		}
	}
	if coverage.TotalRows > 0 {
		denominator := float64(coverage.TotalRows)
		coverage.CacheCoveragePct = float64(cacheKnown) / denominator * 100
		coverage.ReasoningCoveragePct = float64(reasoningKnown) / denominator * 100
	}
	return statsV2Meta{
		GeneratedAt: time.Now().UTC(), DataFrom: query.From, DataTo: query.To,
		Timezone: query.Timezone, BucketSize: bucketSize, Coverage: coverage,
	}
}

func statsV2BucketSize(period time.Duration) string {
	if period <= 48*time.Hour {
		return "hour"
	}
	if period <= 90*24*time.Hour {
		return "day"
	}
	return "week"
}

func buildStatsV2Series(rows []db.LLMCallMetric, query statsV2Query, bucketSize string) []statsV2SeriesPoint {
	localStart := statsV2BucketStart(query.From.In(query.Location), bucketSize)
	result := make([]statsV2SeriesPoint, 0, 120)
	for start := localStart; start.Before(query.To.In(query.Location)) && len(result) < 120; start = statsV2NextBucket(start, bucketSize) {
		end := statsV2NextBucket(start, bucketSize)
		bucketRows := make([]db.LLMCallMetric, 0)
		for _, row := range rows {
			local := row.FinishedAt.In(query.Location)
			if !local.Before(start) && local.Before(end) {
				bucketRows = append(bucketRows, row)
			}
		}
		result = append(result, statsV2SeriesPoint{Start: start.UTC(), End: end.UTC(), Metrics: aggregateStatsV2Metrics(bucketRows)})
	}
	return result
}

func statsV2BucketStart(value time.Time, bucketSize string) time.Time {
	switch bucketSize {
	case "hour":
		return time.Date(value.Year(), value.Month(), value.Day(), value.Hour(), 0, 0, 0, value.Location())
	case "week":
		day := time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, value.Location())
		daysSinceMonday := (int(day.Weekday()) + 6) % 7
		return day.AddDate(0, 0, -daysSinceMonday)
	default:
		return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, value.Location())
	}
}

func statsV2NextBucket(value time.Time, bucketSize string) time.Time {
	switch bucketSize {
	case "hour":
		return value.Add(time.Hour)
	case "week":
		return value.AddDate(0, 0, 7)
	default:
		return value.AddDate(0, 0, 1)
	}
}

func compareStatsV2Metrics(current, previous statsV2Metrics) map[string]statsV2Delta {
	return map[string]statsV2Delta{
		"total_tokens":    statsV2MakeDelta(float64(current.TotalTokens), float64(previous.TotalTokens)),
		"request_count":   statsV2MakeDelta(float64(current.RequestCount), float64(previous.RequestCount)),
		"success_rate":    statsV2MakeDelta(current.SuccessRate, previous.SuccessRate),
		"cache_hit_rate":  statsV2MakeDelta(current.CacheHitRate, previous.CacheHitRate),
		"p95_duration_ms": statsV2MakeDelta(statsV2Int64Value(current.P95DurationMS), statsV2Int64Value(previous.P95DurationMS)),
	}
}

func statsV2MakeDelta(current, previous float64) statsV2Delta {
	delta := statsV2Delta{Current: current, Previous: previous}
	if previous != 0 {
		value := (current - previous) / previous * 100
		delta.ChangePct = &value
	}
	return delta
}

func statsV2Int64Value(value *int64) float64 {
	if value == nil {
		return 0
	}
	return float64(*value)
}

func buildStatsV2Insights(current, previous statsV2Metrics) []statsV2Insight {
	insights := make([]statsV2Insight, 0, 3)
	if current.ErrorCount+current.TimeoutCount > 0 {
		metric := "error_count"
		insights = append(insights, statsV2Insight{
			ID: "reliability-errors", Severity: "warning", Title: "Failed calls detected",
			Detail: fmt.Sprintf("%d calls failed or timed out in this period.", current.ErrorCount+current.TimeoutCount), Metric: &metric,
		})
	}
	delta := statsV2MakeDelta(float64(current.TotalTokens), float64(previous.TotalTokens))
	if delta.ChangePct != nil && math.Abs(*delta.ChangePct) >= 50 {
		metric := "total_tokens"
		severity := "info"
		if *delta.ChangePct > 100 {
			severity = "warning"
		}
		insights = append(insights, statsV2Insight{
			ID: "token-change", Severity: severity, Title: "Token usage changed significantly",
			Detail: fmt.Sprintf("Token usage changed by %.1f%% compared with the previous period.", *delta.ChangePct),
			Metric: &metric, ChangePct: delta.ChangePct,
		})
	}
	return insights
}

func statsV2DimensionKey(row db.LLMCallMetric, dimension string) string {
	switch dimension {
	case "usage_type":
		return row.UsageType
	case "model":
		return modelDimension(row)
	case "task_type":
		return row.TaskType
	case "origin":
		return row.Origin
	case "team":
		return teamDimension(row.TeamID)
	case "status":
		return row.Status
	default:
		return "unknown"
	}
}

func teamDimension(teamID string) string {
	if teamID == "" {
		return "__solo__"
	}
	return teamID
}

func modelDimension(row db.LLMCallMetric) string {
	if row.ProviderID == "" {
		return row.ModelID
	}
	return row.ProviderID + "/" + row.ModelID
}

func statsV2Options(rows []db.LLMCallMetric, value func(db.LLMCallMetric) string) []statsV2FilterOption {
	unique := make(map[string]struct{})
	for _, row := range rows {
		item := value(row)
		if item != "" {
			unique[item] = struct{}{}
		}
	}
	values := make([]string, 0, len(unique))
	for item := range unique {
		values = append(values, item)
	}
	sort.Strings(values)
	options := make([]statsV2FilterOption, 0, len(values))
	for _, item := range values {
		label := item
		if item == "__solo__" {
			label = "Solo"
		}
		options = append(options, statsV2FilterOption{Value: item, Label: label})
	}
	return options
}

func statsV2EventFromMetric(row db.LLMCallMetric) statsV2Event {
	event := statsV2Event{
		CallID: row.CallID, RequestID: optionalString(row.RequestID), SessionID: optionalString(row.SessionID),
		RunID: optionalString(row.RunID), AgentID: optionalString(row.AgentID), TeamID: optionalString(row.TeamID),
		Origin: row.Origin, UsageType: row.UsageType, TaskType: row.TaskType,
		ProviderID: row.ProviderID, ModelID: row.ModelID, StartedAt: row.StartedAt, FinishedAt: row.FinishedAt,
		Status: row.Status, FinishReason: optionalString(row.FinishReason), ErrorCode: optionalString(row.ErrorCode),
		RetryCount: row.RetryCount, DurationMS: row.DurationMS, PromptTokens: row.PromptTokens,
		CompletionTokens: row.CompletionTokens, ReasoningTokens: row.ReasoningTokens,
		TotalTokens: row.TotalTokens, CacheHitTokens: row.CacheHitTokens,
		CacheMissTokens: row.CacheMissTokens, Legacy: row.Legacy,
	}
	return event
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

type statsV2Cursor struct {
	FinishedAt time.Time `json:"finished_at"`
	CallID     string    `json:"call_id"`
}

func encodeStatsV2Cursor(row db.LLMCallMetric) string {
	payload, _ := json.Marshal(statsV2Cursor{FinishedAt: row.FinishedAt, CallID: row.CallID})
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodeStatsV2Cursor(value string) (statsV2Cursor, error) {
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return statsV2Cursor{}, err
	}
	var cursor statsV2Cursor
	if err := json.Unmarshal(payload, &cursor); err != nil {
		return statsV2Cursor{}, err
	}
	if cursor.CallID == "" || cursor.FinishedAt.IsZero() {
		return statsV2Cursor{}, fmt.Errorf("cursor fields are required")
	}
	return cursor, nil
}

func statsV2ActivityLevels(points []statsV2ActivityPoint) map[int64]int {
	values := make([]int64, 0, len(points))
	for _, point := range points {
		if point.TotalTokens > 0 {
			values = append(values, point.TotalTokens)
		}
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	levels := map[int64]int{0: 0}
	if len(values) == 0 {
		return levels
	}
	q25 := values[len(values)/4]
	q50 := values[len(values)/2]
	q75 := values[len(values)*3/4]
	for _, value := range values {
		switch {
		case value <= q25:
			levels[value] = 1
		case value <= q50:
			levels[value] = 2
		case value <= q75:
			levels[value] = 3
		default:
			levels[value] = 4
		}
	}
	return levels
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}
