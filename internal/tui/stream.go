package tui

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
)

// ─── Agent event handling ─────────────────────────────────────────────────────

func (m *model) handleAgentEvent(ev agent.AgentEvent) {
	if m.current == nil {
		return
	}

	switch e := ev.(type) {

	case agent.ReasoningDeltaEvent:
		m.genPhase = phaseThinking
		m.current.thinkingBuf.WriteString(e.Delta)

	case agent.ContentDeltaEvent:
		m.genPhase = phaseGenerating
		// First content delta flushes any pending thinking
		if m.current.content.Len() == 0 {
			m.current.flushThinking()
		}
		m.current.content.WriteString(e.Delta)

	case agent.ToolCallDeltaEvent:
		m.genPhase = phaseToolCall
		if e.Name != "" && e.Name != m.current.curToolName {
			// New tool call starting
			m.current.curToolName = e.Name
			m.current.curToolArgs.Reset()
		}
		if e.CallID != "" {
			m.current.curToolCallID = e.CallID
		}
		if e.ArgsDelta != "" {
			m.current.curToolArgs.WriteString(e.ArgsDelta)
		}

	case agent.ToolExecStartEvent:
		m.genPhase = phaseToolCall
		// Flush any accumulated thinking before this tool
		m.current.flushThinking()
		m.current.curToolName = ""
		m.current.curToolArgs.Reset()
		tb := &toolBlock{
			name:   e.Name,
			args:   e.Args,
			callID: e.CallID,
		}
		m.current.timeline = append(m.current.timeline, timelineEntry{
			kind: timelineTool,
			tool: tb,
		})
		m.current.toolExecMap[e.CallID] = &toolExecInfo{
			name:   e.Name,
			args:   e.Args,
			start:  time.Now(),
			callID: e.CallID,
			tb:     tb,
		}

	case agent.ToolExecDoneEvent:
		if info, ok := m.current.toolExecMap[e.CallID]; ok {
			dur := time.Since(info.start)
			// Count non-empty result lines
			lineCount := 0
			for _, line := range strings.Split(e.Result, "\n") {
				if strings.TrimSpace(line) != "" {
					lineCount++
				}
			}
			info.tb.done = true
			info.tb.duration = dur
			info.tb.err = e.Err
			info.tb.lineCount = lineCount
		}

	case agent.IterationDoneEvent:
		m.promptTokens += e.Usage.PromptTokens
		m.outputTokens += e.Usage.CompletionTokens
		m.genPhase = phaseWaiting

	case agent.DoneEvent:
		// handled by streamDoneMsg (channel close)

	case agent.ToolNeedsConfirmEvent:
		m.handleToolConfirm(e)

	case agent.ErrorEvent:
		m.errMsg = summarizeError(e.Err)
	}
}

// ─── Tool confirmation ────────────────────────────────────────────────────────

func (m *model) handleToolConfirm(e agent.ToolNeedsConfirmEvent) {
	if len(e.Options) > 0 {
		m.confirmState = &confirmState{
			callID:   e.CallID,
			prompt:   e.Prompt,
			options:  e.Options,
			selected: 0,
		}
	} else {
		// Binary choice
		items := []string{"[y] confirm", "[n] deny"}
		if e.AllowInSession {
			items = append(items, "[a] allow in session")
		}
		m.confirmState = &confirmState{
			callID:   e.CallID,
			prompt:   e.Prompt,
			options:  items,
			selected: 0,
		}
	}
}

// flushThinking pushes any buffered thinking text into the timeline as a new entry.
func (s *streamState) flushThinking() {
	if s.thinkingBuf.Len() > 0 {
		s.timeline = append(s.timeline, timelineEntry{
			kind: timelineThinking,
			text: s.thinkingBuf.String(),
		})
		s.thinkingBuf.Reset()
	}
}

// ─── Tool args parsing ────────────────────────────────────────────────────────

// toolArgs defines the common structure for tool arguments.
type toolArgs struct {
	Path    string `json:"path,omitempty"`
	Command string `json:"command,omitempty"`
	File    string `json:"file,omitempty"`
}

// parseToolArgs parses JSON-formatted tool arguments.
func parseToolArgs(argsJSON string) toolArgs {
	var args toolArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return toolArgs{Path: "[parse error]", Command: "[parse error]", File: "[parse error]"}
	}
	return args
}
