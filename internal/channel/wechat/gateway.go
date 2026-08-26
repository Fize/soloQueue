package wechat

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/channel"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

var ErrClosed = errors.New("wechat: gateway closed")

type Gateway struct {
	cfg     Config
	client  *Client
	handler channel.Handler
	buffer  *messageBuffer
	log     *logger.Logger

	mu     sync.Mutex
	cancel context.CancelFunc
	closed bool
}

func NewGateway(cfg Config, client *Client, handler channel.Handler, log *logger.Logger) *Gateway {
	return &Gateway{cfg: cfg, client: client, handler: handler, buffer: newMessageBuffer(time.Second, log, handler), log: log}
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
			go g.dispatch(ctx, msg)
		}
	}
}

func (g *Gateway) dispatch(ctx context.Context, raw Message) {
	normalized, ok := g.normalize(raw)
	if !ok {
		return
	}
	for i := range raw.ItemList {
		if raw.ItemList[i].Type == 3 && raw.ItemList[i].VoiceItem != nil && strings.TrimSpace(raw.ItemList[i].VoiceItem.Text) != "" {
			continue
		}
		media := mediaFromItem(raw.ItemList[i])
		if media == nil {
			continue
		}
		attachmentIndex := attachmentIndexForItem(raw.ItemList[:i+1]) - 1
		if attachmentIndex < 0 || attachmentIndex >= len(normalized.Attachments) {
			continue
		}
		data, err := g.client.DownloadMedia(ctx, *media)
		if err != nil {
			g.log.WarnContext(ctx, logger.CatApp, "wechat media download failed", "item", i+1, "err", err.Error())
			continue
		}
		normalized.Attachments[attachmentIndex].Data = data
		if normalized.Attachments[attachmentIndex].MIMEType == "" {
			normalized.Attachments[attachmentIndex].MIMEType = http.DetectContentType(data)
		}
	}
	nctx := channel.ContextWithChatMeta(ctx, channel.ChatMeta{
		Channel: normalized.Channel, AccountID: normalized.AccountID,
		UserID: normalized.UserID, ConversationID: normalized.ConversationID,
	})
	g.buffer.Push(nctx, normalized)
}

func mediaFromItem(item MessageItem) *CDNMedia {
	switch item.Type {
	case 2:
		if item.ImageItem != nil {
			return item.ImageItem.Media
		}
	case 3:
		if item.VoiceItem != nil {
			return item.VoiceItem.Media
		}
	case 4:
		if item.FileItem != nil {
			return item.FileItem.Media
		}
	case 5:
		if item.VideoItem != nil {
			return item.VideoItem.Media
		}
	}
	return nil
}

func attachmentIndexForItem(items []MessageItem) int {
	count := 0
	for _, item := range items {
		if item.Type >= 2 && item.Type <= 5 {
			count++
		}
	}
	return count
}

func (g *Gateway) Close() {
	g.mu.Lock()
	g.closed = true
	if g.cancel != nil {
		g.cancel()
	}
	if g.buffer != nil {
		g.buffer.Close()
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
				attachments = append(attachments, channel.Attachment{Kind: channel.AttachmentAudio, MIMEType: "audio/silk", Name: fmt.Sprintf("voice-%d.silk", msg.MessageID), Transcript: transcript})
				if transcript != "" {
					parts = append(parts, transcript)
				}
			}
		case 2:
			attachments = append(attachments, channel.Attachment{Kind: channel.AttachmentImage, Name: fmt.Sprintf("image-%d.jpg", msg.MessageID)})
		case 4:
			attachment := channel.Attachment{Kind: channel.AttachmentFile}
			if item.FileItem != nil {
				attachment.Name = item.FileItem.FileName
			}
			attachments = append(attachments, attachment)
		case 5:
			attachments = append(attachments, channel.Attachment{Kind: channel.AttachmentVideo, Name: fmt.Sprintf("video-%d.mp4", msg.MessageID)})
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
		MessageID:      strconv.FormatInt(msg.MessageID, 10),
		Channel:        "wechat",
		AccountID:      g.cfg.BotID,
		ConversationID: conversationID,
		UserID:         msg.FromUserID,
		Text:           text,
		Attachments:    attachments,
		ReplyToken:     msg.ContextToken,
	}, true
}
