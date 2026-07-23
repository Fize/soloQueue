package qq

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/channel"
	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// ─── Constants ───────────────────────────────────────────────────────────────

const (
	// QQ message content limit (approximate). Split longer messages.
	qqMessageLimit = 2000

	// Busy reply when session is occupied.
	busyReply = "Thinking, please try again later~"

	// Error prefix for agent errors.
	errorPrefix = "Sorry, an error occurred while processing your message: "
)

// ─── Session interface (decoupled from session package) ───────────────────────

type PendingMedia = channel.PendingMedia
type AskStreamResult = channel.AskStreamResult
type OnIntermediateFunc = channel.OnIntermediateFunc
type SessionProvider = channel.SessionProvider

// ─── Errors ──────────────────────────────────────────────────────────────────

var ErrSessionBusy = channel.ErrSessionBusy

// ErrQueued is returned when a message has been queued for deferred processing.
// Consumers should treat this as a success — the message will be handled later.
var ErrQueued = channel.ErrQueued

// ErrTaskCancelled is returned by AskStream when the session task was cancelled
// via CancelCurrent (e.g., user sent /cancel command).
var ErrTaskCancelled = channel.ErrTaskCancelled

// ─── SessionBridge ───────────────────────────────────────────────────────────

// SessionBridge connects QQ messages to the SoloQueue Session.
// It receives QQ messages via the EventHandler interface, calls SessionProvider.AskStream,
// and sends the final reply back to QQ via the APIClient.
//
// Active messages (follow-up chunks, intermediate content, media) are routed
// through MessageQueue for rate limiting. Passive replies (ReplyMessage) are
// sent directly and are not rate-limited.
//
// Concurrency: the Session already serializes via inFlight (ErrSessionBusy).
// No additional guard is needed here — during async delegation the session
// correctly releases inFlight, allowing new messages to interleave.
type SessionBridge struct {
	sess             SessionProvider
	api              *APIClient
	log              *logger.Logger
	version          string
	queue            *MessageQueue
	transcriber      *Transcriber
	whitelistEnabled bool
	whitelist        map[string]bool
}

// SessionBridgeOption configures a SessionBridge.
type SessionBridgeOption func(*SessionBridge)

// WithVersion sets the version string for /version slash command replies.
func WithVersion(v string) SessionBridgeOption {
	return func(b *SessionBridge) { b.version = v }
}

// WithWhitelist configures the whitelist settings for the QQ bot.
func WithWhitelist(enabled bool, list []string) SessionBridgeOption {
	return func(b *SessionBridge) {
		b.whitelistEnabled = enabled
		b.whitelist = make(map[string]bool)
		for _, id := range list {
			b.whitelist[id] = true
		}
	}
}

// WithMessageQueue sets the rate-limiting message queue for active messages.
// If set, sendActiveMessage, sendIntermediate, and sendMedia will push sends
// through the queue instead of sending immediately.
func WithMessageQueue(q *MessageQueue) SessionBridgeOption {
	return func(b *SessionBridge) { b.queue = q }
}

// WithTranscriber sets the speech-to-text transcriber for audio message handling.
func WithTranscriber(t *Transcriber) SessionBridgeOption {
	return func(b *SessionBridge) { b.transcriber = t }
}

// NewSessionBridge creates a new SessionBridge.
func NewSessionBridge(sess SessionProvider, api *APIClient, log *logger.Logger, opts ...SessionBridgeOption) *SessionBridge {
	b := &SessionBridge{
		sess: sess,
		api:  api,
		log:  log,
	}
	for _, o := range opts {
		o(b)
	}
	return b
}

