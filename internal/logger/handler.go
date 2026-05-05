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

// MultiHandler 同时写 console（stderr）和文件
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

// writerPool 是共享的 category→rotateWriter 映射
// 通过指针在 FileHandler 的 clone 之间共享，避免 mutex 被值拷贝
type writerPool struct {
	mu      sync.Mutex
	writers map[Category]*rotateWriter
}

// FileHandler 将日志按 category 路由到对应的 rotateWriter
type FileHandler struct {
	baseDir   string
	layer     Layer
	teamID    string
	sessionID string
	levelVar  *slog.LevelVar
	preAttrs  []slog.Attr
	maxSizeMB int
	maxDays   int
	maxFiles  int

	pool *writerPool // 共享指针：clone 后多个 handler 共用同一 writer pool
}

func newFileHandler(baseDir string, layer Layer, teamID, sessionID string, levelVar *slog.LevelVar, maxSizeMB, maxDays, maxFiles int) *FileHandler {
	return &FileHandler{
		baseDir:   baseDir,
		layer:     layer,
		teamID:    teamID,
		sessionID: sessionID,
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
	// pool 指针已通过值拷贝共享，无需额外处理
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

// buildEntry 构造 JSONL 条目
func (h *FileHandler) buildEntry(r slog.Record, cat Category) map[string]any {
	entry := map[string]any{
		"ts":       r.Time.UTC().Format(time.RFC3339Nano),
		"level":    r.Level.String(),
		"layer":    string(h.layer),
		"category": string(cat),
		"msg":      r.Message,
	}

	ctx := map[string]any{}

	// 合并 preAttrs（来自 WithAttrs）
	for _, a := range h.preAttrs {
		applyAttr(entry, ctx, a)
	}

	// 合并 record attrs
	r.Attrs(func(a slog.Attr) bool {
		applyAttr(entry, ctx, a)
		return true
	})

	if h.teamID != "" {
		entry["team_id"] = h.teamID
	}
	if h.sessionID != "" {
		entry["session_id"] = h.sessionID
	}

	if len(ctx) > 0 {
		entry["ctx"] = ctx
	}
	return entry
}

// applyAttr 将 slog.Attr 写入 entry 顶层或 ctx 子对象
func applyAttr(entry, ctx map[string]any, a slog.Attr) {
	key := a.Key
	val := a.Value.Any()

	// 顶层保留字段
	switch key {
	case "trace_id", "actor_id", "team_id", "session_id", "duration_ms", "err", "category":
		entry[key] = val
	default:
		ctx[key] = val
	}
}

// extractCategory 从 record attrs 提取 category
func (h *FileHandler) extractCategory(r slog.Record) Category {
	var cat Category
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "category" {
			cat = Category(a.Value.String())
			return false
		}
		return true
	})
	// 也检查 preAttrs
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

// defaultCategory 返回该 layer 的第一个 category 作为兜底
func (h *FileHandler) defaultCategory() Category {
	cats := layerCategories[h.layer]
	if len(cats) == 0 {
		return CatApp
	}
	return cats[0]
}

// getOrCreateWriter 按 category 惰性创建 rotateWriter
func (h *FileHandler) getOrCreateWriter(cat Category) (*rotateWriter, error) {
	h.pool.mu.Lock()
	defer h.pool.mu.Unlock()

	if w, ok := h.pool.writers[cat]; ok {
		return w, nil
	}

	dir := h.logDir(cat)
	byDate := h.layer != LayerSession
	w, err := newRotateWriter(dir, string(cat), byDate, h.maxSizeMB, h.maxDays, h.maxFiles)
	if err != nil {
		return nil, err
	}
	h.pool.writers[cat] = w
	return w, nil
}

// logDir 根据 layer / category 构建日志目录路径
//
// system:  {baseDir}/logs/system/
// team:    {baseDir}/logs/teams/{teamID}/
// session: {baseDir}/logs/sessions/{teamID}/{sessionID}/
func (h *FileHandler) logDir(cat Category) string {
	_ = cat // 目录按 layer 分，文件名按 category 分
	switch h.layer {
	case LayerTeam:
		return filepath.Join(h.baseDir, "logs", "teams", h.teamID)
	case LayerSession:
		return filepath.Join(h.baseDir, "logs", "sessions", h.teamID, h.sessionID)
	default:
		return filepath.Join(h.baseDir, "logs", "system")
	}
}
