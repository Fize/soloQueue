package qqbot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// ─── Errors ──────────────────────────────────────────────────────────────────

var (
	ErrGatewayClosed = errors.New("qqbot: gateway closed")
	ErrNotConnected  = errors.New("qqbot: not connected")
)

// ─── EventHandler ────────────────────────────────────────────────────────────

// EventHandler receives parsed QQ messages from the gateway.
// Implementations must be safe for concurrent use.
type EventHandler interface {
	OnQQMessage(ctx context.Context, msg QQMessage)
}

// EventHandlerFunc is a convenience adapter for EventHandler.
type EventHandlerFunc func(ctx context.Context, msg QQMessage)

func (f EventHandlerFunc) OnQQMessage(ctx context.Context, msg QQMessage) { f(ctx, msg) }

// ─── Gateway ─────────────────────────────────────────────────────────────────

// TokenProvider is the interface for obtaining access tokens used in
// WebSocket Identify and Resume. *APIClient satisfies this interface.
type TokenProvider interface {
	AccessToken(ctx context.Context) (string, error)
}

// Gateway manages a WebSocket connection to the QQ Bot Gateway.
// It handles connection, authentication, heartbeat, event dispatch,
// and automatic reconnection via Resume.
type Gateway struct {
	cfg Config
	log *logger.Logger

	mu        sync.Mutex
	conn      *websocket.Conn
	sessionID string
	seq       atomic.Int64 // last received sequence number; -1 means unset
	closed    atomic.Bool

	handler EventHandler

	// token provider for WebSocket Identify / Resume
	tokens TokenProvider

	// heartbeat
	heartbeatInterval time.Duration
	heartbeatTicker   *time.Ticker
	heartbeatDone     chan struct{}

	// reconnect
	reconnectCh chan struct{} // signal to trigger reconnect
}

// NewGateway creates a new Gateway instance.
func NewGateway(cfg Config, handler EventHandler, tokens TokenProvider, log *logger.Logger) *Gateway {
	return &Gateway{
		cfg:           cfg,
		log:           log,
		handler:       handler,
		tokens:        tokens,
		reconnectCh:   make(chan struct{}, 1),
	}
}

// Run connects to the gateway and starts the main event loop.
// It blocks until the context is cancelled or an unrecoverable error occurs.
// On disconnection, it automatically reconnects with Resume.
func (g *Gateway) Run(ctx context.Context) error {
	for {
		if g.closed.Load() {
			return ErrGatewayClosed
		}

		err := g.connectAndIdentify(ctx)
		if err != nil {
			if g.closed.Load() {
				return ErrGatewayClosed
			}
			g.log.WarnContext(ctx, logger.CatApp, "qqbot gateway connect failed, retrying in 5s",
				"err", err.Error())
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
				continue
			}
		}

		g.log.InfoContext(ctx, logger.CatApp, "qqbot gateway connected",
			"session_id", g.sessionID)

		// Start heartbeat
		g.startHeartbeat(ctx)

		// Run event loop
		loopErr := g.eventLoop(ctx)

		// Stop heartbeat
		g.stopHeartbeat()

		// Close WebSocket
		g.closeConn()

		if g.closed.Load() {
			return ErrGatewayClosed
		}

		if loopErr != nil {
			g.log.WarnContext(ctx, logger.CatApp, "qqbot gateway event loop ended",
				"err", loopErr.Error())
		}

		// Wait before reconnecting (unless reconnect signal received)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-g.reconnectCh:
			// immediate reconnect (e.g., OpCode 7 Reconnect)
		case <-time.After(5 * time.Second):
			// delayed reconnect
		}
	}
}

// Close gracefully shuts down the gateway.
func (g *Gateway) Close() {
	if g.closed.CompareAndSwap(false, true) {
		g.stopHeartbeat()
		g.closeConn()
	}
}

// ─── Connection ──────────────────────────────────────────────────────────────

