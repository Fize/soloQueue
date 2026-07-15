package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// ─── SessionConfirmStore ────────────────────────────────────────────────────

// SessionConfirmStore is an abstraction for session-level tool approval storage.
type SessionConfirmStore interface {
	// IsConfirmed checks if toolName has been approved for the current session.
	IsConfirmed(toolName string) bool

	// Confirm marks toolName as approved.
	Confirm(toolName string)

	// Clear clears all approval marks; called at Agent.Start to ensure each new session starts from scratch.
	Clear()
}

// ─── memoryConfirmStore ─────────────────────────────────────────────────────

// memoryConfirmStore is an in-memory implementation of SessionConfirmStore.
type memoryConfirmStore struct {
	mu    sync.RWMutex
	tools map[string]struct{}
}

// NewMemoryConfirmStore returns an in-memory implementation of SessionConfirmStore.
func NewMemoryConfirmStore() SessionConfirmStore {
	return &memoryConfirmStore{
		tools: make(map[string]struct{}),
	}
}

func (s *memoryConfirmStore) IsConfirmed(toolName string) bool {
	s.mu.RLock()
	_, ok := s.tools[toolName]
	s.mu.RUnlock()
	return ok
}

func (s *memoryConfirmStore) Confirm(toolName string) {
	s.mu.Lock()
	if s.tools == nil {
		s.tools = make(map[string]struct{})
	}
	s.tools[toolName] = struct{}{}
	s.mu.Unlock()
}

func (s *memoryConfirmStore) Clear() {
	s.mu.Lock()
	s.tools = make(map[string]struct{})
	s.mu.Unlock()
}

// Confirm injects a user's response to a pending tool_call confirmation into the agent.
func (a *Agent) Confirm(callID string, choice string) error {
	a.confirmMu.RLock()
	slot, ok := a.pendingConfirm[callID]
	a.confirmMu.RUnlock()
	if !ok {
		return fmt.Errorf("agent: no pending confirmation for %s", callID)
	}
	if !slot.done.CompareAndSwap(false, true) {
		return fmt.Errorf("agent: confirmation %s already resolved", callID)
	}
	select {
	case slot.ch <- choice:
		return nil
	default:
		return fmt.Errorf("agent: confirmation %s channel blocked", callID)
	}
}

// ToolSpecs returns a snapshot of all llm.ToolDef for tools registered with the current agent.
func (a *Agent) ToolSpecs() []llm.ToolDef {
	if a.tools == nil {
		return nil
	}
	specs := a.tools.Specs()
	level := a.EffectiveTaskLevel()
	if level == "" {
		return specs
	}

	filtered := make([]llm.ToolDef, 0, len(specs))
	for _, spec := range specs {
		if !isToolPruned(level, spec.Function.Name) {
			filtered = append(filtered, spec)
		}
	}
	return filtered
}

// isToolPruned matches the pruning map logic to determine if a tool is pruned at the current level.
func isToolPruned(level string, name string) bool {
	if level == "" {
		return false
	}
	lvl := strings.ToUpper(strings.TrimSpace(level))
	if strings.HasPrefix(lvl, "L0") {
		// L0-Conversation: only allow Read, Grep, Glob, WebSearch, RecallMemory, inspect_agent
		switch name {
		case "Read", "Grep", "Glob", "WebSearch", "RecallMemory", "inspect_agent":
			return false
		default:
			return true
		}
	}
	if strings.HasPrefix(lvl, "L1") {
		// L1-SimpleSingleFile: prune Skill and todo tools only.
		// delegate_* tools are NOT pruned here — the system prompt controls
		// delegation behavior. Pruning delegate tools breaks L2 supervisors
		// that receive L1 task classifications from direct user queries.
		if name == "Skill" {
			return true
		}
		return false
	}
	return false
}

const (
	choiceApprove        = tools.ChoiceApprove
	choiceAllowInSession = tools.ChoiceAllowInSession
)

// Agent does not directly implement tools.Locatable; it is wrapped by locatableAdapter for adaptation.

// routeConfirm creates a ConfirmForwarder closure used by DelegateTool. It
// registers a proxy confirmSlot, blocks until the user makes a choice, handles
// denial (empty choice), then forwards the choice to the child agent.
func (a *Agent) routeConfirm() iface.ConfirmForwarder {
	return func(fwdCtx context.Context, callID string, child iface.Locatable) (string, error) {
		slot := &confirmSlot{ch: make(chan string, 1)}
		a.confirmMu.Lock()
		a.pendingConfirm[callID] = slot
		a.confirmMu.Unlock()

		defer func() {
			a.confirmMu.Lock()
			delete(a.pendingConfirm, callID)
			a.confirmMu.Unlock()
		}()

		select {
		case choice := <-slot.ch:
			if choice == "" {
				a.userDenied.Store(true)
				if f, ok := a.taskCancel.Load().(context.CancelFunc); ok {
					f()
				}
			}
			if err := child.Confirm(callID, choice); err != nil {
				return "", err
			}
			return choice, nil
		case <-fwdCtx.Done():
			return "", fwdCtx.Err()
		}
	}
}