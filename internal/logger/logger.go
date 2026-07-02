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
	levelVar  *slog.LevelVar
	console   bool
	file      bool
	subdir    string
	maxSizeMB int
	maxDays   int
	maxFiles  int
}

type Option func(*options)

func WithLevel(level slog.Level) Option {
	return func(o *options) { o.levelVar.Set(level) }
}

func WithConsole(enabled bool) Option {
	return func(o *options) { o.console = enabled }
}

func WithFile(enabled bool) Option {
	return func(o *options) { o.file = enabled }
}

func WithLogSubdir(dir string) Option {
	return func(o *options) { o.subdir = dir }
}

func defaultOptions() options {
	return options{
		levelVar:  &slog.LevelVar{},
		console:   false,
		file:      true,
		subdir:    "system",
		maxSizeMB: 50,
		maxDays:   15,
		maxFiles:  5,
	}
}

// ─── Logger ───────────────────────────────────────────────────────────────────

// Logger wraps slog.Logger, with all logs uniformly written to the 'system' directory.
type Logger struct {
	inner    *slog.Logger
	baseDir  string
	handler  *MultiHandler
	levelVar *slog.LevelVar // Shared with the handler, supports dynamic log level adjustment at runtime.
}

// ─── Factory Functions ────────────────────────────────────────────────────────

// New creates a Logger instance, with all logs uniformly written to {baseDir}/logs/system/.
func New(baseDir string, opts ...Option) (*Logger, error) {
	return newLogger(baseDir, opts...)
}

// System creates a system-level Logger (an alias, equivalent to New).
// Deprecated: Use New instead.
func System(baseDir string, opts ...Option) (*Logger, error) {
	return newLogger(baseDir, opts...)
}

func newLogger(baseDir string, opts ...Option) (*Logger, error) {
	o := defaultOptions()
	for _, opt := range opts {
		opt(&o)
	}

	handlerOpts := &slog.HandlerOptions{Level: o.levelVar}

	// Console handler
	var consoleHandler slog.Handler
	if o.console {
		consoleHandler = slog.NewTextHandler(os.Stderr, handlerOpts)
	}

	// File handler
	var fileHandler *FileHandler
	if o.file {
		fileHandler = newFileHandler(baseDir, o.subdir, o.levelVar, o.maxSizeMB, o.maxDays, o.maxFiles)
	}

	multi := newMultiHandler(consoleHandler, fileHandler)
	inner := slog.New(multi)

	return &Logger{
		inner:    inner,
		baseDir:  baseDir,
		handler:  multi,
		levelVar: o.levelVar,
	}, nil
}

// ─── Child / Context ──────────────────────────────────────────────────────────

// Slog returns the underlying *slog.Logger for interop with components
// that accept standard library loggers (e.g., router, third-party libs).
func (l *Logger) Slog() *slog.Logger {
	if l == nil {
		return slog.Default()
	}
	return l.inner
}

// Child returns a child Logger with additional attributes.
func (l *Logger) Child(attrs ...slog.Attr) *Logger {
	args := make([]any, len(attrs))
	for i, a := range attrs {
		args[i] = a
	}
	return &Logger{
		inner:    l.inner.With(args...),
		baseDir:  l.baseDir,
		handler:  l.handler,
		levelVar: l.levelVar,
	}
}

// WithTraceID returns a child Logger with the specified trace_id.
func (l *Logger) WithTraceID(id string) *Logger {
	return l.Child(slog.String("trace_id", id))
}

// NewTraceID returns a child Logger with a random 8-character hex trace_id.
func (l *Logger) NewTraceID() *Logger {
	return l.WithTraceID(randomHex(4))
}

// SetLevel dynamically adjusts the log level at runtime, useful for hot-reloading configurations.
// levelVar is shared with the handler, changes take effect immediately and are concurrency-safe.
func (l *Logger) SetLevel(level slog.Level) {
	if l.levelVar != nil {
		l.levelVar.Set(level)
	}
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

// DebugContext / InfoContext / WarnContext / ErrorContext automatically extract
// trace_id / actor_id from ctx and inject them into logs, aligning with slog's standard idiom.
//
// Injection order: context-extracted fields first, then user-provided args.
// This way, explicitly provided fields by the user will override those from the context (consistent with slog semantics: latter overrides former).
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

// LogError logs an error-level message, automatically serializing the error to the "err" field.
//
// Accepts ctx, automatically extracting standard fields like trace_id / actor_id from it.
func (l *Logger) LogError(ctx context.Context, cat Category, msg string, err error, args ...any) {
	errAttr := slog.Any("err", map[string]string{
		"message": err.Error(),
	})
	allArgs := append([]any{errAttr}, args...)
	l.logCtx(ctx, slog.LevelError, cat, msg, allArgs...)
}

// LogDuration executes fn and logs the elapsed time to the "duration_ms" field.
//
// Accepts ctx, which is passed to fn and used for logging (extracting trace_id / actor_id from it).
// Logs at info level on success; logs at error level on failure (including the err field).
func (l *Logger) LogDuration(ctx context.Context, cat Category, msg string, fn func(ctx context.Context) error) error {
	start := time.Now()
	err := fn(ctx)
	ms := time.Since(start).Milliseconds()

	if err != nil {
		l.LogError(ctx, cat, msg, err, slog.Int64("duration_ms", ms))
	} else {
		l.logCtx(ctx, slog.LevelInfo, cat, msg, slog.Int64("duration_ms", ms))
	}
	return err
}

// Close closes the file handler (flushes buffer).
func (l *Logger) Close() error {
	return l.handler.close()
}

// CloseAndCleanup closes the file handler.
// Deprecated: Use Close instead.
func (l *Logger) CloseAndCleanup() error {
	return l.Close()
}

// ─── Internal ─────────────────────────────────────────────────────────────────

// logCtx is the internal implementation for all log methods.
//
// Extracts trace_id / actor_id from ctx and injects them before other attributes.
// Explicitly passed args with the same name will override those from ctx (consistent with slog semantics).
func (l *Logger) logCtx(ctx context.Context, level slog.Level, cat Category, msg string, args ...any) {
	if !l.inner.Enabled(ctx, level) {
		return
	}

	// Build record (skip logger.go's own caller frame)
	var pcs [1]uintptr
	runtime.Callers(3, pcs[:])
	r := slog.NewRecord(time.Now(), level, msg, pcs[0])

	// Append category attr
	r.AddAttrs(slog.String("category", string(cat)))

	// Automatically extract standard fields from ctx and inject them; before user args, allowing user override.
	if id := TraceIDFromContext(ctx); id != "" {
		r.AddAttrs(slog.String("trace_id", id))
	}
	if id := ActorIDFromContext(ctx); id != "" {
		r.AddAttrs(slog.String("actor_id", id))
	}

	// Append user-provided args (supports slog.Attr and key/value pairs)
	r.Add(args...)

	if err := l.inner.Handler().Handle(ctx, r); err != nil {
		// Fallback to stderr if log writing fails to avoid silently losing logs.
		fmt.Fprintf(os.Stderr, "logger Handle error: %v\n", err)
	}
}

// randomHex generates a random hex string of n bytes (length 2n).
func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand.Read failed: %v", err))
	}
	return hex.EncodeToString(b)
}