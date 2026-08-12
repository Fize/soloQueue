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

type statsEnvelope struct {
	Data  any     `json:"data"`
	Error *string `json:"error"`
}

type statsQuery struct {
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

type statsCoverageCount struct {
	KnownRows      int64 `json:"known_rows"`
	ApplicableRows int64 `json:"applicable_rows"`
}

type statsCoverage struct {
	TotalRows       int64              `json:"total_rows"`
	LegacyRows      int64              `json:"legacy_rows"`
	Origin          statsCoverageCount `json:"origin"`
	TaskType        statsCoverageCount `json:"task_type"`
	Status          statsCoverageCount `json:"status"`
	Latency         statsCoverageCount `json:"latency"`
	CacheDetail     statsCoverageCount `json:"cache_detail"`
	ReasoningDetail statsCoverageCount `json:"reasoning_detail"`
}

type statsMeta struct {
	GeneratedAt time.Time     `json:"generated_at"`
	DataFrom    time.Time     `json:"data_from"`
	DataTo      time.Time     `json:"data_to"`
	Timezone    string        `json:"timezone"`
	BucketSize  string        `json:"bucket_size"`
	Coverage    statsCoverage `json:"coverage"`
}

type statsMetrics struct {
	TotalTokens      int64    `json:"total_tokens"`
	PromptTokens     int64    `json:"prompt_tokens"`
	CompletionTokens int64    `json:"completion_tokens"`
	ReasoningTokens  int64    `json:"reasoning_tokens"`
	CacheHitTokens   int64    `json:"cache_hit_tokens"`
	CacheMissTokens  int64    `json:"cache_miss_tokens"`
	RequestCount     int64    `json:"request_count"`
	SuccessCount     int64    `json:"success_count"`
	ErrorCount       int64    `json:"error_count"`
	CancelledCount   int64    `json:"cancelled_count"`
	TimeoutCount     int64    `json:"timeout_count"`
	SuccessRate      *float64 `json:"success_rate"`
	CacheHitRate     *float64 `json:"cache_hit_rate"`
	P95DurationMS    *int64   `json:"p95_duration_ms"`
}

type statsDelta struct {
	Current   *float64 `json:"current"`
	Previous  *float64 `json:"previous"`
	ChangePct *float64 `json:"change_pct"`
}

type statsSeriesPoint struct {
	Start   time.Time    `json:"start"`
	End     time.Time    `json:"end"`
	Metrics statsMetrics `json:"metrics"`
}

type statsInsight struct {
	ID        string   `json:"id"`
	Severity  string   `json:"severity"`
	Title     string   `json:"title"`
	Detail    string   `json:"detail"`
	Metric    *string  `json:"metric"`
	ChangePct *float64 `json:"change_pct"`
}

type statsOverview struct {
	Meta       statsMeta             `json:"meta"`
	Summary    statsMetrics          `json:"summary"`
	Comparison map[string]statsDelta `json:"comparison"`
	Series     []statsSeriesPoint    `json:"series"`
	Insights   []statsInsight        `json:"insights"`
}

type statsBreakdownItem struct {
	Key     string       `json:"key"`
	Label   string       `json:"label"`
	Metrics statsMetrics `json:"metrics"`
}

type statsBreakdown struct {
	Meta      statsMeta            `json:"meta"`
	Dimension string               `json:"dimension"`
	Items     []statsBreakdownItem `json:"items"`
}

type statsEvent struct {
	CallID                   string    `json:"call_id"`
	RequestID                *string   `json:"request_id"`
	SessionID                *string   `json:"session_id"`
	RunID                    *string   `json:"run_id"`
	AgentID                  *string   `json:"agent_id"`
	TeamID                   *string   `json:"team_id"`
	Origin                   *string   `json:"origin"`
	UsageType                *string   `json:"usage_type"`
	TaskType                 *string   `json:"task_type"`
	ProviderID               *string   `json:"provider_id"`
	ModelID                  *string   `json:"model_id"`
	StartedAt                time.Time `json:"started_at"`
	FinishedAt               time.Time `json:"finished_at"`
	Status                   *string   `json:"status"`
	FinishReason             *string   `json:"finish_reason"`
	ErrorCode                *string   `json:"error_code"`
	DurationMS               *int64    `json:"duration_ms"`
	PromptTokens             int       `json:"prompt_tokens"`
	CompletionTokens         int       `json:"completion_tokens"`
	ReasoningTokens          int       `json:"reasoning_tokens"`
	TotalTokens              int       `json:"total_tokens"`
	CacheHitTokens           int       `json:"cache_hit_tokens"`
	CacheMissTokens          int       `json:"cache_miss_tokens"`
	ReasoningDetailsReported bool      `json:"reasoning_details_reported"`
	CacheDetailsReported     bool      `json:"cache_details_reported"`
	Legacy                   bool      `json:"legacy"`
}

type statsEvents struct {
	Meta       statsMeta    `json:"meta"`
	Items      []statsEvent `json:"items"`
	NextCursor *string      `json:"next_cursor"`
}

type statsFilterOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type statsFilters struct {
	Meta       statsMeta           `json:"meta"`
	Teams      []statsFilterOption `json:"teams"`
	Origins    []statsFilterOption `json:"origins"`
	UsageTypes []statsFilterOption `json:"usage_types"`
	TaskTypes  []statsFilterOption `json:"task_types"`
	Providers  []statsFilterOption `json:"providers"`
	Models     []statsFilterOption `json:"models"`
	Statuses   []statsFilterOption `json:"statuses"`
}

type statsActivityPoint struct {
	Date         string `json:"date"`
	TotalTokens  int64  `json:"total_tokens"`
	RequestCount int64  `json:"request_count"`
	Level        int    `json:"level"`
}

type statsActivity struct {
	Meta        statsMeta            `json:"meta"`
	ActiveDays  int                  `json:"active_days"`
	TotalTokens int64                `json:"total_tokens"`
	Points      []statsActivityPoint `json:"points"`
}

func (m *Mux) handleGetStatsOverview(w http.ResponseWriter, r *http.Request) {
	query, ok := m.parseStatsQuery(w, r, true)
	if !ok {
		return
	}
	period := query.To.Sub(query.From)
	rows, ok := m.loadStatsRows(w, r, query.From.Add(-period), query.To)
	if !ok {
		return
	}
	current := filterStatsRows(rows, query, query.From, query.To)
	previous := filterStatsRows(rows, query, query.From.Add(-period), query.From)
	bucketSize := statsBucketSize(period)
	summary := aggregateStatsMetrics(current)
	previousSummary := aggregateStatsMetrics(previous)
	data := statsOverview{
		Meta:       buildStatsMeta(query, bucketSize, current),
		Summary:    summary,
		Comparison: compareStatsMetrics(summary, previousSummary),
		Series:     buildStatsSeries(current, query, bucketSize),
		Insights:   buildStatsInsights(summary, previousSummary),
	}
	m.writeStatsData(w, data)
}

func (m *Mux) handleGetStatsBreakdowns(w http.ResponseWriter, r *http.Request) {
	query, ok := m.parseStatsQuery(w, r, true)
	if !ok {
		return
	}
	dimension := r.URL.Query().Get("dimension")
	if !containsString([]string{"usage_type", "model", "task_type", "origin", "team", "status"}, dimension) {
		m.writeStatsError(w, http.StatusBadRequest, "invalid_dimension: dimension is required and must be supported")
		return
	}
	rows, ok := m.loadStatsRows(w, r, query.From, query.To)
	if !ok {
		return
	}
	rows = filterStatsRows(rows, query, query.From, query.To)
	groups := make(map[string][]db.LLMCallMetric)
	for _, row := range rows {
		key := statsDimensionKey(row, dimension)
		if key == "" {
			continue
		}
		groups[key] = append(groups[key], row)
	}
	items := make([]statsBreakdownItem, 0, len(groups))
	for key, group := range groups {
		label := key
		if dimension == "team" && key == "__solo__" {
			label = "Solo"
		}
		items = append(items, statsBreakdownItem{Key: key, Label: label, Metrics: aggregateStatsMetrics(group)})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Metrics.TotalTokens == items[j].Metrics.TotalTokens {
			return items[i].Key < items[j].Key
		}
		return items[i].Metrics.TotalTokens > items[j].Metrics.TotalTokens
	})
	m.writeStatsData(w, statsBreakdown{
		Meta: buildStatsMeta(query, "none", rows), Dimension: dimension, Items: items,
	})
}

