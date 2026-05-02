// Package server exposes SoloQueue's REST API.
//
// Routes:
//
//	GET /healthz → {"status":"ok"}
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// Mux is the root HTTP handler.
type Mux struct {
	log *logger.Logger
	mux *http.ServeMux
}

// NewMux creates a new HTTP handler with registered routes.
func NewMux(log *logger.Logger) *Mux {
	m := &Mux{
		log: log,
		mux: http.NewServeMux(),
	}

	m.mux.HandleFunc("GET /healthz", m.handleHealth)

	return m
}

// ServeHTTP implements http.Handler.
func (m *Mux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			m.logError(r.Context(), "panic in handler", fmt.Errorf("%v", rec))
			m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal"})
		}
	}()
	m.mux.ServeHTTP(w, r)
}

func (m *Mux) handleHealth(w http.ResponseWriter, _ *http.Request) {
	m.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (m *Mux) writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(payload)
	if err != nil {
		if m.log != nil {
			m.log.ErrorContext(context.Background(), logger.CatHTTP, "writeJSON marshal failed", "err", err.Error())
		}
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(status)
	_, _ = w.Write(data)
	_, _ = w.Write([]byte("\n"))
}

func (m *Mux) logError(ctx context.Context, msg string, err error) {
	if m.log == nil {
		return
	}
	m.log.LogError(ctx, logger.CatHTTP, msg, err)
}
