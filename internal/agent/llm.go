package agent

import (
	"context"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/llm"
)

// LLMMessage 是传给 LLM 的一条消息
//
// 支持 tool-calling 协议：
//   - role="system" / "user"：填 Content
//   - role="assistant"：Content + 可选 ToolCalls（允许 Content 为空，仅有 tool_calls）
//   - role="tool"：ToolCallID + Content（工具执行结果）
type LLMMessage struct {
	Role            string
	Content         string
	ReasoningContent string // DeepSeek thinking mode；有 tool_calls 时必须回传
	Name            string
	ToolCallID      string         // role="tool" 时必填
	ToolCalls       []llm.ToolCall // role="assistant" 可选
}

// LLMRequest 是 LLMClient.Chat / ChatStream 的输入
type LLMRequest struct {
	Model       string
	Messages    []LLMMessage
	Temperature float64
	MaxTokens   int

	// 扩展采样参数
	TopP             float64
	FrequencyPenalty float64
	PresencePenalty  float64
	StopSequences    []string

	// 推理努力等级（V4 模型思考模式）
	// "high" | "max" | ""（空表示不发送此参数）
	ReasoningEffort string

	// ThinkingEnabled 是否启用思考模式
	ThinkingEnabled bool

	// Tool-calling
	Tools      []llm.ToolDef // 空表示无 tool
	ToolChoice string        // "" | "none" | "auto" | "required"

	// 输出格式
	ResponseJSON bool // 对应 response_format: json_object

	// Streaming 选项（仅 ChatStream 生效）
	IncludeUsage bool // 对应 stream_options.include_usage
}

// LLMResponse 是 LLMClient.Chat 的返回
type LLMResponse struct {
	Content          string
	ReasoningContent string // deepseek-reasoner 专用
	ToolCalls        []llm.ToolCall
	FinishReason     llm.FinishReason
	Usage            llm.Usage
}

// LLMClient 是 LLM 调用的最小接口
//
// 实现必须并发安全（多 goroutine 可能同时调 Chat / ChatStream）。
// ctx 取消时应尽快返回 ctx.Err()。
type LLMClient interface {
	// Chat 同步调用：阻塞直到完整响应（内部可能是 streaming 累积）
	Chat(ctx context.Context, req LLMRequest) (*LLMResponse, error)

	// ChatStream 返回 Event channel
	// channel 被关闭时表示流结束（正常或异常）；error 事件会先投递再 close
	ChatStream(ctx context.Context, req LLMRequest) (<-chan llm.Event, error)
}

// ─── FakeLLM ─────────────────────────────────────────────────────────────────

// FakeLLM 是供测试 / demo 使用的 LLMClient 实现
//
// Chat 行为：
//   - Err 非 nil：直接返回该 error
//   - Delay > 0：发响应前等待 Delay；期间 ctx 可取消
//   - ToolCallsByTurn 非空：按顺序消费 —— 第 i 次 Chat 若 i < len 且该 turn 非空，
//     返回 ToolCalls=ToolCallsByTurn[i] + FinishReason=FinishToolCalls；
//     空 turn（nil / 长度 0）fall through 到 Responses 路径
//   - Responses 为空：content 空；否则按顺序循环返回
//
// ChatStream 行为（P1 新）：
//   - 按轮切片（turn idx 独立于 Chat 的 idx）：第 i 次 ChatStream 调用按
//     序消费 StreamDeltas / ReasoningDeltasByTurn / ToolCallDeltasByTurn 的
//     第 i 项，最后发 EventDone（FinishReason 取自 FinishByTurn[i] 或默认）
//   - 若所有 per-turn 字段都为空，**回退**到旧行为：把 Responses 当前 slot
//     作为一个 EventDelta + 一个 EventDone（保持向后兼容）
//   - Err 非 nil：发 EventError 再 close
//   - Delay > 0：发第一条事件前等待
//
// 并发安全：idx / toolIdx / streamIdx 由 mu 保护。
type FakeLLM struct {
	Responses []string
	Delay     time.Duration
	Err       error

	// ToolCallsByTurn 按调用顺序预设 tool_calls（仅 Chat 路径使用）
	// 支持测试脚本化多轮 tool-use 场景；默认 nil 时行为与旧 FakeLLM 完全一致
	ToolCallsByTurn [][]llm.ToolCall

	// ─── ChatStream per-turn 脚本（P1）──────────────────────────────────
	//
	// 设计原则：
	//   - 每个字段都是 "[][]X"：外层 index 对应"第几次 ChatStream 调用"，
	//     内层是该次调用发出的增量序列
	//   - 同一轮内 content / reasoning / tool_call 的增量按 "round-robin"
	//     交错发出：先 content[0] / reasoning[0] / 各 tool_call[0]，再
	//     content[1] / ...（直到所有内层序列用尽）—— 更贴近真实 LLM
	//     先出 role / 再交错出 content 和 tool_call 的流模式
	//   - 任一轮索引溢出（i >= len）时只发 Done 事件

	// StreamDeltas[i] 是第 i 次 ChatStream 要依次发出的 content 增量
	StreamDeltas [][]string

	// ReasoningDeltasByTurn[i] 是第 i 次要发的 reasoning_content 增量
	ReasoningDeltasByTurn [][]string

	// ToolCallDeltasByTurn[i] 是第 i 次要发的 tool_call 增量
	// 单个 delta 按 llm.ToolCallDelta 原样投递；Index 字段决定 slot 归位
	ToolCallDeltasByTurn [][]llm.ToolCallDelta

	// FinishByTurn[i] 指定第 i 次流式结束时的 FinishReason
	// 未设置时：若本轮有 tool_call deltas 则 FinishToolCalls，否则 FinishStop
	FinishByTurn []llm.FinishReason

	// Hook 可选：每次调用（Chat 或 ChatStream）被触发
	Hook func(req LLMRequest)

	mu        sync.Mutex
	idx       int // Responses 下一个消费位置
	toolIdx   int // ToolCallsByTurn 下一个消费位置
	streamIdx int // ChatStream per-turn 脚本的下一个消费位置
}