func (m *Mux) handleGetStatsEvents(w http.ResponseWriter, r *http.Request) {
	query, ok := m.parseStatsQuery(w, r, true)
	if !ok {
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 100 {
			m.writeStatsError(w, http.StatusBadRequest, "invalid_limit: limit must be between 1 and 100")
			return
		}
		limit = value
	}
	rows, ok := m.loadStatsRows(w, r, query.From, query.To)
	if !ok {
		return
	}
	rows = filterStatsRows(rows, query, query.From, query.To)
	start := 0
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		cursor, err := decodeStatsCursor(raw)
		if err != nil {
			m.writeStatsError(w, http.StatusBadRequest, "invalid_cursor: cursor is malformed")
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
			m.writeStatsError(w, http.StatusBadRequest, "invalid_cursor: cursor is no longer in the result set")
			return
		}
	}
	end := start + limit
	if end > len(rows) {
		end = len(rows)
	}
	items := make([]statsEvent, 0, end-start)
	for _, row := range rows[start:end] {
		items = append(items, statsEventFromMetric(row))
	}
	var next *string
	if end < len(rows) && end > start {
		cursor := encodeStatsCursor(rows[end-1])
		next = &cursor
	}
	m.writeStatsData(w, statsEvents{
		Meta: buildStatsMeta(query, "none", rows), Items: items, NextCursor: next,
	})
}

