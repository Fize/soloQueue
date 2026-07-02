package logger

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"sync"
	"time"
)

// ─── MultiHandler ───────────────────────────────────────────────────────────

// MultiHandler writes to both console (stderr) and a file simultaneously.
type MultiHandler struct {
	console slog.Handler
	file    *FileHandler
}

func newMultiHandler(console slog.Handler, file *FileHandler) *MultiHandler {
	return &MultiHandler{console: console, file: file}
}

func (h *MultiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	if h.console != nil && h.console.Enabled(ctx, level) {
		return true
	}
	if h.file != nil && h.file.Enabled(ctx, level) {
		return true
	}
	return false
}

func (h *MultiHandler) Handle(ctx context.Context, r slog.Record) error {
	var firstErr error
	if h.console != nil && h.console.Enabled(ctx, r.Level) {
		if err := h.console.Handle(ctx, r); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if h.file != nil && h.file.Enabled(ctx, r.Level) {
		if err := h.file.Handle(ctx, r); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (h *MultiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	var newConsole slog.Handler
	if h.console != nil {
		newConsole = h.console.WithAttrs(attrs)
	}
	var newFile *FileHandler
	if h.file != nil {
		newFile = h.file.withAttrs(attrs)
	}
	return &MultiHandler{console: newConsole, file: newFile}
}

func (h *MultiHandler) WithGroup(name string) slog.Handler {
	var newConsole slog.Handler
	if h.console != nil {
		newConsole = h.console.WithGroup(name)
	}
	var newFile *FileHandler
	if h.file != nil {
		newFile = h.file.withGroup(name)
	}
	return &MultiHandler{console: newConsole, file: newFile}
}

func (h *MultiHandler) close() error {
	if h.file != nil {
		return h.file.close()
	}
	return nil
}

// ─── FileHandler ─────────────────────────────────────────────────────────────

// writerPool is a shared category→rotateWriter map.
// It's shared by pointer between FileHandler clones to avoid mutex value copying.
type writerPool struct {
	mu      sync.Mutex
	writers map[Category]*rotateWriter
}

// FileHandler routes logs by category to the corresponding rotateWriter.
type FileHandler struct {
	baseDir   string
	subdir    string
	levelVar  *slog.LevelVar
	preAttrs  []slog.Attr
	maxSizeMB int
	maxDays   int
	maxFiles  int

	pool *writerPool // Shared pointer: multiple handlers use the same writer pool after cloning.
}

func newFileHandler(baseDir, subdir string, levelVar *slog.LevelVar, maxSizeMB, maxDays, maxFiles int) *FileHandler {
	return &FileHandler{
		baseDir:   baseDir,
		subdir:    subdir,
		levelVar:  levelVar,
		maxSizeMB: maxSizeMB,
		maxDays:   maxDays,
		maxFiles:  maxFiles,
		pool:      &writerPool{writers: make(map[Category]*rotateWriter)},
	}
}

func (h *FileHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.levelVar.Level()
}

func (h *FileHandler) Handle(_ context.Context, r slog.Record) error {
	cat := h.extractCategory(r)

	if cat == "" {
		cat = h.defaultCategory()
	}

	w, err := h.getOrCreateWriter(cat)
	if err != nil {
		return err
	}

	entry := h.buildEntry(r, cat)
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

func (h *FileHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h.withAttrs(attrs)
}

func (h *FileHandler) WithGroup(_ string) slog.Handler {
	return h.withGroup("")
}

func (h *FileHandler) withAttrs(attrs []slog.Attr) *FileHandler {
	newH := h.clone()
	newH.preAttrs = append(newH.preAttrs, attrs...)
	return newH
}

func (h *FileHandler) withGroup(_ string) *FileHandler {
	return h.clone()
}

func (h *FileHandler) clone() *FileHandler {
	newH := *h
	newH.preAttrs = append([]slog.Attr{}, h.preAttrs...)
	// The pool pointer is already shared through value copying; no additional handling needed.
	return &newH
}

func (h *FileHandler) close() error {
	h.pool.mu.Lock()
	defer h.pool.mu.Unlock()
	var firstErr error
	for _, w := range h.pool.writers {
		if err := w.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// buildEntry constructs a JSONL entry.
func (h *FileHandler) buildEntry(r slog.Record, cat Category) map[string]any {
	entry := map[string]any{
		"ts":       r.Time.Local().Format(time.RFC3339Nano),
		"level":    r.Level.String(),
		"category": string(cat),
		"msg":      r.Message,
	}

	ctx := map[string]any{}

	// Merge preAttrs (from WithAttrs)
	for _, a := range h.preAttrs {
		applyAttr(entry, ctx, a)
	}

	// Merge record attrs
	r.Attrs(func(a slog.Attr) bool {
		applyAttr(entry, ctx, a)
		return true
	})

	if len(ctx) > 0 {
		entry["ctx"] = ctx
	}
	return entry
}

// applyAttr writes slog.Attr to the top level of entry or to the ctx sub-object.
func applyAttr(entry, ctx map[string]any, a slog.Attr) {
	key := a.Key
	val := a.Value.Any()

	// Top-level reserved fields
	switch key {
	case "trace_id", "actor_id", "team_id", "session_id", "duration_ms", "err", "category":
		entry[key] = val
	default:
		ctx[key] = val
	}
}

// extractCategory extracts the category from record attrs.
func (h *FileHandler) extractCategory(r slog.Record) Category {
	var cat Category
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "category" {
			cat = Category(a.Value.String())
			return false
		}
		return true
	})
	// Also check preAttrs
	if cat == "" {
		for _, a := range h.preAttrs {
			if a.Key == "category" {
				cat = Category(a.Value.String())
				break
			}
		}
	}
	return cat
}

// defaultCategory returns the first category as a fallback.
func (h *FileHandler) defaultCategory() Category {
	if len(systemCategories) == 0 {
		return CatApp
	}
	return systemCategories[0]
}

// getOrCreateWriter lazily creates a rotateWriter by category.
func (h *FileHandler) getOrCreateWriter(cat Category) (*rotateWriter, error) {
	h.pool.mu.Lock()
	defer h.pool.mu.Unlock()

	if w, ok := h.pool.writers[cat]; ok {
		return w, nil
	}

	dir := h.logDir()
	w, err := newRotateWriter(dir, string(cat), true, h.maxSizeMB, h.maxDays, h.maxFiles)
	if err != nil {
		return nil, err
	}
	h.pool.writers[cat] = w
	return w, nil
}

// logDir returns the log directory path: {baseDir}/logs/{subdir}/
func (h *FileHandler) logDir() string {
	return filepath.Join(h.baseDir, "logs", h.subdir)
}