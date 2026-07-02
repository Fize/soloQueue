package agent

import (
	"context"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/llm"
)

// LLMMessage is a message passed to the LLM
//
// Supports tool-calling protocol:
//   - role="system" / "user": Fill Content; user can optionally include Images (multimodal)
//   - role="assistant": Content + optional ToolCalls (allows empty Content, with only tool_calls)
//   - role="tool": ToolCallID + Content (tool execution result)
type LLMMessage struct {
	Role             string
	Content          string
	Images           []llm.ImageContent // Multimodal images (used only in user messages)
	ReasoningContent string             // DeepSeek thinking mode; must be returned when tool_calls are present
	Name             string
	ToolCallID       string             // Required when role="tool"
	ToolCalls        []llm.ToolCall      // Optional for role="assistant"
}

// LLMRequest is the input for LLMClient.Chat / ChatStream
type LLMRequest struct {
	ProviderID  string
	Model       string
	Messages    []LLMMessage
	Temperature float64
	MaxTokens   int

	// Extended sampling parameters
	TopP             float64
	FrequencyPenalty float64
	PresencePenalty  float64
	StopSequences    []string

	// Reasoning effort level (V4 model thinking mode)
	// "high" | "max" | "" (empty means this parameter is not sent)
	ReasoningEffort string

	// ThinkingEnabled enables thinking mode
	ThinkingEnabled bool

	// Tool-calling
	Tools      []llm.ToolDef // Empty means no tool
	ToolChoice string        // "" | "none" | "auto" | "required"

	// Output format
	ResponseJSON bool // Corresponds to response_format: json_object

	// Streaming options (only effective for ChatStream)
	IncludeUsage bool // Corresponds to stream_options.include_usage

	// Vision indicates whether the model supports multimodal image_url content.
	// If false, the wire layer will discard image data and fall back to plain text.
	Vision bool
}

// LLMResponse is the return value of LLMClient.Chat
type LLMResponse struct {
	Content          string
	ReasoningContent string // For deepseek-reasoner only
	ToolCalls        []llm.ToolCall
	FinishReason     llm.FinishReason
	Usage            llm.Usage
}

// LLMClient is the minimal interface for LLM calls
//
// Implementations must be concurrent-safe (multiple goroutines may call Chat / ChatStream simultaneously).
// When ctx is cancelled, it should return ctx.Err() as soon as possible.
type LLMClient interface {
	// Chat is a synchronous call: blocks until a complete response (internally may be accumulated from streaming)
	Chat(ctx context.Context, req LLMRequest) (*LLMResponse, error)

	// ChatStream returns an Event channel
	// When the channel is closed, it means the stream has ended (normally or abnormally); an error event will be delivered before closing
	ChatStream(ctx context.Context, req LLMRequest) (<-chan llm.Event, error)
}

// --- FakeLLM -----------------------------------------------------------------

