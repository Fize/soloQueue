package timeline

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/rotating"
)

// ─── Segment ─────────────────────────────────────────────────────────────────

// Segment 是两个 /clear 之间的消息集合
type Segment struct {
	Messages []MessagePayload
}

// ─── ReadLastSegments ────────────────────────────────────────────────────────

// ReadLastSegments 读取所有轮转文件，返回最后一个截断点之后的 segment
//
// 截断点优先级（从后往前找第一个）：
//   - /clear：只返回之后的消息
//   - summary：返回 summary 内容（system 消息）+ 之后的消息
//
// 如果最近的控制事件是 /clear 且之后没有新消息，返回 nil。
func ReadLastSegments(dir, baseName string) ([]Segment, error) {
	files, err := rotating.ListFiles(dir, baseName)
	if err != nil {
		return nil, fmt.Errorf("timeline: list files: %w", err)
	}
	if len(files) == 0 {
		return nil, nil
	}

	// 读取所有事件
	var allEvents []Event
	for _, path := range files {
		events, err := readFile(path)
		if err != nil {
			continue
		}
		allEvents = append(allEvents, events...)
	}

	if len(allEvents) == 0 {
		return nil, nil
	}

	// 从后往前找到最后一个截断点（/clear 或 summary）
	lastCutIdx := -1
	var lastCutAction string
	var lastCutContent string
	for i := len(allEvents) - 1; i >= 0; i-- {
		evt := allEvents[i]
		if evt.EventType == EventControl && evt.Control != nil {
			switch evt.Control.Action {
			case "clear", "summary":
				lastCutIdx = i
				lastCutAction = evt.Control.Action
				lastCutContent = evt.Control.Content
			}
		}
		if lastCutIdx != -1 {
			break
		}
	}

	startIdx := lastCutIdx + 1
	var msgs []MessagePayload

	// 如果是 summary 截断点，先插入 summary 内容作为 system 消息
	if lastCutAction == "summary" && lastCutContent != "" {
		msgs = append(msgs, MessagePayload{
			Role:    "system",
			Content: "[Conversation Summary]\n" + lastCutContent,
		})
	}

	// 收集截断点之后的消息
	for i := startIdx; i < len(allEvents); i++ {
		if allEvents[i].EventType == EventMessage && allEvents[i].Message != nil {
			msgs = append(msgs, *allEvents[i].Message)
		}
	}

	if len(msgs) == 0 {
		return nil, nil
	}

	return []Segment{{Messages: msgs}}, nil
}

// ─── ReplayInto ──────────────────────────────────────────────────────────────

// ReplayInto 将 segments 回放到 ContextWindow
//
// 跳过 system prompt（factory 已 push），将其余消息按顺序 Push 到 ContextWindow。
// 调用方应确保 ContextWindow 处于 replayMode（禁用 Push Hook）。
//
// Orphaned tool_calls 修复：如果 assistant 消息带有 tool_calls 但缺少对应的
// tool result 消息（例如 async delegation yield 后 session 退出），整个
// assistant(tool_calls) + 已到达的 partial tool results 都会被跳过，
// 防止 LLM API 因 "insufficient tool messages" 返回 HTTP 400。
func ReplayInto(cw *ctxwin.ContextWindow, segments []Segment) {
	for _, seg := range segments {
		replaySegment(cw, seg.Messages)
	}
}

