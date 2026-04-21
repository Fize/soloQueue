package agent

import (
	"context"
	"sync"
	"time"
)

// LLMMessage 是传给 LLM 的一条消息
type LLMMessage struct {
	Role    string // "system" | "user" | "assistant" | "tool"
	Content string
}

// LLMRequest 是 LLMClient.Chat 的输入
type LLMRequest struct {
	Model       string
	Messages    []LLMMessage
	Temperature float64
	MaxTokens   int
}

// LLMResponse 是 LLMClient.Chat 的输出
//
// 本 phase 仅含 Content；下 phase 加 tool_calls / usage / finish_reason
type LLMResponse struct {
	Content string
}

// LLMClient 是 LLM 调用的最小接口
//
// 实现必须是并发安全的（多个 goroutine 可能同时调 Chat）。
// ctx 取消时 Chat 必须尽快返回 ctx.Err()。
type LLMClient interface {
	Chat(ctx context.Context, req LLMRequest) (*LLMResponse, error)
}

// ─── FakeLLM ─────────────────────────────────────────────────────────────────

// FakeLLM 是供测试和 demo 使用的 LLMClient 实现
//
// 行为：
//   - Err 非 nil：每次 Chat 都返回该 error（忽略 Responses）
//   - Delay > 0：Chat 等待 Delay 后再返回；期间 ctx 可取消
//   - Responses 为空：返回空字符串响应
//   - 否则：按顺序循环返回 Responses 里的字符串
//
// 并发安全：idx 由 mu 保护。
type FakeLLM struct {
	Responses []string
	Delay     time.Duration
	Err       error

	// Hook 可选：每次 Chat 被调用时以 req 为参数触发
	// 测试可用它检查 request 结构（model / system prompt / messages 顺序等）
	Hook func(req LLMRequest)

	mu  sync.Mutex
	idx int
}

// Chat 返回预设响应
func (f *FakeLLM) Chat(ctx context.Context, req LLMRequest) (*LLMResponse, error) {
	if f.Hook != nil {
		f.Hook(req)
	}
	if f.Err != nil {
		// 即使有 Err 也要遵守 ctx
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, f.Err
	}

	if f.Delay > 0 {
		select {
		case <-time.After(f.Delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	} else if err := ctx.Err(); err != nil {
		return nil, err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	var content string
	if len(f.Responses) > 0 {
		content = f.Responses[f.idx%len(f.Responses)]
		f.idx++
	}
	return &LLMResponse{Content: content}, nil
}

// CallCount 返回 Chat 被成功调用的次数（仅 fake 使用，测试辅助）
func (f *FakeLLM) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.idx
}
