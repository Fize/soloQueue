package channel

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/runwatch"
)

type fakeSession struct {
	prompt string
	askErr error
}

type busySession struct{ fakeSession }

type attachmentSession struct {
	fakeSession
	files []ctxwin.FileAttachment
}

type transcribedAudioSession struct {
	fakeSession
	files     []ctxwin.FileAttachment
	saveCalls int
}

func (s *transcribedAudioSession) AskStream(ctx context.Context, prompt string, _ OnIntermediateFunc) (*AskStreamResult, error) {
	s.prompt = prompt
	s.files, _ = ctx.Value(ctxwin.FilesContextKey).([]ctxwin.FileAttachment)
	return &AskStreamResult{Content: "reply"}, nil
}

func (s *transcribedAudioSession) SaveUploadedFile(context.Context, string, []byte) (string, error) {
	s.saveCalls++
	return "/tmp/voice.silk", nil
}

func (s *attachmentSession) AskStream(ctx context.Context, prompt string, _ OnIntermediateFunc) (*AskStreamResult, error) {
	s.prompt = prompt
	s.files, _ = ctx.Value(ctxwin.FilesContextKey).([]ctxwin.FileAttachment)
	return &AskStreamResult{Content: "reply"}, nil
}

func (*attachmentSession) SaveUploadedFile(context.Context, string, []byte) (string, error) {
	return "/tmp/saved-image.jpg", nil
}

func (*busySession) AskStream(context.Context, string, OnIntermediateFunc) (*AskStreamResult, error) {
	return nil, ErrSessionBusy
}

func (s *fakeSession) AskStream(_ context.Context, prompt string, _ OnIntermediateFunc) (*AskStreamResult, error) {
	s.prompt = prompt
	if s.askErr != nil {
		return nil, s.askErr
	}
	return &AskStreamResult{Content: "reply"}, nil
}
func (*fakeSession) Clear(context.Context) error                                              { return nil }
func (*fakeSession) Compact(context.Context) error                                            { return nil }
func (*fakeSession) CancelCurrent(string) error                                               { return nil }
func (*fakeSession) SaveUploadedFile(context.Context, string, []byte) (string, error)         { return "", nil }
func (*fakeSession) SetChannelSenderData(string, []byte, func(context.Context, string) error) {}

type fakeSender struct {
	message Message
	text    string
}

func (s *fakeSender) SendText(_ context.Context, msg Message, text string) error {
	s.message, s.text = msg, text
	return nil
}

type activitySender struct {
	fakeSender
	started        bool
	stopped        bool
	sentBeforeStop bool
}

type unavailableActivitySender struct{ fakeSender }

func (*unavailableActivitySender) StartResponseActivity(context.Context, Message) (func(), error) {
	return nil, errors.New("typing unavailable")
}

func (s *activitySender) StartResponseActivity(context.Context, Message) (func(), error) {
	s.started = true
	return func() { s.stopped = true }, nil
}

func (s *activitySender) SendText(ctx context.Context, msg Message, text string) error {
	if !s.stopped {
		s.sentBeforeStop = true
	}
	return s.fakeSender.SendText(ctx, msg, text)
}

func TestTextBridgePreservesOpaqueReplyToken(t *testing.T) {
	log, err := logger.New(t.TempDir(), logger.WithConsole(false), logger.WithFile(false))
	if err != nil {
		t.Fatal(err)
	}
	sess := &fakeSession{}
	sender := &fakeSender{}
	bridge := NewTextBridge(sess, sender, log, "1.0.0", false, nil)
	bridge.OnMessage(context.Background(), Message{Channel: "test", UserID: "u1", Text: "hello", ReplyToken: "opaque"})
	if sess.prompt != "hello" || sender.text != "reply" || sender.message.ReplyToken != "opaque" {
		t.Fatalf("session=%q sender=%#v", sess.prompt, sender)
	}
}

func TestTextBridgeStopsResponseActivityBeforeFinalReply(t *testing.T) {
	log, _ := logger.New(t.TempDir(), logger.WithConsole(false), logger.WithFile(false))
	sess := &fakeSession{}
	sender := &activitySender{}
	bridge := NewTextBridge(sess, sender, log, "1.0.0", false, nil)

	bridge.OnMessage(context.Background(), Message{Channel: "test", UserID: "u1", Text: "hello", ReplyToken: "opaque"})

	if !sender.started || !sender.stopped {
		t.Fatalf("response activity lifecycle: started=%v stopped=%v", sender.started, sender.stopped)
	}
	if sender.sentBeforeStop {
		t.Fatal("final reply was sent before response activity stopped")
	}
}