// OnQQMessage implements EventHandler. Called by the Gateway when a QQ message arrives.
func (b *SessionBridge) OnQQMessage(ctx context.Context, msg QQMessage) {
	// 1. Intercept /myid or /openid query commands locally
	trimmed := strings.ToLower(strings.TrimSpace(msg.Content))
	if trimmed == "/myid" || trimmed == "/openid" {
		b.sendReply(ctx, msg, MsgTypeText, fmt.Sprintf("您的 OpenID 是：\n%s", msg.OpenID))
		return
	}

	// 2. Check whitelist if enabled
	if b.whitelistEnabled && !b.whitelist[msg.OpenID] {
		b.log.InfoContext(ctx, logger.CatApp, "qqbot message ignored: user not in whitelist", "open_id", msg.OpenID)
		return
	}

	// 2b. Register channel sender for system notifications (cron, etc.).
	if s, ok := b.sess.(interface{ SetChannelSender(string, func(context.Context, string) error) }); ok {
		s.SetChannelSender("qq", func(ctx context.Context, text string) error {
			formatted := QQMarkdown(text)
			return b.SendActiveMessage(ctx, msg, MsgTypeMarkdown, formatted)
		})
		b.log.InfoContext(ctx, logger.CatApp, "qqbot: channelSender registered",
			"open_id", msg.OpenID,
		)
	}

	// 3. Handle audio messages — download SILK, transcribe via whisper.cpp,
	//    then treat the transcript as normal text input.
	if msg.AudioURL != "" {
		if !b.processAudioMessage(ctx, &msg) {
			return
		}
	}

	b.log.InfoContext(ctx, logger.CatApp, "qqbot message received",
		"source", msg.Source,
		"content_len", len(msg.Content),
		"open_id", msg.OpenID)

	// Embed QQ message into context so downstream code (e.g. cron task creation
	// via the create_cron_job tool) can extract QQ metadata.
	ctx = context.WithValue(ctx, "qq_message", msg)
	ctx = channel.ContextWithChatMeta(ctx, channel.ChatMeta{
		Channel:        "qq",
		UserID:         msg.OpenID,
		ConversationID: msg.ChatID,
	})

	// Handle slash commands locally.
	// Known builtins are handled here; unrecognized slash commands and skills
	// are forwarded to the LLM as normal input.
	if b.handleSlashCommand(ctx, msg) {
		return
	}

	// Process file attachments first
	var promptBuilder strings.Builder
	promptBuilder.WriteString(msg.Content)

	if len(msg.Files) > 0 {
		var fileBlocks []string
		for _, file := range msg.Files {
			b.log.InfoContext(ctx, logger.CatApp, "qqbot downloading file attachment", "url", file.URL)
			data, filename, err := downloadFile(ctx, file.URL)
			if err != nil {
				b.log.WarnContext(ctx, logger.CatApp, "qqbot failed to download attachment", "url", file.URL, "err", err.Error())
				continue
			}

			localPath, err := b.sess.SaveUploadedFile(ctx, filename, data)
			if err != nil {
				b.log.WarnContext(ctx, logger.CatApp, "qqbot failed to save attachment locally", "filename", filename, "err", err.Error())
				continue
			}

			b.log.InfoContext(ctx, logger.CatApp, "qqbot saved file attachment", "path", localPath, "size", len(data))

			binary := isBinary(data)
			var block string
			if binary {
				block = fmt.Sprintf("- Filename: %s\n  Saved path: %s (Size: %d bytes)\n  Type: Binary (This file is in binary format and cannot be read directly with the Read tool. You can use shell or other tools to process it.)", filename, localPath, len(data))
			} else {
				block = fmt.Sprintf("- Filename: %s\n  Saved path: %s (Size: %d bytes)\n  Type: Text (Please prioritize using the Read tool to read the content of this text file to continue the task.)", filename, localPath, len(data))
			}
			fileBlocks = append(fileBlocks, block)
		}

		if len(fileBlocks) > 0 {
			promptBuilder.WriteString("\n\n[User uploaded files, saved locally:\n")
			promptBuilder.WriteString(strings.Join(fileBlocks, "\n"))
			promptBuilder.WriteString("]\n")
		}
	}

	// Download and base64-encode image attachments for multimodal models.
	// Images are passed through context so the session layer can attach them
	// to the LLM request as image_url content parts.
	if len(msg.ImageURLs) > 0 {
		var images []llm.ImageContent
		for i, url := range msg.ImageURLs {
			b.log.InfoContext(ctx, logger.CatApp, "qqbot downloading image", "url", url, "index", i)
			data, filename, err := downloadFile(ctx, url)
			if err != nil {
				b.log.WarnContext(ctx, logger.CatApp, "qqbot failed to download image", "url", url, "err", err.Error())
				continue
			}

			// Determine MIME type: try Content-Type from download response first,
			// then fall back to extension-based detection.
			mimeType := detectMimeType(filename, data)

			// Save image locally (same as file attachments).
			if localPath, saveErr := b.sess.SaveUploadedFile(ctx, filename, data); saveErr != nil {
				b.log.WarnContext(ctx, logger.CatApp, "qqbot failed to save image locally", "filename", filename, "err", saveErr.Error())
			} else {
				b.log.InfoContext(ctx, logger.CatApp, "qqbot saved image locally", "path", localPath, "size", len(data), "mime", mimeType)
			}

			b64 := base64.StdEncoding.EncodeToString(data)
			images = append(images, llm.ImageContent{
				Data:     b64,
				MimeType: mimeType,
			})
			b.log.InfoContext(ctx, logger.CatApp, "qqbot image encoded", "index", i, "size", len(data), "base64_len", len(b64), "mime", mimeType)
		}
		if len(images) > 0 {
			ctx = context.WithValue(ctx, ctxwin.ImageContextKey, images)
			// Still note images in the prompt text so the LLM knows images are attached.
			promptBuilder.WriteString("\n[User uploaded images, processed by visual recognition]")
		}
	}

	msg.Content = promptBuilder.String()
	prompt := msg.Content
	// Note: buildPrompt previously appended image URLs as text markers.
	// Now image data is passed via context as base64 for multimodal models,
	// so the prompt text only needs the user's message + file/upload annotations.

	// Use AskStream to capture the full response including reasoning content.
	// Pass onIntermediate to send intermediate assistant responses (content
	// from iterations that also produced tool calls) as active messages.
	result, err := b.sess.AskStream(ctx, prompt, func(ctx context.Context, content string) {
		b.sendIntermediate(ctx, msg, content)
	})
	if err != nil {
		if errors.Is(err, ErrSessionBusy) {
			b.sendReply(ctx, msg, MsgTypeText, busyReply)
			return
		}
		if errors.Is(err, ErrQueued) {
			return
		}
		if errors.Is(err, ErrTaskCancelled) {
			// Already handled by /cancel command — don't send a reply.
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
		b.sendReply(ctx, msg, MsgTypeText, "(Thinking complete, no reply content)")
		return
	}

	// Send media attachments (if any) or generated images before the text reply.
	if len(result.MediaList) > 0 {
		b.SendMediaList(ctx, msg, result.MediaList)
	} else if len(result.ImageURLs) > 0 {
		b.sendImages(ctx, msg, result.ImageURLs)
	}

	b.log.InfoContext(ctx, logger.CatApp, "qqbot reply ready",
		"content_len", len(result.Content),
		"reasoning_len", len(result.ReasoningContent),
		"media_count", len(result.MediaList),
		"image_count", len(result.ImageURLs),
		"reply_len", len(reply))

	// Format as QQ-compatible markdown and send
	formatted := QQMarkdown(reply)
	b.sendReply(ctx, msg, MsgTypeMarkdown, formatted)
}

// ─── Slash Command Handling ──────────────────────────────────────────────────

// isSlashCommandInput checks if the message is a single-line slash command.
func isSlashCommandInput(input string) bool {
	trimmed := strings.TrimSpace(input)
	return !strings.Contains(trimmed, "\n") && strings.HasPrefix(trimmed, "/")
}

// handleSlashCommand processes slash commands locally.
// Returns true if the command was handled (caller should not forward to LLM).
// Unrecognized slash commands return false so they are forwarded to LLM as normal input.
func (b *SessionBridge) handleSlashCommand(ctx context.Context, msg QQMessage) bool {
	content := strings.TrimSpace(msg.Content)
	if !isSlashCommandInput(content) {
		return false
	}

	name := strings.ToLower(content)

	switch name {
	case "/help", "/?":
		text := "/help — View available commands\n/cancel — Cancel current task\n/clear — Clear dialogue history\n/compact — Compact context window (no memory save)\n/init — Create/update AGENTS.md in project directory (L2 only)\n/version — View version number"
		b.sendReply(ctx, msg, MsgTypeText, text)
		return true

	case "/cancel":
		if err := b.sess.CancelCurrent("user requested cancellation"); err != nil {
			b.sendReply(ctx, msg, MsgTypeText, "Cancellation failed: "+err.Error())
		} else {
			b.sendReply(ctx, msg, MsgTypeText, "Current task cancelled")
		}
		return true

	case "/clear":
		if err := b.sess.Clear(ctx); err != nil {
			b.sendReply(ctx, msg, MsgTypeText, "Clear failed: "+err.Error())
		} else {
			b.sendReply(ctx, msg, MsgTypeText, "Conversation history cleared")
		}
		return true

	case "/compact":
		if err := b.sess.Compact(ctx); err != nil {
			b.sendReply(ctx, msg, MsgTypeText, "Compact failed: "+err.Error())
		} else {
			b.sendReply(ctx, msg, MsgTypeText, "Context window compacted (history summarized, no memory save)")
		}
		return true

	case "/version":
		v := b.version
		if v == "" {
			v = "SoloQueue"
		} else {
			v = "SoloQueue " + v
		}
		b.sendReply(ctx, msg, MsgTypeText, v)
		return true

	default:
		// Unknown slash command: forward to LLM as normal input.
		return false
	}
}

// processAudioMessage downloads the SILK audio from msg.AudioURL, transcodes to
// WAV via ffmpeg, and transcribes using whisper.cpp. On success, msg.Content is
// set to the transcript and the caller continues normal text processing.
// Returns false if the audio could not be processed (error reply already sent).
func (b *SessionBridge) processAudioMessage(ctx context.Context, msg *QQMessage) bool {
	if b.transcriber == nil || !b.transcriber.Available() {
		b.sendReply(ctx, *msg, MsgTypeText, "语音转写未配置，请发送文字消息。")
		return false
	}

	b.log.InfoContext(ctx, logger.CatApp, "qqbot audio message received",
		"url", msg.AudioURL)

	audioData, _, err := downloadFile(ctx, msg.AudioURL)
	if err != nil {
		b.log.WarnContext(ctx, logger.CatApp, "qqbot failed to download audio",
			"url", msg.AudioURL, "err", err.Error())
		b.sendReply(ctx, *msg, MsgTypeText, "无法下载语音消息，请重试。")
		return false
	}
	b.log.InfoContext(ctx, logger.CatApp, "qqbot audio downloaded",
		"size", len(audioData))

	transcript, err := b.transcriber.Transcribe(ctx, audioData)
	if err != nil {
		b.log.WarnContext(ctx, logger.CatApp, "qqbot audio transcription failed",
			"err", err.Error())
		b.sendReply(ctx, *msg, MsgTypeText, "语音识别失败，请发送文字消息。")
		return false
	}

	b.log.InfoContext(ctx, logger.CatApp, "qqbot audio transcribed",
		"text", transcript)
	msg.Content = transcript
	return true
}

// sendReply sends the reply text to QQ, splitting into chunks if it exceeds the limit.
// The first chunk is sent as a passive reply (with msg_id/msg_seq reference) so it
// appears threaded to the original message. Subsequent chunks are sent as active
// messages because QQ Bot API only allows one passive reply per incoming message.
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

	// First chunk: passive reply (with msg_id/msg_seq)
	if err := b.api.ReplyMessage(ctx, msg, msgType, chunks[0]); err != nil {
		b.log.WarnContext(ctx, logger.CatApp, "qqbot first chunk reply failed",
			"err", err.Error())
		return
	}

	// Remaining chunks: active messages (no msg_id/msg_seq) — QQ only allows one passive reply
	for i := 1; i < len(chunks); i++ {
		if err := b.sendActiveMessage(ctx, msg, msgType, chunks[i]); err != nil {
			b.log.WarnContext(ctx, logger.CatApp, "qqbot follow-up chunk send failed",
				"chunk", i+1, "total", len(chunks), "err", err.Error())
			return
		}
	}
}

