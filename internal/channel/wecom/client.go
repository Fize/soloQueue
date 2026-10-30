// Package wecom adapts the official AI bot long connection to the shared channel
// contracts so sessions and commands retain the same behavior as other bots.
package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/xiaobaitu/soloqueue/internal/channel"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
)

const DefaultURL = "wss://openws.work.weixin.qq.com"
const maxTextBytes = 20480

var ErrClosed = errors.New("wecom connection closed")
var errReplaced = errors.New("wecom connection replaced by another instance")
var errAuth = errors.New("wecom authentication failed; check Bot ID and Secret")

type Config struct {
	AccountID, BotID, Secret string
	URL                      string // Overrides are used by local protocol tests, never accepted by the config API.
	Heartbeat                time.Duration
	WhitelistEnabled         bool
	Whitelist                []string
}
type Status struct {
	State     string `json:"state"`
	Connected bool   `json:"connected"`
	Error     string `json:"error,omitempty"`
}
type Headers struct {
	ReqID string `json:"req_id"`
}
type Frame struct {
	Cmd     string          `json:"cmd,omitempty"`
	Headers Headers         `json:"headers"`
	Body    json.RawMessage `json:"body,omitempty"`
	ErrCode *int            `json:"errcode,omitempty"`
}
type incoming struct {
	MsgID    string `json:"msgid"`
	BotID    string `json:"aibotid"`
	ChatID   string `json:"chatid"`
	ChatType string `json:"chattype"`
	From     struct {
		UserID string `json:"userid"`
	} `json:"from"`
	MsgType string `json:"msgtype"`
	Text    struct {
		Content string `json:"content"`
	} `json:"text"`
	Image mediaImage `json:"image"`
	Voice struct {
		Content string `json:"content"`
	} `json:"voice"`
	Mixed struct {
		Items []mixedItem `json:"msg_item"`
	} `json:"mixed"`
	Event struct {
		Type string `json:"eventtype"`
	} `json:"event"`
}
type connectionKey struct{}
type downloadContextKey struct{}

type response struct {
	mu                sync.Mutex
	streamID, content string
	conn              *websocket.Conn
	failed, async     bool
	terminal          bool
	timer             *time.Timer
}
type Client struct {
	cfg       Config
	log       *logger.Logger
	handler   channel.Handler
	mu        sync.Mutex
	conn      *websocket.Conn
	status    Status
	pending   map[string]chan Frame
	responses map[string]*response
	seen      map[string]time.Time
	slots     chan struct{}
	writeMu   sync.Mutex
	cancel    context.CancelFunc
	closed    bool
}

func NewClient(cfg Config, log *logger.Logger) *Client {
	if cfg.URL == "" {
		cfg.URL = DefaultURL
	}
	if cfg.Heartbeat <= 0 {
		cfg.Heartbeat = 30 * time.Second
	}
	return &Client{cfg: cfg, log: log, status: Status{State: "disconnected"}, pending: map[string]chan Frame{}, responses: map[string]*response{}, seen: map[string]time.Time{}, slots: make(chan struct{}, 16)}
}
func (c *Client) SetHandler(h channel.Handler) { c.handler = h }
func (c *Client) Status() Status               { c.mu.Lock(); defer c.mu.Unlock(); return c.status }
func (c *Client) setStatus(state, reason string) {
	c.mu.Lock()
	c.status = Status{State: state, Connected: state == "connected", Error: reason}
	c.mu.Unlock()
}
func (c *Client) Close() {
	c.mu.Lock()
	c.closed = true
	c.status = Status{State: "disconnected"}
	if c.cancel != nil {
		c.cancel()
	}
	if c.conn != nil {
		_ = c.conn.Close()
	}
	states := c.responses
	c.responses = map[string]*response{}
	c.mu.Unlock()
	for _, state := range states {
		state.mu.Lock()
		if state.timer != nil {
			state.timer.Stop()
		}
		state.failed = true
		state.mu.Unlock()
	}
}

