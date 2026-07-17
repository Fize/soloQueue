package server

import (
	"context"
	"net/http"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// handleGetTokenStats returns aggregated token usage statistics from the DB.
func (m *Mux) handleGetTokenStats(w http.ResponseWriter, r *http.Request) {
	if m.sharedDB == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "database not available"})
		return
	}

	timeframe := r.URL.Query().Get("timeframe") // "hourly", "daily", "weekly", "monthly"
	if timeframe == "" {
		timeframe = "daily"
	}
	teamID := r.URL.Query().Get("team_id")
	usageType := r.URL.Query().Get("usage_type")
	fromDate := r.URL.Query().Get("from")
	toDate := r.URL.Query().Get("to")

	// Create a short timeout context
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	stats, err := m.sharedDB.GetTokenUsageAggregated(ctx, timeframe, teamID, usageType, fromDate, toDate)
	if err != nil {
		m.log.Error(logger.CatApp, "failed to get token stats", "err", err)
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	m.writeJSON(w, http.StatusOK, stats)
}

// handleGetRouterStats returns router classification statistics from the DB.
func (m *Mux) handleGetRouterStats(w http.ResponseWriter, r *http.Request) {
	if m.sharedDB == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "database not available"})
		return
	}

	timeframe := r.URL.Query().Get("timeframe") // "hourly", "daily", "weekly", "monthly"
	if timeframe == "" {
		timeframe = "daily"
	}
	teamID := r.URL.Query().Get("team_id")
	fromDate := r.URL.Query().Get("from")
	toDate := r.URL.Query().Get("to")

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	stats, err := m.sharedDB.GetRouterStatsAggregated(ctx, timeframe, teamID, fromDate, toDate)
	if err != nil {
		m.log.Error(logger.CatApp, "failed to get router stats", "err", err)
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	m.writeJSON(w, http.StatusOK, stats)
}

// handleGetTeams returns distinct team IDs from usage_metrics.
func (m *Mux) handleGetTeams(w http.ResponseWriter, r *http.Request) {
	if m.sharedDB == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "database not available"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	teams, err := m.sharedDB.GetDistinctTeams(ctx)
	if err != nil {
		m.log.Error(logger.CatApp, "failed to get teams", "err", err)
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	m.writeJSON(w, http.StatusOK, teams)
}

// handleGetClassifierStats returns classifier decision statistics from the DB.
func (m *Mux) handleGetClassifierStats(w http.ResponseWriter, r *http.Request) {
	if m.sharedDB == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "database not available"})
		return
	}

	timeframe := r.URL.Query().Get("timeframe") // "hourly", "daily", "weekly", "monthly"
	if timeframe == "" {
		timeframe = "daily"
	}
	fromDate := r.URL.Query().Get("from")
	toDate := r.URL.Query().Get("to")

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	stats, err := m.sharedDB.GetClassifierStatsAggregated(ctx, timeframe, fromDate, toDate)
	if err != nil {
		m.log.Error(logger.CatApp, "failed to get classifier stats", "err", err)
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	m.writeJSON(w, http.StatusOK, stats)
}