// SendActiveMessage sends an active message (no message reference) to the QQ conversation.
func (b *SessionBridge) SendActiveMessage(ctx context.Context, msg QQMessage, msgType int, text string) error {
	return b.sendActiveMessage(ctx, msg, msgType, text)
}

// sendActiveMessage sends an active message (no msg_id/msg_seq reference) to
// the same conversation as msg. If a MessageQueue is configured, the send is
// enqueued for rate-limited delivery (non-blocking, error silenced). Otherwise
// it is sent immediately.
func (b *SessionBridge) sendActiveMessage(ctx context.Context, msg QQMessage, msgType int, text string) error {
	if b.queue != nil {
		b.queue.Push(func() {
			_ = b.sendActiveMessageDirect(context.Background(), msg, msgType, text)
		})
		return nil
	}
	return b.sendActiveMessageDirect(ctx, msg, msgType, text)
}

// sendActiveMessageDirect sends the message immediately, bypassing the rate
// limiter queue. This is used internally by sendActiveMessage (for direct mode)
// and by the queue worker goroutine.
func (b *SessionBridge) sendActiveMessageDirect(ctx context.Context, msg QQMessage, msgType int, text string) error {
	var err error
	switch msg.Source {
	case SourceC2C:
		err = b.api.SendC2CMessage(ctx, msg.TargetOpenID, msgType, text, "", 0)
	case SourceGroup:
		err = b.api.SendGroupMessage(ctx, msg.TargetOpenID, msgType, text, "", 0)
	case SourceDirect:
		err = b.api.SendDirectMessage(ctx, msg.ChatID, msgType, text, "", 0)
	case SourcePublicGuild:
		err = b.api.SendDirectMessage(ctx, msg.ChatID, msgType, text, "", 0)
	}
	return err
}

