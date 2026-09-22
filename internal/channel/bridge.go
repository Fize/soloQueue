package channel

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
)

const (
	busyReply  = "Thinking, please try again later~"
	errorReply = "Sorry, an error occurred while processing your message: "
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
	transcribeVoice  func(context.Context, []byte) (string, error)
}

// SetVoiceTranscriber enables the existing local SILK ASR fallback for
// channels whose server did not provide a voice transcript.
func (b *TextBridge) SetVoiceTranscriber(fn func(context.Context, []byte) (string, error)) {
	b.transcribeVoice = fn
}

func NewTextBridge(sess SessionProvider, sender TextSender, log *logger.Logger, version string, whitelistEnabled bool, whitelist []string) *TextBridge {
	allowed := make(map[string]struct{}, len(whitelist))
	for _, id := range whitelist {
		allowed[id] = struct{}{}
	}
	return &TextBridge{sess: sess, sender: sender, log: log, version: version, whitelistEnabled: whitelistEnabled, whitelist: allowed}
}

func (b *TextBridge) OnMessage(ctx context.Context, msg Message) {
	// Register channel sender for system notifications (cron, etc.).
	if s, ok := b.sess.(interface {
		SetChannelSenderData(string, []byte, func(context.Context, string) error)
	}); ok {
		msgBytes, _ := json.Marshal(msg)
		registeredMsg := msg
		s.SetChannelSenderData(msg.Channel, msgBytes, func(ctx context.Context, text string) error {
			return b.sender.SendText(ctx, registeredMsg, text)
		})
		b.log.InfoContext(ctx, logger.CatApp, "bridge: channelSender registered with metadata",
			"channel", msg.Channel,
		)
	} else if s, ok := b.sess.(interface {
		SetChannelSender(string, func(context.Context, string) error)
	}); ok {
		registeredMsg := msg
		s.SetChannelSender(msg.Channel, func(ctx context.Context, text string) error {
			return b.sender.SendText(ctx, registeredMsg, text)
		})
		b.log.InfoContext(ctx, logger.CatApp, "bridge: channelSender registered",
			"channel", msg.Channel,
		)
	}
	if mediaSender, ok := b.sender.(MediaSender); ok {
		if s, ok := b.sess.(interface {
			SetChannelMediaSender(string, func(context.Context, []OutboundMedia) error)
		}); ok {
			registeredMsg := msg
			s.SetChannelMediaSender(msg.Channel, func(ctx context.Context, media []OutboundMedia) error {
				return mediaSender.SendMedia(ctx, registeredMsg, media)
			})
		}
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
	prompt, ctx := b.preparePrompt(ctx, msg)
	if strings.TrimSpace(prompt) == "" {
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

	result, err := b.sess.AskStream(ctx, prompt, func(ctx context.Context, content string) {
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
			b.send(ctx, msg, errorReply+UserFacingError(err))
		}
		return
	}
	if result != nil {
		if len(result.MediaList) > 0 {
			mediaSender, ok := b.sender.(MediaSender)
			if !ok {
				b.log.WarnContext(ctx, logger.CatApp, "channel media sender unavailable", "channel", msg.Channel)
			} else if err := mediaSender.SendMedia(ctx, msg, result.MediaList); err != nil {
				b.log.WarnContext(ctx, logger.CatApp, "channel media reply failed", "channel", msg.Channel, "err", err.Error())
			}
		}
		if strings.TrimSpace(result.Content) != "" {
			b.send(ctx, msg, result.Content)
		}
	}
}

func (b *TextBridge) preparePrompt(ctx context.Context, msg Message) (string, context.Context) {
	var prompt strings.Builder
	prompt.WriteString(msg.Text)
	var files []string
	var fileAttachments []ctxwin.FileAttachment
	var images []llm.ImageContent
	for i, attachment := range msg.Attachments {
		if attachment.Kind == AttachmentAudio && strings.TrimSpace(attachment.Transcript) != "" {
			if !strings.Contains(msg.Text, attachment.Transcript) {
				if prompt.Len() > 0 {
					prompt.WriteString("\n")
				}
				prompt.WriteString(attachment.Transcript)
			}
			continue
		}
		if attachment.Kind == AttachmentAudio && strings.TrimSpace(attachment.Transcript) == "" && len(attachment.Data) > 0 && b.transcribeVoice != nil {
			transcript, err := b.transcribeVoice(ctx, attachment.Data)
			if err != nil {
				b.log.WarnContext(ctx, logger.CatApp, "channel voice transcription failed", "channel", msg.Channel, "err", err.Error())
			} else {
				attachment.Transcript = strings.TrimSpace(transcript)
			}
		}
		if attachment.Kind == AttachmentAudio && attachment.Transcript != "" {
			if prompt.Len() > 0 {
				prompt.WriteString("\n")
			}
			prompt.WriteString(attachment.Transcript)
			continue
		}
		name := filepath.Base(strings.TrimSpace(attachment.Name))
		if name == "." || name == "" {
			name = fmt.Sprintf("attachment-%d%s", i+1, attachmentExtension(attachment))
		}
		localPath := attachment.LocalPath
		if len(attachment.Data) > 0 {
			var err error
			localPath, err = b.sess.SaveUploadedFile(ctx, name, attachment.Data)
			if err != nil {
				b.log.WarnContext(ctx, logger.CatApp, "channel attachment save failed", "channel", msg.Channel, "name", name, "err", err.Error())
				continue
			}
		}
		if attachment.Kind == AttachmentImage && len(attachment.Data) > 0 {
			mimeType := attachment.MIMEType
			if mimeType == "" {
				mimeType = http.DetectContentType(attachment.Data)
			}
			images = append(images, llm.ImageContent{Data: base64.StdEncoding.EncodeToString(attachment.Data), MimeType: mimeType})
		}
		if strings.TrimSpace(attachment.Transcript) != "" && !strings.Contains(msg.Text, attachment.Transcript) {
			files = append(files, fmt.Sprintf("- Voice transcript: %s", attachment.Transcript))
		}
		if localPath != "" {
			files = append(files, fmt.Sprintf("- Filename: %s\n  Saved path: %s", name, localPath))
			fileAttachments = append(fileAttachments, ctxwin.FileAttachment{Name: name, Path: localPath})
		} else if attachment.Transcript == "" {
			files = append(files, fmt.Sprintf("- Attachment: %s (%s)", name, attachment.Kind))
		}
	}
	if len(files) > 0 {
		prompt.WriteString("\n\n[User uploaded attachments:\n")
		prompt.WriteString(strings.Join(files, "\n"))
		prompt.WriteString("\n]")
	}
	if len(images) > 0 {
		ctx = context.WithValue(ctx, ctxwin.ImageContextKey, images)
		prompt.WriteString("\n[User uploaded images, processed by visual recognition]")
	}
	if len(fileAttachments) > 0 {
		ctx = context.WithValue(ctx, ctxwin.FilesContextKey, fileAttachments)
	}
	return strings.TrimSpace(prompt.String()), ctx
}

func attachmentExtension(attachment Attachment) string {
	if extensions, _ := mime.ExtensionsByType(attachment.MIMEType); len(extensions) > 0 {
		return extensions[0]
	}
	switch attachment.Kind {
	case AttachmentImage:
		return ".jpg"
	case AttachmentAudio:
		return ".silk"
	case AttachmentVideo:
		return ".mp4"
	default:
		return ".bin"
	}
}

func channelUserRef(userID string) string {
	sum := sha256.Sum256([]byte(userID))
	return hex.EncodeToString(sum[:6])
}

func (b *TextBridge) handleCommand(ctx context.Context, msg Message) bool {
	switch strings.ToLower(strings.TrimSpace(msg.Text)) {
	case "/myid", "/openid":
		b.send(ctx, msg, fmt.Sprintf("Your User ID is:\n%s", msg.UserID))
	case "/help":
		b.send(ctx, msg, "Available commands: /help /cancel /clear /compact /version /myid")
	case "/version":
		b.send(ctx, msg, "SoloQueue "+b.version)
	case "/cancel":
		if err := b.sess.CancelCurrent("user requested cancellation"); err != nil {
			b.log.WarnContext(ctx, logger.CatApp, "channel cancel failed", "channel", msg.Channel, "err", err.Error())
			b.send(ctx, msg, errorReply+UserFacingError(err))
		} else {
			b.send(ctx, msg, "Current task cancelled.")
		}
	case "/clear":
		if err := b.sess.Clear(ctx); err != nil {
			b.log.WarnContext(ctx, logger.CatApp, "channel clear failed", "channel", msg.Channel, "err", err.Error())
			b.send(ctx, msg, errorReply+UserFacingError(err))
		} else {
			b.send(ctx, msg, "Context cleared.")
		}
	case "/compact":
		if err := b.sess.Compact(ctx); err != nil {
			b.log.WarnContext(ctx, logger.CatApp, "channel compact failed", "channel", msg.Channel, "err", err.Error())
			b.send(ctx, msg, errorReply+UserFacingError(err))
		} else {
			b.send(ctx, msg, "Context compressed.")
		}
	default:
		return false
	}
	return true
}

func (b *TextBridge) send(ctx context.Context, msg Message, text string) {
	_ = b.Send(ctx, msg, text)
}

// Send sends a text response and returns any error encountered.
func (b *TextBridge) Send(ctx context.Context, msg Message, text string) error {
	start := time.Now()
	err := b.sender.SendText(ctx, msg, text)
	if err != nil {
		b.log.WarnContext(ctx, logger.CatApp, "channel reply failed", "channel", msg.Channel, "text_len", len(text), "duration_ms", time.Since(start).Milliseconds(), "err", err.Error())
		return err
	}
	b.log.InfoContext(ctx, logger.CatApp, "channel reply sent", "channel", msg.Channel, "text_len", len(text), "duration_ms", time.Since(start).Milliseconds())
	return nil
}
