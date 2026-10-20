package wechat

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/channel"
)

func TestNotificationErrorsDoNotExposeSecrets(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response *http.Response
		err      error
		want     string
	}{
		{"http", &http.Response{StatusCode: 401, Status: "401 Unauthorized", Body: io.NopCloser(strings.NewReader("fake-auth-secret fake-context-secret"))}, nil, "401"},
		{"business", jsonResponse(`{"ret":-1,"errcode":123,"errmsg":"fake-auth-secret fake-context-secret"}`), nil, "123"},
		{"malformed-json", jsonResponse(`fake-auth-secret fake-context-secret`), nil, "wechat_response_invalid"},
		{"timeout", nil, context.DeadlineExceeded, "unknown"},
		{"transport", nil, errors.New("fake-auth-secret fake-context-secret"), "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := NewClientWithHTTP(Config{Token: "fake-auth-secret", BaseURL: "https://example.test"}, &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return tc.response, tc.err })})
			err := c.SendText(context.Background(), channel.Message{UserID: "user", ReplyToken: "fake-context-secret"}, "notification")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("unexpected diagnostic: %v", err)
			}
			if strings.Contains(err.Error(), "fake-auth-secret") || strings.Contains(err.Error(), "fake-context-secret") {
				t.Fatal("diagnostic contains a credential")
			}
		})
	}
}

type notificationBodyError struct {
	err        error
	beforeRead func()
}

func (b notificationBodyError) Read([]byte) (int, error) {
	if b.beforeRead != nil {
		b.beforeRead()
	}
	return 0, b.err
}
func (b notificationBodyError) Close() error { return nil }

func TestNotificationResponseBodyRequestCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := NewClientWithHTTP(Config{Token: "fake-auth-secret", BaseURL: "https://example.test"}, &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body: notificationBodyError{
				beforeRead: cancel,
				err:        errors.New("fake-auth-secret fake-context-secret"),
			},
		}, nil
	})})
	err := c.SendText(ctx, channel.Message{UserID: "user", ReplyToken: "fake-context-secret"}, "notification")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("lost request cancellation identity: %v", err)
	}
	if strings.Contains(err.Error(), "fake-auth-secret") || strings.Contains(err.Error(), "fake-context-secret") {
		t.Fatal("diagnostic contains a credential")
	}
}

func TestNotificationResponseBodyContextErrors(t *testing.T) {
	for _, operation := range []string{"sendmessage", "getupdates"} {
		for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
			t.Run(operation+"/"+cause.Error(), func(t *testing.T) {
				c := NewClientWithHTTP(Config{Token: "fake-auth-secret", BaseURL: "https://example.test"}, &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       notificationBodyError{err: fmt.Errorf("fake-auth-secret fake-context-secret: %w", cause)},
					}, nil
				})})
				var err error
				if operation == "sendmessage" {
					err = c.SendText(context.Background(), channel.Message{UserID: "user", ReplyToken: "fake-context-secret"}, "notification")
				} else {
					_, err = c.GetUpdates(context.Background(), "", time.Second)
				}
				if !errors.Is(err, cause) {
					t.Errorf("lost response-body context identity: got %v, want %v", err, cause)
				}
				if err == nil {
					t.Fatal("expected response-body error")
				}
				if strings.Contains(err.Error(), "fake-auth-secret") || strings.Contains(err.Error(), "fake-context-secret") {
					t.Fatal("diagnostic contains a credential")
				}
				if operation == "sendmessage" && !strings.Contains(err.Error(), "delivery_unknown") {
					t.Error("send failure must preserve unknown acceptance diagnostic")
				}
				if operation == "getupdates" && strings.Contains(err.Error(), "delivery_unknown") {
					t.Error("polling labeled as delivery")
				}
			})
		}
	}
}

func TestNotificationAPIErrorRetainsCodesAndContextCause(t *testing.T) {
	err := responseError("sendmessage", APIResponse{Ret: -1, ErrCode: 123, ErrMsg: "fake-secret"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Ret != -1 || apiErr.ErrCode != 123 {
		t.Fatalf("missing structured codes: %v", err)
	}
	for _, operation := range []string{"sendmessage", "getupdates"} {
		err := safeTransportError(context.Background(), operation, fmt.Errorf("fake-secret: %w", context.DeadlineExceeded))
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("lost deadline identity")
		}
		if operation == "getupdates" && strings.Contains(err.Error(), "delivery_unknown") {
			t.Fatal("polling labeled as delivery")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(safeTransportError(ctx, "getupdates", errors.New("fake-secret")), context.Canceled) {
		t.Fatal("lost cancellation identity")
	}
}
