// Package server 暴露 SoloQueue 的 REST + WebSocket 接口
//
// 路由：
//
//	GET    /v1/session/history → {"messages":[...]}
//	GET    /v1/session/stream  → WebSocket upgrade
//	GET    /healthz             → {"status":"ok"}
//
// WebSocket 协议（JSON per frame）：
//
//	Client → Server:  {"type":"ask","prompt":"..."} | {"type":"cancel"} | {"type":"ping"} | {"type":"clear"}
//	Server → Client:  (1:1 映射 agent.AgentEvent) content_delta / reasoning_delta /
//	                  tool_call_delta / tool_exec_start / tool_exec_done /
//	                  iteration_done / done / error
//
// 连接粒度：一 WS 连接 = 一次"针对该 Session 的流式订阅"。
// Cancel 粒度：client 发 cancel → server 取消"当前 Ask 的派生 ctx"；
// Session 本身不会因此停止，下轮 Ask 仍可继续。
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/router"
	"github.com/xiaobaitu/soloqueue/internal/session"
)

// ─── Mux ──────────────────────────────────────────────────────────────────

// Mux 是根路由
type Mux struct {
	sess   *session.Session
	router *router.Router
	log    *logger.Logger
	mux    *http.ServeMux

	// wsOpts 传给 websocket.Accept；测试可覆盖以跳过 origin 检查
	wsOpts *websocket.AcceptOptions
}

// NewMux 构造路由
func NewMux(sess *session.Session, rtr *router.Router, log *logger.Logger) *Mux {
	m := &Mux{
		sess:   sess,
		router: rtr,
		log:    log,
		mux:    http.NewServeMux(),
		wsOpts: &websocket.AcceptOptions{
			// 本地开发默认 InsecureSkipVerify（缺省 WS 规范要求 Origin）
			// 生产中应由调用方（main.go）根据 settings 覆写
			InsecureSkipVerify: true,
		},
	}

	m.mux.HandleFunc("GET /healthz", m.handleHealth)

	m.mux.HandleFunc("GET /v1/session/history", m.handleHistory)
	m.mux.HandleFunc("GET /v1/session/stream", m.handleStream)

	return m
}

// ServeHTTP 实现 http.Handler
func (m *Mux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			m.logError(r.Context(), "panic in handler",
				fmt.Errorf("%v", rec),
				slog.String("path", r.URL.Path),
			)
			m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal", "path": r.URL.Path})
		}
	}()
	m.mux.ServeHTTP(w, r)
}

// ─── Handlers ─────────────────────────────────────────────────────────────

