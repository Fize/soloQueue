package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// handleGetTokenStats returns aggregated token usage statistics from the DB.
func (m *Mux) handleGetTokenStats(w http.ResponseWriter, r *http.Request) {
	if m.sharedDB == nil {
		http.Error(w, "database not available", http.StatusServiceUnavailable)
		return
	}

	timeframe := r.URL.Query().Get("timeframe") // "daily", "weekly", "monthly"
	if timeframe == "" {
		timeframe = "daily"
	}
	teamID := r.URL.Query().Get("team_id")
	usageType := r.URL.Query().Get("usage_type")

	// Create a short timeout context
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	stats, err := m.sharedDB.GetTokenUsageAggregated(ctx, timeframe, teamID, usageType)
	if err != nil {
		m.log.Error(logger.CatApp, "failed to get token stats", "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(stats); err != nil {
		m.log.Error(logger.CatApp, "failed to encode token stats response", "err", err)
	}
}

// handleGetRouterStats returns router classification statistics from the DB.
func (m *Mux) handleGetRouterStats(w http.ResponseWriter, r *http.Request) {
	if m.sharedDB == nil {
		http.Error(w, "database not available", http.StatusServiceUnavailable)
		return
	}

	timeframe := r.URL.Query().Get("timeframe") // "daily", "weekly", "monthly"
	if timeframe == "" {
		timeframe = "daily"
	}
	teamID := r.URL.Query().Get("team_id")

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	stats, err := m.sharedDB.GetRouterStatsAggregated(ctx, timeframe, teamID)
	if err != nil {
		m.log.Error(logger.CatApp, "failed to get router stats", "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(stats); err != nil {
		m.log.Error(logger.CatApp, "failed to encode router stats response", "err", err)
	}
}

// handleGetTeams returns distinct team IDs from usage_metrics.
func (m *Mux) handleGetTeams(w http.ResponseWriter, r *http.Request) {
	if m.sharedDB == nil {
		http.Error(w, "database not available", http.StatusServiceUnavailable)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	teams, err := m.sharedDB.GetDistinctTeams(ctx)
	if err != nil {
		m.log.Error(logger.CatApp, "failed to get teams", "err", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(teams); err != nil {
		m.log.Error(logger.CatApp, "failed to encode teams response", "err", err)
	}
}
