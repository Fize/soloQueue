package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/xiaobaitu/soloqueue/internal/rotating"
)

// ─── HTTP Access Logger ─────────────────────────────────────────────────────

// httpAccessLogger writes HTTP access logs to JSONL files rotated by date and size.
type httpAccessLogger struct {
	writer *rotating.DateSizeWriter
}

// newHTTPAccessLogger creates a new access logger.
func newHTTPAccessLogger(dir string, maxSizeMB, maxDays int) (*httpAccessLogger, error) {
	w, err := rotating.OpenDateSize(dir, "access", int64(maxSizeMB)*1024*1024, maxDays)
	if err != nil {
		return nil, err
	}
	return &httpAccessLogger{writer: w}, nil
}

// Middleware returns a chi-compatible HTTP middleware that logs each request.
func (l *httpAccessLogger) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		l.logRequest(r, ww, time.Since(start))
	})
}

// Close closes the current log file.
func (l *httpAccessLogger) Close() error {
	if l.writer != nil {
		return l.writer.Close()
	}
	return nil
}

// ─── Internal ───────────────────────────────────────────────────────────────

type accessEntry struct {
	Timestamp  string `json:"ts"`
	Method     string `json:"method"`
	Path       string `json:"path"`
	Status     int    `json:"status"`
	DurationMs int64  `json:"duration_ms"`
	Bytes      int    `json:"bytes"`
	RemoteAddr string `json:"remote_addr"`
	UserAgent  string `json:"user_agent,omitempty"`
}

func (l *httpAccessLogger) logRequest(r *http.Request, ww middleware.WrapResponseWriter, duration time.Duration) {
	entry := accessEntry{
		Timestamp:  time.Now().UTC().Format(time.RFC3339Nano),
		Method:     r.Method,
		Path:       r.URL.RequestURI(),
		Status:     ww.Status(),
		DurationMs: duration.Milliseconds(),
		Bytes:      ww.BytesWritten(),
		RemoteAddr: r.RemoteAddr,
		UserAgent:  r.UserAgent(),
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_, _ = l.writer.Write(data)
}