// sendIntermediate sends an intermediate assistant message as an active message
// (no message reference), so it appears as a new message from the bot rather
// than a reply to the original user message. This allows multiple intermediate
// messages per incoming message.
func (b *SessionBridge) sendIntermediate(ctx context.Context, msg QQMessage, text string) {
	if err := b.sendActiveMessage(ctx, msg, MsgTypeText, text); err != nil {
		b.log.WarnContext(ctx, logger.CatApp, "qqbot intermediate send failed",
			"err", err.Error(),
		)
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

// sendImages uploads each image URL to QQ and sends as rich media (active message, NOT reply).
// We use active messages here because QQ only delivers one passive reply per incoming message.
func (b *SessionBridge) sendImages(ctx context.Context, msg QQMessage, urls []string) {
	targetType, targetID := imageUploadTarget(msg)
	if targetType == "" {
		b.log.WarnContext(ctx, logger.CatApp, "qqbot: unsupported source for image upload",
			"source", msg.Source)
		return
	}
	for i, url := range urls {
		fi, err := b.api.UploadFile(ctx, targetType, targetID, FileTypeImage, url, "")
		if err != nil {
			b.log.WarnContext(ctx, logger.CatApp, "qqbot: image upload failed",
				"url", url, "err", err.Error())
			continue
		}
		if err := b.sendMedia(ctx, msg, fi.FileInfo); err != nil {
			b.log.WarnContext(ctx, logger.CatApp, "qqbot: image send failed",
				"index", i+1, "err", err.Error())
		} else {
			b.log.InfoContext(ctx, logger.CatApp, "qqbot: image sent",
				"index", i+1)
		}
	}
}

// SendMediaList uploads each media in the list to QQ and sends as rich media (active message, NOT reply).
func (b *SessionBridge) SendMediaList(ctx context.Context, msg QQMessage, mediaList []PendingMedia) {
	targetType, targetID := imageUploadTarget(msg)
	if targetType == "" {
		b.log.WarnContext(ctx, logger.CatApp, "qqbot: unsupported source for media upload",
			"source", msg.Source)
		return
	}
	for i, media := range mediaList {
		fi, err := b.api.UploadFile(ctx, targetType, targetID, media.FileType, media.URL, media.Base64Data)
		if err != nil {
			b.log.WarnContext(ctx, logger.CatApp, "qqbot: media upload failed",
				"index", i+1, "type", media.FileType, "err", err.Error())
			continue
		}
		if err := b.sendMedia(ctx, msg, fi.FileInfo); err != nil {
			b.log.WarnContext(ctx, logger.CatApp, "qqbot: media send failed",
				"index", i+1, "err", err.Error())
		} else {
			b.log.InfoContext(ctx, logger.CatApp, "qqbot: media sent",
				"index", i+1)
		}
	}
}

// sendMedia sends a rich media message as an active message (no msg_id reference).
func (b *SessionBridge) sendMedia(ctx context.Context, msg QQMessage, fileInfo string) error {
	return b.sendActiveMessage(ctx, msg, MsgTypeMedia, fileInfo)
}

// imageUploadTarget returns the (targetType, targetID) pair for QQ rich media upload.
// targetType is "user" for C2C/Direct and "group" for Group, per QQ Bot API.
func imageUploadTarget(msg QQMessage) (targetType, targetID string) {
	switch msg.Source {
	case SourceC2C:
		return "user", msg.TargetOpenID
	case SourceGroup:
		return "group", msg.TargetOpenID
	case SourceDirect:
		return "user", msg.OpenID
	case SourcePublicGuild:
		return "user", msg.OpenID
	default:
		return "", ""
	}
}

// downloadFile downloads a file from URL and extracts its filename.
func downloadFile(ctx context.Context, url string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("bad status code: %d", resp.StatusCode)
	}

	// Try to get filename from Content-Disposition
	filename := ""
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		_, params, err := mime.ParseMediaType(cd)
		if err == nil {
			if fn, ok := params["filename"]; ok && fn != "" {
				filename = fn
			}
		}
	}

	// Fallback to URL path
	if filename == "" {
		filename = filepath.Base(resp.Request.URL.Path)
	}

	// Fallback to timestamp + extension from Content-Type
	if filename == "" || filename == "." || filename == "/" {
		ext := ".bin"
		if ct := resp.Header.Get("Content-Type"); ct != "" {
			mediatype, _, err := mime.ParseMediaType(ct)
			if err == nil {
				exts, err := mime.ExtensionsByType(mediatype)
				if err == nil && len(exts) > 0 {
					ext = exts[0]
				}
			}
		}
		filename = fmt.Sprintf("file_%d%s", time.Now().Unix(), ext)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}

	return data, filename, nil
}

// isBinary checks if the file content contains NUL bytes in the first 512 bytes.
func isBinary(data []byte) bool {
	n := len(data)
	if n > 512 {
		n = 512
	}
	for i := 0; i < n; i++ {
		if data[i] == 0 {
			return true
		}
	}
	return false
}

// detectMimeType determines the MIME type of file data by sniffing the first
// 512 bytes using http.DetectContentType. Falls back to extension-based
// detection if the sniff result is generic (application/octet-stream).
func detectMimeType(filename string, data []byte) string {
	mimeType := http.DetectContentType(data)
	if mimeType != "application/octet-stream" {
		return mimeType
	}
	// Fallback: try extension-based detection
	ext := filepath.Ext(filename)
	if ext != "" {
		if mt := mime.TypeByExtension(ext); mt != "" {
			return mt
		}
	}
	// Default to image/png if all detection fails
	return "image/png"
}
