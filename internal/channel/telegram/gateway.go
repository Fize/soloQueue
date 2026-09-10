package telegram

import (
	"context"
	"errors"
	"fmt"
	"github.com/xiaobaitu/soloqueue/internal/channel"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"strconv"
	"strings"
	"sync"
	"time"
)

var ErrClosed = errors.New("telegram: gateway closed")

type Gateway struct {
	cfg     Config
	client  *Client
	handler channel.Handler
	log     *logger.Logger
	mu      sync.Mutex
	cancel  context.CancelFunc
	closed  bool
	offset  int64
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
	for {
		updates, err := g.client.GetUpdates(ctx, g.offset)
		if err != nil {
			if ctx.Err() != nil {
				return ErrClosed
			}
			if g.log != nil {
				g.log.WarnContext(ctx, logger.CatApp, "telegram long poll failed", "err", err.Error())
			}
			select {
			case <-ctx.Done():
				return ErrClosed
			case <-time.After(3 * time.Second):
			}
			continue
		}
		for _, u := range updates {
			if u.UpdateID >= g.offset {
				g.offset = u.UpdateID + 1
			}
			go g.dispatch(ctx, u)
		}
	}
}
func (g *Gateway) dispatch(ctx context.Context, u Update) {
	if msg, ok := g.normalize(ctx, u); ok {
		g.handler.OnMessage(ctx, msg)
	}
}
func (g *Gateway) normalize(ctx context.Context, u Update) (channel.Message, bool) {
	raw := u.Message
	if raw == nil {
		raw = u.EditedMessage
	}
	if raw == nil || raw.Chat.ID == 0 {
		return channel.Message{}, false
	}
	userID := ""
	if raw.From != nil {
		userID = strconv.FormatInt(raw.From.ID, 10)
	}
	attachments := []channel.Attachment{}
	add := func(kind channel.AttachmentKind, fileID, name, mime string) {
		data, path, err := g.client.Download(ctx, fileID)
		if err != nil {
			if g.log != nil {
				g.log.WarnContext(ctx, logger.CatApp, "telegram media download failed", "err", err.Error())
			}
			return
		}
		if name == "" {
			name = path
		}
		attachments = append(attachments, channel.Attachment{Kind: kind, Name: name, MIMEType: mime, Data: data})
	}
	if len(raw.Photo) > 0 {
		p := raw.Photo[len(raw.Photo)-1]
		add(channel.AttachmentImage, p.FileID, fmt.Sprintf("photo-%d.jpg", raw.MessageID), "image/jpeg")
	}
	if raw.Document != nil {
		add(channel.AttachmentFile, raw.Document.FileID, raw.Document.FileName, raw.Document.MIMEType)
	}
	if raw.Video != nil {
		add(channel.AttachmentVideo, raw.Video.FileID, raw.Video.FileName, raw.Video.MIMEType)
	}
	if raw.Audio != nil {
		add(channel.AttachmentAudio, raw.Audio.FileID, raw.Audio.FileName, raw.Audio.MIMEType)
	}
	if raw.Voice != nil {
		add(channel.AttachmentAudio, raw.Voice.FileID, fmt.Sprintf("voice-%d.ogg", raw.MessageID), raw.Voice.MIMEType)
	}
	text := strings.TrimSpace(raw.Text)
	if text == "" {
		text = strings.TrimSpace(raw.Caption)
	}
	if text == "" && len(attachments) == 0 {
		return channel.Message{}, false
	}
	accountID := g.cfg.AccountID
	if accountID == "" {
		accountID = strconv.FormatInt(g.cfg.BotID, 10)
	}
	return channel.Message{MessageID: strconv.FormatInt(raw.MessageID, 10), Channel: "telegram", AccountID: accountID, ConversationID: strconv.FormatInt(raw.Chat.ID, 10), UserID: userID, Text: text, Attachments: attachments, ReplyToken: strconv.FormatInt(raw.MessageID, 10)}, true
}
func (g *Gateway) Close() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.closed = true
	if g.cancel != nil {
		g.cancel()
	}
}
