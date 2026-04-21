package agent

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// Agent 是一个与 LLM 绑定的同步调用单元
//
// 本 phase 不维护历史；每次 Run 是独立的单轮对话（system + user → assistant）。
// 多轮对话 / tool loop 留给下 phase 的 tool 系统。
//
// 并发安全：Agent 本身无可变状态；Run 可被多个 goroutine 同时调用。
// LLMClient 实现需要线程安全（FakeLLM 已满足）。
type Agent struct {
	Def Definition
	LLM LLMClient
	Log *logger.Logger
}

// NewAgent 构造 agent
//
// log 可以为 nil（此时日志调用被跳过），通常 caller 传入带 actor_id 的 Child logger。
func NewAgent(def Definition, llm LLMClient, log *logger.Logger) *Agent {
	return &Agent{Def: def, LLM: llm, Log: log}
}

// Run 以 prompt 为用户输入，调用一次 LLM 并返回内容
//
// ctx 取消会取消 LLM 调用；返回 ctx.Err()。
// LLM 返回的 error 会被透传（加日志）。
func (a *Agent) Run(ctx context.Context, prompt string) (string, error) {
	if a.LLM == nil {
		return "", fmt.Errorf("agent %q: llm client is nil", a.Def.ID)
	}

	req := LLMRequest{
		Model:       a.Def.ModelID,
		Temperature: a.Def.Temperature,
		MaxTokens:   a.Def.MaxTokens,
		Messages:    buildMessages(a.Def.SystemPrompt, prompt),
	}

	a.logInfo(ctx, logger.CatLLM, "llm chat start",
		slog.String("model", req.Model),
		slog.Int("prompt_len", len(prompt)),
	)

	resp, err := a.LLM.Chat(ctx, req)
	if err != nil {
		a.logError(ctx, logger.CatLLM, "llm chat failed", err)
		return "", err
	}

	a.logInfo(ctx, logger.CatLLM, "llm chat done",
		slog.Int("response_len", len(resp.Content)),
	)
	return resp.Content, nil
}

// buildMessages 组装 system + user 两条消息
//
// 如果 systemPrompt 为空，跳过 system 消息（避免 `{"role":"system","content":""}`）。
func buildMessages(systemPrompt, userPrompt string) []LLMMessage {
	msgs := make([]LLMMessage, 0, 2)
	if systemPrompt != "" {
		msgs = append(msgs, LLMMessage{Role: "system", Content: systemPrompt})
	}
	msgs = append(msgs, LLMMessage{Role: "user", Content: userPrompt})
	return msgs
}

// logInfo / logError 是 nil-safe 的日志包装
func (a *Agent) logInfo(ctx context.Context, cat logger.Category, msg string, args ...any) {
	if a.Log == nil {
		return
	}
	a.Log.InfoContext(ctx, cat, msg, args...)
}

func (a *Agent) logError(ctx context.Context, cat logger.Category, msg string, err error) {
	if a.Log == nil {
		return
	}
	a.Log.ErrorContext(ctx, cat, msg, slog.String("err", err.Error()))
}
