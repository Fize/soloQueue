package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

// ─── Helpers ─────────────────────────────────────────────────────────────────


func payloadToLLMMessages(payload []ctxwin.PayloadMessage) []LLMMessage {
	out := make([]LLMMessage, 0, len(payload))
	for _, p := range payload {
		content := p.Content
		if p.Role == "user" && !p.Timestamp.IsZero() {
			content = fmt.Sprintf("[%s] %s", p.Timestamp.Format("2006-01-02 15:04:05"), p.Content)
		}
		out = append(out, LLMMessage{
			Role:             p.Role,
			Content:          content,
			Images:           p.Images,
			ReasoningContent: p.ReasoningContent,
			Name:             p.Name,
			ToolCallID:       p.ToolCallID,
			ToolCalls:        p.ToolCalls,
		})
	}
	return out
}

// buildMessages assembles system + user messages. Empty systemPrompt is skipped.
func buildMessages(systemPrompt, userPrompt string) []LLMMessage {
	msgs := make([]LLMMessage, 0, 2)
	if systemPrompt != "" {
		msgs = append(msgs, LLMMessage{Role: "system", Content: systemPrompt})
	}
	msgs = append(msgs, LLMMessage{Role: "user", Content: userPrompt})
	return msgs
}

// logInfo / logError are nil-safe log wrappers.
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
	a.Log.LogError(ctx, cat, msg, err, args...)
}

// mergeCtx returns a context cancelled if either a or b is cancelled.
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
			// a is cancelled or caller called cancel; no extra action needed, goroutine can exit.
		}
	}()
	return merged, cancel
}

// ─── Trace / actor_id injection ─────────────────────────────────────────────

// ensureTraceID reuses existing trace_id or generates a new 8-byte hex one.
func ensureTraceID(ctx context.Context) context.Context {
	if logger.TraceIDFromContext(ctx) != "" {
		return ctx
	}
	return logger.WithTraceID(ctx, newTraceID())
}

// ctxWithAgentAttrs injects actor_id (InstanceID) for structured logging.
func (a *Agent) ctxWithAgentAttrs(ctx context.Context) context.Context {
	if a.InstanceID != "" {
		ctx = logger.WithActorID(ctx, a.InstanceID)
	}
	return ctx
}

// newTraceID returns an 8-byte hex-encoded random trace ID (16 characters).
func newTraceID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("crypto/rand.Read failed: %v", err))
	}
	return hex.EncodeToString(b[:])
}