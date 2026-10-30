package wecom

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/xiaobaitu/soloqueue/internal/channel"
)

func TestDecryptImage(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	plain := []byte("image payload")
	n := 32 - len(plain)%32
	padded := append(append([]byte{}, plain...), bytes.Repeat([]byte{byte(n)}, n)...)
	block, _ := aes.NewCipher(key)
	encrypted := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, key[:16]).CryptBlocks(encrypted, padded)
	got, err := decryptImage(encrypted, base64.StdEncoding.EncodeToString(key))
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("decrypt: %q %v", got, err)
	}
	encrypted[len(encrypted)-1] ^= 1
	if _, err := decryptImage(encrypted, base64.StdEncoding.EncodeToString(key)); err == nil {
		t.Fatal("accepted damaged padding")
	}
	for _, data := range [][]byte{nil, {1}} {
		if _, err := decryptImage(data, "bad"); err == nil {
			t.Fatal("accepted malformed ciphertext")
		}
	}
}
func TestNormalizeVoiceAndMixedText(t *testing.T) {
	c := NewClient(Config{}, nil)
	var in incoming
	_ = json.Unmarshal([]byte(`{"msgtype":"voice","voice":{"content":"hello"}}`), &in)
	msg, err := c.normalize(context.Background(), channel.Message{}, in)
	if err != nil || msg.Text != "hello" || len(msg.Attachments) != 1 || msg.Attachments[0].Transcript != "hello" {
		t.Fatalf("%+v %v", msg, err)
	}
	in.Voice.Content = " "
	if _, err := c.normalize(context.Background(), channel.Message{}, in); err == nil {
		t.Fatal("empty transcript accepted")
	}
	_ = json.Unmarshal([]byte(`{"msgtype":"mixed","mixed":{"msg_item":[{"msgtype":"text","text":{"content":"a"}},{"msgtype":"text","text":{"content":"b"}}]}}`), &in)
	msg, err = c.normalize(context.Background(), channel.Message{}, in)
	if err != nil || msg.Text != "a\nb" {
		t.Fatalf("%+v %v", msg, err)
	}
}
func TestDownloadRejectsLocalTargets(t *testing.T) {
	for _, u := range []string{"http://example.com/a", "https://127.0.0.1/a", "https://[::1]/a", "file:///tmp/a"} {
		if _, err := downloadMedia(context.Background(), u); err == nil {
			t.Fatalf("accepted %s", u)
		}
	}
}
func TestUploadAndSendVoice(t *testing.T) {
	upgrader := websocket.Upgrader{}
	seen := make(chan Frame, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			var f Frame
			if conn.ReadJSON(&f) != nil {
				return
			}
			seen <- f
			body := json.RawMessage(`{}`)
			switch f.Cmd {
			case "aibot_upload_media_init":
				body = json.RawMessage(`{"upload_id":"upload"}`)
			case "aibot_upload_media_finish":
				body = json.RawMessage(`{"media_id":"media"}`)
			}
			if conn.WriteJSON(Frame{Headers: f.Headers, ErrCode: intPtr(0), Body: body}) != nil {
				return
			}
		}
	}))
	defer server.Close()
	c := NewClient(Config{AccountID: "account", BotID: "bot", Secret: "fake", URL: "ws" + strings.TrimPrefix(server.URL, "http")}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	defer func() { c.Close(); <-done }()
	<-seen
	deadline := time.Now().Add(time.Second)
	for !c.Status().Connected && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	path := t.TempDir() + "/test.amr"
	if err := writeTestVoice(path); err != nil {
		t.Fatal(err)
	}
	err := c.SendMedia(ctx, channel.Message{AccountID: "account", AccountIdentity: "bot", ConversationID: "chat"}, []channel.OutboundMedia{{Kind: channel.MediaVoice, Path: path}})
	if err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []string{"aibot_upload_media_init", "aibot_upload_media_chunk", "aibot_upload_media_finish", "aibot_send_msg"} {
		select {
		case f := <-seen:
			if f.Cmd != cmd {
				t.Fatalf("got %s want %s", f.Cmd, cmd)
			}
			if cmd == "aibot_upload_media_chunk" {
				var body struct {
					Index int `json:"chunk_index"`
				}
				_ = json.Unmarshal(f.Body, &body)
				if body.Index != 0 {
					t.Fatal("index must match SDK implementation")
				}
			}
		case <-time.After(time.Second):
			t.Fatal("missing frame")
		}
	}
}

func writeTestVoice(path string) error {
	return os.WriteFile(path, []byte("#!AMR\nvoice fixture"), 0600)
}

func TestVoiceCallbackReachesHandler(t *testing.T) {
	upgrader := websocket.Upgrader{}
	received := make(chan channel.Message, 1)
	handlerContext := make(chan context.Context, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		var auth Frame
		if conn.ReadJSON(&auth) != nil {
			return
		}
		_ = conn.WriteJSON(Frame{Headers: auth.Headers, ErrCode: intPtr(0)})
		_ = conn.WriteJSON(Frame{Cmd: "aibot_msg_callback", Headers: Headers{"voice-request"}, Body: json.RawMessage(`{"msgid":"voice","aibotid":"bot","chattype":"single","from":{"userid":"user"},"msgtype":"voice","voice":{"content":"hello from voice"}}`)})
		for {
			var f Frame
			if conn.ReadJSON(&f) != nil {
				return
			}
			_ = conn.WriteJSON(Frame{Headers: f.Headers, ErrCode: intPtr(0)})
		}
	}))
	defer server.Close()
	c := NewClient(Config{AccountID: "account", BotID: "bot", Secret: "fake", URL: "ws" + strings.TrimPrefix(server.URL, "http")}, nil)
	c.SetHandler(channel.HandlerFunc(func(ctx context.Context, msg channel.Message) {
		handlerContext <- ctx
		received <- msg
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	defer func() { c.Close(); <-done }()
	select {
	case msg := <-received:
		if msg.Text != "hello from voice" || len(msg.Attachments) != 1 || msg.ReplyToken != "voice-request" || msg.ConversationID != "user" {
			t.Fatalf("unexpected message %+v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("voice callback did not reach handler")
	}
	handledCtx := <-handlerContext
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	_ = conn.Close()
	select {
	case <-handledCtx.Done():
		t.Fatal("connection loss canceled accepted handler")
	case <-time.After(30 * time.Millisecond):
	}
}
