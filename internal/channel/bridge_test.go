package channel

import (
	"context"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

type fakeSession struct {
	prompt string
}

func (s *fakeSession) AskStream(_ context.Context, prompt string, _ OnIntermediateFunc) (*AskStreamResult, error) {
	s.prompt = prompt
	return &AskStreamResult{Content: "reply"}, nil
}
func (*fakeSession) Clear(context.Context) error                                      { return nil }
func (*fakeSession) Compact(context.Context) error                                    { return nil }
func (*fakeSession) CancelCurrent(string) error                                       { return nil }
func (*fakeSession) SaveUploadedFile(context.Context, string, []byte) (string, error) { return "", nil }

type fakeSender struct {
	message Message
	text    string
}

func (s *fakeSender) SendText(_ context.Context, msg Message, text string) error {
	s.message, s.text = msg, text
	return nil
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