func (g *Gateway) connectAndIdentify(ctx context.Context) error {
	url := g.cfg.GatewayURL()

	g.log.DebugContext(ctx, logger.CatApp, "qqbot connecting to gateway", "url", url)

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, url, nil)
	if err != nil {
		return fmt.Errorf("dial gateway: %w", err)
	}

	g.mu.Lock()
	g.conn = conn
	g.mu.Unlock()

	// Step 1: Read Hello (OpCode 10)
	var helloPayload GatewayPayload
	if err := g.readJSON(ctx, &helloPayload); err != nil {
		return fmt.Errorf("read hello: %w", err)
	}
	if helloPayload.Op != OpHello {
		return fmt.Errorf("expected OpCode 10 Hello, got OpCode %d", helloPayload.Op)
	}

	var helloData HelloData
	if err := json.Unmarshal(helloPayload.D, &helloData); err != nil {
		return fmt.Errorf("parse hello data: %w", err)
	}

	g.heartbeatInterval = time.Duration(helloData.HeartbeatInterval) * time.Millisecond
	g.log.DebugContext(ctx, logger.CatApp, "qqbot hello received",
		"heartbeat_interval_ms", helloData.HeartbeatInterval)

	// Step 2: Obtain access token for Identify / Resume.
	accessToken, err := g.tokens.AccessToken(ctx)
	if err != nil {
		return fmt.Errorf("get access token: %w", err)
	}

	// Step 3: Identify (OpCode 2) or Resume (OpCode 6)
	if g.sessionID != "" && g.seq.Load() > 0 {
		// Resume
		seq := int(g.seq.Load())
		resumeData := ResumeData{
			Token:     accessToken,
			SessionID: g.sessionID,
			Seq:       seq,
		}
		if err := g.sendOp(ctx, OpResume, resumeData); err != nil {
			return fmt.Errorf("send resume: %w", err)
		}
		g.log.InfoContext(ctx, logger.CatApp, "qqbot resume sent",
			"session_id", g.sessionID, "seq", seq)
	} else {
		// Identify
		identifyData := IdentifyData{
			Token:    "QQBot " + accessToken,
			Intents:  g.cfg.EffectiveIntents(),
			Shard:    [2]int{0, 1},
			Properties: map[string]string{
				"$os":       "linux",
				"$browser":  "soloqueue",
				"$device":   "soloqueue",
			},
		}
		if err := g.sendOp(ctx, OpIdentify, identifyData); err != nil {
			return fmt.Errorf("send identify: %w", err)
		}
		g.log.DebugContext(ctx, logger.CatApp, "qqbot identify sent",
				"token_prefix", accessToken[:min(20, len(accessToken))])
	}

	return nil
}

// ─── Event Loop ───────────────────────────────────────────────────────────────

func (g *Gateway) eventLoop(ctx context.Context) error {
	for {
		if g.closed.Load() {
			return ErrGatewayClosed
		}

		var payload GatewayPayload
		if err := g.readJSON(ctx, &payload); err != nil {
			return fmt.Errorf("read message: %w", err)
		}

		// Update sequence number
		if payload.S != nil {
			g.seq.Store(int64(*payload.S))
		}

		switch payload.Op {
		case OpDispatch:
			g.handleDispatch(ctx, payload)
		case OpHeartbeatACK:
			g.log.DebugContext(ctx, logger.CatApp, "qqbot heartbeat ack received")
		case OpReconnect:
			g.log.InfoContext(ctx, logger.CatApp, "qqbot reconnect requested by server")
			// Trigger immediate reconnect
			select {
			case g.reconnectCh <- struct{}{}:
			default:
			}
			return nil
		case OpHello:
			// Server sent a new Hello (shouldn't happen in normal flow, but handle gracefully)
			var helloData HelloData
			if err := json.Unmarshal(payload.D, &helloData); err == nil {
				g.heartbeatInterval = time.Duration(helloData.HeartbeatInterval) * time.Millisecond
			}
		case OpInvalidSession:
			g.log.WarnContext(ctx, logger.CatApp, "qqbot invalid session, performing fresh identify")
			g.sessionID = ""
			g.seq.Store(-1)
			return nil
		default:
			g.log.DebugContext(ctx, logger.CatApp, "qqbot unknown opcode received",
				"op", payload.Op)
		}
	}
}

// ─── Dispatch Handler ─────────────────────────────────────────────────────────

func (g *Gateway) handleDispatch(ctx context.Context, payload GatewayPayload) {
	switch payload.T {
	case EventReady:
		var readyData ReadyData
		if err := json.Unmarshal(payload.D, &readyData); err == nil {
			g.sessionID = readyData.SessionID
			g.log.InfoContext(ctx, logger.CatApp, "qqbot ready event received",
				"session_id", readyData.SessionID,
				"bot_id", readyData.User.ID)
		}
	case EventResumed:
		g.log.InfoContext(ctx, logger.CatApp, "qqbot resumed event received",
			"session_id", g.sessionID)
	case EventC2CMessageCreate:
		g.handleC2CMessage(ctx, payload.D)
	case EventGroupAtMessageCreate:
		g.handleGroupAtMessage(ctx, payload.D)
	case EventDirectMessageCreate:
		g.handleDirectMessage(ctx, payload.D)
	case EventPublicAtMessageCreate:
		g.handlePublicAtMessage(ctx, payload.D)
	default:
		g.log.DebugContext(ctx, logger.CatApp, "qqbot unhandled dispatch event",
			"type", payload.T)
	}
}

func (g *Gateway) handleC2CMessage(ctx context.Context, raw json.RawMessage) {
	var event C2CMessageEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		g.log.WarnContext(ctx, logger.CatApp, "qqbot failed to parse C2C message event",
			"err", err.Error())
		return
	}
	msg := QQMessage{
		Source:       SourceC2C,
		Content:      event.Content,
		OpenID:       event.Author.UserOpenid,
		TargetOpenID: event.Author.UserOpenid,
		ChatID:       event.Author.UserOpenid,
		EventID:      event.ID,
		Seq:          event.SeqInChat,
	}
	g.handler.OnQQMessage(ctx, msg)
}

