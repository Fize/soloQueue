package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/manifoldco/promptui"

	"github.com/xiaobaitu/soloqueue/internal/agent"
)

// ─── Stream done ──────────────────────────────────────────────────────────────

func (a *App) handleStreamDone(streamErr error) {
	// Flush remaining content buffer
	if tail := a.contentBuf.String(); tail != "" {
		fmt.Println(Styled(tail, styleAI))
	}
	a.finalizeCurrentThink()
	if streamErr != nil && streamErr != context.Canceled {
		fmt.Println(Styled("✗ "+streamErr.Error(), styleError))
	}
	if !a.lastLineEmpty {
		fmt.Println()
	}
	fmt.Println() // ensure blank line before next prompt

	// Reset stream state
	a.resetStreamState()
}

// resetStreamState resets all stream-related fields between turns.
func (a *App) resetStreamState() {
	a.contentBuf.Reset()
	a.reasonBuf.Reset()
	a.lastLineEmpty = false
	a.currentTool = ""
	a.toolArgs.Reset()
	a.streamPhase = ""
	a.toolExecMap = make(map[string]*toolExecInfo)
	a.streamStart = time.Now()
	a.reasonBlocks = nil
	a.curThinkIdx = -1
}

// flushContentDelta appends delta to contentBuf, flushing complete lines.
func (a *App) flushContentDelta(delta string) {
	combined := a.contentBuf.String() + delta
	a.contentBuf.Reset()

	for {
		idx := strings.Index(combined, "\n")
		if idx < 0 {
			break
		}
		line := combined[:idx]
		combined = combined[idx+1:]
		if line == "" {
			if !a.lastLineEmpty {
				fmt.Println()
			}
		} else {
			fmt.Println(Styled(line, styleAI))
		}
		a.lastLineEmpty = (line == "")
	}

	a.contentBuf.WriteString(combined)
}

// flushContentBuf flushes the content buffer as a partial line.
func (a *App) flushContentBuf() {
	if a.contentBuf.Len() == 0 {
		return
	}
	line := a.contentBuf.String()
	a.contentBuf.Reset()
	fmt.Println(Styled(line, styleAI))
}

// ─── Agent event handling ─────────────────────────────────────────────────────

func (a *App) handleAgentEvent(ev agent.AgentEvent) {
	switch e := ev.(type) {

	case agent.ReasoningDeltaEvent:
		if a.curThinkIdx < 0 {
			a.startNewThinkBlock()
		}
		a.appendReasoning(e.Delta)
		a.streamPhase = "thinking"

	case agent.ContentDeltaEvent:
		if a.curThinkIdx >= 0 {
			a.finalizeCurrentThink()
		}
		a.streamPhase = "generating"
		a.flushContentDelta(e.Delta)

	case agent.ToolCallDeltaEvent:
		if a.curThinkIdx >= 0 {
			a.finalizeCurrentThink()
		}
		if e.Name != "" && e.Name != a.currentTool {
			a.flushContentBuf()
			a.currentTool = e.Name
			a.toolArgs.Reset()
		}
		if e.ArgsDelta != "" {
			a.toolArgs.WriteString(e.ArgsDelta)
		}
		a.streamPhase = "generating"

	case agent.ToolExecStartEvent:
		a.flushContentBuf()
		a.renderToolStartBlock(e.Name, e.Args, e.CallID)
		a.toolExecMap[e.CallID] = &toolExecInfo{
			name:   e.Name,
			args:   e.Args,
			start:  time.Now(),
			callID: e.CallID,
		}
		a.currentTool = e.Name
		a.toolArgs.Reset()
		a.streamPhase = "tool_exec"

	case agent.ToolExecDoneEvent:
		dur := time.Since(a.toolExecMap[e.CallID].start)
		info := &toolExecInfo{
			name:     e.Name,
			duration: dur,
			err:      e.Err,
			result:   e.Result,
			done:     true,
			callID:   e.CallID,
		}
		a.toolExecMap[e.CallID] = info
		a.renderToolDoneBlock(info)
		a.currentTool = ""
		a.streamPhase = "generating"

	case agent.IterationDoneEvent:
		// no-op

	case agent.DoneEvent:
		// handled by streamDone (channel close)

	case agent.ToolNeedsConfirmEvent:
		a.handleToolConfirm(e)

	case agent.ErrorEvent:
		fmt.Println(Styled("✗ "+e.Err.Error(), styleError))
	}
}

// ─── Tool confirmation ────────────────────────────────────────────────────────

func (a *App) handleToolConfirm(e agent.ToolNeedsConfirmEvent) {
	fmt.Println()
	fmt.Println(Styled("⚠ "+e.Prompt, Fg(11), BOLD))

	if len(e.Options) > 0 {
		// Multi-option: use promptui.Select
		sel := promptui.Select{
			Label: "Choose",
			Items: e.Options,
			Size:  len(e.Options),
		}
		_, choice, err := sel.Run()
		if err != nil {
			// User cancelled — deny
			_ = a.sess.Agent.Confirm(e.CallID, string(agent.ChoiceDeny))
			return
		}
		if err := a.sess.Agent.Confirm(e.CallID, choice); err != nil {
			fmt.Println(Styled("✗ confirm error: "+err.Error(), styleError))
		}
	} else {
		// Binary: use promptui.Prompt with IsConfirm
		items := []string{"[y] confirm", "[n] deny"}
		if e.AllowInSession {
			items = append(items, "[a] allow in session")
		}
		sel := promptui.Select{
			Label: "Choose",
			Items: items,
			Size:  len(items),
		}
		idx, _, err := sel.Run()
		if err != nil {
			_ = a.sess.Agent.Confirm(e.CallID, string(agent.ChoiceDeny))
			return
		}
		var choice string
		switch idx {
		case 0:
			choice = string(agent.ChoiceApprove)
		case 1:
			choice = string(agent.ChoiceDeny)
		case 2:
			choice = string(agent.ChoiceAllowInSession)
		default:
			choice = string(agent.ChoiceDeny)
		}
		if err := a.sess.Agent.Confirm(e.CallID, choice); err != nil {
			fmt.Println(Styled("✗ confirm error: "+err.Error(), styleError))
		}
	}

	fmt.Println()
}
