package channel

import (
	"context"
	"encoding/json"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"testing"
)

type notificationSession struct {
	fakeSession
	registrations int
	metadata      []byte
	send          func(context.Context, string) error
}

func (s *notificationSession) SetChannelSenderData(_ string, data []byte, send func(context.Context, string) error) {
	s.registrations++
	s.metadata = data
	s.send = send
}
func TestUnauthorizedMessageCannotReplaceNotificationRoute(t *testing.T) {
	log, _ := logger.New(t.TempDir(), logger.WithConsole(false), logger.WithFile(false))
	sess := &notificationSession{}
	bridge := NewTextBridge(sess, &fakeSender{}, log, "test", true, []string{"allowed"})
	bridge.OnMessage(context.Background(), Message{Channel: "wechat", UserID: "denied", Text: "/help"})
	if sess.registrations != 0 {
		t.Fatal("unauthorized user registered a notification route")
	}
}

func TestWechatSavedNotificationRouteIsMinimal(t *testing.T) {
	log, _ := logger.New(t.TempDir(), logger.WithConsole(false), logger.WithFile(false))
	defer log.Close()
	sess := &notificationSession{}
	bridge := NewTextBridge(sess, &fakeSender{}, log, "test", false, nil)
	bridge.OnMessage(context.Background(), Message{Channel: "wechat", AccountID: "bot", UserID: "recipient", ConversationID: "conversation", ReplyToken: "fake-context", Text: "/help", Attachments: []Attachment{{LocalPath: "private-local-path"}}})
	var route Message
	if err := json.Unmarshal(sess.metadata, &route); err != nil {
		t.Fatal(err)
	}
	if route.AccountID != "bot" || route.UserID != "recipient" || route.ReplyToken != "fake-context" || route.ConversationID != "conversation" {
		t.Fatal("route fields lost")
	}
	if route.Text != "" || len(route.Attachments) != 0 {
		t.Fatal("saved route contains message content")
	}
}

func TestWecomNotificationRouteOmitsEphemeralReplyAndContent(t *testing.T) {
	log, _ := logger.New(t.TempDir(), logger.WithFile(false))
	defer log.Close()
	sess := &notificationSession{}
	bridge := NewTextBridge(sess, &fakeSender{}, log, "test", false, nil)
	bridge.OnMessage(context.Background(), Message{Channel: "wecom", AccountID: "logical", AccountIdentity: "bot", ConversationID: "group", UserID: "user", ReplyToken: "temporary", Text: "/help"})
	var route Message
	if err := json.Unmarshal(sess.metadata, &route); err != nil {
		t.Fatal(err)
	}
	if route.ReplyToken != "" || route.Text != "" || route.AccountIdentity != "bot" || route.ConversationID != "group" {
		t.Fatalf("route=%+v", route)
	}
}
