package wecom

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/xiaobaitu/soloqueue/internal/channel"
)

func TestClientAuthenticatesAndRepliesWithoutBlockingReads(t *testing.T) {
	frames := make(chan Frame, 16)
	received := make(chan channel.Message, 2)
	upgrader := websocket.Upgrader{}
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
		frames <- auth
		_ = conn.WriteJSON(Frame{Headers: auth.Headers, ErrCode: intPtr(0)})
		body := json.RawMessage(`{"msgid":"message-1","aibotid":"bot-1","chattype":"group","chatid":"group-1","from":{"userid":"user-1"},"msgtype":"text","text":{"content":"hello"}}`)
		incoming := Frame{Cmd: "aibot_msg_callback", Headers: Headers{ReqID: "callback-1"}, Body: body}
		_ = conn.WriteJSON(incoming)
		_ = conn.WriteJSON(incoming)
		for {
			var frame Frame
			if conn.ReadJSON(&frame) != nil {
				return
			}
			frames <- frame
			_ = conn.WriteJSON(Frame{Headers: frame.Headers, ErrCode: intPtr(0)})
		}
	}))
	defer server.Close()
	client := NewClient(Config{BotID: "bot-1", Secret: "fake-secret", AccountID: "account-1", URL: "ws" + strings.TrimPrefix(server.URL, "http"), Heartbeat: 20 * time.Millisecond}, nil)
	client.SetHandler(channel.HandlerFunc(func(ctx context.Context, msg channel.Message) {
		received <- msg
		if err := client.BeginResponse(ctx, msg); err != nil {
			t.Error(err)
			return
		}
		if err := client.SendText(ctx, msg, "answer"); err != nil {
			t.Error(err)
		}
		if err := client.FinishResponse(ctx, msg, ""); err != nil {
			t.Error(err)
		}
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- client.Run(ctx) }()
	defer func() { client.Close(); <-done }()
	select {
	case auth := <-frames:
		if auth.Cmd != "aibot_subscribe" {
			t.Fatalf("auth=%s", auth.Cmd)
		}
	case <-time.After(time.Second):
		t.Fatal("auth timeout")
	}
	select {
	case msg := <-received:
		if msg.ConversationID != "group-1" || msg.AccountID != "account-1" {
			t.Fatalf("msg=%+v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("message timeout")
	}
	finished := false
	heartbeat := false
	timeout := time.After(2 * time.Second)
	for !finished || !heartbeat {
		select {
		case frame := <-frames:
			if frame.Cmd == "ping" {
				heartbeat = true
			}
			if frame.Cmd == "aibot_respond_msg" {
				var body struct {
					Stream struct {
						Content string
						Finish  bool
					}
				}
				_ = json.Unmarshal(frame.Body, &body)
				if frame.Headers.ReqID != "callback-1" {
					t.Fatal("reply lost req_id")
				}
				if body.Stream.Finish {
					finished = true
					if body.Stream.Content != "answer" {
						t.Fatalf("final=%s", body.Stream.Content)
					}
				}
			}
		case <-timeout:
			t.Fatal("reply or heartbeat timeout")
		}
	}
	select {
	case <-received:
		t.Fatal("duplicate dispatched")
	default:
	}
}

func TestClientStopsAfterAuthenticationFailure(t *testing.T) {
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		var auth Frame
		_ = conn.ReadJSON(&auth)
		_ = conn.WriteJSON(Frame{Headers: auth.Headers, ErrCode: intPtr(40001)})
	}))
	defer server.Close()
	c := NewClient(Config{BotID: "bot", Secret: "secret", AccountID: "account", URL: "ws" + strings.TrimPrefix(server.URL, "http")}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.Run(ctx); err == nil || c.Status().State != "auth_failed" {
		t.Fatalf("err=%v status=%+v", err, c.Status())
	}
}
func intPtr(n int) *int { return &n }

func TestConnectionReplacementStopsReconnect(t *testing.T) {
	upgrader := websocket.Upgrader{}
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
		_ = conn.WriteJSON(Frame{Cmd: "aibot_event_callback", Headers: Headers{"event"}, Body: json.RawMessage(`{"event":{"eventtype":"disconnected_event"}}`)})
	}))
	defer server.Close()
	c := NewClient(Config{BotID: "bot", Secret: "fake", URL: "ws" + strings.TrimPrefix(server.URL, "http")}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.Run(ctx); err != errReplaced || c.Status().State != "conflict" {
		t.Fatalf("err=%v state=%+v", err, c.Status())
	}
}

