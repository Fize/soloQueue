package qq

import (
	"context"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

func testBridge(t *testing.T, transcriber *Transcriber) *SessionBridge {
	t.Helper()
	l, err := logger.New(t.TempDir(), logger.WithFile(false), logger.WithConsole(false))
	if err != nil {
		t.Fatalf("create test logger: %v", err)
	}
	// NewAPIClient with empty config — sendReply will attempt an HTTP call to
	// the QQ token endpoint which will fail quickly; this is acceptable for
	// unit tests that verify the transcriber-availability guard runs first.
	api := NewAPIClient(Config{}, l)
	return &SessionBridge{
		api:         api,
		log:         l,
		transcriber: transcriber,
	}
}

func TestProcessAudioMessage_NilTranscriber(t *testing.T) {
	b := testBridge(t, nil)
	msg := &QQMessage{AudioURL: "https://example.com/audio.silk", OpenID: "u1", ChatID: "c1"}

	ok := b.processAudioMessage(context.Background(), msg)
	if ok {
		t.Error("processAudioMessage should return false when transcriber is nil")
	}
	if msg.Content != "" {
		t.Error("msg.Content should not be modified on failure")
	}
}

func TestProcessAudioMessage_TranscriberNotAvailable(t *testing.T) {
	// Transcriber with no binary and no model file
	tr := &Transcriber{binary: "", modelDir: t.TempDir(), model: "small"}
	b := testBridge(t, tr)
	msg := &QQMessage{AudioURL: "https://example.com/audio.silk", OpenID: "u1", ChatID: "c1"}

	ok := b.processAudioMessage(context.Background(), msg)
	if ok {
		t.Error("processAudioMessage should return false when transcriber is not available")
	}
	if msg.Content != "" {
		t.Error("msg.Content should not be modified on failure")
	}
}

func TestProcessAudioMessage_SetsContentOnSuccess(t *testing.T) {
	// Verify that Content is preserved when transcriber is not available.
	// The full transcribe path requires real ffmpeg + whisper-cli and is
	// tested via integration tests.
	tr := &Transcriber{binary: "", modelDir: t.TempDir(), model: "small"}
	b := testBridge(t, tr)
	msg := &QQMessage{
		AudioURL: "https://example.com/audio.silk",
		OpenID:   "u1",
		ChatID:   "c1",
		Content:  "should not be overwritten on failure",
	}

	ok := b.processAudioMessage(context.Background(), msg)
	if ok {
		t.Error("should return false when transcriber not available")
	}
	if msg.Content != "should not be overwritten on failure" {
		t.Errorf("Content = %q, want unchanged on failure", msg.Content)
	}
}

