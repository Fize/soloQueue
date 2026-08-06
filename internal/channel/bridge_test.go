package channel

import (
	"context"
	"errors"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

type fakeSession struct {
	prompt string
}

type busySession struct{ fakeSession }

func (*busySession) AskStream(context.Context, string, OnIntermediateFunc) (*AskStreamResult, error) {
	return nil, ErrSessionBusy
}

func (s *fakeSession) AskStream(_ context.Context, prompt string, _ OnIntermediateFunc) (*AskStreamResult, error) {
	s.prompt = prompt
	return &AskStreamResult{Content: "reply"}, nil
}
func (*fakeSession) Clear(context.Context) error                                      { return nil }
func (*fakeSession) Compact(context.Context) error                                    { return nil }
func (*fakeSession) CancelCurrent(string) error                                       { return nil }
func (*fakeSession) SaveUploadedFile(context.Context, string, []byte) (string, error) { return "", nil }
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

func TestTextBridgeMediaOnlyReturnsExplicitReply(t *testing.T) {
	log, _ := logger.New(t.TempDir(), logger.WithConsole(false), logger.WithFile(false))
	sess := &fakeSession{}
	sender := &fakeSender{}
	bridge := NewTextBridge(sess, sender, log, "1.0.0", false, nil)
	bridge.OnMessage(context.Background(), Message{
		Channel:     "test",
		UserID:      "u1",
		Attachments: []Attachment{{Kind: AttachmentAudio, LocalPath: "/tmp/voice.wav"}},
	})
	if sess.prompt != "" || sender.text != mediaOnlyReply {
		t.Fatalf("prompt=%q reply=%q", sess.prompt, sender.text)
	}
}