func TestTextBridgeStopsResponseActivityBeforeErrorReply(t *testing.T) {
	log, _ := logger.New(t.TempDir(), logger.WithConsole(false), logger.WithFile(false))
	sender := &activitySender{}
	bridge := NewTextBridge(&busySession{}, sender, log, "1.0.0", false, nil)

	bridge.OnMessage(context.Background(), Message{Channel: "test", UserID: "u1", Text: "hello", ReplyToken: "opaque"})

	if !sender.stopped || sender.sentBeforeStop {
		t.Fatalf("activity stopped=%v sentBeforeStop=%v", sender.stopped, sender.sentBeforeStop)
	}
	if sender.text != busyReply {
		t.Fatalf("reply = %q", sender.text)
	}
}

func TestTextBridgeHidesInternalOperationIDFromErrorReply(t *testing.T) {
	log, _ := logger.New(t.TempDir(), logger.WithConsole(false), logger.WithFile(false))
	sender := &fakeSender{}
	sess := &fakeSession{askErr: &runwatch.Cause{
		Code:        runwatch.CodeDelegationOrphaned,
		OperationID: "dlg_internal_secret",
	}}
	bridge := NewTextBridge(sess, sender, log, "1.0.0", false, nil)

	bridge.OnMessage(context.Background(), Message{Channel: "test", UserID: "u1", Text: "hello"})

	if strings.Contains(sender.text, "dlg_internal_secret") {
		t.Fatalf("error reply leaked operation ID: %q", sender.text)
	}
	if !strings.Contains(strings.ToLower(sender.text), "delegated") {
		t.Fatalf("error reply lost actionable failure context: %q", sender.text)
	}
}

func TestTextBridgeContinuesWhenResponseActivityIsUnavailable(t *testing.T) {
	log, _ := logger.New(t.TempDir(), logger.WithConsole(false), logger.WithFile(false))
	sender := &unavailableActivitySender{}
	bridge := NewTextBridge(&fakeSession{}, sender, log, "1.0.0", false, nil)

	bridge.OnMessage(context.Background(), Message{Channel: "test", UserID: "u1", Text: "hello", ReplyToken: "opaque"})

	if sender.text != "reply" {
		t.Fatalf("reply = %q", sender.text)
	}
}

func TestTextBridgeEmptyEnabledWhitelistDeniesAll(t *testing.T) {
	log, _ := logger.New(t.TempDir(), logger.WithConsole(false), logger.WithFile(false))
	sess := &fakeSession{}
	sender := &fakeSender{}
	bridge := NewTextBridge(sess, sender, log, "1.0.0", true, nil)
	bridge.OnMessage(context.Background(), Message{Channel: "test", UserID: "u1", Text: "hello"})
	if sess.prompt != "" || sender.text != "" {
		t.Fatal("message should be ignored")
	}
}

func TestTextBridgeMediaOnlyReachesAgent(t *testing.T) {
	log, _ := logger.New(t.TempDir(), logger.WithConsole(false), logger.WithFile(false))
	sess := &fakeSession{}
	sender := &fakeSender{}
	bridge := NewTextBridge(sess, sender, log, "1.0.0", false, nil)
	bridge.OnMessage(context.Background(), Message{
		Channel:     "test",
		UserID:      "u1",
		Attachments: []Attachment{{Kind: AttachmentAudio, LocalPath: "/tmp/voice.wav"}},
	})
	if sess.prompt == "" || sender.text != "reply" {
		t.Fatalf("prompt=%q reply=%q", sess.prompt, sender.text)
	}
}

func TestTextBridgeAddsSavedImageToFilesContext(t *testing.T) {
	log, _ := logger.New(t.TempDir(), logger.WithConsole(false), logger.WithFile(false))
	sess := &attachmentSession{}
	bridge := NewTextBridge(sess, &fakeSender{}, log, "1.0.0", false, nil)
	bridge.OnMessage(context.Background(), Message{
		Channel: "wechat", AccountID: "bot", ConversationID: "chat", UserID: "user", Text: "describe",
		Attachments: []Attachment{{Kind: AttachmentImage, Name: "image.jpg", MIMEType: "image/jpeg", Data: []byte("image")}},
	})

	if len(sess.files) != 1 || sess.files[0].Name != "image.jpg" || sess.files[0].Path != "/tmp/saved-image.jpg" {
		t.Fatalf("files context = %#v", sess.files)
	}
}

