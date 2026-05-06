package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// ─── HTTP Access Logger ─────────────────────────────────────────────────────

// httpAccessLogger writes HTTP access logs to a daily-rotated JSONL file
// under {dir}/access-{date}.jsonl. Files are rotated when the date changes
// or the current file exceeds maxSize bytes. Files older than maxDays are
// removed on startup and on each rotation.
type httpAccessLogger struct {
	dir     string
	maxSize int64
	maxDays int

	mu      sync.Mutex
	current *os.File
	curDate string
	curSeq  int // sequence number for same-day size rotation (0 = first file)
}

// newHTTPAccessLogger creates a new access logger.
// dir will be created if it doesn't exist.
func newHTTPAccessLogger(dir string, maxSizeMB, maxDays int) (*httpAccessLogger, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create access log dir %s: %w", dir, err)
	}

	l := &httpAccessLogger{
		dir:     dir,
		maxSize: int64(maxSizeMB) * 1024 * 1024,
		maxDays: maxDays,
	}
	l.cleanup()
	if err := l.openFile(); err != nil {
		return nil, err
	}
	return l, nil
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
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.current != nil {
		return l.current.Close()
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

	l.mu.Lock()
	defer l.mu.Unlock()

	l.maybeRotate()
	if l.current != nil {
		l.current.Write(data)
		l.current.Write([]byte("\n"))
	}
}

// maybeRotate checks if rotation is needed (date change or size exceeded).
func (l *httpAccessLogger) maybeRotate() {
	today := time.Now().Format("2006-01-02")

	// Date rotation
	if today != l.curDate {
		l.curSeq = 0
		l.curDate = today
		l.openFileLocked()
		l.cleanupLocked()
		return
	}

	// Size rotation: if current file exceeds maxSize, increment sequence
	info, err := l.current.Stat()
	if err == nil && info.Size() >= l.maxSize {
		l.curSeq++
		l.openFileLocked()
	}
}

func (l *httpAccessLogger) openFile() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.curDate = time.Now().Format("2006-01-02")
	return l.openFileLocked()
}

func (l *httpAccessLogger) openFileLocked() error {
	if l.current != nil {
		l.current.Close()
	}

	name := fmt.Sprintf("access-%s", l.curDate)
	if l.curSeq > 0 {
		name += fmt.Sprintf("-%d", l.curSeq+1)
	}
	name += ".jsonl"

	path := filepath.Join(l.dir, name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		l.current = nil
		return fmt.Errorf("open access log %s: %w", path, err)
	}
	l.current = f
	return nil
}

// cleanup removes access log files older than maxDays.
func (l *httpAccessLogger) cleanup() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cleanupLocked()
}

func (l *httpAccessLogger) cleanupLocked() {
	if l.maxDays <= 0 {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -l.maxDays)
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(l.dir, e.Name()))
		}
	}
}
