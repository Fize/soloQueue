package wecom

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	imagepkg "image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/xiaobaitu/soloqueue/internal/channel"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

func TestMediaFailureLogsSafeDiagnostics(t *testing.T) {
	for _, kind := range []string{"image", "mixed"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			log, err := logger.System(dir, logger.WithConsole(false))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = log.Close() })
			c := NewClient(Config{AccountID: "account", BotID: "bot"}, log)
			body := map[string]any{
				"msgid": "failed-image", "aibotid": "bot", "chattype": "single",
				"from": map[string]string{"userid": "user"}, "msgtype": kind,
			}
			image := mediaImage{URL: "http://example.com/private-image?token=private-token", AESKey: "private-key"}
			if kind == "mixed" {
				body["mixed"] = map[string]any{"msg_item": []any{
					map[string]any{"msgtype": "text", "text": map[string]string{"content": "private-text"}},
					map[string]any{"msgtype": "image", "image": image},
				}}
			} else {
				body["image"] = image
			}
			payload, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			c.dispatch(context.Background(), nil, Frame{Headers: Headers{ReqID: "request"}, Body: payload})
			deadline := time.Now().Add(time.Second)
			for len(c.slots) != 0 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if len(c.slots) != 0 {
				t.Fatal("dispatch did not finish")
			}
			if err := log.Close(); err != nil {
				t.Fatal(err)
			}
			files, err := filepath.Glob(filepath.Join(dir, "logs", "system", "app-*.jsonl"))
			if err != nil || len(files) != 1 {
				t.Fatalf("expected one app log, got %d: %v", len(files), err)
			}
			data, err := os.ReadFile(files[0])
			if err != nil {
				t.Fatal(err)
			}
			var entry map[string]any
			if err := json.Unmarshal(bytes.TrimSpace(data), &entry); err != nil {
				t.Fatal(err)
			}
			fields, ok := entry["ctx"].(map[string]any)
			if !ok || entry["msg"] != "wecom message media normalization failed" || fields["message_type"] != kind || fields["message_id"] != "failed-image" || entry["err"] != "image 1 download: invalid media URL" {
				t.Fatal("missing failure diagnostics")
			}
			for _, secret := range []string{image.URL, image.AESKey, "private-token", "private-text"} {
				if bytes.Contains(data, []byte(secret)) {
					t.Fatal("sensitive media data leaked to logs")
				}
			}
		})
	}
}

func TestDownloadFailureRedactsURL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := downloadMedia(ctx, "https://example.com/private-image?token=private-token")
	if err == nil || !strings.Contains(err.Error(), "canceled=true") {
		t.Fatal("missing cancellation diagnostic")
	}
	for _, secret := range []string{"example.com", "private-image", "private-token"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatal("download error leaked signed URL")
		}
	}
}

func TestDecryptImage(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	plain := []byte("image payload")
	n := 32 - len(plain)%32
	padded := append(append([]byte{}, plain...), bytes.Repeat([]byte{byte(n)}, n)...)
	block, _ := aes.NewCipher(key)
	encrypted := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, key[:16]).CryptBlocks(encrypted, padded)
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding} {
		got, err := decryptImage(encrypted, encoding.EncodeToString(key))
		if err != nil || !bytes.Equal(got, plain) {
			t.Fatalf("decrypt key length %d: %q %v", len(encoding.EncodeToString(key)), got, err)
		}
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
	for _, encoded := range []string{
		"", "not-base64!", base64.RawStdEncoding.EncodeToString(key) + "==",
		base64.StdEncoding.EncodeToString(key[:31]), base64.RawStdEncoding.EncodeToString(key[:31]),
		base64.StdEncoding.EncodeToString(append(append([]byte{}, key...), 7)),
		base64.RawStdEncoding.EncodeToString(append(append([]byte{}, key...), 7)),
	} {
		if _, err := decryptImage(encrypted, encoded); err == nil || err.Error() != "invalid media key" {
			t.Fatalf("invalid key accepted or wrong error: %v", err)
		}
	}
}

func TestNormalizeImagesWithPaddedAndUnpaddedKeys(t *testing.T) {
	key := bytes.Repeat([]byte{0xfb}, 32)
	var image bytes.Buffer
	if err := png.Encode(&image, imagepkg.NewRGBA(imagepkg.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	plain := image.Bytes()
	padding := 32 - len(plain)%32
	padded := append(append([]byte{}, plain...), bytes.Repeat([]byte{byte(padding)}, padding)...)
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	encrypted := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, key[:aes.BlockSize]).CryptBlocks(encrypted, padded)

	for _, tc := range []struct {
		name     string
		encoding *base64.Encoding
	}{
		{"padded", base64.StdEncoding},
		{"unpadded", base64.RawStdEncoding},
	} {
		for _, kind := range []string{"image", "mixed"} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				media := mediaImage{URL: "https://example.com/image", AESKey: tc.encoding.EncodeToString(key)}
				body := map[string]any{"msgtype": kind, "image": media}
				wantText := ""
				wantImages := 1
				if kind == "mixed" {
					delete(body, "image")
					body["mixed"] = map[string]any{"msg_item": []any{
						map[string]any{"msgtype": "text", "text": map[string]string{"content": "before"}},
						map[string]any{"msgtype": "image", "image": media},
						map[string]any{"msgtype": "text", "text": map[string]string{"content": "between"}},
						map[string]any{"msgtype": "image", "image": media},
						map[string]any{"msgtype": "text", "text": map[string]string{"content": "after"}},
					}}
					wantText = "before\n[Image 1]\nbetween\n[Image 2]\nafter"
					wantImages = 2
				}
				payload, err := json.Marshal(body)
				if err != nil {
					t.Fatal(err)
				}
				var in incoming
				if err := json.Unmarshal(payload, &in); err != nil {
					t.Fatal(err)
				}
				calls := 0
				download := func(_ context.Context, url string) ([]byte, error) {
					if url != media.URL {
						t.Fatal("unexpected image URL")
					}
					calls++
					return encrypted, nil
				}
				c := NewClient(Config{}, nil)
				msg, err := c.normalizeWithDownloader(context.Background(), channel.Message{}, in, download)
				if err != nil {
					t.Fatalf("normalize: %v", err)
				}
				if msg.Text != wantText || len(msg.Attachments) != wantImages || calls != wantImages {
					t.Fatalf("unexpected text or image count: text=%q images=%d downloads=%d", msg.Text, len(msg.Attachments), calls)
				}
				for _, attachment := range msg.Attachments {
					if attachment.Kind != channel.AttachmentImage || attachment.MIMEType != "image/png" || !bytes.Equal(attachment.Data, plain) {
						t.Fatal("image attachment differs from original PNG")
					}
				}
			})
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