func (c *Client) Run(parent context.Context) error {
	ctx, cancel := context.WithCancel(parent)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		cancel()
		return ErrClosed
	}
	c.cancel = cancel
	c.mu.Unlock()
	defer cancel()
	delay := time.Second
	for {
		if ctx.Err() != nil {
			c.setStatus("disconnected", "")
			return ErrClosed
		}
		c.setStatus("connecting", "")
		err := c.connect(ctx)
		if errors.Is(err, errAuth) {
			c.setStatus("auth_failed", errAuth.Error())
			return err
		}
		if errors.Is(err, errReplaced) {
			c.setStatus("conflict", errReplaced.Error())
			return err
		}
		if ctx.Err() != nil {
			c.setStatus("disconnected", "")
			return ErrClosed
		}
		c.setStatus("reconnecting", "Connection interrupted; reconnecting")
		if c.log != nil {
			c.log.Warn(logger.CatApp, "wecom connection interrupted", "account_id", c.cfg.AccountID)
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			c.setStatus("disconnected", "")
			return ErrClosed
		case <-timer.C:
		}
		if delay < 30*time.Second {
			delay *= 2
			if delay > 30*time.Second {
				delay = 30 * time.Second
			}
		}
	}
}
func (c *Client) connect(ctx context.Context) error {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, c.cfg.URL, nil)
	if err != nil {
		return errors.New("wecom connection failed")
	}
	conn.SetReadLimit(1 << 20)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		conn.Close()
		return ErrClosed
	}
	c.conn = conn
	c.mu.Unlock()
	defer func() {
		conn.Close()
		c.mu.Lock()
		if c.conn == conn {
			c.conn = nil
		}
		c.mu.Unlock()
	}()
	connectionCtx, stop := context.WithCancel(ctx)
	defer stop()
	go func() { <-connectionCtx.Done(); _ = conn.Close() }()
	reqID := "aibot_subscribe_" + uuid.NewString()
	body, _ := json.Marshal(map[string]string{"bot_id": c.cfg.BotID, "secret": c.cfg.Secret})
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	if err := c.write(conn, Frame{Cmd: "aibot_subscribe", Headers: Headers{reqID}, Body: body}); err != nil {
		return err
	}
	var ack Frame
	if conn.ReadJSON(&ack) != nil {
		return errors.New("wecom authentication response unavailable")
	}
	if ack.Headers.ReqID != reqID || ack.ErrCode == nil || *ack.ErrCode != 0 {
		return errAuth
	}
	_ = conn.SetReadDeadline(time.Time{})
	c.setStatus("connected", "")
	go func() {
		ticker := time.NewTicker(c.cfg.Heartbeat)
		defer ticker.Stop()
		for {
			select {
			case <-connectionCtx.Done():
				return
			case <-ticker.C:
				if err := c.request(connectionCtx, conn, "ping", "ping_"+uuid.NewString(), nil); err != nil {
					_ = conn.Close()
					return
				}
			}
		}
	}()
	for {
		var frame Frame
		if err := conn.ReadJSON(&frame); err != nil {
			return errors.New("wecom connection interrupted")
		}
		if frame.Cmd == "aibot_event_callback" {
			var body incoming
			_ = json.Unmarshal(frame.Body, &body)
			if body.Event.Type == "disconnected_event" {
				return errReplaced
			}
			continue
		}
		if frame.Cmd == "aibot_msg_callback" {
			c.dispatch(context.WithValue(ctx, downloadContextKey{}, connectionCtx), conn, frame)
			continue
		}
		c.mu.Lock()
		pending := c.pending[frame.Headers.ReqID]
		c.mu.Unlock()
		if pending != nil {
			select {
			case pending <- frame:
			default:
			}
		}
	}
}
func (c *Client) write(conn *websocket.Conn, frame Frame) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if err := conn.WriteJSON(frame); err != nil {
		return errors.New("wecom send failed; delivery is unknown")
	}
	return nil
}
func (c *Client) request(ctx context.Context, conn *websocket.Conn, cmd, reqID string, body any) error {
	_, err := c.requestFrame(ctx, conn, cmd, reqID, body)
	return err
}
func (c *Client) requestFrame(ctx context.Context, conn *websocket.Conn, cmd, reqID string, body any) (Frame, error) {
	if err := ctx.Err(); err != nil {
		return Frame{}, err
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return Frame{}, err
	}
	pending := make(chan Frame, 1)
	c.mu.Lock()
	if conn == nil || c.conn != conn || !c.status.Connected {
		c.mu.Unlock()
		return Frame{}, ErrClosed
	}
	if c.pending[reqID] != nil {
		c.mu.Unlock()
		return Frame{}, errors.New("wecom reply already awaiting acknowledgement")
	}
	c.pending[reqID] = pending
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, reqID); c.mu.Unlock() }()
	if err := c.write(conn, Frame{Cmd: cmd, Headers: Headers{reqID}, Body: payload}); err != nil {
		return Frame{}, err
	}
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case ack := <-pending:
		if ack.ErrCode == nil {
			return Frame{}, errors.New("wecom invalid acknowledgement")
		}
		if *ack.ErrCode != 0 {
			return Frame{}, fmt.Errorf("wecom request rejected (code %d)", *ack.ErrCode)
		}
		return ack, nil
	case <-ctx.Done():
		return Frame{}, fmt.Errorf("wecom send interrupted; delivery is unknown: %w", ctx.Err())
	case <-timer.C:
		return Frame{}, errors.New("wecom acknowledgement timeout; delivery is unknown")
	}
}
func (c *Client) allowed(user string) bool {
	if !c.cfg.WhitelistEnabled {
		return true
	}
	for _, id := range c.cfg.Whitelist {
		if id == user {
			return true
		}
	}
	return false
}
func (c *Client) dispatch(ctx context.Context, conn *websocket.Conn, frame Frame) {
	var in incoming
	if json.Unmarshal(frame.Body, &in) != nil || in.MsgID == "" || frame.Headers.ReqID == "" || in.From.UserID == "" || in.BotID != c.cfg.BotID {
		return
	}
	if in.ChatType != "single" && in.ChatType != "group" {
		return
	}
	if !c.allowed(in.From.UserID) {
		return
	}
	destination := in.From.UserID
	if in.ChatType == "group" {
		destination = in.ChatID
		if destination == "" {
			return
		}
	}
	c.mu.Lock()
	now := time.Now()
	for id, seen := range c.seen {
		if now.Sub(seen) > 10*time.Minute {
			delete(c.seen, id)
		}
	}
	if _, ok := c.seen[in.MsgID]; ok {
		c.mu.Unlock()
		return
	}
	if len(c.seen) >= 4096 {
		c.mu.Unlock()
		return
	}
	c.seen[in.MsgID] = now
	c.mu.Unlock()
	msg := channel.Message{Channel: "wecom", AccountID: c.cfg.AccountID, AccountIdentity: c.cfg.BotID, MessageID: in.MsgID, ConversationID: destination, UserID: in.From.UserID, Text: in.Text.Content, ReplyToken: frame.Headers.ReqID}
	select {
	case c.slots <- struct{}{}:
	default:
		// Bound ingress while leaving the socket reader free to receive heartbeats.
		if c.log != nil {
			c.log.Warn(logger.CatApp, "wecom request dropped: busy", "account_id", c.cfg.AccountID)
		}
		return
	}
	go func() {
		defer func() { <-c.slots }()
		ctx = context.WithValue(ctx, connectionKey{}, conn)
		ctx = channel.ContextWithChatMeta(ctx, channel.ChatMeta{Channel: "wecom", AccountID: c.cfg.AccountID, ConversationID: destination, UserID: in.From.UserID})
		if in.MsgType != "text" && in.MsgType != "image" && in.MsgType != "mixed" && in.MsgType != "voice" {
			if c.BeginResponse(ctx, msg) == nil {
				_ = c.SendText(ctx, msg, "This message type is not supported. Send text, images, or voice.")
				_ = c.FinishResponse(ctx, msg, "")
			}
			return
		}
		normalized, err := c.normalize(ctx, msg, in)
		if err != nil {
			if c.BeginResponse(ctx, msg) == nil {
				_ = c.SendText(ctx, msg, "Unable to read message media. Please resend it.")
				_ = c.FinishResponse(ctx, msg, "")
			}
			return
		}
		msg = normalized

		if c.handler != nil {
			c.handler.OnMessage(ctx, msg)
		}
	}()
}
func (c *Client) BeginResponse(ctx context.Context, msg channel.Message) error {
	if msg.ReplyToken == "" {
		return errors.New("wecom reply context missing")
	}
	c.mu.Lock()
	original, _ := ctx.Value(connectionKey{}).(*websocket.Conn)
	if original != nil && original != c.conn {
		c.mu.Unlock()
		return ErrClosed
	}
	state := &response{streamID: uuid.NewString(), conn: c.conn}
	c.responses[msg.ReplyToken] = state
	c.mu.Unlock()
	state.mu.Lock()
	defer state.mu.Unlock()
	if err := c.stream(ctx, msg, state, "Processing…", false); err != nil {
		state.failed = true
		c.mu.Lock()
		delete(c.responses, msg.ReplyToken)
		c.mu.Unlock()
		return err
	}
	state.timer = time.AfterFunc(5*time.Minute, func() { c.timeoutResponse(msg, state) })
	return nil
}

