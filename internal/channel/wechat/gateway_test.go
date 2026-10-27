package wechat

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"testing"
	"time"

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

func TestGatewayNormalizePreservesMessageID(t *testing.T) {
	gateway := &Gateway{cfg: Config{BotID: "bot-1"}}
	got, ok := gateway.normalize(Message{
		MessageID: 42, FromUserID: "user-1", SessionID: "session-1", MessageType: 1, ContextToken: "context-1",
		ItemList: []MessageItem{{Type: 1, TextItem: &TextItem{Text: "hello"}}},
	})
	if !ok {
		t.Fatal("message was not normalized")
	}
	if got.MessageID != "42" {
		t.Fatalf("message ID = %q, want 42", got.MessageID)
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
	type handled struct {
		msg  channel.Message
		meta channel.ChatMeta
	}
	received := make(chan handled, 1)
	gateway := NewGateway(Config{BotID: "bot-a"}, client, channel.HandlerFunc(func(ctx context.Context, msg channel.Message) {
		meta, _ := channel.ChatMetaFromContext(ctx)
		received <- handled{msg: msg, meta: meta}
	}), log)
	t.Cleanup(gateway.Close)
	gateway.dispatch(context.Background(), Message{
		MessageID: 7, FromUserID: "user-a", SessionID: "chat-a", MessageType: 1, ContextToken: "reply-a",
		ItemList: []MessageItem{{Type: 2, ImageItem: &ImageItem{Media: &CDNMedia{EncryptQueryParam: "q", AESKey: base64.StdEncoding.EncodeToString(key)}}}},
	})
	var got handled
	select {
	case got = <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for buffered image")
	}
	if len(got.msg.Attachments) != 1 || !bytes.Equal(got.msg.Attachments[0].Data, plain) {
		t.Fatalf("attachments=%#v", got.msg.Attachments)
	}
	if got.meta.Channel != "wechat" || got.meta.AccountID != "bot-a" || got.meta.ConversationID != "chat-a" || got.meta.UserID != "user-a" {
		t.Fatalf("route meta=%#v", got.meta)
	}
}

func TestGatewayDispatchSkipsDownloadForServerTranscribedVoice(t *testing.T) {
	downloadCalls := 0
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		downloadCalls++
		return &http.Response{StatusCode: http.StatusInternalServerError, Status: "500 Internal Server Error", Body: io.NopCloser(bytes.NewReader(nil)), Header: make(http.Header)}, nil
	})
	client := NewClientWithHTTP(Config{BotID: "bot-a", CDNBaseURL: "https://cdn.test"}, &http.Client{Transport: transport})
	log, _ := logger.New(t.TempDir(), logger.WithConsole(false), logger.WithFile(false))
	received := make(chan channel.Message, 1)
	gateway := NewGateway(Config{BotID: "bot-a"}, client, channel.HandlerFunc(func(_ context.Context, msg channel.Message) {
		received <- msg
	}), log)
	t.Cleanup(gateway.Close)
	gateway.dispatch(context.Background(), Message{
		MessageID: 8, FromUserID: "user-a", SessionID: "chat-a", MessageType: 1, ContextToken: "reply-a",
		ItemList: []MessageItem{{Type: 3, VoiceItem: &VoiceItem{
			Text:  "  server transcript  ",
			Media: &CDNMedia{EncryptQueryParam: "q", AESKey: base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))},
		}}},
	})

	var got channel.Message
	select {
	case got = <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for transcribed voice")
	}
	if downloadCalls != 0 {
		t.Fatalf("media download calls = %d", downloadCalls)
	}
	if got.Text != "server transcript" || len(got.Attachments) != 1 || got.Attachments[0].Transcript != "server transcript" || len(got.Attachments[0].Data) != 0 {
		t.Fatalf("normalized message = %#v", got)
	}
}

func TestGatewayBuffersComplementaryMessagesForOneSecond(t *testing.T) {
	log, _ := logger.New(t.TempDir(), logger.WithConsole(false), logger.WithFile(false))
	received := make(chan channel.Message, 2)
	gateway := NewGateway(Config{BotID: "bot-a"}, nil, channel.HandlerFunc(func(_ context.Context, msg channel.Message) {
		received <- msg
	}), log)
	t.Cleanup(gateway.Close)

	gateway.dispatch(context.Background(), Message{
		MessageID: 1, FromUserID: "user-a", SessionID: "chat-a", MessageType: 1, ContextToken: "text-token",
		ItemList: []MessageItem{{Type: 1, TextItem: &TextItem{Text: "describe this"}}},
	})
	gateway.dispatch(context.Background(), Message{
		MessageID: 2, FromUserID: "user-a", SessionID: "chat-a", MessageType: 1, ContextToken: "media-token",
		ItemList: []MessageItem{{Type: 2, ImageItem: &ImageItem{}}},
	})

	select {
	case got := <-received:
		if got.Text != "describe this" || len(got.Attachments) != 1 || got.ReplyToken != "media-token" {
			t.Fatalf("merged message = %#v", got)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("gateway did not merge complementary messages")
	}
}
