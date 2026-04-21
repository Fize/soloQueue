package logger

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"time"
)

// ─── Options ─────────────────────────────────────────────────────────────────

type options struct {
	level      slog.Level
	console    bool
	file       bool
	maxSizeMB  int
	maxDays    int
}

type Option func(*options)

func WithLevel(level slog.Level) Option {
	return func(o *options) { o.level = level }
}

func WithConsole(enabled bool) Option {
	return func(o *options) { o.console = enabled }
}

func WithFile(enabled bool) Option {
	return func(o *options) { o.file = enabled }
}

func defaultOptions() options {
	return options{
		level:     slog.LevelInfo,
		console:   true,
		file:      true,
		maxSizeMB: 50,
		maxDays:   30,
	}
}

// ─── Logger ───────────────────────────────────────────────────────────────────

// Logger 是三层日志封装，每个 Logger 绑定到一个 Layer
type Logger struct {
	inner     *slog.Logger
	layer     Layer
	baseDir   string
	teamID    string
	sessionID string
	handler   *MultiHandler
}

// ─── Factory Functions ────────────────────────────────────────────────────────

// System 创建 system 层 Logger
func System(baseDir string, opts ...Option) (*Logger, error) {
	return newLogger(baseDir, LayerSystem, "", "", opts...)
}

// Team 创建 team 层 Logger
func Team(baseDir, teamID string, opts ...Option) (*Logger, error) {
	return newLogger(baseDir, LayerTeam, teamID, "", opts...)
}

// Session 创建 session 层 Logger
func Session(baseDir, teamID, sessionID string, opts ...Option) (*Logger, error) {
	return newLogger(baseDir, LayerSession, teamID, sessionID, opts...)
}

func newLogger(baseDir string, layer Layer, teamID, sessionID string, opts ...Option) (*Logger, error) {
	o := defaultOptions()
	for _, opt := range opts {
		opt(&o)
	}

	handlerOpts := &slog.HandlerOptions{Level: o.level}

	// Console handler
	var consoleHandler slog.Handler
	if o.console {
		consoleHandler = slog.NewTextHandler(os.Stderr, handlerOpts)
	}

	// File handler
	var fileHandler *FileHandler
	if o.file {
		fileHandler = newFileHandler(baseDir, layer, teamID, sessionID, o.level, o.maxSizeMB, o.maxDays)
	}

	multi := newMultiHandler(consoleHandler, fileHandler)
	inner := slog.New(multi)

	return &Logger{
		inner:     inner,
		layer:     layer,
		baseDir:   baseDir,
		teamID:    teamID,
		sessionID: sessionID,
		handler:   multi,
	}, nil
}

// ─── Child / Context ──────────────────────────────────────────────────────────

// Child 返回携带额外属性的子 Logger
func (l *Logger) Child(attrs ...slog.Attr) *Logger {
	args := make([]any, len(attrs))
	for i, a := range attrs {
		args[i] = a
	}
	return &Logger{
		inner:     l.inner.With(args...),
		layer:     l.layer,
		baseDir:   l.baseDir,
		teamID:    l.teamID,
		sessionID: l.sessionID,
		handler:   l.handler,
	}
}

// WithTraceID 返回携带指定 trace_id 的子 Logger
func (l *Logger) WithTraceID(id string) *Logger {
	return l.Child(slog.String("trace_id", id))
}

// NewTraceID 返回携带随机 8 位 hex trace_id 的子 Logger
func (l *Logger) NewTraceID() *Logger {
	return l.WithTraceID(randomHex(4))
}

// ─── Log Methods ──────────────────────────────────────────────────────────────

func (l *Logger) Debug(cat Category, msg string, args ...any) {
	l.logCtx(context.Background(), slog.LevelDebug, cat, msg, args...)
}

func (l *Logger) Info(cat Category, msg string, args ...any) {
	l.logCtx(context.Background(), slog.LevelInfo, cat, msg, args...)
}

func (l *Logger) Warn(cat Category, msg string, args ...any) {
	l.logCtx(context.Background(), slog.LevelWarn, cat, msg, args...)
}

