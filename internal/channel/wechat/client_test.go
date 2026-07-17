package wechat

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/xiaobaitu/soloqueue/internal/channel"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func jsonResponse(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestClientGetUpdatesUsesIlinkHeadersAndCursor(t *testing.T) {
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/ilink/bot/getupdates" {
			t.Fatalf("path = %s", req.URL.Path)
		}
		if got := req.Header.Get("Authorization"); got != "Bearer token" {
			t.Fatalf("Authorization = %q", got)
		}
		encodedUIN := req.Header.Get("X-WECHAT-UIN")
		if _, err := base64.StdEncoding.DecodeString(encodedUIN); err != nil {
			t.Fatalf("X-WECHAT-UIN is not base64: %v", err)
		}
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["get_updates_buf"] != "cursor-1" {
			t.Fatalf("cursor = %#v", body["get_updates_buf"])
		}
		return jsonResponse(`{"ret":0,"get_updates_buf":"cursor-2"}`), nil
	})
	client := NewClientWithHTTP(Config{Token: "token", BaseURL: "https://example.test", Version: "1.2.3"}, &http.Client{Transport: transport})
	resp, err := client.GetUpdates(context.Background(), "cursor-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetUpdatesBuf != "cursor-2" {
		t.Fatalf("next cursor = %q", resp.GetUpdatesBuf)
	}
}

func TestClientSendTextPreservesReplyContext(t *testing.T) {
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body struct {
			Message Message `json:"msg"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Message.ToUserID != "user-1" || body.Message.ContextToken != "ctx-1" {
			t.Fatalf("reply target = %#v", body.Message)
		}
		if got := body.Message.ItemList[0].TextItem.Text; got != "hello" {
			t.Fatalf("text = %q", got)
		}
		return jsonResponse(`{"ret":0}`), nil
	})
	client := NewClientWithHTTP(Config{Token: "token", BaseURL: "https://example.test"}, &http.Client{Transport: transport})
	err := client.SendText(context.Background(), channel.Message{UserID: "user-1", ReplyToken: "ctx-1"}, "hello")
	if err != nil {
		t.Fatal(err)
	}
}

func TestClientSendTextFormatsWechatMarkdown(t *testing.T) {
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var body struct {
			Message Message `json:"msg"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if got := body.Message.ItemList[0].TextItem.Text; got != "title\ndeleted" {
			t.Fatalf("formatted text = %q", got)
		}
		return jsonResponse(`{"ret":0}`), nil
	})
	client := NewClientWithHTTP(Config{Token: "token", BaseURL: "https://example.test"}, &http.Client{Transport: transport})
	if err := client.SendText(context.Background(), channel.Message{UserID: "user", ReplyToken: "ctx"}, "##### title\n~~deleted~~"); err != nil {
		t.Fatal(err)
	}
}

func TestStartLoginUsesCurrentPostProtocol(t *testing.T) {
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.Path != "/ilink/bot/get_bot_qrcode" || req.URL.Query().Get("bot_type") != "3" {
			t.Fatalf("unexpected request: %s %s", req.Method, req.URL.String())
		}
		var body struct {
			Tokens []string `json:"local_token_list"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body.Tokens) != 1 || body.Tokens[0] != "old-token" {
			t.Fatalf("local tokens = %#v", body.Tokens)
		}
		return jsonResponse(`{"qrcode":"qr","qrcode_img_content":"https://qr.example"}`), nil
	})
	client := NewClientWithHTTP(Config{}, &http.Client{Transport: transport})
	resp, err := client.StartLogin(context.Background(), []string{"old-token"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.QRCode != "qr" {
		t.Fatalf("qrcode = %q", resp.QRCode)
	}
}

func TestClientVersion(t *testing.T) {
	if got := clientVersion("v1.2.3-beta"); got != "66051" {
		t.Fatalf("clientVersion = %s", got)
	}
}