func (c *Client) timeoutResponse(msg channel.Message, state *response) {
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.failed || state.async || state.terminal {
		return
	}
	timeout, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := c.stream(timeout, msg, state, "The task is still running. Its result will be sent separately.", true)
	state.async = true
	if err != nil {
		state.failed = true
	}
}

func (c *Client) SendText(ctx context.Context, msg channel.Message, text string) error {
	c.mu.Lock()
	state := c.responses[msg.ReplyToken]
	c.mu.Unlock()
	if state == nil {
		return c.SendNotification(ctx, msg, text)
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.failed {
		return errors.New("wecom reply unavailable after send failure")
	}
	if len(state.content) > 0 {
		state.content += "\n\n"
	}
	state.content += text
	// Memory and payload limits are independent of model output size.
	if len(state.content) > 1<<20 {
		state.content = clip(state.content, 1<<20)
	}
	if state.async {
		return nil
	}
	if err := c.stream(ctx, msg, state, clip(state.content, maxTextBytes), false); err != nil {
		state.failed = true
		return err
	}
	return nil
}
func (c *Client) FinishResponse(ctx context.Context, msg channel.Message, fallback string) error {
	c.mu.Lock()
	state := c.responses[msg.ReplyToken]
	delete(c.responses, msg.ReplyToken)
	c.mu.Unlock()
	if state == nil {
		return nil
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	// Stop cannot cancel a callback already waiting for this mutex. Establish
	// terminal state before delivery so that callback cannot send a stale reply.
	state.terminal = true
	if state.timer != nil {
		state.timer.Stop()
	}
	if state.failed {
		return errors.New("wecom reply unavailable after send failure")
	}
	content := state.content
	if content == "" {
		content = fallback
	}
	if content == "" {
		content = "Done."
	}
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if state.async {
		return c.SendNotification(finishCtx, msg, content)
	}
	first := clip(content, maxTextBytes)
	if err := c.stream(finishCtx, msg, state, first, true); err != nil {
		return err
	}
	if len(content) > len(first) {
		return c.SendNotification(finishCtx, msg, content[len(first):])
	}
	return nil
}
func (c *Client) stream(ctx context.Context, msg channel.Message, state *response, text string, finish bool) error {
	return c.request(ctx, state.conn, "aibot_respond_msg", msg.ReplyToken, map[string]any{"msgtype": "stream", "stream": map[string]any{"id": state.streamID, "content": text, "finish": finish}})
}
func (c *Client) SendNotification(ctx context.Context, msg channel.Message, text string) error {
	if msg.ConversationID == "" {
		return errors.New("wecom notification destination missing")
	}
	for text != "" {
		part := clip(text, maxTextBytes)
		c.mu.Lock()
		conn := c.conn
		c.mu.Unlock()
		if err := c.request(ctx, conn, "aibot_send_msg", "aibot_send_msg_"+uuid.NewString(), map[string]any{"chatid": msg.ConversationID, "msgtype": "markdown", "markdown": map[string]string{"content": part}}); err != nil {
			return err
		}
		text = text[len(part):]
	}
	return nil
}
func clip(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	for limit > 0 && !utf8.RuneStart(text[limit]) {
		limit--
	}
	return text[:limit]
}
