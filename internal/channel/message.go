package channel

import "context"

// AttachmentKind is the transport-neutral kind of an inbound attachment.
type AttachmentKind string

const (
	AttachmentImage AttachmentKind = "image"
	AttachmentAudio AttachmentKind = "audio"
	AttachmentVideo AttachmentKind = "video"
	AttachmentFile  AttachmentKind = "file"
)

// Attachment describes media after channel-specific normalization. LocalPath is
// optional when the channel already supplied a transcript and the binary media
// does not need to be downloaded.
type Attachment struct {
	Kind       AttachmentKind
	LocalPath  string
	MIMEType   string
	Name       string
	Transcript string
	Data       []byte `json:"-"`
}

// Message is the transport-neutral part of an inbound channel message.
// ReplyToken is opaque and must be passed back to the originating transport.
type Message struct {
	Channel        string
	AccountID      string
	ConversationID string
	UserID         string
	Text           string
	Attachments    []Attachment
	ReplyToken     string
}

// Handler receives normalized inbound messages.
type Handler interface {
	OnMessage(ctx context.Context, msg Message)
}

// HandlerFunc adapts a function to Handler.
type HandlerFunc func(ctx context.Context, msg Message)

func (f HandlerFunc) OnMessage(ctx context.Context, msg Message) { f(ctx, msg) }

// TextSender sends a text response using the reply context in msg.
type TextSender interface {
	SendText(ctx context.Context, msg Message, text string) error
}

// MediaSender sends media using the immutable reply route carried by msg.
type MediaSender interface {
	SendMedia(ctx context.Context, msg Message, media []OutboundMedia) error
}

// ResponseActivityStarter optionally keeps a channel's reply window active
// while the session is producing a response. Transports without this concept
// only need to implement TextSender.
type ResponseActivityStarter interface {
	StartResponseActivity(ctx context.Context, msg Message) (stop func(), err error)
}

// TextFormatter converts model Markdown into the subset supported by a channel.
type TextFormatter interface {
	FormatText(text string) string
}

// ─── Context metadata for cross-channel notification routing ───────────────

// ChatMeta carries channel source metadata through context for cron task creation.
type ChatMeta struct {
	Channel        string // "qq" | "wechat" | "" (web)
	AccountID      string
	UserID         string
	ConversationID string
}

type chatCtxKeyType struct{}

var chatCtxKey = chatCtxKeyType{}

// ContextWithChatMeta returns a context with the given channel metadata.
func ContextWithChatMeta(ctx context.Context, meta ChatMeta) context.Context {
	return context.WithValue(ctx, chatCtxKey, meta)
}

// ChatMetaFromContext extracts channel metadata from context.
// Returns (meta, true) if present, (zero, false) otherwise.
func ChatMetaFromContext(ctx context.Context) (ChatMeta, bool) {
	v, ok := ctx.Value(chatCtxKey).(ChatMeta)
	return v, ok
}