// replaySegment 回放一段消息，跳过 orphaned tool_calls。
//
// 算法：前向扫描，对每个 assistant(tool_calls) 消息缓冲，等收集到所有
// tool result 后再统一 push。如果遇到非 tool result 消息（说明 tool results
// 不完整），则丢弃缓冲的 assistant(tool_calls) 及其 partial results。
func replaySegment(cw *ctxwin.ContextWindow, msgs []MessagePayload) {
	// pending: 等待 tool result 的 assistant(tool_calls) 及已收集的 tool results
	type pendingGroup struct {
		assistant  *MessagePayload
		toolCallIDs map[string]bool   // 需要的 tool_call_id → 是否已到达
		toolResults []MessagePayload  // 已到达的 tool result 消息
		allFound    bool
	}

	var pending *pendingGroup

	// flushPending 将缓冲的 assistant + tool results push 到 cw
	flushPending := func() {
		if pending == nil {
			return
		}
		if pending.allFound {
			pushMessage(cw, *pending.assistant)
			for _, tr := range pending.toolResults {
				pushMessage(cw, tr)
			}
		}
		// allFound == false: orphaned，整组丢弃
		pending = nil
	}

	for _, msg := range msgs {

		// 如果有 pending group，检查当前消息是否为其 tool result
		if pending != nil && !pending.allFound {
			if msg.Role == string(ctxwin.RoleTool) && msg.ToolCallID != "" {
				if _, needed := pending.toolCallIDs[msg.ToolCallID]; needed {
					pending.toolCallIDs[msg.ToolCallID] = true
					pending.toolResults = append(pending.toolResults, msg)
					// 检查是否全部到齐
					allFound := true
					for _, found := range pending.toolCallIDs {
						if !found {
							allFound = false
							break
						}
					}
					if allFound {
						pending.allFound = true
					}
					continue
				}
			}
			// 当前消息不是 pending group 的 tool result
			// → pending group 的 tool results 不完整，丢弃
			pending = nil
		}

		// 如果 pending.allFound == true，先 flush 再处理当前消息
		if pending != nil && pending.allFound {
			flushPending()
		}

		// 跳过无效 assistant 消息（content 为空且无 tool_calls）。
		// 这种消息会导致 LLM API 返回 HTTP 400 "Invalid assistant"。
		if msg.Role == string(ctxwin.RoleAssistant) && msg.Content == "" && len(msg.ToolCalls) == 0 {
			continue
		}

		// 当前消息是 assistant(tool_calls)？开启新的 pending group
		if msg.Role == string(ctxwin.RoleAssistant) && len(msg.ToolCalls) > 0 {
			ids := make(map[string]bool, len(msg.ToolCalls))
			for _, tc := range msg.ToolCalls {
				ids[tc.ID] = false
			}
			pending = &pendingGroup{
				assistant:   &msg,
				toolCallIDs: ids,
			}
			continue
		}

		// 普通消息：直接 push
		pushMessage(cw, msg)
	}

	// 处理末尾的 pending group
	flushPending()
}

// pushMessage 将单条消息 push 到 ContextWindow
func pushMessage(cw *ctxwin.ContextWindow, msg MessagePayload) {
	opts := make([]ctxwin.PushOption, 0, 4)
	if msg.ReasoningContent != "" {
		opts = append(opts, ctxwin.WithReasoningContent(msg.ReasoningContent))
	}
	if msg.IsEphemeral {
		opts = append(opts, ctxwin.WithEphemeral(true))
	}
	if msg.Name != "" {
		opts = append(opts, ctxwin.WithToolName(msg.Name))
	}
	if msg.ToolCallID != "" {
		opts = append(opts, ctxwin.WithToolCallID(msg.ToolCallID))
	}
	if len(msg.ToolCalls) > 0 {
		tcs := make([]llm.ToolCall, len(msg.ToolCalls))
		for i, tc := range msg.ToolCalls {
			tcs[i] = llm.ToolCall{
				ID:   tc.ID,
				Type: tc.Type,
				Function: llm.FunctionCall{
					Name:      tc.Name,
					Arguments: tc.Arguments,
				},
			}
		}
		opts = append(opts, ctxwin.WithToolCalls(tcs))
	}

	cw.Push(ctxwin.MessageRole(msg.Role), msg.Content, opts...)
}

// ─── 内部方法 ────────────────────────────────────────────────────────────────

// readFile 读取单个 JSONL 文件中的所有事件
func readFile(path string) ([]Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var events []Event
	scanner := bufio.NewScanner(f)
	// 增大 buffer 以适应大行
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var evt Event
		if err := json.Unmarshal(line, &evt); err != nil {
			// 跳过无法解析的行
			continue
		}
		events = append(events, evt)
	}

	return events, scanner.Err()
}

// splitSegments 按控制事件 /clear 分割成 segments
func splitSegments(events []Event) []Segment {
	var segments []Segment
	var current Segment

	for _, evt := range events {
		if evt.EventType == EventControl && evt.Control != nil && evt.Control.Action == "clear" {
			// /clear：结束当前 segment，开始新的
			segments = append(segments, current)
			current = Segment{}
			continue
		}

		if evt.EventType == EventMessage && evt.Message != nil {
			current.Messages = append(current.Messages, *evt.Message)
		}
	}

	// 最后一个 segment（没有 /clear 结尾）
	if len(current.Messages) > 0 {
		segments = append(segments, current)
	}

	return segments
}