func (l *Logger) Error(cat Category, msg string, args ...any) {
	l.logCtx(context.Background(), slog.LevelError, cat, msg, args...)
}

// ─── Context Log Methods ──────────────────────────────────────────────────────

// DebugContext / InfoContext / WarnContext / ErrorContext 从 ctx 中自动提取
// trace_id / actor_id 注入到日志，与 slog 标准 idiom 对齐
//
// 注入顺序：先放 ctx 提取的字段，再放用户传的 args
// 这样用户显式传的同名字段会覆盖 ctx 的（符合 slog 语义：后者覆盖前者）
func (l *Logger) DebugContext(ctx context.Context, cat Category, msg string, args ...any) {
	l.logCtx(ctx, slog.LevelDebug, cat, msg, args...)
}

func (l *Logger) InfoContext(ctx context.Context, cat Category, msg string, args ...any) {
	l.logCtx(ctx, slog.LevelInfo, cat, msg, args...)
}

func (l *Logger) WarnContext(ctx context.Context, cat Category, msg string, args ...any) {
	l.logCtx(ctx, slog.LevelWarn, cat, msg, args...)
}

func (l *Logger) ErrorContext(ctx context.Context, cat Category, msg string, args ...any) {
	l.logCtx(ctx, slog.LevelError, cat, msg, args...)
}

// LogError 记录 error 级别日志，自动将 err 序列化到 "err" 字段
func (l *Logger) LogError(cat Category, msg string, err error, args ...any) {
	errAttr := slog.Any("err", map[string]string{
		"message": err.Error(),
	})
	allArgs := append([]any{errAttr}, args...)
	l.logCtx(context.Background(), slog.LevelError, cat, msg, allArgs...)
}

// LogDuration 执行 fn 并记录耗时到 "duration_ms" 字段
func (l *Logger) LogDuration(cat Category, msg string, fn func() error) error {
	start := time.Now()
	err := fn()
	ms := time.Since(start).Milliseconds()

	if err != nil {
		l.LogError(cat, msg, err, slog.Int64("duration_ms", ms))
	} else {
		l.logCtx(context.Background(), slog.LevelInfo, cat, msg, slog.Int64("duration_ms", ms))
	}
	return err
}

// Close 关闭文件 handler（刷新缓冲）
func (l *Logger) Close() error {
	return l.handler.close()
}

// ─── Internal ─────────────────────────────────────────────────────────────────

// logCtx 是所有 log 方法的内部实现
//
// 从 ctx 中提取 trace_id / actor_id 注入到 attrs 前面
// 用户显式传入的同名 args 会覆盖 ctx 的（符合 slog 语义）
func (l *Logger) logCtx(ctx context.Context, level slog.Level, cat Category, msg string, args ...any) {
	if !l.inner.Enabled(ctx, level) {
		return
	}

	// 验证 category 合法性
	if !ValidCategory(l.layer, cat) {
		_, _ = fmt.Fprintf(os.Stderr, "logger: invalid category %q for layer %q\n", cat, l.layer)
		cat = l.defaultCategory()
	}

	// 构建 record（跳过 logger.go 自身的 caller 帧）
	var pcs [1]uintptr
	runtime.Callers(3, pcs[:])
	r := slog.NewRecord(time.Now(), level, msg, pcs[0])

	// 追加 category attr
	r.AddAttrs(slog.String("category", string(cat)))

	// 从 ctx 中自动提取标准字段注入；在用户 args 之前，用户可覆盖
	if id := TraceIDFromContext(ctx); id != "" {
		r.AddAttrs(slog.String("trace_id", id))
	}
	if id := ActorIDFromContext(ctx); id != "" {
		r.AddAttrs(slog.String("actor_id", id))
	}

	// 追加用户传入的 args（支持 slog.Attr 和 key/value 对）
	r.Add(args...)

	_ = l.inner.Handler().Handle(ctx, r)
}

func (l *Logger) defaultCategory() Category {
	cats := layerCategories[l.layer]
	if len(cats) == 0 {
		return CatApp
	}
	return cats[0]
}

// randomHex 生成 n 字节的随机 hex 字符串（长度 2n）
func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
