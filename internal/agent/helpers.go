package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"

	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// ─── Helpers ─────────────────────────────────────────────────────────────────

// payloadToLLMMessages 将 ctxwin.PayloadMessage 切片转为 LLMMessage 切片
func payloadToLLMMessages(payload []ctxwin.PayloadMessage) []LLMMessage {
	out := make([]LLMMessage, 0, len(payload))
	for _, p := range payload {
		out = append(out, LLMMessage{
			Role:             p.Role,
			Content:          p.Content,
			ReasoningContent: p.ReasoningContent,
			Name:             p.Name,
			ToolCallID:       p.ToolCallID,
			ToolCalls:        p.ToolCalls,
		})
	}
	return out
}

// buildMessages 组装 system + user 两条消息
//
// 如 systemPrompt 为空，跳过 system 消息（避免 `{"role":"system","content":""}`）
func buildMessages(systemPrompt, userPrompt string) []LLMMessage {
	msgs := make([]LLMMessage, 0, 2)
	if systemPrompt != "" {
		msgs = append(msgs, LLMMessage{Role: "system", Content: systemPrompt})
	}
	msgs = append(msgs, LLMMessage{Role: "user", Content: userPrompt})
	return msgs
}

// logInfo / logError 是 nil-safe 的日志包装
func (a *Agent) logInfo(ctx context.Context, cat logger.Category, msg string, args ...any) {
	if a.Log == nil {
		return
	}
	a.Log.InfoContext(ctx, cat, msg, args...)
}

func (a *Agent) logError(ctx context.Context, cat logger.Category, msg string, err error, args ...any) {
	if a.Log == nil {
		return
	}
	allArgs := append([]any{slog.String("err", err.Error())}, args...)
	a.Log.ErrorContext(ctx, cat, msg, allArgs...)
}

// mergeCtx 返回一个 context，a 或 b 任一取消都会取消返回的 context
//
// 实现：起一个 goroutine 监听两个源；返回的 cancel func 保证 goroutine
// 总能退出（调用 cancel 或任一源取消都会让 goroutine 退出），无泄漏。
func mergeCtx(a, b context.Context) (context.Context, context.CancelFunc) {
	merged, cancel := context.WithCancel(a)
	if b == nil || b.Done() == nil {
		return merged, cancel
	}
	go func() {
		select {
		case <-b.Done():
			cancel()
		case <-merged.Done():
			// a 取消或 caller 调 cancel；不需要额外动作，goroutine 退出即可
		}
	}()
	return merged, cancel
}

// ─── Trace / actor_id injection ─────────────────────────────────────────────

// ensureTraceID 保证 ctx 里带 trace_id
//
// 策略（用户确认）：有则用、无则自生
// 新生成的是 8 字节 hex（16 个字符），足够在单个进程内区分并发 Ask。
func ensureTraceID(ctx context.Context) context.Context {
	if logger.TraceIDFromContext(ctx) != "" {
		return ctx
	}
	return logger.WithTraceID(ctx, newTraceID())
}

// ctxWithAgentAttrs 把 actor_id 注入 ctx，供后续 Logger 自动从 ctx 提取
//
// Agent 构造时 Def.ID 就固定了；每次 Ask/Submit/lifecycle 日志都应该带。
func (a *Agent) ctxWithAgentAttrs(ctx context.Context) context.Context {
	if a.Def.ID != "" {
		ctx = logger.WithActorID(ctx, a.Def.ID)
	}
	return ctx
}

// newTraceID 返回一个 8 字节 hex 编码的随机 trace ID（16 字符）
func newTraceID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("crypto/rand.Read failed: %v", err))
	}
	return hex.EncodeToString(b[:])
}
