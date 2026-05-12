package session

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

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
func (a *SessionAskAdapter) AskStream(ctx context.Context, prompt string, onIntermediate qqbot.OnIntermediateFunc) (*qqbot.AskStreamResult, error) {
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

	var contentBuf strings.Builder
	var sentLen int
	var reasoningContent string
	var imageURLs []string

	for ev := range eventCh {
		switch e := ev.(type) {
		case agent.ContentDeltaEvent:
			contentBuf.WriteString(e.Delta)

		case agent.ToolExecStartEvent:
			// A tool is about to execute — the content accumulated so far
			// that hasn't been sent yet is the LLM's intermediate response.
			if onIntermediate != nil && contentBuf.Len() > sentLen {
				intermediate := contentBuf.String()[sentLen:]
				onIntermediate(ctx, intermediate)
				sentLen = contentBuf.Len()
			}

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
			reasoningContent = e.ReasoningContent
		case agent.ErrorEvent:
			return nil, e.Err
		}
	}

	// Content that hasn't been sent as intermediate is the final reply.
	finalContent := contentBuf.String()[sentLen:]
	if finalContent == "" && reasoningContent != "" {
		finalContent = reasoningContent
	}

	return &qqbot.AskStreamResult{
		Content:          finalContent,
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
