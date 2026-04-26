// Package server 暴露 SoloQueue 的 REST + WebSocket 接口
//
// 路由：
//
//	POST   /v1/sessions              → {"session_id":"..."}
//	DELETE /v1/sessions/{id}         → 204
//	GET    /v1/sessions/{id}/history → {"messages":[...]}
//	GET    /v1/sessions/{id}/stream  → WebSocket upgrade
//	GET    /healthz                  → {"status":"ok"}
//
// WebSocket 协议（JSON per frame）：
//
//	Client → Server:  {"type":"ask","prompt":"..."} | {"type":"cancel"} | {"type":"ping"}
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
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/session"
)

// ─── Mux ──────────────────────────────────────────────────────────────────

// Mux 是根路由
type Mux struct {
	mgr *session.SessionManager
	log *logger.Logger
	mux *http.ServeMux

	// wsOpts 传给 websocket.Accept；测试可覆盖以跳过 origin 检查
	wsOpts *websocket.AcceptOptions
}

// NewMux 构造路由
func NewMux(mgr *session.SessionManager, log *logger.Logger) *Mux {
	m := &Mux{
		mgr: mgr,
		log: log,
		mux: http.NewServeMux(),
		wsOpts: &websocket.AcceptOptions{
			// 本地开发默认 InsecureSkipVerify（缺省 WS 规范要求 Origin）
			// 生产中应由调用方（main.go）根据 settings 覆写
			InsecureSkipVerify: true,
		},
	}

	m.mux.HandleFunc("GET /healthz", m.handleHealth)

	m.mux.HandleFunc("POST /v1/sessions", m.handleCreate)
	m.mux.HandleFunc("DELETE /v1/sessions/{id}", m.handleDelete)
	m.mux.HandleFunc("GET /v1/sessions/{id}/history", m.handleHistory)
	m.mux.HandleFunc("GET /v1/sessions/{id}/stream", m.handleStream)

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
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal", "path": r.URL.Path})
		}
	}()
	m.mux.ServeHTTP(w, r)
}

// ─── Handlers ─────────────────────────────────────────────────────────────

func (m *Mux) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type createReq struct {
	TeamID string `json:"team_id"`
}
type createResp struct {
	SessionID string `json:"session_id"`
}

func (m *Mux) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req createReq
	// body 可选；缺失时 TeamID 为空字符串
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
			return
		}
	}
	s, err := m.mgr.Create(r.Context(), req.TeamID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	m.logInfo(r.Context(), "session created",
		slog.String("session_id", s.ID),
		slog.String("team_id", s.TeamID),
	)
	writeJSON(w, http.StatusCreated, createResp{SessionID: s.ID})
}

func (m *Mux) handleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := m.mgr.Delete(id, 5*time.Second)
	if errors.Is(err, session.ErrSessionNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	m.logInfo(r.Context(), "session deleted", slog.String("session_id", id))
	w.WriteHeader(http.StatusNoContent)
}

type historyResp struct {
	Messages []historyMsg `json:"messages"`
}
type historyMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (m *Mux) handleHistory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s, ok := m.mgr.Get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	h := s.History()
	msgs := make([]historyMsg, 0, len(h))
	for _, m := range h {
		msgs = append(msgs, historyMsg{Role: m.Role, Content: m.Content})
	}
	writeJSON(w, http.StatusOK, historyResp{Messages: msgs})
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
)

// handleStream accepts WS upgrade and runs read+write loops.
//
// Lifecycle:
//   - connCtx cancels when: conn closes / client sends cancel / handler returns.
//   - each "ask" starts an askCtx (child of connCtx). "cancel" cancels it.
//   - read goroutine drives write goroutine via Session.AskStream events.
func (m *Mux) handleStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	s, ok := m.mgr.Get(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	c, err := websocket.Accept(w, r, m.wsOpts)
	if err != nil {
		m.logError(r.Context(), "ws accept failed", err,
			slog.String("session_id", id),
		)
		return
	}
	defer c.CloseNow()

	connCtx, cancel := context.WithCancel(r.Context())
	defer cancel()

	m.logInfo(r.Context(), "ws connected", slog.String("session_id", id))
	defer m.logInfo(r.Context(), "ws disconnected", slog.String("session_id", id))

	// active ask cancel (protected by askMu)
	var (
		askCancel context.CancelFunc
	)

	for {
		_, data, err := c.Read(connCtx)
		if err != nil {
			return
		}
		var in wsInFrame
		if err := json.Unmarshal(data, &in); err != nil {
			if wsErr := writeWSFrame(connCtx, c, map[string]string{
				"type":  "error",
				"error": "invalid json",
			}); wsErr != nil {
				m.logError(connCtx, "ws write error", wsErr)
			}
			continue
		}

		switch in.Type {
		case framePing:
			if err := writeWSFrame(connCtx, c, map[string]string{"type": "pong"}); err != nil {
				m.logError(connCtx, "ws write pong error", err)
			}

		case frameCancel:
			if askCancel != nil {
				askCancel()
				askCancel = nil
				if err := writeWSFrame(connCtx, c, map[string]string{"type": "error", "err": "cancelled"}); err != nil {
					m.logError(connCtx, "ws write cancel error", err)
				}
			}

		case frameAsk:
			// stop any in-flight ask
			if askCancel != nil {
				askCancel()
				askCancel = nil
			}
			askCtx, ac := context.WithCancel(connCtx)
			askCancel = ac

			events, serr := s.AskStream(askCtx, in.Prompt)
			if serr != nil {
				if err := writeWSFrame(connCtx, c, map[string]string{
					"type":  "error",
					"error": serr.Error(),
				}); err != nil {
					m.logError(connCtx, "ws write ask error", err)
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
			if err := s.Agent.Confirm(in.CallID, in.Choice); err != nil {
				m.logError(connCtx, "confirm failed", err,
					slog.String("session_id", id),
					slog.String("call_id", in.CallID),
					slog.String("choice", in.Choice),
				)
			}

		default:
			if err := writeWSFrame(connCtx, c, map[string]string{
				"type":  "error",
				"error": "unknown frame type",
			}); err != nil {
				m.logError(connCtx, "ws write error", err)
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
		if err := writeWSFrame(ctx, c, frame); err != nil {
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

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(payload)
	if err != nil {
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

func (m *Mux) logInfo(ctx context.Context, msg string, args ...any) {
	if m.log == nil {
		return
	}
	// server handlers run at system layer → use CatHTTP / CatWS as appropriate
	m.log.InfoContext(ctx, logger.CatHTTP, msg, args...)
}

func (m *Mux) logError(ctx context.Context, msg string, err error, args ...any) {
	if m.log == nil {
		return
	}
	all := append([]any{slog.String("err", err.Error())}, args...)
	m.log.ErrorContext(ctx, logger.CatHTTP, msg, all...)
}
