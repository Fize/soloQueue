package logger

import "context"

// ─── Context Keys ─────────────────────────────────────────────────────────────

// ctxKey is the type for logger field keys in a context
// Using an unexported custom type prevents key collisions
type ctxKey int

const (
	ctxKeyTraceID ctxKey = iota
	ctxKeyActorID
)

// WithTraceID returns a new context carrying the trace_id
// The logger's *Context method automatically extracts the trace_id from the context and injects it into logs
func WithTraceID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, ctxKeyTraceID, id)
}

// WithActorID returns a new context carrying the actor_id
func WithActorID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, ctxKeyActorID, id)
}

// TraceIDFromContext extracts the trace_id from the context; returns "" if not present
func TraceIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(ctxKeyTraceID).(string)
	return v
}

// ActorIDFromContext extracts the actor_id from the context; returns "" if not present
func ActorIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(ctxKeyActorID).(string)
	return v
}