// FakeLLM is an LLMClient implementation for testing / demo purposes
//
// Chat behavior:
//   - Err not nil: Directly returns the error
//   - Delay > 0: Waits for Delay before sending a response; ctx can be cancelled during this time
//   - ToolCallsByTurn not empty: Consumed in order - if the i-th Chat call has i < len and the turn is not empty,
//     returns ToolCalls=ToolCallsByTurn[i] + FinishReason=FinishToolCalls;
//     empty turn (nil / length 0) falls through to the Responses path
//   - Responses is empty: content is empty; otherwise, returns in a circular order
//
// ChatStream behavior (P1 new):
//   - Sliced by turn (turn idx independent of Chat's idx): The i-th ChatStream call consumes in
//     order StreamDeltas / ReasoningDeltasByTurn / ToolCallDeltasByTurn's
//     i-th item, finally sending EventDone (FinishReason from FinishByTurn[i] or default)
//   - If all per-turn fields are empty, **falls back** to old behavior: treats the current Responses slot
//     as one EventDelta + one EventDone (maintains backward compatibility)
//   - Err not nil: Sends EventError then closes
//   - Delay > 0: Waits before sending the first event
//
// Concurrency safe: idx / toolIdx / streamIdx are protected by mu.
type FakeLLM struct {
	Responses []string
	Delay     time.Duration
	Err       error

	// ToolCallsByTurn presets tool_calls in call order (used only by Chat path)
	// Supports scripted multi-turn tool-use scenarios for testing; when nil, behavior is identical to old FakeLLM
	ToolCallsByTurn [][]llm.ToolCall

	// --- ChatStream per-turn script (P1) ---------------------------------
	//
	// Design principles:
	//   - Each field is "[][]X": the outer index corresponds to "which ChatStream call",
	//     the inner is the sequence of deltas emitted by that call
	//   - Within the same turn, content / reasoning / tool_call deltas are emitted in a "round-robin"
	//     interleaved manner: first content[0] / reasoning[0] / each tool_call[0], then
	//     content[1] / ... (until all inner sequences are exhausted) - closer to real LLM
	//     streaming mode where role comes first, then content and tool_call are interleaved
	//   - If any turn index overflows (i >= len), only a Done event is sent

	// StreamDeltas[i] is the sequence of content deltas to be emitted by the i-th ChatStream call
	StreamDeltas [][]string

	// ReasoningDeltasByTurn[i] is the sequence of reasoning_content deltas to be emitted by the i-th call
	ReasoningDeltasByTurn [][]string

	// ToolCallDeltasByTurn[i] is the sequence of tool_call deltas to be emitted by the i-th call
	// Each delta is delivered as an llm.ToolCallDelta; the Index field determines slot assignment
	ToolCallDeltasByTurn [][]llm.ToolCallDelta

	// FinishByTurn[i] specifies the FinishReason when the i-th stream ends
	// If not set: if there are tool_call deltas in this turn, then FinishToolCalls, otherwise FinishStop
	FinishByTurn []llm.FinishReason

	// Hook optional: triggered on each call (Chat or ChatStream)
	Hook func(req LLMRequest)

	mu        sync.Mutex
	idx       int // Next consumption position in Responses
	toolIdx   int // Next consumption position in ToolCallsByTurn
	streamIdx int // Next consumption position for ChatStream per-turn script
}

// Chat returns a preset response
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

	// Prioritize tool_calls preset: if this turn is not empty → return tool_calls, do not consume Responses
	if f.toolIdx < len(f.ToolCallsByTurn) {
		tcs := f.ToolCallsByTurn[f.toolIdx]
		f.toolIdx++
		if len(tcs) > 0 {
			return &LLMResponse{
				ToolCalls:    tcs,
				FinishReason: llm.FinishToolCalls,
			}, nil
		}
		// Empty turn: fall-through to Responses path (allows "not sending tool_call in the n-th turn")
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

// ChatStream returns an event channel
//
// Behavior path (by priority):
//  1. Err not nil → send an EventError then close
//  2. per-turn script (StreamDeltas / ReasoningDeltasByTurn / ToolCallDeltasByTurn
//     any non-empty) → take this turn's script by streamIdx; send deltas interleaved in round-robin;
//     finally send EventDone (FinishReason from FinishByTurn or inferred)
//  3. Fallback path: take one item from Responses as a full EventDelta + EventDone
//     (maintains backward compatibility for old FakeLLM tests)
//
// Delay is applied **before** sending the first event; if ctx is cancelled during this time, an EventError will be produced before closing.
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

		// Snapshot of this turn's script
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

		// Backward compatibility for old scripts: when per-turn is empty but ToolCallsByTurn has content,
		// compose this turn's ToolCalls into a ToolCallDelta sequence.
		// Share toolIdx with Chat path to align ToolCallCount() behavior.
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
			// If tcs is empty (explicit nil turn): fall-through to Responses path
		}

		// Fallback path: if all per-turn are empty → use Responses as per old behavior
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

		// Send interleaved in round-robin: in the i-th round, send content[i] / reasoning[i] / toolDelta[i]
		// (skip if any index overflows, until all sequences are exhausted)
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

		// FinishReason inference: if not explicitly specified, use FinishToolCalls if there are tool_call deltas
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

// sendEvent attempts to send ev to ch
//
// Prioritizes non-blocking send (if buffer has space, send directly, regardless of ctx status);
// only if it fails (buffer full) does it block, waiting for caller to consume or ctx to cancel.
// Returns false if ctx is cancelled and ch is full, meaning the event was dropped.
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

// CallCount returns the number of successful responses (for fake usage only, testing utility)
func (f *FakeLLM) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.idx
}

// ToolCallCount returns the number of times ToolCallsByTurn has been consumed (including empty turns)
func (f *FakeLLM) ToolCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.toolIdx
}

// StreamCallCount returns the number of times ChatStream has been called (for testing assertions)
func (f *FakeLLM) StreamCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.streamIdx
}