func (m *Mux) handleHealth(w http.ResponseWriter, _ *http.Request) {
	m.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type historyResp struct {
	Messages []historyMsg `json:"messages"`
}
type historyMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (m *Mux) handleHistory(w http.ResponseWriter, r *http.Request) {
	h := m.sess.History()
	msgs := make([]historyMsg, 0, len(h))
	for _, msg := range h {
		msgs = append(msgs, historyMsg{Role: msg.Role, Content: msg.Content})
	}
	m.writeJSON(w, http.StatusOK, historyResp{Messages: msgs})
}

// ─── WebSocket ────────────────────────────────────────────────────────────

type wsInFrame struct {
	Type   string `json:"type"`
	Prompt string `json:"prompt,omitempty"`
	CallID string `json:"call_id,omitempty"`
	Choice string `json:"choice,omitempty"` // 用户选择的选项值；"yes" 表示确认，"" 表示拒绝
}

const (
	frameAsk     = "ask"
	frameCancel  = "cancel"
	framePing    = "ping"
	frameConfirm = "confirm"
	frameClear   = "clear"
)

func (m *Mux) handleStream(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, m.wsOpts)
	if err != nil {
		m.logWSError(r.Context(), "ws accept failed", err,
			slog.String("session_id", m.sess.ID),
		)
		return
	}
	defer c.CloseNow()

	connCtx, cancel := context.WithCancel(r.Context())
	defer cancel()

	m.logWS(r.Context(), "ws connected", slog.String("session_id", m.sess.ID))
	defer m.logWS(r.Context(), "ws disconnected", slog.String("session_id", m.sess.ID))

	// active ask cancel (protected by askMu)
	var (
		askCancel context.CancelFunc
	)

	for {
		_, data, err := c.Read(connCtx)
		if err != nil {
			m.logWS(connCtx, "ws read closed", slog.String("err", err.Error()))
			return
		}
		var in wsInFrame
		if err := json.Unmarshal(data, &in); err != nil {
			m.logWS(connCtx, "ws invalid json frame",
				slog.String("raw", truncateBytes(data, 200)),
				slog.String("err", err.Error()),
			)
			if wsErr := writeWSFrame(connCtx, c, map[string]string{
				"type":  "error",
				"error": "invalid json",
			}); wsErr != nil {
				m.logWSError(connCtx, "ws write error", wsErr)
			}
			continue
		}

		switch in.Type {
		case framePing:
			if err := writeWSFrame(connCtx, c, map[string]string{"type": "pong"}); err != nil {
				m.logWSError(connCtx, "ws write pong error", err)
			}

		case frameCancel:
			if askCancel != nil {
				m.logWS(connCtx, "ws ask cancelled by client")
				askCancel()
				askCancel = nil
				if err := writeWSFrame(connCtx, c, map[string]string{"type": "error", "err": "cancelled"}); err != nil {
					m.logWSError(connCtx, "ws write cancel error", err)
				}
			}

		case frameAsk:
			// stop any in-flight ask
			if askCancel != nil {
				m.logWS(connCtx, "ws ask preempted by new ask")
				askCancel()
				askCancel = nil
			}
			askCtx, ac := context.WithCancel(connCtx)
			askCancel = ac

			// Perform task routing classification (informational logging + routing_info frame)
			// Note: The actual model override is now applied inside Session.AskStream()
			if m.router != nil {
				routingDecision, routeErr := m.router.Route(askCtx, in.Prompt)
				if routeErr != nil {
					m.logInfo(connCtx, "routing classification failed",
						slog.String("session_id", m.sess.ID),
						slog.String("reason", routeErr.Error()),
					)
				} else {
					m.logInfo(connCtx, "routing decision",
						slog.String("session_id", m.sess.ID),
						slog.String("level", routingDecision.Level.String()),
						slog.String("model", routingDecision.ModelID),
						slog.Int("confidence", routingDecision.Classification.Confidence),
					)

					// Send routing info frame to client (informational, for UI display)
					_ = writeWSFrame(connCtx, c, map[string]any{
						"type":       "routing_info",
						"level":      routingDecision.Level.String(),
						"model":      routingDecision.ModelID,
						"confidence": routingDecision.Classification.Confidence,
						"reason":     routingDecision.Classification.Reason,
					})
				}
			}

			events, serr := m.sess.AskStream(askCtx, in.Prompt)
			if serr != nil {
				if err := writeWSFrame(connCtx, c, map[string]string{
					"type":  "error",
					"error": serr.Error(),
				}); err != nil {
					m.logWSError(connCtx, "ws write ask error", err)
				}
				ac()
				askCancel = nil
				continue
			}

			// 在独立 goroutine 中转发事件，主循环继续读消息（处理 confirm / cancel）
			go func() {
				defer ac()
				m.forwardEvents(connCtx, c, events)
			}()

		case frameConfirm:
			if err := m.sess.Agent.Confirm(in.CallID, in.Choice); err != nil {
				m.logError(connCtx, "confirm failed", err,
					slog.String("session_id", m.sess.ID),
					slog.String("call_id", in.CallID),
					slog.String("choice", in.Choice),
				)
			}

		case frameClear:
			if err := m.sess.Clear(); err != nil {
				m.logError(connCtx, "clear failed", err,
					slog.String("session_id", m.sess.ID),
				)
			}
			if err := writeWSFrame(connCtx, c, map[string]string{"type": "cleared"}); err != nil {
				m.logWSError(connCtx, "ws write clear error", err)
			}

		default:
			if err := writeWSFrame(connCtx, c, map[string]string{
				"type":  "error",
				"error": "unknown frame type",
			}); err != nil {
				m.logWSError(connCtx, "ws write error", err)
			}
		}
	}
}

// forwardEvents drains the event channel and writes each event as a JSON frame.
//
// Does not return until the channel closes.
func (m *Mux) forwardEvents(ctx context.Context, c *websocket.Conn, events <-chan agent.AgentEvent) {
	for ev := range events {
		frame := agentEventToFrame(ev)
		if frame["type"] == "unknown" {
			if m.log != nil {
				m.log.WarnContext(ctx, logger.CatWS, "ws unknown agent event type",
					slog.String("event_type", fmt.Sprintf("%T", ev)))
			}
		}
		if err := writeWSFrame(ctx, c, frame); err != nil {
			m.logWS(ctx, "ws event write failed, draining", slog.String("err", err.Error()))
			// best-effort: if write fails (client gone), drain remaining events
			// so Session goroutine can finish
			for range events {
			}
			return
		}
	}
}

