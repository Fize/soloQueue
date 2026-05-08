package tui

import (
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/agent"
)

// loadMessagesFromHistory converts session history into TUI messages for replay display.
//
// It groups assistant(tool_calls) + tool results + assistant(final) into a single
// agent message with a timeline, matching the streaming display format.
// If isHistory is true, marks all messages as historical (rendered with muted style).
func loadMessagesFromHistory(history []agent.LLMMessage, isHistory bool) []message {
	if len(history) == 0 {
		return nil
	}

	var msgs []message
	var pending *message // agent message being built (tool_calls → results → final)

	flushPending := func() {
		if pending == nil {
			return
		}
		pending.dirty = true
		msgs = append(msgs, *pending)
		pending = nil
	}

	for _, h := range history {
		switch h.Role {
		case "system":
			if len(msgs) == 0 {
				continue // skip initial system prompt
			}

		case "user":
			flushPending()
			msgs = append(msgs, message{
				role:      "user",
				content:   h.Content,
				dirty:     true,
				isHistory: isHistory,
			})

		case "assistant":
			if len(h.ToolCalls) > 0 {
				flushPending()
				pending = &message{role: "agent", isHistory: isHistory}
				if h.ReasoningContent != "" {
					pending.timeline = append(pending.timeline, timelineEntry{
						kind: timelineThinking,
						text: h.ReasoningContent,
					})
				}
				for _, tc := range h.ToolCalls {
					pending.timeline = append(pending.timeline, timelineEntry{
						kind: timelineTool,
						tool: &toolBlock{
							name:   tc.Function.Name,
							args:   tc.Function.Arguments,
							callID: tc.ID,
						},
					})
				}
				if h.Content != "" {
					pending.timeline = append(pending.timeline, timelineEntry{
						kind: timelineContent,
						text: h.Content,
					})
				}
			} else if pending != nil && hasUnfilledTools(pending) {
				// Final response after tool calls — append to pending timeline
				if h.ReasoningContent != "" {
					pending.timeline = append(pending.timeline, timelineEntry{
						kind: timelineThinking,
						text: h.ReasoningContent,
					})
				}
				if h.Content != "" {
					pending.timeline = append(pending.timeline, timelineEntry{
						kind: timelineContent,
						text: h.Content,
					})
				}
				flushPending()
			} else {
				// Standalone assistant reply — content in timeline, msg.content stays ""
				flushPending()
				agMsg := message{role: "agent", dirty: true, isHistory: isHistory}
				if h.ReasoningContent != "" {
					agMsg.timeline = append(agMsg.timeline, timelineEntry{
						kind: timelineThinking,
						text: h.ReasoningContent,
					})
				}
				if h.Content != "" {
					agMsg.timeline = append(agMsg.timeline, timelineEntry{
						kind: timelineContent,
						text: h.Content,
					})
				}
				msgs = append(msgs, agMsg)
			}

		case "tool":
			if pending != nil {
				toolID := h.ToolCallID
				result := h.Content
				lineCount := 0
				for _, line := range strings.Split(result, "\n") {
					if strings.TrimSpace(line) != "" {
						lineCount++
					}
				}
				for i, entry := range pending.timeline {
					if entry.kind == timelineTool && entry.tool != nil && entry.tool.callID == toolID {
						pending.timeline[i].tool.done = true
						pending.timeline[i].tool.lineCount = lineCount
						break
					}
				}
			}
		}
	}

	flushPending()
	return msgs
}

func hasUnfilledTools(msg *message) bool {
	for _, entry := range msg.timeline {
		if entry.kind == timelineTool && entry.tool != nil {
			return true
		}
	}
	return false
}

// replayHistoryIntoMessages loads session history into the model's message list.
// Called once after session initialization to display past conversation in the TUI.
// isHistory: if true, marks all loaded messages as historical (rendered with muted style).
func (m *model) replayHistoryIntoMessages(isHistory bool) {
	if m.sess == nil {
		return
	}
	history := m.sess.History()
	msgs := loadMessagesFromHistory(history, isHistory)
	if len(msgs) == 0 {
		return
	}
	m.messages = append(m.messages, msgs...)
	m.rebuildViewportContent()
	m.viewport.GotoBottom()
}