func (m *Mux) handleGetStatsFilters(w http.ResponseWriter, r *http.Request) {
	query, ok := m.parseStatsQuery(w, r, true)
	if !ok {
		return
	}
	rows, ok := m.loadStatsRows(w, r, query.From, query.To)
	if !ok {
		return
	}
	rows = filterStatsRows(rows, statsQuery{Location: query.Location, Timezone: query.Timezone}, query.From, query.To)
	m.writeStatsData(w, statsFilters{
		Meta:       buildStatsMeta(query, "none", rows),
		Teams:      statsOptions(rows, func(row db.LLMCallMetric) string { return teamDimension(row.TeamID) }),
		Origins:    statsOptions(rows, func(row db.LLMCallMetric) string { return statsDimensionKey(row, "origin") }),
		UsageTypes: statsOptions(rows, func(row db.LLMCallMetric) string { return statsDimensionKey(row, "usage_type") }),
		TaskTypes:  statsOptions(rows, func(row db.LLMCallMetric) string { return statsDimensionKey(row, "task_type") }),
		Providers:  statsOptions(rows, func(row db.LLMCallMetric) string { return row.ProviderID }),
		Models:     statsOptions(rows, func(row db.LLMCallMetric) string { return modelDimension(row) }),
		Statuses:   statsOptions(rows, func(row db.LLMCallMetric) string { return statsDimensionKey(row, "status") }),
	})
}

func (m *Mux) handleGetStatsActivity(w http.ResponseWriter, r *http.Request) {
	query, ok := m.parseStatsQuery(w, r, false)
	if !ok {
		return
	}
	days := 365
	if raw := r.URL.Query().Get("days"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 730 {
			m.writeStatsError(w, http.StatusBadRequest, "invalid_days: days must be between 1 and 730")
			return
		}
		days = value
	}
	now := time.Now().In(query.Location)
	localEnd := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, query.Location)
	localStart := localEnd.AddDate(0, 0, -days)
	query.From, query.To = localStart.UTC(), localEnd.UTC()
	rows, ok := m.loadStatsRows(w, r, query.From, query.To)
	if !ok {
		return
	}
	rows = filterStatsRows(rows, query, query.From, query.To)
	byDate := make(map[string]*statsActivityPoint, days)
	points := make([]statsActivityPoint, 0, days)
	for index := 0; index < days; index++ {
		date := localStart.AddDate(0, 0, index).Format("2006-01-02")
		points = append(points, statsActivityPoint{Date: date})
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
	levels := statsActivityLevels(points)
	activeDays := 0
	for index := range points {
		if points[index].RequestCount > 0 {
			activeDays++
		}
		points[index].Level = levels[points[index].TotalTokens]
	}
	m.writeStatsData(w, statsActivity{
		Meta: buildStatsMeta(query, "day", rows), ActiveDays: activeDays, TotalTokens: total, Points: points,
	})
}