// agentEventToFrame maps an AgentEvent to a JSON-serializable frame with a
// discriminator "type" field (see package doc).
func agentEventToFrame(ev agent.AgentEvent) map[string]any {
	switch e := ev.(type) {
	case agent.ContentDeltaEvent:
		return map[string]any{"type": "content_delta", "iter": e.Iter, "delta": e.Delta}
	case agent.ReasoningDeltaEvent:
		return map[string]any{"type": "reasoning_delta", "iter": e.Iter, "delta": e.Delta}
	case agent.ToolCallDeltaEvent:
		return map[string]any{
			"type": "tool_call_delta", "iter": e.Iter,
			"call_id": e.CallID, "name": e.Name, "args_delta": e.ArgsDelta,
		}
	case agent.ToolExecStartEvent:
		return map[string]any{
			"type": "tool_exec_start", "iter": e.Iter,
			"call_id": e.CallID, "name": e.Name, "args": e.Args,
		}
	case agent.ToolNeedsConfirmEvent:
		return map[string]any{
			"type": "tool_needs_confirm", "iter": e.Iter,
			"call_id": e.CallID, "name": e.Name, "args": e.Args, "prompt": e.Prompt,
			"allow_in_session": e.AllowInSession,
		}
	case agent.ToolExecDoneEvent:
		errStr := ""
		if e.Err != nil {
			errStr = e.Err.Error()
		}
		return map[string]any{
			"type": "tool_exec_done", "iter": e.Iter,
			"call_id":     e.CallID,
			"name":        e.Name,
			"result":      e.Result,
			"err":         errStr,
			"duration_ms": e.Duration.Milliseconds(),
		}
	case agent.IterationDoneEvent:
		return map[string]any{
			"type": "iteration_done", "iter": e.Iter,
			"finish_reason": string(e.FinishReason),
			"usage": map[string]any{
				"prompt_tokens":          e.Usage.PromptTokens,
				"completion_tokens":      e.Usage.CompletionTokens,
				"total_tokens":           e.Usage.TotalTokens,
				"prompt_cache_hit_tokens":  e.Usage.PromptCacheHitTokens,
				"prompt_cache_miss_tokens": e.Usage.PromptCacheMissTokens,
				"reasoning_tokens":        e.Usage.ReasoningTokens,
			},
		}
	case agent.DoneEvent:
		return map[string]any{"type": "done", "content": e.Content}
	case agent.ErrorEvent:
		msg := ""
		if e.Err != nil {
			msg = e.Err.Error()
		}
		return map[string]any{"type": "error", "err": msg}
	default:
		return map[string]any{"type": "unknown"}
	}
}

// ─── Helpers ──────────────────────────────────────────────────────────────

func (m *Mux) writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(payload)
	if err != nil {
		if m.log != nil {
			m.log.ErrorContext(context.Background(), logger.CatHTTP, "writeJSON marshal failed",
				"err", err.Error())
		}
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.WriteHeader(status)
	_, _ = w.Write(data)
	_, _ = w.Write([]byte("\n"))
}

func writeWSFrame(ctx context.Context, c *websocket.Conn, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return c.Write(writeCtx, websocket.MessageText, data)
}

func truncateBytes(b []byte, max int) string {
	s := string(b)
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func (m *Mux) logInfo(ctx context.Context, msg string, args ...any) {
	if m.log == nil {
		return
	}
	m.log.InfoContext(ctx, logger.CatHTTP, msg, args...)
}

func (m *Mux) logWS(ctx context.Context, msg string, args ...any) {
	if m.log == nil {
		return
	}
	m.log.InfoContext(ctx, logger.CatWS, msg, args...)
}

func (m *Mux) logWSError(ctx context.Context, msg string, err error, args ...any) {
	if m.log == nil {
		return
	}
	m.log.LogError(ctx, logger.CatWS, msg, err, args...)
}

func (m *Mux) logError(ctx context.Context, msg string, err error, args ...any) {
	if m.log == nil {
		return
	}
	m.log.LogError(ctx, logger.CatHTTP, msg, err, args...)
}
