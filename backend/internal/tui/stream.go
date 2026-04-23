package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/xiaobaitu/soloqueue/internal/agent"
)

// ─── Stream ───────────────────────────────────────────────────────────────────

func (m *model) startStream(prompt string) tea.Cmd {
	askCtx, cancel := context.WithCancel(m.ctx)
	evCh, err := m.sess.AskStream(askCtx, prompt)
	if err != nil {
		cancel()
		return tea.Sequence(
			tea.Println(styleError.Render("✗ "+err.Error())),
			tea.Println(""),
		)
	}

	m.streaming = true
	m.streamCancel = cancel
	m.evCh = evCh
	m.contentBuf.Reset()
	m.reasonBuf.Reset()
	m.reasonBlocks = nil
	m.curThinkIdx = -1
	m.lastLineEmpty = false
	m.currentTool = ""
	m.toolArgs.Reset()
	m.streamPhase = ""
	m.toolExecMap = make(map[string]*toolExecInfo)
	m.spinnerFrame = 0
	m.streamStart = time.Now()

	// 添加用户输入到 scrollback
	m.addScrollLine("> "+prompt, styleUser)
	m.addScrollLine("", lipgloss.NewStyle())

	return tea.Batch(
		spinnerTick(),
		m.pollEvent(),
	)
}

func (m *model) pollEvent() tea.Cmd {
	ch := m.evCh
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return streamDoneMsg{}
		}
		return agentEventMsg{ev: ev}
	}
}

// flushContentDelta 将 delta 追加到 contentBuf，遇到 \n 立即写入 scrollback。
func (m *model) flushContentDelta(delta string) {
	combined := m.contentBuf.String() + delta
	m.contentBuf.Reset()

	for {
		idx := strings.Index(combined, "\n")
		if idx < 0 {
			break
		}
		line := combined[:idx]
		combined = combined[idx+1:]
		if line == "" {
			if !m.lastLineEmpty {
				m.addScrollLine("", lipgloss.NewStyle())
			}
		} else {
			m.addScrollLine(line, styleAI)
		}
	}

	m.contentBuf.WriteString(combined)
}

// flushContentBuf 将 contentBuf 中的半行立即写入 scrollback
func (m *model) flushContentBuf() {
	if m.contentBuf.Len() == 0 {
		return
	}
	line := m.contentBuf.String()
	m.contentBuf.Reset()
	m.addScrollLine(line, styleAI)
}

// ─── Agent event handling ─────────────────────────────────────────────────────

func (m *model) handleAgentEvent(ev agent.AgentEvent) []tea.Cmd {
	var cmds []tea.Cmd

	switch e := ev.(type) {

	case agent.ReasoningDeltaEvent:
		if m.curThinkIdx < 0 {
			m.startNewThinkBlock()
		}
		m.appendReasoning(e.Delta)
		m.streamPhase = "thinking"

	case agent.ContentDeltaEvent:
		if m.curThinkIdx >= 0 {
			m.finalizeCurrentThink()
		}
		m.streamPhase = "generating"
		m.flushContentDelta(e.Delta)

	case agent.ToolCallDeltaEvent:
		if m.curThinkIdx >= 0 {
			m.finalizeCurrentThink()
		}
		if e.Name != "" && e.Name != m.currentTool {
			m.flushContentBuf()
			m.currentTool = e.Name
			m.toolArgs.Reset()
		}
		if e.ArgsDelta != "" {
			m.toolArgs.WriteString(e.ArgsDelta)
		}
		m.streamPhase = "generating"

	case agent.ToolExecStartEvent:
		m.flushContentBuf()
		m.renderToolStartBlock(e.Name, e.Args)
		m.toolExecMap[e.CallID] = &toolExecInfo{
			name:  e.Name,
			args:  e.Args,
			start: time.Now(),
		}
		m.currentTool = e.Name
		m.toolArgs.Reset()
		m.streamPhase = "tool_exec"

	case agent.ToolExecDoneEvent:
		dur := time.Since(m.toolExecMap[e.CallID].start)
		info := &toolExecInfo{
			name:     e.Name,
			duration: dur,
			err:      e.Err,
			result:   e.Result,
			done:     true,
		}
		m.toolExecMap[e.CallID] = info
		m.renderToolDoneBlock(info)
		m.currentTool = ""
		m.streamPhase = "generating"

	case agent.IterationDoneEvent:
		// no-op

	case agent.DoneEvent:
		// 内容由 streamDoneMsg 统一提交

	case agent.ErrorEvent:
		m.addScrollLine("✗ "+e.Err.Error(), styleError)
	}

	return cmds
}
