package wechat

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/channel"
	"github.com/xiaobaitu/soloqueue/internal/logger"
)

var ErrClosed = errors.New("wechat: gateway closed")

type Gateway struct {
	cfg     Config
	client  *Client
	handler channel.Handler
	log     *logger.Logger

	mu     sync.Mutex
	cancel context.CancelFunc
	closed bool
}

func NewGateway(cfg Config, client *Client, handler channel.Handler, log *logger.Logger) *Gateway {
	return &Gateway{cfg: cfg, client: client, handler: handler, log: log}
}

func (g *Gateway) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	g.mu.Lock()
	if g.closed {
		g.mu.Unlock()
		cancel()
		return ErrClosed
	}
	g.cancel = cancel
	g.mu.Unlock()
	defer cancel()

	cursor := ""
	timeout := defaultLongPollTimeout
	for {
		resp, err := g.client.GetUpdates(ctx, cursor, timeout)
		if err != nil {
			if ctx.Err() != nil {
				return ErrClosed
			}
			g.log.WarnContext(ctx, logger.CatApp, "wechat long poll failed, retrying", "err", err.Error())
			select {
			case <-ctx.Done():
				return ErrClosed
			case <-time.After(3 * time.Second):
			}
			continue
		}
		if resp.GetUpdatesBuf != "" {
			cursor = resp.GetUpdatesBuf
		}
		if resp.LongPollingTimeoutMS > 0 {
			timeout = time.Duration(resp.LongPollingTimeoutMS+5000) * time.Millisecond
		}
		for _, msg := range resp.Messages {
			if normalized, ok := g.normalize(msg); ok {
				nctx := channel.ContextWithChatMeta(ctx, channel.ChatMeta{
					Channel:        "wechat",
					UserID:         normalized.UserID,
					ConversationID: normalized.ConversationID,
				})
				go g.handler.OnMessage(nctx, normalized)
			}
		}
	}
}

func (g *Gateway) Close() {
	g.mu.Lock()
	g.closed = true
	if g.cancel != nil {
		g.cancel()
	}
	g.mu.Unlock()
}

func (g *Gateway) normalize(msg Message) (channel.Message, bool) {
	if msg.MessageType != 1 || msg.FromUserID == "" || msg.ContextToken == "" {
		return channel.Message{}, false
	}
	var parts []string
	var attachments []channel.Attachment
	for _, item := range msg.ItemList {
		switch item.Type {
		case 1:
			if item.TextItem != nil && item.TextItem.Text != "" {
				parts = append(parts, item.TextItem.Text)
			}
		case 3:
			if item.VoiceItem != nil {
				transcript := strings.TrimSpace(item.VoiceItem.Text)
				attachments = append(attachments, channel.Attachment{Kind: channel.AttachmentAudio, MIMEType: "audio/silk", Transcript: transcript})
				if transcript != "" {
					parts = append(parts, transcript)
				}
			}
		case 2:
			attachments = append(attachments, channel.Attachment{Kind: channel.AttachmentImage})
		case 4:
			attachment := channel.Attachment{Kind: channel.AttachmentFile}
			if item.FileItem != nil {
				attachment.Name = item.FileItem.FileName
			}
			attachments = append(attachments, attachment)
		case 5:
			attachments = append(attachments, channel.Attachment{Kind: channel.AttachmentVideo})
		}
	}
	text := strings.TrimSpace(strings.Join(parts, "\n"))
	if text == "" && len(attachments) == 0 {
		return channel.Message{}, false
	}
	conversationID := msg.SessionID
	if msg.GroupID != "" {
		conversationID = msg.GroupID
	}
	return channel.Message{
		Channel:        "wechat",
		AccountID:      g.cfg.BotID,
		ConversationID: conversationID,
		UserID:         msg.FromUserID,
		Text:           text,
		Attachments:    attachments,
		ReplyToken:     msg.ContextToken,
	}, true
}
