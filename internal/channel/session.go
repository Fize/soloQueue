// Package channel defines the transport-neutral contract between messaging
// channels and SoloQueue sessions.
package channel

import (
	"context"
	"errors"
)

// MediaKind is the transport-neutral type of outbound media.
type MediaKind string

const (
	MediaImage MediaKind = "image"
	MediaVideo MediaKind = "video"
	MediaVoice MediaKind = "voice"
	MediaFile  MediaKind = "file"
)

// OutboundMedia represents media produced by an agent. Channel-specific
// protocol values are assigned only inside the channel adapter.
type OutboundMedia struct {
	Kind     MediaKind
	Path     string
	URL      string
	FileName string
	MIMEType string
}

// PendingMedia is retained as a source-compatible name for outbound media.
type PendingMedia = OutboundMedia

// AskStreamResult contains the result of a streaming session request.
type AskStreamResult struct {
	Content          string
	ReasoningContent string
	ImageURLs        []string
	MediaList        []PendingMedia
}

// OnIntermediateFunc receives assistant content emitted before a tool call.
type OnIntermediateFunc func(ctx context.Context, content string)

// SessionProvider is the session functionality shared by messaging channels.
type SessionProvider interface {
	AskStream(ctx context.Context, prompt string, onIntermediate OnIntermediateFunc) (*AskStreamResult, error)
	Clear(ctx context.Context) error
	Compact(ctx context.Context) error
	CancelCurrent(reason string) error
	SaveUploadedFile(ctx context.Context, filename string, content []byte) (string, error)
	SetChannelSenderData(channelType string, metadata []byte, fn func(context.Context, string) error)
}

var (
	ErrSessionBusy   = errors.New("session: busy")
	ErrQueued        = errors.New("session: message queued")
	ErrTaskCancelled = errors.New("task cancelled")
)
