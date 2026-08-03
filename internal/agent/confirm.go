package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// ─── SessionConfirmStore ────────────────────────────────────────────────────

// SessionConfirmStore manages session-level tool approval state.
type SessionConfirmStore interface {
	IsConfirmed(toolName string) bool

	Confirm(toolName string)

	Clear() // called at Agent.Start for each new session
}

// ─── memoryConfirmStore ─────────────────────────────────────────────────────

type memoryConfirmStore struct {
	mu    sync.RWMutex
	tools map[string]struct{}
}

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

// Confirm injects a user's tool confirmation response.
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


func (a *Agent) ToolSpecs() []llm.ToolDef {
	if a.tools == nil {
		return nil
	}
	return a.tools.Specs()
}

const (
	choiceApprove        = tools.ChoiceApprove
	choiceAllowInSession = tools.ChoiceAllowInSession
)

// routeConfirm creates a ConfirmForwarder that proxies user confirmations to child agents.
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