func (g *Gateway) handleGroupAtMessage(ctx context.Context, raw json.RawMessage) {
	var event GroupAtMessageEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		g.log.WarnContext(ctx, logger.CatApp, "qqbot failed to parse group at message event",
			"err", err.Error())
		return
	}
	msg := QQMessage{
		Source:       SourceGroup,
		Content:      event.Content,
		OpenID:       event.Author.MemberOpenid,
		TargetOpenID: event.GroupOpenid,
		ChatID:       event.GroupOpenid,
		EventID:      event.ID,
		Seq:          event.SeqInChat,
	}
	g.handler.OnQQMessage(ctx, msg)
}

func (g *Gateway) handleDirectMessage(ctx context.Context, raw json.RawMessage) {
	var event DirectMessageEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		g.log.WarnContext(ctx, logger.CatApp, "qqbot failed to parse direct message event",
			"err", err.Error())
		return
	}
	msg := QQMessage{
		Source:       SourceDirect,
		Content:      event.Content,
		OpenID:       event.Author.UserOpenid,
		TargetOpenID: event.Author.UserOpenid,
		ChatID:       event.ChannelID,
		EventID:      event.ID,
		Seq:          event.SeqInChat,
	}
	g.handler.OnQQMessage(ctx, msg)
}

func (g *Gateway) handlePublicAtMessage(ctx context.Context, raw json.RawMessage) {
	var event PublicAtMessageEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		g.log.WarnContext(ctx, logger.CatApp, "qqbot failed to parse public at message event",
			"err", err.Error())
		return
	}
	msg := QQMessage{
		Source:       SourcePublicGuild,
		Content:      event.Content,
		OpenID:       event.Author.MemberOpenid,
		TargetOpenID: event.ChannelID,
		ChatID:       event.ChannelID,
		EventID:      event.ID,
		Seq:          event.SeqInChat,
	}
	g.handler.OnQQMessage(ctx, msg)
}

// ─── Heartbeat ────────────────────────────────────────────────────────────────

func (g *Gateway) startHeartbeat(ctx context.Context) {
	g.stopHeartbeat() // ensure no previous ticker running

	g.heartbeatTicker = time.NewTicker(g.heartbeatInterval)
	g.heartbeatDone = make(chan struct{})

	go func() {

		// First heartbeat uses null
		if err := g.sendHeartbeat(ctx, nil); err != nil {
			g.log.WarnContext(ctx, logger.CatApp, "qqbot first heartbeat failed",
				"err", err.Error())
		}

		for {
			select {
			case <-g.heartbeatTicker.C:
				seq := int(g.seq.Load())
				if seq <= 0 {
					_ = g.sendHeartbeat(ctx, nil)
				} else {
					_ = g.sendHeartbeat(ctx, &seq)
				}
			case <-g.heartbeatDone:
				return
			}
		}
	}()
}

func (g *Gateway) stopHeartbeat() {
	if g.heartbeatTicker != nil {
		g.heartbeatTicker.Stop()
		g.heartbeatTicker = nil
	}
	if g.heartbeatDone != nil {
		select {
		case <-g.heartbeatDone:
		default:
			close(g.heartbeatDone)
		}
		g.heartbeatDone = nil
	}
}

func (g *Gateway) sendHeartbeat(ctx context.Context, seq *int) error {
	return g.sendOp(ctx, OpHeartbeat, seq)
}

// ─── WebSocket I/O ───────────────────────────────────────────────────────────

func (g *Gateway) readJSON(ctx context.Context, v any) error {
	g.mu.Lock()
	conn := g.conn
	g.mu.Unlock()

	if conn == nil {
		return ErrNotConnected
	}

	// Set read deadline based on heartbeat interval + buffer
	deadline := time.Now().Add(g.heartbeatInterval * 2)
	if g.heartbeatInterval == 0 {
		deadline = time.Now().Add(60 * time.Second)
	}
	_ = conn.SetReadDeadline(deadline)

	err := conn.ReadJSON(v)
	if err != nil {
		return fmt.Errorf("read json: %w", err)
	}
	return nil
}

func (g *Gateway) sendOp(ctx context.Context, op int, data any) error {
	g.mu.Lock()
	conn := g.conn
	g.mu.Unlock()

	if conn == nil {
		return ErrNotConnected
	}

	payload := GatewayPayload{Op: op}
	if data != nil {
		raw, err := json.Marshal(data)
		if err != nil {
			return fmt.Errorf("marshal payload data: %w", err)
		}
		payload.D = raw
	}

	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := conn.WriteJSON(payload); err != nil {
		return fmt.Errorf("write json: %w", err)
	}
	return nil
}

func (g *Gateway) closeConn() {
	g.mu.Lock()
	conn := g.conn
	g.conn = nil
	g.mu.Unlock()

	if conn != nil {
		// Send close frame
		_ = conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		_ = conn.Close()
	}
}
