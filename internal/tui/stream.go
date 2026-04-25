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
		m.current.thoughts.WriteString(e.Delta)

	case agent.ContentDeltaEvent:
		m.current.content.WriteString(e.Delta)

	case agent.ToolCallDeltaEvent:
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
		m.current.curToolName = ""
		m.current.curToolArgs.Reset()
		tb := toolBlock{
			name:   e.Name,
			args:   e.Args,
			callID: e.CallID,
		}
		m.current.tools = append(m.current.tools, tb)
		m.current.toolExecMap[e.CallID] = &toolExecInfo{
			name:   e.Name,
			args:   e.Args,
			start:  time.Now(),
			callID: e.CallID,
		}

	case agent.ToolExecDoneEvent:
		dur := time.Duration(0)
		if info, ok := m.current.toolExecMap[e.CallID]; ok {
			dur = time.Since(info.start)
		}
		// Count non-empty result lines
		lineCount := 0
		for _, line := range strings.Split(e.Result, "\n") {
			if strings.TrimSpace(line) != "" {
				lineCount++
			}
		}
		// Update existing toolBlock
		for i := range m.current.tools {
			if m.current.tools[i].callID == e.CallID {
				m.current.tools[i].done = true
				m.current.tools[i].duration = dur
				m.current.tools[i].err = e.Err
				m.current.tools[i].lineCount = lineCount
				break
			}
		}

	case agent.IterationDoneEvent:
		// no-op

	case agent.DoneEvent:
		// handled by streamDoneMsg (channel close)

	case agent.ToolNeedsConfirmEvent:
		m.handleToolConfirm(e)

	case agent.ErrorEvent:
		m.current.content.WriteString(errorStyle.Render("✗ " + e.Err.Error()))
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
		return toolArgs{}
	}
	return args
}
