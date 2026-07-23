package channel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

const (
	busyReply      = "Thinking, please try again later~"
	errorReply     = "Sorry, an error occurred while processing your message: "
	mediaOnlyReply = "暂时无法识别这条媒体消息，请发送文字或语音转写后重试。"
)

// TextBridge connects a text-capable channel to a SoloQueue session. Rich
// channel-specific behavior can remain in the transport adapter.
type TextBridge struct {
	sess             SessionProvider
	sender           TextSender
	log              *logger.Logger
	version          string
	whitelistEnabled bool
	whitelist        map[string]struct{}
	lastMsg          Message // latest message context for system notifications
}

func NewTextBridge(sess SessionProvider, sender TextSender, log *logger.Logger, version string, whitelistEnabled bool, whitelist []string) *TextBridge {
	allowed := make(map[string]struct{}, len(whitelist))
	for _, id := range whitelist {
		allowed[id] = struct{}{}
	}
	return &TextBridge{sess: sess, sender: sender, log: log, version: version, whitelistEnabled: whitelistEnabled, whitelist: allowed}
}

func (b *TextBridge) OnMessage(ctx context.Context, msg Message) {
	b.lastMsg = msg

	// Register channel sender for system notifications (cron, etc.).
	if s, ok := b.sess.(interface{ SetChannelSender(string, func(context.Context, string) error) }); ok {
		s.SetChannelSender(msg.Channel, func(ctx context.Context, text string) error {
			return b.sender.SendText(ctx, b.lastMsg, text)
		})
		b.log.InfoContext(ctx, logger.CatApp, "bridge: channelSender registered",
			"channel", msg.Channel,
		)
	}

	if b.whitelistEnabled {
		if _, ok := b.whitelist[msg.UserID]; !ok {
			b.log.InfoContext(ctx, logger.CatApp, "channel message ignored: user not in whitelist", "channel", msg.Channel, "user_ref", channelUserRef(msg.UserID))
			return
		}
	}

	if b.handleCommand(ctx, msg) {
		return
	}
	if strings.TrimSpace(msg.Text) == "" {
		if len(msg.Attachments) > 0 {
			b.send(ctx, msg, mediaOnlyReply)
		}
		return
	}

	b.log.InfoContext(ctx, logger.CatApp, "channel message received", "channel", msg.Channel, "user_ref", channelUserRef(msg.UserID), "content_len", len(msg.Text))
	stopActivity := func() {}
	if starter, ok := b.sender.(ResponseActivityStarter); ok {
		stop, err := starter.StartResponseActivity(ctx, msg)
		if err != nil {
			b.log.WarnContext(ctx, logger.CatApp, "channel response activity unavailable", "channel", msg.Channel, "err", err.Error())
		} else if stop != nil {
			stopActivity = sync.OnceFunc(stop)
			b.log.InfoContext(ctx, logger.CatApp, "channel response activity started", "channel", msg.Channel)
		}
	}
	defer stopActivity()

	result, err := b.sess.AskStream(ctx, msg.Text, func(ctx context.Context, content string) {
		if strings.TrimSpace(content) != "" {
			b.send(ctx, msg, content)
		}
	})
	stopActivity()
	if err != nil {
		switch {
		case errors.Is(err, ErrSessionBusy):
			b.send(ctx, msg, busyReply)
		case errors.Is(err, ErrQueued), errors.Is(err, ErrTaskCancelled):
			return
		default:
			b.log.WarnContext(ctx, logger.CatApp, "channel session request failed", "channel", msg.Channel, "err", err.Error())
			b.send(ctx, msg, errorReply+err.Error())
		}
		return
	}
	if result != nil && strings.TrimSpace(result.Content) != "" {
		b.send(ctx, msg, result.Content)
	}
}

func channelUserRef(userID string) string {
	sum := sha256.Sum256([]byte(userID))
	return hex.EncodeToString(sum[:6])
}

func (b *TextBridge) handleCommand(ctx context.Context, msg Message) bool {
	switch strings.ToLower(strings.TrimSpace(msg.Text)) {
	case "/myid", "/openid":
		b.send(ctx, msg, fmt.Sprintf("您的用户 ID 是：\n%s", msg.UserID))
	case "/help":
		b.send(ctx, msg, "可用命令：/help /cancel /clear /compact /version /myid")
	case "/version":
		b.send(ctx, msg, "SoloQueue "+b.version)
	case "/cancel":
		if err := b.sess.CancelCurrent("user requested cancellation"); err != nil {
			b.send(ctx, msg, errorReply+err.Error())
		} else {
			b.send(ctx, msg, "已取消当前任务。")
		}
	case "/clear":
		if err := b.sess.Clear(ctx); err != nil {
			b.send(ctx, msg, errorReply+err.Error())
		} else {
			b.send(ctx, msg, "上下文已清除。")
		}
	case "/compact":
		if err := b.sess.Compact(ctx); err != nil {
			b.send(ctx, msg, errorReply+err.Error())
		} else {
			b.send(ctx, msg, "上下文已压缩。")
		}
	default:
		return false
	}
	return true
}

func (b *TextBridge) send(ctx context.Context, msg Message, text string) {
	start := time.Now()
	if err := b.sender.SendText(ctx, msg, text); err != nil {
		b.log.WarnContext(ctx, logger.CatApp, "channel reply failed", "channel", msg.Channel, "text_len", len(text), "duration_ms", time.Since(start).Milliseconds(), "err", err.Error())
		return
	}
	b.log.InfoContext(ctx, logger.CatApp, "channel reply sent", "channel", msg.Channel, "text_len", len(text), "duration_ms", time.Since(start).Milliseconds())
}
