package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/xiaobaitu/soloqueue/internal/agent"
)

// ─── Stream ───────────────────────────────────────────────────────────────────

func (m *model) startStream(prompt string) tea.Cmd {
	askCtx, cancel := context.WithCancel(m.ctx)
	evCh, err := m.sess.AskStream(askCtx, prompt)
	if err != nil {
		cancel()
		m.addScrollLine("✗ "+err.Error(), styleError)
		return nil
	}

	m.stream.streaming = true
	m.stream.streamCancel = cancel
	m.stream.evCh = evCh
	m.stream.contentBuf.Reset()
	m.reasoning.reasonBuf.Reset()
	m.reasoning.reasonBlocks = nil
	m.reasoning.curThinkIdx = -1
	m.lastLineEmpty = false
	m.tool.currentTool = ""
	m.tool.toolArgs.Reset()
	m.stream.streamPhase = ""
	m.tool.toolExecMap = make(map[string]*toolExecInfo)
	m.ui.spinnerFrame = 0
	m.stream.streamStart = time.Now()

	// 添加用户输入到 scrollback
	m.addScrollLine("> "+prompt, styleUser)
	m.addScrollLine("", lipgloss.NewStyle())

	return tea.Batch(
		m.spinnerTick(),
		m.pollEvent(),
	)
}

func (m *model) pollEvent() tea.Cmd {
	ch := m.stream.evCh
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
	combined := m.stream.contentBuf.String() + delta
	m.stream.contentBuf.Reset()

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

	m.stream.contentBuf.WriteString(combined)
}

// flushContentBuf 将 contentBuf 中的半行立即写入 scrollback
func (m *model) flushContentBuf() {
	if m.stream.contentBuf.Len() == 0 {
		return
	}
	line := m.stream.contentBuf.String()
	m.stream.contentBuf.Reset()
	m.addScrollLine(line, styleAI)
}

// ─── Agent event handling ─────────────────────────────────────────────────────

func (m *model) handleAgentEvent(ev agent.AgentEvent) []tea.Cmd {
	var cmds []tea.Cmd

	switch e := ev.(type) {

	case agent.ReasoningDeltaEvent:
		if m.reasoning.curThinkIdx < 0 {
			m.startNewThinkBlock()
		}
		m.appendReasoning(e.Delta)
		m.stream.streamPhase = "thinking"

	case agent.ContentDeltaEvent:
		if m.reasoning.curThinkIdx >= 0 {
			m.finalizeCurrentThink()
		}
		m.stream.streamPhase = "generating"
		m.flushContentDelta(e.Delta)

	case agent.ToolCallDeltaEvent:
		if m.reasoning.curThinkIdx >= 0 {
			m.finalizeCurrentThink()
		}
		if e.Name != "" && e.Name != m.tool.currentTool {
			m.flushContentBuf()
			m.tool.currentTool = e.Name
			m.tool.toolArgs.Reset()
		}
		if e.ArgsDelta != "" {
			m.tool.toolArgs.WriteString(e.ArgsDelta)
		}
		m.stream.streamPhase = "generating"

	case agent.ToolExecStartEvent:
		m.flushContentBuf()
		m.renderToolStartBlock(e.Name, e.Args, e.CallID)
		m.tool.toolExecMap[e.CallID] = &toolExecInfo{
			name:   e.Name,
			args:   e.Args,
			start:  time.Now(),
			callID: e.CallID,
		}
		m.tool.currentTool = e.Name
		m.tool.toolArgs.Reset()
		m.stream.streamPhase = "tool_exec"

	case agent.ToolExecDoneEvent:
		dur := time.Since(m.tool.toolExecMap[e.CallID].start)
		info := &toolExecInfo{
			name:     e.Name,
			duration: dur,
			err:      e.Err,
			result:   e.Result,
			done:     true,
			callID:   e.CallID,
		}
		m.tool.toolExecMap[e.CallID] = info
		m.renderToolDoneBlock(info)
		m.tool.currentTool = ""
		m.stream.streamPhase = "generating"

	case agent.IterationDoneEvent:
		// no-op

	case agent.DoneEvent:
		// 内容由 streamDoneMsg 统一提交

	case agent.ToolNeedsConfirmEvent:
		m.confirm = confirmState{
			active:         true,
			callID:         e.CallID,
			prompt:         e.Prompt,
			options:        e.Options,
			allowInSession: e.AllowInSession,
		}
		m.renderConfirmPrompt()
		// 重置 pendingExit 以允许正常操作
		m.ui.pendingExit = false
		// 暂停事件轮询，等待用户确认
		return cmds

	case agent.ErrorEvent:
		m.addScrollLine("✗ "+e.Err.Error(), styleError)
	}

	return cmds
}

// renderConfirmPrompt 将确认提示渲染到 scrollback
func (m *model) renderConfirmPrompt() {
	promptStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	detailStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("7"))

	m.addScrollLine("", lipgloss.NewStyle())
	m.addScrollLine("⚠ "+m.confirm.prompt, promptStyle)

	if len(m.confirm.options) > 0 {
		for i, opt := range m.confirm.options {
			m.addScrollLine(fmt.Sprintf("  [%d] %s", i+1, opt), detailStyle)
		}
	} else {
		m.addScrollLine("  [y] confirm  [n] deny", hintStyle)
		if m.confirm.allowInSession {
			m.addScrollLine("  [a] allow in session", hintStyle)
		}
	}
	m.addScrollLine("", lipgloss.NewStyle())
}
