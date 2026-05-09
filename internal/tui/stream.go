package tui

import (
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
		// If we already have content, flush it before new thinking starts
		// (this happens on iteration boundaries: content→thinking)
		if m.current.content.Len() > 0 {
			m.current.flushContent()
		}
		m.current.thinkingBuf.WriteString(e.Delta)

	case agent.ContentDeltaEvent:
		m.genPhase = phaseGenerating
		m.contentDeltas++
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
		// Flush any accumulated content and thinking before this tool
		m.current.flushContent()
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
		m.current.flushContent()
		m.current.flushThinking()
		m.promptTokens += e.Usage.PromptTokens
		m.outputTokens += e.Usage.CompletionTokens
		m.cacheHitTokens += e.Usage.PromptCacheHitTokens
		m.cacheMissTokens += e.Usage.PromptCacheMissTokens
		m.reasoningTokens += e.Usage.ReasoningTokens
		m.currentIter = e.Iter
		m.genPhase = phaseWaiting

	case agent.DelegationStartedEvent:
		m.activeDelegations = e.NumTasks

	case agent.DelegationCompletedEvent:
		if m.activeDelegations > 0 {
			m.activeDelegations--
		}

	case agent.DoneEvent:
		// handled by streamDoneMsg (channel close)

	case agent.ToolNeedsConfirmEvent:
		m.handleToolConfirm(e)

	case agent.ErrorEvent:
		m.errMsg = summarizeError(e.Err)
	}

	// Push updated metrics to the HTTP API layer.
	m.syncRuntimeMetrics()
}

// ─── Tool confirmation ────────────────────────────────────────────────────────

func (m *model) handleToolConfirm(e agent.ToolNeedsConfirmEvent) {
	const maxConfirmQueue = 16
	if len(m.confirmQueue) >= maxConfirmQueue {
		return // overflow protection; agent will timeout gracefully
	}

	var opts []string
	if len(e.Options) > 0 {
		opts = e.Options
	} else {
		// Binary choice
		opts = []string{"[y] confirm", "[n] deny"}
		if e.AllowInSession {
			opts = append(opts, "[a] allow in session")
		}
	}
	m.confirmQueue = append(m.confirmQueue, confirmState{
		callID:   e.CallID,
		prompt:   e.Prompt,
		options:  opts,
		selected: 0,
	})
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

// flushContent pushes any accumulated content text into the timeline as a new entry.
func (s *streamState) flushContent() {
	if s.content.Len() > 0 {
		s.timeline = append(s.timeline, timelineEntry{
			kind: timelineContent,
			text: s.content.String(),
		})
		s.content.Reset()
	}
}

// syncRuntimeMetrics pushes the current TUI model state to the shared HTTP
// RuntimeMetrics writer so /api/runtime reflects live data.
func (m *model) syncRuntimeMetrics() {
	rm := m.runtimeMetrics
	if rm == nil {
		return
	}
	// Phase
	phase := "ready"
	if m.loading {
		phase = "initializing"
	} else if m.isGenerating {
		phase = m.genPhase.String()
	}
	rm.SetPhase(phase)
	rm.SetTokens(int64(m.promptTokens), int64(m.outputTokens), int64(m.cacheHitTokens), int64(m.cacheMissTokens))
	rm.SetIter(m.currentIter)
	rm.SetContentDeltas(m.contentDeltas)
	rm.SetActiveDelegations(m.activeDelegations)

	// Context window %
	var pct int
	if m.sess != nil {
		cur, maxTokens, _ := m.sess.CW().TokenUsage()
		if maxTokens > 0 {
			pct = cur * 100 / maxTokens
		}
	}
	rm.SetContext(pct)
}