func TestActiveNotificationAndRejectedReplyAreNotRetried(t *testing.T) {
	frames := make(chan Frame, 16)
	upgrader := websocket.Upgrader{}
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
		for {
			var frame Frame
			if conn.ReadJSON(&frame) != nil {
				return
			}
			frames <- frame
			code := 0
			if frame.Cmd == "aibot_respond_msg" {
				code = 45009
			}
			_ = conn.WriteJSON(Frame{Headers: frame.Headers, ErrCode: intPtr(code)})
		}
	}))
	defer server.Close()
	c := NewClient(Config{BotID: "bot", Secret: "fake", URL: "ws" + strings.TrimPrefix(server.URL, "http")}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	defer func() { c.Close(); <-done }()
	deadline := time.After(time.Second)
	for !c.Status().Connected {
		select {
		case <-deadline:
			t.Fatal("not connected")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	msg := channel.Message{ConversationID: "user", ReplyToken: "reply"}
	if err := c.SendNotification(ctx, msg, "scheduled result"); err != nil {
		t.Fatal(err)
	}
	active := <-frames
	var body map[string]any
	_ = json.Unmarshal(active.Body, &body)
	if active.Cmd != "aibot_send_msg" || body["chatid"] != "user" {
		t.Fatalf("active=%+v", active)
	}
	if err := c.BeginResponse(ctx, msg); err == nil {
		t.Fatal("rejected reply accepted")
	}
	<-frames
	if err := c.FinishResponse(ctx, msg, ""); err != nil {
		t.Fatal(err)
	}
	if len(frames) != 0 {
		t.Fatal("failed reply retried")
	}
	staleCtx := context.WithValue(ctx, connectionKey{}, &websocket.Conn{})
	if err := c.BeginResponse(staleCtx, channel.Message{ReplyToken: "old"}); err != ErrClosed {
		t.Fatalf("stale reply err=%v", err)
	}
}

func TestUnsupportedMediaAndWhitelist(t *testing.T) {
	frames := make(chan Frame, 16)
	called := make(chan bool, 1)
	upgrader := websocket.Upgrader{}
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
		for _, user := range []string{"denied", "allowed"} {
			body, _ := json.Marshal(map[string]any{"msgid": user, "aibotid": "bot", "chattype": "single", "from": map[string]string{"userid": user}, "msgtype": "image"})
			_ = conn.WriteJSON(Frame{Cmd: "aibot_msg_callback", Headers: Headers{user}, Body: body})
		}
		for {
			var frame Frame
			if conn.ReadJSON(&frame) != nil {
				return
			}
			frames <- frame
			_ = conn.WriteJSON(Frame{Headers: frame.Headers, ErrCode: intPtr(0)})
		}
	}))
	defer server.Close()
	c := NewClient(Config{BotID: "bot", Secret: "fake", URL: "ws" + strings.TrimPrefix(server.URL, "http"), WhitelistEnabled: true, Whitelist: []string{"allowed"}}, nil)
	c.SetHandler(channel.HandlerFunc(func(context.Context, channel.Message) { called <- true }))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	defer func() { c.Close(); <-done }()
	deadline := time.After(time.Second)
	for {
		select {
		case frame := <-frames:
			if frame.Headers.ReqID == "denied" {
				t.Fatal("replied to unauthorized user")
			}
			var body struct {
				Stream struct {
					Finish  bool
					Content string
				}
			}
			_ = json.Unmarshal(frame.Body, &body)
			if body.Stream.Finish {
				if !strings.Contains(body.Stream.Content, "text messages only") {
					t.Fatal("media response missing")
				}
				select {
				case <-called:
					t.Fatal("unsupported media reached agent")
				default:
				}
				return
			}
		case <-deadline:
			t.Fatal("media response timeout")
		}
	}
}

func TestClipPreservesUTF8(t *testing.T) {
	if got := clip("abc你好", 7); got != "abc你" {
		t.Fatalf("clip=%q", got)
	}
}