func (m *Mux) parseStatsQuery(w http.ResponseWriter, r *http.Request, withRange bool) (statsQuery, bool) {
	query := statsQuery{
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
		m.writeStatsError(w, http.StatusBadRequest, "invalid_timezone: timezone must be a valid IANA timezone")
		return statsQuery{}, false
	}
	query.Location = location
	validEnums := []struct {
		name  string
		value string
		valid []string
	}{
		{"origin", query.Origin, []string{"desktop", "api", "qq", "wechat", "cron", "workflow", "simulation", "system"}},
		{"usage_type", query.UsageType, []string{"chat", "router", "compactor", "memory", "simulation"}},
		{"task_type", query.TaskType, []string{"general", "engineering", "research"}},
		{"status", query.Status, []string{"success", "error", "cancelled", "timeout"}},
	}
	for _, item := range validEnums {
		if item.value != "" && !containsString(item.valid, item.value) {
			m.writeStatsError(w, http.StatusBadRequest, "invalid_"+item.name+": unsupported value")
			return statsQuery{}, false
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
			m.writeStatsError(w, http.StatusBadRequest, "invalid_to: to must be an RFC 3339 instant")
			return statsQuery{}, false
		}
	}
	if raw := r.URL.Query().Get("from"); raw != "" {
		query.From, err = time.Parse(time.RFC3339, raw)
		if err != nil {
			m.writeStatsError(w, http.StatusBadRequest, "invalid_from: from must be an RFC 3339 instant")
			return statsQuery{}, false
		}
	}
	query.From, query.To = query.From.UTC(), query.To.UTC()
	if !query.From.Before(query.To) {
		m.writeStatsError(w, http.StatusBadRequest, "invalid_time_range: from must be earlier than to")
		return statsQuery{}, false
	}
	return query, true
}