func TestTextBridgeForwardsServerTranscribedAudioAsTextOnly(t *testing.T) {
	log, _ := logger.New(t.TempDir(), logger.WithConsole(false), logger.WithFile(false))
	sess := &transcribedAudioSession{}
	bridge := NewTextBridge(sess, &fakeSender{}, log, "1.0.0", false, nil)
	bridge.OnMessage(context.Background(), Message{
		Channel: "wechat", UserID: "user", Text: "server transcript",
		Attachments: []Attachment{{Kind: AttachmentAudio, Name: "voice.silk", Transcript: "server transcript", Data: []byte("silk")}},
	})

	if sess.prompt != "server transcript" {
		t.Fatalf("prompt = %q", sess.prompt)
	}
	if sess.saveCalls != 0 {
		t.Fatalf("SaveUploadedFile calls = %d", sess.saveCalls)
	}
	if len(sess.files) != 0 {
		t.Fatalf("files context = %#v", sess.files)
	}
}

func TestTextBridgeForwardsLocallyTranscribedAudioAsTextOnly(t *testing.T) {
	log, _ := logger.New(t.TempDir(), logger.WithConsole(false), logger.WithFile(false))
	sess := &transcribedAudioSession{}
	bridge := NewTextBridge(sess, &fakeSender{}, log, "1.0.0", false, nil)
	transcribeCalls := 0
	bridge.SetVoiceTranscriber(func(context.Context, []byte) (string, error) {
		transcribeCalls++
		return "local transcript", nil
	})
	bridge.OnMessage(context.Background(), Message{
		Channel: "wechat", UserID: "user",
		Attachments: []Attachment{{Kind: AttachmentAudio, Name: "voice.silk", Data: []byte("silk")}},
	})

	if transcribeCalls != 1 {
		t.Fatalf("voice transcriber calls = %d", transcribeCalls)
	}
	if sess.prompt != "local transcript" {
		t.Fatalf("prompt = %q", sess.prompt)
	}
	if sess.saveCalls != 0 {
		t.Fatalf("SaveUploadedFile calls = %d", sess.saveCalls)
	}
	if len(sess.files) != 0 {
		t.Fatalf("files context = %#v", sess.files)
	}
}

func TestTextBridgePreservesAudioAttachmentWhenLocalTranscriptionFails(t *testing.T) {
	log, _ := logger.New(t.TempDir(), logger.WithConsole(false), logger.WithFile(false))
	sess := &transcribedAudioSession{}
	bridge := NewTextBridge(sess, &fakeSender{}, log, "1.0.0", false, nil)
	bridge.SetVoiceTranscriber(func(context.Context, []byte) (string, error) {
		return "", errors.New("transcription failed")
	})
	bridge.OnMessage(context.Background(), Message{
		Channel: "wechat", UserID: "user",
		Attachments: []Attachment{{Kind: AttachmentAudio, Name: "voice.silk", Data: []byte("silk")}},
	})

	if sess.saveCalls != 1 {
		t.Fatalf("SaveUploadedFile calls = %d", sess.saveCalls)
	}
	if len(sess.files) != 1 || sess.files[0].Name != "voice.silk" {
		t.Fatalf("files context = %#v", sess.files)
	}
	if !strings.Contains(sess.prompt, "User uploaded attachments") || !strings.Contains(sess.prompt, "voice.silk") {
		t.Fatalf("prompt = %q", sess.prompt)
	}
}

type mediaFakeSession struct{ fakeSession }

func (s *mediaFakeSession) AskStream(ctx context.Context, prompt string, fn OnIntermediateFunc) (*AskStreamResult, error) {
	s.prompt = prompt
	return &AskStreamResult{MediaList: []OutboundMedia{{Kind: MediaFile, Path: "/export/report.csv", FileName: "report.csv"}}}, nil
}

type mediaFakeSender struct {
	fakeSender
	mediaMessage Message
	media        []OutboundMedia
}

func (s *mediaFakeSender) SendMedia(_ context.Context, msg Message, media []OutboundMedia) error {
	s.mediaMessage = msg
	s.media = append([]OutboundMedia(nil), media...)
	return nil
}

func TestTextBridgeSendsMediaWithoutFinalTextOnOriginalRoute(t *testing.T) {
	log, _ := logger.New(t.TempDir(), logger.WithConsole(false), logger.WithFile(false))
	sess := &mediaFakeSession{}
	sender := &mediaFakeSender{}
	bridge := NewTextBridge(sess, sender, log, "1.0.0", false, nil)
	msg := Message{Channel: "wechat", AccountID: "bot-a", ConversationID: "chat-a", UserID: "user-a", Text: "send", ReplyToken: "token-a"}
	bridge.OnMessage(context.Background(), msg)
	if len(sender.media) != 1 || sender.mediaMessage.AccountID != "bot-a" || sender.mediaMessage.ReplyToken != "token-a" {
		t.Fatalf("media route=%#v media=%#v", sender.mediaMessage, sender.media)
	}
	if sender.text != "" {
		t.Fatalf("unexpected text reply %q", sender.text)
	}
}
