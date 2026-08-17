package wechat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/channel"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

type bufferedMessage struct {
	ctx   context.Context
	msg   channel.Message
	start time.Time
	timer *time.Timer
}

type messageBuffer struct {
	mu      sync.Mutex
	window  time.Duration
	log     *logger.Logger
	handler channel.Handler
	pending map[string]*bufferedMessage
	closed  bool
}

func newMessageBuffer(window time.Duration, log *logger.Logger, handler channel.Handler) *messageBuffer {
	return &messageBuffer{window: window, log: log, handler: handler, pending: make(map[string]*bufferedMessage)}
}

func (b *messageBuffer) Push(ctx context.Context, msg channel.Message) {
	if strings.EqualFold(strings.TrimSpace(msg.Text), "/cancel") {
		key := messageRouteKey(msg)
		b.mu.Lock()
		if pending := b.pending[key]; pending != nil {
			delete(b.pending, key)
			pending.timer.Stop()
		}
		closed := b.closed
		b.mu.Unlock()
		if !closed {
			b.handler.OnMessage(ctx, msg)
		}
		return
	}
	if isBuiltInCommand(msg.Text) || strings.TrimSpace(msg.Text) != "" && len(msg.Attachments) > 0 {
		b.handler.OnMessage(ctx, msg)
		return
	}
	key := messageRouteKey(msg)
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	if previous := b.pending[key]; previous != nil && time.Since(previous.start) <= b.window && complementaryMessages(previous.msg, msg) {
		delete(b.pending, key)
		previous.timer.Stop()
		merged := mergeMessages(previous.msg, msg)
		b.mu.Unlock()
		b.logEvent(ctx, "wechat_turn_merged", merged, "wait_ms", time.Since(previous.start).Milliseconds(), "reason", "complementary")
		b.handler.OnMessage(ctx, merged)
		return
	}
	if previous := b.pending[key]; previous != nil {
		delete(b.pending, key)
		previous.timer.Stop()
		b.mu.Unlock()
		b.logEvent(previous.ctx, "wechat_turn_flushed", previous.msg, "wait_ms", time.Since(previous.start).Milliseconds(), "reason", "non_complementary")
		go b.handler.OnMessage(previous.ctx, previous.msg)
		b.Push(ctx, msg)
		return
	}
	entry := &bufferedMessage{ctx: ctx, msg: msg, start: time.Now()}
	entry.timer = time.AfterFunc(b.window, func() { b.flush(key, entry) })
	b.pending[key] = entry
	b.mu.Unlock()
	b.logEvent(ctx, "wechat_turn_buffered", msg, "reason", "awaiting_complement")
}

func (b *messageBuffer) logEvent(ctx context.Context, event string, msg channel.Message, fields ...any) {
	if b.log == nil {
		return
	}
	base := []any{
		"route_hash", messageRouteHash(msg),
		"text_present", strings.TrimSpace(msg.Text) != "",
		"attachment_count", len(msg.Attachments),
	}
	b.log.InfoContext(ctx, logger.CatApp, event, append(base, fields...)...)
}

func messageRouteHash(msg channel.Message) string {
	sum := sha256.Sum256([]byte(messageRouteKey(msg)))
	return hex.EncodeToString(sum[:6])
}

func isBuiltInCommand(text string) bool {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "/myid", "/openid", "/help", "/version", "/cancel", "/clear", "/compact":
		return true
	default:
		return false
	}
}

func (b *messageBuffer) flush(key string, entry *bufferedMessage) {
	b.mu.Lock()
	if b.closed || b.pending[key] != entry {
		b.mu.Unlock()
		return
	}
	delete(b.pending, key)
	b.mu.Unlock()
	waitMS := time.Since(entry.start).Milliseconds()
	b.logEvent(entry.ctx, "wechat_turn_timeout", entry.msg, "wait_ms", waitMS, "reason", "timeout")
	b.logEvent(entry.ctx, "wechat_turn_flushed", entry.msg, "wait_ms", waitMS, "reason", "timeout")
	b.handler.OnMessage(entry.ctx, entry.msg)
}

func (b *messageBuffer) Close() {
	b.mu.Lock()
	b.closed = true
	for _, entry := range b.pending {
		entry.timer.Stop()
	}
	b.pending = nil
	b.mu.Unlock()
}

func messageRouteKey(msg channel.Message) string {
	return strings.Join([]string{msg.Channel, msg.AccountID, msg.ConversationID, msg.UserID}, "\x00")
}

func complementaryMessages(a, b channel.Message) bool {
	return strings.TrimSpace(a.Text) != "" && len(a.Attachments) == 0 && strings.TrimSpace(b.Text) == "" && len(b.Attachments) > 0 ||
		strings.TrimSpace(b.Text) != "" && len(b.Attachments) == 0 && strings.TrimSpace(a.Text) == "" && len(a.Attachments) > 0
}

func mergeMessages(first, second channel.Message) channel.Message {
	merged := second
	if strings.TrimSpace(merged.Text) == "" {
		merged.Text = first.Text
	}
	if len(merged.Attachments) == 0 {
		merged.Attachments = append([]channel.Attachment(nil), first.Attachments...)
	}
	return merged
}
