package wechat

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/channel"
)

func TestClientSendMediaUsesOriginalAccountAndReplyRoute(t *testing.T) {
	plain := []byte("report contents")
	path := filepath.Join(t.TempDir(), "report.csv")
	if err := os.WriteFile(path, plain, 0o600); err != nil {
		t.Fatal(err)
	}
	var sent Message
	requests := 0
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		switch req.URL.Path {
		case "/ilink/bot/getuploadurl":
			var body map[string]any
			_ = json.NewDecoder(req.Body).Decode(&body)
			if body["to_user_id"] != "user-a" || int(body["media_type"].(float64)) != 3 {
				t.Fatalf("upload request=%#v", body)
			}
			return jsonResponse(`{"ret":0,"upload_full_url":"https://cdn.test/upload"}`), nil
		case "/upload":
			ciphertext, _ := io.ReadAll(req.Body)
			if bytes.Equal(ciphertext, plain) || len(ciphertext)%16 != 0 {
				t.Fatalf("invalid ciphertext size=%d", len(ciphertext))
			}
			resp := jsonResponse(`{}`)
			resp.Header.Set("x-encrypted-param", "download-token")
			return resp, nil
		case "/ilink/bot/sendmessage":
			var body struct {
				Message Message `json:"msg"`
			}
			_ = json.NewDecoder(req.Body).Decode(&body)
			sent = body.Message
			return jsonResponse(`{"ret":0}`), nil
		default:
			t.Fatalf("unexpected request %s", req.URL.String())
			return nil, nil
		}
	})
	client := NewClientWithHTTP(Config{Token: "token", BotID: "bot-a", BaseURL: "https://api.test"}, &http.Client{Transport: transport})
	msg := channel.Message{Channel: "wechat", AccountID: "bot-a", UserID: "user-a", ConversationID: "chat-a", ReplyToken: "reply-a"}
	err := client.SendMedia(context.Background(), msg, []channel.OutboundMedia{{Kind: channel.MediaFile, Path: path, FileName: "report.csv"}})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 3 || sent.ToUserID != "user-a" || sent.ContextToken != "reply-a" || sent.ItemList[0].Type != 4 {
		t.Fatalf("requests=%d sent=%#v", requests, sent)
	}
}

func TestClientSendMediaRejectsCrossAccountRoute(t *testing.T) {
	client := NewClientWithHTTP(Config{BotID: "bot-a"}, &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("network must not be called"); return nil, nil })})
	err := client.SendMedia(context.Background(), channel.Message{AccountID: "bot-b", UserID: "u", ReplyToken: "t"}, []channel.OutboundMedia{{URL: "https://example.test/a"}})
	if err == nil {
		t.Fatal("expected account mismatch")
	}
}

func TestClientSendMediaRejectsPrivateRemoteURL(t *testing.T) {
	client := NewClientWithHTTP(Config{BotID: "bot-a"}, &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("network must not be called")
		return nil, nil
	})})
	err := client.SendMedia(context.Background(), channel.Message{AccountID: "bot-a", UserID: "u", ReplyToken: "t"}, []channel.OutboundMedia{{Kind: channel.MediaFile, URL: "http://127.0.0.1/private"}})
	if err == nil {
		t.Fatal("expected private URL rejection")
	}
}

func TestClientSendMediaVoiceUsesNativeSILKOtherwiseFile(t *testing.T) {
	tests := []struct {
		name        string
		data        []byte
		mediaType   int
		messageType int
	}{
		{name: "silk", data: []byte("#!SILK_V3payload"), mediaType: 4, messageType: 3},
		{name: "other audio", data: []byte("ID3payload"), mediaType: 3, messageType: 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "voice.bin")
			if err := os.WriteFile(path, tc.data, 0o600); err != nil {
				t.Fatal(err)
			}
			gotMediaType := 0
			gotMessageType := 0
			transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/ilink/bot/getuploadurl":
					var body map[string]any
					_ = json.NewDecoder(req.Body).Decode(&body)
					gotMediaType = int(body["media_type"].(float64))
					return jsonResponse(`{"ret":0,"upload_full_url":"https://cdn.test/upload"}`), nil
				case "/upload":
					resp := jsonResponse(`{}`)
					resp.Header.Set("x-encrypted-param", "download-token")
					return resp, nil
				case "/ilink/bot/sendmessage":
					var body struct {
						Message Message `json:"msg"`
					}
					_ = json.NewDecoder(req.Body).Decode(&body)
					gotMessageType = body.Message.ItemList[0].Type
					return jsonResponse(`{"ret":0}`), nil
				default:
					t.Fatalf("unexpected path %s", req.URL.Path)
					return nil, nil
				}
			})
			client := NewClientWithHTTP(Config{Token: "token", BotID: "bot", BaseURL: "https://api.test"}, &http.Client{Transport: transport})
			err := client.SendMedia(context.Background(), channel.Message{AccountID: "bot", UserID: "user", ReplyToken: "reply"}, []channel.OutboundMedia{{Kind: channel.MediaVoice, Path: path, FileName: "voice.bin"}})
			if err != nil {
				t.Fatal(err)
			}
			if gotMediaType != tc.mediaType || gotMessageType != tc.messageType {
				t.Fatalf("upload media_type=%d message type=%d", gotMediaType, gotMessageType)
			}
		})
	}
}

func TestClientDownloadMediaDecryptsRawAndHexKeyEncodings(t *testing.T) {
	key, _ := hex.DecodeString("00112233445566778899aabbccddeeff")
	plain := []byte("wechat attachment")
	ciphertext, err := encryptAESECB(plain, key)
	if err != nil {
		t.Fatal(err)
	}
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Status: "200 OK", Body: io.NopCloser(bytes.NewReader(ciphertext)), Header: make(http.Header)}, nil
	})
	client := NewClientWithHTTP(Config{CDNBaseURL: "https://cdn.test"}, &http.Client{Transport: transport})
	keys := []string{base64.StdEncoding.EncodeToString(key), base64.StdEncoding.EncodeToString([]byte(hex.EncodeToString(key)))}
	for _, encoded := range keys {
		got, err := client.DownloadMedia(context.Background(), CDNMedia{EncryptQueryParam: "q", AESKey: encoded})
		if err != nil || !bytes.Equal(got, plain) {
			t.Fatalf("got=%q err=%v", got, err)
		}
	}
}
