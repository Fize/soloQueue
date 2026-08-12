package wechat

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/channel"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

func TestGatewayNormalizeTextAndVoiceTranscript(t *testing.T) {
	gateway := &Gateway{cfg: Config{BotID: "bot-1"}}
	got, ok := gateway.normalize(Message{
		FromUserID:   "user-1",
		SessionID:    "session-1",
		MessageType:  1,
		ContextToken: "context-1",
		ItemList: []MessageItem{
			{Type: 1, TextItem: &TextItem{Text: "hello"}},
			{Type: 3, VoiceItem: &VoiceItem{Text: "voice text"}},
		},
	})
	if !ok {
		t.Fatal("message was not normalized")
	}
	if got.Text != "hello\nvoice text" || got.ReplyToken != "context-1" || got.AccountID != "bot-1" || got.Channel != "wechat" {
		t.Fatalf("normalized message = %#v", got)
	}
	if len(got.Attachments) != 1 || got.Attachments[0].Kind != "audio" || got.Attachments[0].Transcript != "voice text" {
		t.Fatalf("attachments = %#v", got.Attachments)
	}
}

func TestGatewayNormalizeAcceptsMediaOnlyMessage(t *testing.T) {
	gateway := &Gateway{}
	got, ok := gateway.normalize(Message{FromUserID: "user", MessageType: 1, ContextToken: "ctx", ItemList: []MessageItem{{Type: 3, VoiceItem: &VoiceItem{}}}})
	if !ok || len(got.Attachments) != 1 || got.Attachments[0].Kind != "audio" {
		t.Fatalf("media-only message = %#v, ok=%v", got, ok)
	}
}

func TestGatewayDispatchDownloadsAndDecryptsInboundImage(t *testing.T) {
	key := []byte("0123456789abcdef")
	plain := []byte("fake image bytes")
	ciphertext, err := encryptAESECB(plain, key)
	if err != nil {
		t.Fatal(err)
	}
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Status: "200 OK", Body: io.NopCloser(bytes.NewReader(ciphertext)), Header: make(http.Header)}, nil
	})
	client := NewClientWithHTTP(Config{BotID: "bot-a", CDNBaseURL: "https://cdn.test"}, &http.Client{Transport: transport})
	log, _ := logger.New(t.TempDir(), logger.WithConsole(false), logger.WithFile(false))
	var got channel.Message
	var meta channel.ChatMeta
	gateway := NewGateway(Config{BotID: "bot-a"}, client, channel.HandlerFunc(func(ctx context.Context, msg channel.Message) {
		got = msg
		meta, _ = channel.ChatMetaFromContext(ctx)
	}), log)
	gateway.dispatch(context.Background(), Message{
		MessageID: 7, FromUserID: "user-a", SessionID: "chat-a", MessageType: 1, ContextToken: "reply-a",
		ItemList: []MessageItem{{Type: 2, ImageItem: &ImageItem{Media: &CDNMedia{EncryptQueryParam: "q", AESKey: base64.StdEncoding.EncodeToString(key)}}}},
	})
	if len(got.Attachments) != 1 || !bytes.Equal(got.Attachments[0].Data, plain) {
		t.Fatalf("attachments=%#v", got.Attachments)
	}
	if meta.Channel != "wechat" || meta.AccountID != "bot-a" || meta.ConversationID != "chat-a" || meta.UserID != "user-a" {
		t.Fatalf("route meta=%#v", meta)
	}
}
