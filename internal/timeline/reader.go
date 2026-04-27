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

// ReadLastSegments 读取所有轮转文件，返回最后一个 /clear 之后的 segment
//
// /clear 是截断点：replay 遇到 /clear 就终止，只 replay /clear 之后的内容。
// 如果最近的事件就是 /clear（之后没有新消息），返回 nil。
// n 参数保留兼容，当前未使用（/clear 截断语义下只返回最后一段）。
func ReadLastSegments(dir, baseName string, n int) ([]Segment, error) {
	if n <= 0 {
		return nil, nil
	}

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
			// 跳过损坏的文件，不中断整体回放
			continue
		}
		allEvents = append(allEvents, events...)
	}

	if len(allEvents) == 0 {
		return nil, nil
	}

	// 从后往前找到最后一个 /clear 的位置
	lastClearIdx := -1
	for i := len(allEvents) - 1; i >= 0; i-- {
		evt := allEvents[i]
		if evt.EventType == EventControl && evt.Control != nil && evt.Control.Action == "clear" {
			lastClearIdx = i
			break
		}
	}

	// 只取最后一个 /clear 之后的消息事件
	var msgs []MessagePayload
	startIdx := lastClearIdx + 1
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
func ReplayInto(cw *ctxwin.ContextWindow, segments []Segment) {
	for _, seg := range segments {
		for _, msg := range seg.Messages {
			// 跳过 system prompt，factory 已 push
			if msg.Role == string(ctxwin.RoleSystem) {
				continue
			}

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
	}
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
