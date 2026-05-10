package qqbot

import (
	"context"
	"errors"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// ─── Constants ───────────────────────────────────────────────────────────────

const (
	// QQ message content limit (approximate). Split longer messages.
	qqMessageLimit = 2000

	// Busy reply when session is occupied.
	busyReply = "正在思考中，请稍后重试~"

	// Error prefix for agent errors.
	errorPrefix = "抱歉，处理消息时出错："
)

// ─── Session interface (decoupled from session package) ───────────────────────

// AskStreamResult contains the result of a streaming ask operation.
type AskStreamResult struct {
	Content          string // final LLM response content
	ReasoningContent string // thinking/reasoning content (if any)
}

// SessionProvider is the interface qqbot needs from the session layer.
// This decouples qqbot from the concrete session package, avoiding import cycles.
type SessionProvider interface {
	// AskStream performs a streaming ask and returns the final result.
	// It consumes the entire event stream internally.
	// Returns ErrSessionBusy if another ask is in progress.
	AskStream(ctx context.Context, prompt string) (*AskStreamResult, error)
}

// ─── Errors ──────────────────────────────────────────────────────────────────

var ErrSessionBusy = errors.New("session: busy")

// ─── SessionBridge ───────────────────────────────────────────────────────────

// SessionBridge connects QQ messages to the SoloQueue Session.
// It receives QQ messages via the EventHandler interface, calls SessionProvider.AskStream,
// and sends the final reply back to QQ via the APIClient.
//
// Concurrency: the Session already serializes via inFlight (ErrSessionBusy).
// No additional guard is needed here — during async delegation the session
// correctly releases inFlight, allowing new messages to interleave.
type SessionBridge struct {
	sess SessionProvider
	api  *APIClient
	log  *logger.Logger
}

// NewSessionBridge creates a new SessionBridge.
func NewSessionBridge(sess SessionProvider, api *APIClient, log *logger.Logger) *SessionBridge {
	return &SessionBridge{
		sess: sess,
		api:  api,
		log:  log,
	}
}

// OnQQMessage implements EventHandler. Called by the Gateway when a QQ message arrives.
func (b *SessionBridge) OnQQMessage(ctx context.Context, msg QQMessage) {
	b.log.InfoContext(ctx, logger.CatApp, "qqbot message received",
		"source", msg.Source,
		"content_len", len(msg.Content),
		"open_id", msg.OpenID)

	// Use AskStream to capture the full response including reasoning content
	result, err := b.sess.AskStream(ctx, msg.Content)
	if err != nil {
		if errors.Is(err, ErrSessionBusy) {
			b.sendReply(ctx, msg, MsgTypeText, busyReply)
			return
		}
		b.log.WarnContext(ctx, logger.CatApp, "qqbot ask stream failed",
			"err", err.Error())
		b.sendReply(ctx, msg, MsgTypeText, errorPrefix+err.Error())
		return
	}

	// Determine what to send:
	// - If content is non-empty, send content only (not reasoning/tool calls)
	// - If content is empty but reasoning is non-empty, send reasoning as fallback
	reply := result.Content
	if reply == "" && result.ReasoningContent != "" {
		reply = result.ReasoningContent
	}
	if reply == "" {
		b.sendReply(ctx, msg, MsgTypeText, "（思考完毕，无回复内容）")
		return
	}

	b.log.InfoContext(ctx, logger.CatApp, "qqbot reply ready",
		"content_len", len(result.Content),
		"reasoning_len", len(result.ReasoningContent),
		"reply_len", len(reply))

	// Format as QQ-compatible markdown and send
	formatted := QQMarkdown(reply)
	b.sendReply(ctx, msg, MsgTypeMarkdown, formatted)
}

// sendReply sends the reply text to QQ, splitting into chunks if it exceeds the limit.
func (b *SessionBridge) sendReply(ctx context.Context, msg QQMessage, msgType int, text string) {
	if len(text) <= qqMessageLimit {
		if err := b.api.ReplyMessage(ctx, msg, msgType, text); err != nil {
			b.log.WarnContext(ctx, logger.CatApp, "qqbot reply send failed",
				"err", err.Error())
		}
		return
	}

	// Use markdown-aware splitting for markdown, plain split for text
	var chunks []string
	if msgType == MsgTypeMarkdown {
		chunks = SplitMarkdown(text, qqMessageLimit)
	} else {
		chunks = splitMessage(text, qqMessageLimit)
	}
	for i, chunk := range chunks {
		if err := b.api.ReplyMessage(ctx, msg, msgType, chunk); err != nil {
			b.log.WarnContext(ctx, logger.CatApp, "qqbot reply chunk send failed",
				"chunk", i+1, "total", len(chunks), "err", err.Error())
			return
		}
	}
}

// ─── Message Splitting ────────────────────────────────────────────────────────

// splitMessage splits text into chunks of at most maxLen bytes,
// preferring to split at newline boundaries.
func splitMessage(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}

	var chunks []string
	for len(text) > maxLen {
		// Try to find a newline near the end of the chunk
		splitAt := maxLen
		idx := strings.LastIndex(text[:maxLen], "\n")
		if idx > maxLen/2 {
			splitAt = idx + 1
		}
		chunks = append(chunks, text[:splitAt])
		text = text[splitAt:]
	}
	if text != "" {
		chunks = append(chunks, text)
	}
	return chunks
}
