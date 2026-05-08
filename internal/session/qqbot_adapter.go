package session

import (
	"context"
	"errors"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/qqbot"
)

// SessionAskAdapter adapts *SessionManager to the qqbot.SessionProvider interface.
// It wraps AskStream and extracts the final content/reasoning from the event stream.
type SessionAskAdapter struct {
	mgr *SessionManager
}

// NewQQBotAdapter creates a SessionProvider backed by the given SessionManager.
func NewQQBotAdapter(mgr *SessionManager) *SessionAskAdapter {
	return &SessionAskAdapter{mgr: mgr}
}

// AskStream implements qqbot.SessionProvider.
// It calls Session.AskStream, consumes the event stream, and returns
// the final content and reasoning content.
func (a *SessionAskAdapter) AskStream(ctx context.Context, prompt string) (*qqbot.AskStreamResult, error) {
	sess := a.mgr.Session()
	if sess == nil {
		return nil, errors.New("no active session")
	}

	eventCh, err := sess.AskStream(ctx, prompt)
	if err != nil {
		if errors.Is(err, ErrSessionBusy) {
			return nil, qqbot.ErrSessionBusy
		}
		return nil, err
	}

	var content string
	var reasoningContent string
	for ev := range eventCh {
		switch e := ev.(type) {
		case agent.DoneEvent:
			content = e.Content
			reasoningContent = e.ReasoningContent
		case agent.ErrorEvent:
			return nil, e.Err
		}
	}

	return &qqbot.AskStreamResult{
		Content:          content,
		ReasoningContent: reasoningContent,
	}, nil
}