func TestMissingAcknowledgementDoesNotRetryOrFinishUnknownReply(t *testing.T) {
	frames := make(chan Frame, 4)
	upgrader := websocket.Upgrader{}
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
		for {
			var frame Frame
			if conn.ReadJSON(&frame) != nil {
				return
			}
			frames <- frame
		}
	}))
	defer server.Close()
	c := NewClient(Config{BotID: "bot", Secret: "fake", URL: "ws" + strings.TrimPrefix(server.URL, "http")}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	defer func() { c.Close(); <-done }()
	deadline := time.After(time.Second)
	for !c.Status().Connected {
		select {
		case <-deadline:
			t.Fatal("not connected")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	sendCtx, stop := context.WithTimeout(ctx, 30*time.Millisecond)
	defer stop()
	msg := channel.Message{ReplyToken: "original"}
	if err := c.BeginResponse(sendCtx, msg); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("missing ack err=%v", err)
	}
	<-frames
	if err := c.FinishResponse(ctx, msg, "finished"); err != nil {
		t.Fatal(err)
	}
	if len(frames) != 0 {
		t.Fatal("unknown reply retried")
	}
	c.mu.Lock()
	left := len(c.responses)
	c.mu.Unlock()
	if left != 0 {
		t.Fatal("failed response leaked")
	}
}

func TestResponseTimeoutAndCompletion(t *testing.T) {
	for _, mode := range []string{"finish_first", "failed_finish_first", "timeout_first"} {
		t.Run(mode, func(t *testing.T) {
			frames := make(chan Frame, 8)
			finalStarted := make(chan struct{})
			releaseFinal := make(chan struct{})
			upgrader := websocket.Upgrader{}
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
				for {
					var frame Frame
					if conn.ReadJSON(&frame) != nil {
						return
					}
					frames <- frame
					code := 0
					var body struct {
						Stream struct {
							Content string
							Finish  bool
						}
					}
					_ = json.Unmarshal(frame.Body, &body)
					if mode != "timeout_first" && body.Stream.Finish && body.Stream.Content == "final answer" {
						close(finalStarted)
						<-releaseFinal
						if mode == "failed_finish_first" {
							code = 45009
						}
					}
					_ = conn.WriteJSON(Frame{Headers: frame.Headers, ErrCode: intPtr(code)})
				}
			}))
			defer server.Close()
			c := NewClient(Config{BotID: "bot", Secret: "fake", URL: "ws" + strings.TrimPrefix(server.URL, "http")}, nil)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- c.Run(ctx) }()
			defer func() { c.Close(); <-done }()
			for !c.Status().Connected {
				select {
				case <-ctx.Done():
					t.Fatal("connection timeout")
				case <-time.After(time.Millisecond):
				}
			}
			msg := channel.Message{ReplyToken: "callback", ConversationID: "chat"}
			if err := c.BeginResponse(ctx, msg); err != nil {
				t.Fatal(err)
			}
			<-frames // Initial Processing frame.
			c.mu.Lock()
			state := c.responses[msg.ReplyToken]
			c.mu.Unlock()
			state.mu.Lock()
			state.timer.Stop()
			state.mu.Unlock()
			if mode == "timeout_first" {
				c.timeoutResponse(msg, state)
				timeoutFrame := <-frames
				if !strings.Contains(string(timeoutFrame.Body), "still running") {
					t.Fatalf("timeout frame = %s", timeoutFrame.Body)
				}
				if err := c.SendText(ctx, msg, "later result"); err != nil {
					t.Fatal(err)
				}
				if err := c.FinishResponse(ctx, msg, ""); err != nil {
					t.Fatal(err)
				}
				result := <-frames
				if result.Cmd != "aibot_send_msg" || !strings.Contains(string(result.Body), "later result") {
					t.Fatalf("later result = %+v", result)
				}
				if len(frames) != 0 {
					t.Fatal("unexpected extra reply")
				}
				return
			}
			finished := make(chan error, 1)
			go func() { finished <- c.FinishResponse(ctx, msg, "final answer") }()
			select {
			case <-finalStarted: // FinishResponse now owns state.mu while awaiting its ack.
			case <-ctx.Done():
				t.Fatal("final frame timeout")
			}
			<-frames
			started := make(chan struct{})
			timedOut := make(chan struct{})
			// Start the same timeout callback while final delivery holds
			// state.mu, reproducing a timer that Stop cannot cancel.
			timer := time.AfterFunc(0, func() {
				close(started)
				c.timeoutResponse(msg, state)
				close(timedOut)
			})
			<-started
			if timer.Stop() {
				t.Fatal("timeout callback was not already started")
			}
			close(releaseFinal)
			err := <-finished
			if (err != nil) != (mode == "failed_finish_first") {
				t.Fatalf("finish error = %v", err)
			}
			select {
			case <-timedOut:
			case <-ctx.Done():
				t.Fatal("timeout callback did not finish")
			}
			if len(frames) != 0 {
				t.Fatalf("timeout sent a stale reply after final delivery: %+v", <-frames)
			}
		})
	}
}