// Chat 返回预设响应
func (f *FakeLLM) Chat(ctx context.Context, req LLMRequest) (*LLMResponse, error) {
	if f.Hook != nil {
		f.Hook(req)
	}
	if f.Err != nil {
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

	// 优先走 tool_calls 预设：本次 turn 非空 → 返回 tool_calls，不消费 Responses
	if f.toolIdx < len(f.ToolCallsByTurn) {
		tcs := f.ToolCallsByTurn[f.toolIdx]
		f.toolIdx++
		if len(tcs) > 0 {
			return &LLMResponse{
				ToolCalls:    tcs,
				FinishReason: llm.FinishToolCalls,
			}, nil
		}
		// 空 turn：fall-through 到 Responses 路径（允许"第 n 轮不发 tool_call"）
	}

	var content string
	if len(f.Responses) > 0 {
		content = f.Responses[f.idx%len(f.Responses)]
		f.idx++
	}
	return &LLMResponse{
		Content:      content,
		FinishReason: llm.FinishStop,
	}, nil
}

// ChatStream 返回一个事件通道
//
// 行为路径（按优先级）：
//  1. Err 非 nil → 发一个 EventError 后 close
//  2. per-turn 脚本（StreamDeltas / ReasoningDeltasByTurn / ToolCallDeltasByTurn
//     任一非空）→ 按 streamIdx 取本轮脚本；按 round-robin 交错发增量；
//     最后发 EventDone（FinishReason 见 FinishByTurn 或推导）
//  3. 回退路径：从 Responses 拿一条作为整段 EventDelta + EventDone
//     （保持向后兼容旧 FakeLLM 的测试）
//
// Delay 应用在发第一条事件**之前**；期间 ctx 取消会产出 EventError 再 close。
func (f *FakeLLM) ChatStream(ctx context.Context, req LLMRequest) (<-chan llm.Event, error) {
	if f.Hook != nil {
		f.Hook(req)
	}

	ch := make(chan llm.Event, 8)
	go func() {
		defer close(ch)

		if f.Err != nil {
			sendEvent(ctx, ch, llm.Event{Type: llm.EventError, Err: f.Err})
			return
		}

		if f.Delay > 0 {
			select {
			case <-time.After(f.Delay):
			case <-ctx.Done():
				sendEvent(ctx, ch, llm.Event{Type: llm.EventError, Err: ctx.Err()})
				return
			}
		} else if err := ctx.Err(); err != nil {
			sendEvent(ctx, ch, llm.Event{Type: llm.EventError, Err: err})
			return
		}

		// 本轮脚本快照
		f.mu.Lock()
		turn := f.streamIdx
		f.streamIdx++

		var (
			contentDeltas   []string
			reasoningDeltas []string
			toolDeltas      []llm.ToolCallDelta
			finish          llm.FinishReason
			hasPerTurn      bool
		)

		if turn < len(f.StreamDeltas) {
			contentDeltas = f.StreamDeltas[turn]
			if len(contentDeltas) > 0 {
				hasPerTurn = true
			}
		}
		if turn < len(f.ReasoningDeltasByTurn) {
			reasoningDeltas = f.ReasoningDeltasByTurn[turn]
			if len(reasoningDeltas) > 0 {
				hasPerTurn = true
			}
		}
		if turn < len(f.ToolCallDeltasByTurn) {
			toolDeltas = f.ToolCallDeltasByTurn[turn]
			if len(toolDeltas) > 0 {
				hasPerTurn = true
			}
		}
		if turn < len(f.FinishByTurn) && f.FinishByTurn[turn] != "" {
			finish = f.FinishByTurn[turn]
		}

		// 兼容旧脚本：当 per-turn 为空但 ToolCallsByTurn 有内容时，
		// 把本轮 ToolCalls 合成为 ToolCallDelta 序列。
		// 共享 toolIdx 与 Chat 路径，使 ToolCallCount() 行为对齐。
		if !hasPerTurn && f.toolIdx < len(f.ToolCallsByTurn) {
			tcs := f.ToolCallsByTurn[f.toolIdx]
			f.toolIdx++
			if len(tcs) > 0 {
				for i, tc := range tcs {
					toolDeltas = append(toolDeltas, llm.ToolCallDelta{
						Index:     i,
						ID:        tc.ID,
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					})
				}
				hasPerTurn = true
				if finish == "" {
					finish = llm.FinishToolCalls
				}
			}
			// 若 tcs 为空（显式 nil turn）：fall-through 到 Responses 路径
		}

		// 回退路径：per-turn 全部为空 → 按旧行为用 Responses
		var fallbackContent string
		if !hasPerTurn {
			if len(f.Responses) > 0 {
				fallbackContent = f.Responses[f.idx%len(f.Responses)]
				f.idx++
			}
		}
		f.mu.Unlock()

		if !hasPerTurn {
			if fallbackContent != "" {
				if !sendEvent(ctx, ch, llm.Event{Type: llm.EventDelta, ContentDelta: fallbackContent}) {
					return
				}
			}
			fr := finish
			if fr == "" {
				fr = llm.FinishStop
			}
			sendEvent(ctx, ch, llm.Event{Type: llm.EventDone, FinishReason: fr})
			return
		}

		// 按 round-robin 交错发：第 i 轮把 content[i] / reasoning[i] / toolDelta[i]
		// 都发出去（任何一个 index 溢出就跳过，直到所有序列用完）
		maxLen := len(contentDeltas)
		if n := len(reasoningDeltas); n > maxLen {
			maxLen = n
		}
		if n := len(toolDeltas); n > maxLen {
			maxLen = n
		}

		for i := 0; i < maxLen; i++ {
			if err := ctx.Err(); err != nil {
				sendEvent(ctx, ch, llm.Event{Type: llm.EventError, Err: err})
				return
			}
			if i < len(contentDeltas) {
				if !sendEvent(ctx, ch, llm.Event{
					Type:         llm.EventDelta,
					ContentDelta: contentDeltas[i],
				}) {
					return
				}
			}
			if i < len(reasoningDeltas) {
				if !sendEvent(ctx, ch, llm.Event{
					Type:                  llm.EventDelta,
					ReasoningContentDelta: reasoningDeltas[i],
				}) {
					return
				}
			}
			if i < len(toolDeltas) {
				d := toolDeltas[i]
				if !sendEvent(ctx, ch, llm.Event{
					Type:          llm.EventDelta,
					ToolCallDelta: &d,
				}) {
					return
				}
			}
		}

		// FinishReason 推导：未显式指定时，有 tool_call deltas 用 FinishToolCalls
		if finish == "" {
			if len(toolDeltas) > 0 {
				finish = llm.FinishToolCalls
			} else {
				finish = llm.FinishStop
			}
		}
		sendEvent(ctx, ch, llm.Event{Type: llm.EventDone, FinishReason: finish})
	}()
	return ch, nil
}

// sendEvent 尝试向 ch 发送 ev
//
// 优先非阻塞发送（buffer 有空位时直接进，不管 ctx 状态）；
// 失败（buffer 满）才阻塞等待 caller 消费或 ctx 取消。
// 返回 false 表示 ctx 取消且 ch 满，事件被丢弃。
func sendEvent(ctx context.Context, ch chan<- llm.Event, ev llm.Event) bool {
	select {
	case ch <- ev:
		return true
	default:
	}
	select {
	case ch <- ev:
		return true
	case <-ctx.Done():
		return false
	}
}

// CallCount 返回成功响应的次数（仅 fake 使用，测试辅助）
func (f *FakeLLM) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.idx
}

// ToolCallCount 返回 ToolCallsByTurn 已被消费的次数（含空 turn）
func (f *FakeLLM) ToolCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.toolIdx
}

// StreamCallCount 返回 ChatStream 已被调用的次数（用于测试断言）
func (f *FakeLLM) StreamCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.streamIdx
}
