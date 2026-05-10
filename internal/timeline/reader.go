package timeline

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/rotating"
)

// ─── Segment ─────────────────────────────────────────────────────────────────

// Segment 是两个 /clear 之间的消息集合
type Segment struct {
	Messages []MessagePayload
}

// ─── ReadTail / ReadTailBefore ───────────────────────────────────────────────

// ReadTail reads the last maxTurns conversation turns from timeline files.
// Returns messages and a cursor for paginating further back.
func ReadTail(dir, baseName string, maxTurns int) ([]Segment, *time.Time, error) {
	return readTailSince(dir, baseName, maxTurns, time.Time{})
}

// ReadTailBefore reads up to maxTurns turns strictly before the cursor.
func ReadTailBefore(dir, baseName string, maxTurns int, before time.Time) ([]Segment, *time.Time, error) {
	return readTailSince(dir, baseName, maxTurns, before)
}

// readTailSince is the shared implementation.
func readTailSince(dir, baseName string, maxTurns int, since time.Time) ([]Segment, *time.Time, error) {
	files, err := rotating.ListFiles(dir, baseName)
	if err != nil {
		return nil, nil, fmt.Errorf("timeline: list files: %w", err)
	}
	if len(files) == 0 {
		return nil, nil, nil
	}

	// Read files newest-first
	var allEvents []Event
	for i := len(files) - 1; i >= 0; i-- {
		events, err := readFile(files[i])
		if err != nil {
			continue
		}
		allEvents = append(allEvents, events...)
	}

	if len(allEvents) == 0 {
		return nil, nil, nil
	}

	// Collect messages from the end, stopping after maxTurns user messages
	// or when hitting a message at/after the since cursor.
	type collected struct {
		msg  MessagePayload
		role string
	}
	var rev []collected
	userCount := 0

	for i := len(allEvents) - 1; i >= 0 && userCount < maxTurns; i-- {
		evt := allEvents[i]
		if evt.EventType != EventMessage || evt.Message == nil {
			continue
		}
		msg := *evt.Message

		// Pagination: skip messages at or after the cursor (already loaded).
		if !since.IsZero() && msg.Timestamp != "" {
			if ts, err := time.Parse(time.RFC3339Nano, msg.Timestamp); err == nil {
				if !ts.Before(since) {
					continue
				}
			}
		}

		// Skip system prompts (CW metadata, not conversation).
		if msg.Role == "system" && strings.Contains(msg.Content, "<identity>") {
			continue
		}
		// Skip summary system messages (handled by control events).
		if msg.Role == "system" && strings.Contains(msg.Content, "[Conversation Summary]") {
			continue
		}
		// Skip empty assistant messages.
		if msg.Role == "assistant" && msg.Content == "" && len(msg.ToolCalls) == 0 {
			continue
		}

		if msg.Role == "user" {
			userCount++
		}
		rev = append(rev, collected{msg: msg, role: msg.Role})
	}

	if len(rev) == 0 {
		return nil, nil, nil
	}

	// Reverse to chronological order
	msgs := make([]MessagePayload, len(rev))
	for i, c := range rev {
		msgs[len(rev)-1-i] = c.msg
	}

	cursorMsgs := readTailCursor(msgs)
	return []Segment{{Messages: msgs}}, cursorMsgs, nil
}

// readTailCursor extracts the timestamp of the oldest message as a cursor
// for the next page. Returns nil if no parsable timestamp found.
func readTailCursor(msgs []MessagePayload) *time.Time {
	if len(msgs) == 0 {
		return nil
	}
	if ts, err := time.Parse(time.RFC3339Nano, msgs[0].Timestamp); err == nil {
		return &ts
	}
	return nil
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
func replaySegment(cw *ctxwin.ContextWindow, msgs []MessagePayload) {
	type pendingGroup struct {
		assistant   *MessagePayload
		toolCallIDs map[string]bool
		toolResults []MessagePayload
		allFound    bool
	}

	var pending *pendingGroup

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
		pending = nil
	}

	for _, msg := range msgs {
		if pending != nil && !pending.allFound {
			if msg.Role == string(ctxwin.RoleTool) && msg.ToolCallID != "" {
				if _, needed := pending.toolCallIDs[msg.ToolCallID]; needed {
					pending.toolCallIDs[msg.ToolCallID] = true
					pending.toolResults = append(pending.toolResults, msg)
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
			pending = nil
			if msg.Role == string(ctxwin.RoleTool) {
				continue
			}
		}

		if pending != nil && pending.allFound {
			flushPending()
		}

		// Skip identity and summary system messages.
		if msg.Role == string(ctxwin.RoleSystem) {
			if strings.Contains(msg.Content, "<identity>") || strings.Contains(msg.Content, "[Conversation Summary]") {
				continue
			}
		}

		// Skip empty assistant.
		if msg.Role == string(ctxwin.RoleAssistant) && msg.Content == "" && len(msg.ToolCalls) == 0 {
			continue
		}

		// New assistant(tool_calls) → start pending group.
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

		if msg.Role == string(ctxwin.RoleTool) {
			continue
		}

		pushMessage(cw, msg)
	}

	flushPending()
}

// pushMessage 将单条消息 push 到 ContextWindow
func pushMessage(cw *ctxwin.ContextWindow, msg MessagePayload) {
	opts := make([]ctxwin.PushOption, 0, 5)
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
	if msg.Timestamp != "" {
		if ts, err := time.Parse(time.RFC3339Nano, msg.Timestamp); err == nil {
			opts = append(opts, ctxwin.WithTimestamp(ts))
		}
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
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var evt Event
		if err := json.Unmarshal(line, &evt); err != nil {
			continue
		}
		events = append(events, evt)
	}

	return events, scanner.Err()
}