func (m *Mux) loadStatsRows(w http.ResponseWriter, r *http.Request, from, to time.Time) ([]db.LLMCallMetric, bool) {
	if m.sharedDB == nil {
		m.writeStatsError(w, http.StatusServiceUnavailable, "stats_unavailable: usage metrics storage is not configured")
		return nil, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	rows, err := m.sharedDB.ListLLMCallMetrics(ctx, from, to)
	if err != nil {
		if m.log != nil {
			m.log.Error(logger.CatApp, "failed to query usage metrics", "err", err)
		}
		m.writeStatsError(w, http.StatusInternalServerError, "stats_query_failed: failed to query usage metrics")
		return nil, false
	}
	return rows, true
}

func (m *Mux) writeStatsData(w http.ResponseWriter, data any) {
	m.writeJSON(w, http.StatusOK, statsEnvelope{Data: data, Error: nil})
}

func (m *Mux) writeStatsError(w http.ResponseWriter, status int, message string) {
	m.writeJSON(w, status, statsEnvelope{Data: nil, Error: &message})
}

func filterStatsRows(rows []db.LLMCallMetric, query statsQuery, from, to time.Time) []db.LLMCallMetric {
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

func aggregateStatsMetrics(rows []db.LLMCallMetric) statsMetrics {
	var metrics statsMetrics
	durations := make([]int64, 0, len(rows))
	var cacheDetailRows int64
	for _, row := range rows {
		metrics.TotalTokens += int64(row.TotalTokens)
		metrics.PromptTokens += int64(row.PromptTokens)
		metrics.CompletionTokens += int64(row.CompletionTokens)
		if row.ReasoningDetailsReported {
			metrics.ReasoningTokens += int64(row.ReasoningTokens)
		}
		if row.CacheDetailsReported {
			metrics.CacheHitTokens += int64(row.CacheHitTokens)
			metrics.CacheMissTokens += int64(row.CacheMissTokens)
			cacheDetailRows++
		}
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
		value := float64(metrics.SuccessCount) / float64(knownStatus)
		metrics.SuccessRate = &value
	}
	cacheTotal := metrics.CacheHitTokens + metrics.CacheMissTokens
	if cacheDetailRows > 0 {
		value := 0.0
		if cacheTotal > 0 {
			value = float64(metrics.CacheHitTokens) / float64(cacheTotal)
		}
		metrics.CacheHitRate = &value
	}
	if len(durations) > 0 {
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		value := durations[int(math.Ceil(float64(len(durations))*0.95))-1]
		metrics.P95DurationMS = &value
	}
	return metrics
}

func buildStatsMeta(query statsQuery, bucketSize string, rows []db.LLMCallMetric) statsMeta {
	coverage := statsCoverage{TotalRows: int64(len(rows))}
	for _, row := range rows {
		if row.Legacy {
			coverage.LegacyRows++
			continue
		}
		coverage.Origin.ApplicableRows++
		coverage.Status.ApplicableRows++
		coverage.Latency.ApplicableRows++
		coverage.CacheDetail.ApplicableRows++
		coverage.ReasoningDetail.ApplicableRows++
		if isKnownOrigin(row.Origin) {
			coverage.Origin.KnownRows++
		}
		if isKnownStatus(row.Status) {
			coverage.Status.KnownRows++
		}
		coverage.Latency.KnownRows++
		if row.CacheDetailsReported {
			coverage.CacheDetail.KnownRows++
		}
		if row.ReasoningDetailsReported {
			coverage.ReasoningDetail.KnownRows++
		}
		if isTaskTypeApplicable(row) {
			coverage.TaskType.ApplicableRows++
			if isKnownTaskType(row.TaskType) {
				coverage.TaskType.KnownRows++
			}
		}
	}
	return statsMeta{
		GeneratedAt: time.Now().UTC(), DataFrom: query.From, DataTo: query.To,
		Timezone: query.Timezone, BucketSize: bucketSize, Coverage: coverage,
	}
}

func isKnownOrigin(value string) bool {
	return containsString([]string{"desktop", "api", "qq", "wechat", "cron", "workflow", "simulation", "system"}, value)
}

func isKnownStatus(value string) bool {
	return containsString([]string{"success", "error", "cancelled", "timeout"}, value)
}

func isKnownTaskType(value string) bool {
	return containsString([]string{"general", "engineering", "research"}, value)
}

func isTaskTypeApplicable(row db.LLMCallMetric) bool {
	if row.Legacy || row.UsageType != "chat" {
		return false
	}
	return containsString([]string{"desktop", "api", "qq", "wechat", "cron"}, row.Origin)
}

func statsBucketSize(period time.Duration) string {
	if period <= 48*time.Hour {
		return "hour"
	}
	if period <= 90*24*time.Hour {
		return "day"
	}
	return "week"
}

func buildStatsSeries(rows []db.LLMCallMetric, query statsQuery, bucketSize string) []statsSeriesPoint {
	localStart := statsBucketStart(query.From.In(query.Location), bucketSize)
	result := make([]statsSeriesPoint, 0, 120)
	for start := localStart; start.Before(query.To.In(query.Location)) && len(result) < 120; start = statsNextBucket(start, bucketSize) {
		end := statsNextBucket(start, bucketSize)
		bucketRows := make([]db.LLMCallMetric, 0)
		for _, row := range rows {
			local := row.FinishedAt.In(query.Location)
			if !local.Before(start) && local.Before(end) {
				bucketRows = append(bucketRows, row)
			}
		}
		result = append(result, statsSeriesPoint{Start: start.UTC(), End: end.UTC(), Metrics: aggregateStatsMetrics(bucketRows)})
	}
	return result
}

func statsBucketStart(value time.Time, bucketSize string) time.Time {
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

func statsNextBucket(value time.Time, bucketSize string) time.Time {
	switch bucketSize {
	case "hour":
		return value.Add(time.Hour)
	case "week":
		return value.AddDate(0, 0, 7)
	default:
		return value.AddDate(0, 0, 1)
	}
}

func compareStatsMetrics(current, previous statsMetrics) map[string]statsDelta {
	return map[string]statsDelta{
		"total_tokens":    statsMakeDelta(statsFloat64(float64(current.TotalTokens)), statsFloat64(float64(previous.TotalTokens))),
		"request_count":   statsMakeDelta(statsFloat64(float64(current.RequestCount)), statsFloat64(float64(previous.RequestCount))),
		"success_rate":    statsMakeDelta(current.SuccessRate, previous.SuccessRate),
		"cache_hit_rate":  statsMakeDelta(current.CacheHitRate, previous.CacheHitRate),
		"p95_duration_ms": statsMakeDelta(statsInt64Float(current.P95DurationMS), statsInt64Float(previous.P95DurationMS)),
	}
}

func statsMakeDelta(current, previous *float64) statsDelta {
	delta := statsDelta{Current: current, Previous: previous}
	if current != nil && previous != nil && *previous != 0 {
		value := (*current - *previous) / *previous * 100
		delta.ChangePct = &value
	}
	return delta
}

func statsFloat64(value float64) *float64 {
	return &value
}

func statsInt64Float(value *int64) *float64 {
	if value == nil {
		return nil
	}
	result := float64(*value)
	return &result
}

func buildStatsInsights(current, previous statsMetrics) []statsInsight {
	insights := make([]statsInsight, 0, 3)
	if current.ErrorCount+current.TimeoutCount > 0 {
		metric := "error_count"
		insights = append(insights, statsInsight{
			ID: "reliability-errors", Severity: "warning", Title: "Failed calls detected",
			Detail: fmt.Sprintf("%d calls failed or timed out in this period.", current.ErrorCount+current.TimeoutCount), Metric: &metric,
		})
	}
	delta := statsMakeDelta(statsFloat64(float64(current.TotalTokens)), statsFloat64(float64(previous.TotalTokens)))
	if delta.ChangePct != nil && math.Abs(*delta.ChangePct) >= 50 {
		metric := "total_tokens"
		severity := "info"
		if *delta.ChangePct > 100 {
			severity = "warning"
		}
		insights = append(insights, statsInsight{
			ID: "token-change", Severity: severity, Title: "Token usage changed significantly",
			Detail: fmt.Sprintf("Token usage changed by %.1f%% compared with the previous period.", *delta.ChangePct),
			Metric: &metric, ChangePct: delta.ChangePct,
		})
	}
	return insights
}

func statsDimensionKey(row db.LLMCallMetric, dimension string) string {
	switch dimension {
	case "usage_type":
		if containsString([]string{"chat", "router", "compactor", "memory", "simulation"}, row.UsageType) {
			return row.UsageType
		}
	case "model":
		return modelDimension(row)
	case "task_type":
		if isKnownTaskType(row.TaskType) {
			return row.TaskType
		}
	case "origin":
		if isKnownOrigin(row.Origin) {
			return row.Origin
		}
	case "team":
		return teamDimension(row.TeamID)
	case "status":
		if isKnownStatus(row.Status) {
			return row.Status
		}
	}
	return ""
}

func teamDimension(teamID string) string {
	if teamID == "" {
		return "__solo__"
	}
	return teamID
}

func modelDimension(row db.LLMCallMetric) string {
	if row.ModelID == "" || row.ModelID == "unknown" {
		return ""
	}
	if row.ProviderID == "" {
		return row.ModelID
	}
	return row.ProviderID + "/" + row.ModelID
}

func statsOptions(rows []db.LLMCallMetric, value func(db.LLMCallMetric) string) []statsFilterOption {
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
	options := make([]statsFilterOption, 0, len(values))
	for _, item := range values {
		label := item
		if item == "__solo__" {
			label = "Solo"
		}
		options = append(options, statsFilterOption{Value: item, Label: label})
	}
	return options
}

func statsEventFromMetric(row db.LLMCallMetric) statsEvent {
	var duration *int64
	if !row.Legacy {
		value := row.DurationMS
		duration = &value
	}
	event := statsEvent{
		CallID: row.CallID, RequestID: optionalString(row.RequestID), SessionID: optionalString(row.SessionID),
		RunID: optionalString(row.RunID), AgentID: optionalString(row.AgentID), TeamID: optionalString(row.TeamID),
		Origin: optionalKnownString(row.Origin, isKnownOrigin), UsageType: optionalKnownString(row.UsageType, func(value string) bool {
			return containsString([]string{"chat", "router", "compactor", "memory", "simulation"}, value)
		}), TaskType: optionalKnownString(row.TaskType, isKnownTaskType),
		ProviderID: optionalString(row.ProviderID), ModelID: optionalString(row.ModelID), StartedAt: row.StartedAt, FinishedAt: row.FinishedAt,
		Status: optionalKnownString(row.Status, isKnownStatus), FinishReason: optionalString(row.FinishReason), ErrorCode: optionalString(row.ErrorCode),
		DurationMS: duration, PromptTokens: row.PromptTokens,
		CompletionTokens: row.CompletionTokens, ReasoningTokens: row.ReasoningTokens,
		TotalTokens: row.TotalTokens, CacheHitTokens: row.CacheHitTokens,
		CacheMissTokens: row.CacheMissTokens, ReasoningDetailsReported: row.ReasoningDetailsReported,
		CacheDetailsReported: row.CacheDetailsReported, Legacy: row.Legacy,
	}
	return event
}

func optionalKnownString(value string, known func(string) bool) *string {
	if !known(value) {
		return nil
	}
	return &value
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

type statsCursor struct {
	FinishedAt time.Time `json:"finished_at"`
	CallID     string    `json:"call_id"`
}

func encodeStatsCursor(row db.LLMCallMetric) string {
	payload, _ := json.Marshal(statsCursor{FinishedAt: row.FinishedAt, CallID: row.CallID})
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodeStatsCursor(value string) (statsCursor, error) {
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return statsCursor{}, err
	}
	var cursor statsCursor
	if err := json.Unmarshal(payload, &cursor); err != nil {
		return statsCursor{}, err
	}
	if cursor.CallID == "" || cursor.FinishedAt.IsZero() {
		return statsCursor{}, fmt.Errorf("cursor fields are required")
	}
	return cursor, nil
}

func statsActivityLevels(points []statsActivityPoint) map[int64]int {
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
