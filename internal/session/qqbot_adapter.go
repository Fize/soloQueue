package session

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/qqbot"
)

// SessionAskAdapter adapts *SessionManager to the qqbot.SessionProvider interface.
// It wraps AskStream and extracts the final content/reasoning from the event stream.
type SessionAskAdapter struct {
	mgr *SessionManager
	log *logger.Logger
}

// NewQQBotAdapter creates a SessionProvider backed by the given SessionManager.
func NewQQBotAdapter(mgr *SessionManager, log *logger.Logger) *SessionAskAdapter {
	return &SessionAskAdapter{mgr: mgr, log: log}
}

// Clear implements qqbot.SessionProvider.Clear.
// It clears the session context (conversation history).
func (a *SessionAskAdapter) Clear(ctx context.Context) error {
	sess := a.mgr.Session()
	if sess == nil {
		return errors.New("no active session")
	}
	return sess.Clear()
}

// AskStream implements qqbot.SessionProvider.
// It calls Session.AskStream, consumes the event stream, and returns
// the final content and reasoning content.
func (a *SessionAskAdapter) AskStream(ctx context.Context, prompt string) (*qqbot.AskStreamResult, error) {
	sess := a.mgr.Session()
	if sess == nil {
		return nil, errors.New("no active session")
	}

	// Log CW state for debugging shared-session context.
	cw := sess.CW()
	if cw != nil {
		tokens, _, _ := cw.TokenUsage()
		a.log.InfoContext(ctx, logger.CatApp, "qqbot adapter: session CW state",
			"session_id", sess.ID,
			"cw_tokens", tokens,
			"cw_msgs", cw.Len(),
		)
	}

	// QQ bot always bypasses tool confirmations at the agent level.
	ctx = agent.WithBypassConfirmCtx(ctx)

	eventCh, err := sess.AskStream(ctx, prompt)
	if err != nil {
		if errors.Is(err, ErrSessionBusy) {
			return nil, qqbot.ErrSessionBusy
		}
		return nil, err
	}

	var content string
	var reasoningContent string
	var imageURLs []string
	for ev := range eventCh {
		switch e := ev.(type) {
		case agent.ToolNeedsConfirmEvent:
			// QQ bot has no interactive UI — auto-approve all confirmations.
			a.log.InfoContext(ctx, logger.CatApp, "qqbot adapter: auto-approving tool",
				"session_id", sess.ID,
				"tool_name", e.Name,
				"call_id", e.CallID,
			)
			if err := sess.Agent.Confirm(e.CallID, "approve"); err != nil {
				a.log.WarnContext(ctx, logger.CatApp, "qqbot adapter: auto-approve failed",
					"session_id", sess.ID,
					"call_id", e.CallID,
					"err", err.Error(),
				)
			}
		case agent.ToolExecDoneEvent:
			if e.Name == "ImageGenerate" && e.Result != "" {
				urls := parseImageGenResult(e.Result)
				if len(urls) > 0 {
					imageURLs = append(imageURLs, urls...)
				}
			}
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
		ImageURLs:        imageURLs,
	}, nil
}

// parseImageGenResult extracts image URLs from an ImageGenerate tool result JSON.
func parseImageGenResult(raw string) []string {
	var r struct {
		Status    string   `json:"status"`
		ImageURLs []string `json:"image_urls"`
	}
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return nil
	}
	if r.Status != "completed" || len(r.ImageURLs) == 0 {
		return nil
	}
	return r.ImageURLs
}
