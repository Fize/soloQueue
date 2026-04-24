package logger

import "context"

// ─── Context Keys ─────────────────────────────────────────────────────────────

// ctxKey 是 context 中 logger 字段的 key 类型
// 使用非导出的自定义类型防止 key 冲突
type ctxKey int

const (
	ctxKeyTraceID ctxKey = iota
	ctxKeyActorID
)

// WithTraceID 返回携带 trace_id 的新 context
// logger 的 *Context 方法会自动从 context 中提取 trace_id 注入日志
func WithTraceID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, ctxKeyTraceID, id)
}

// WithActorID 返回携带 actor_id 的新 context
func WithActorID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, ctxKeyActorID, id)
}

// TraceIDFromContext 从 context 中提取 trace_id；不存在返回 ""
func TraceIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(ctxKeyTraceID).(string)
	return v
}

// ActorIDFromContext 从 context 中提取 actor_id；不存在返回 ""
func ActorIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(ctxKeyActorID).(string)
	return v
}
