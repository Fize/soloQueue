package session

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/qqbot"
)

// SessionAskAdapter adapts *SessionManager to the qqbot.SessionProvider interface.
// It wraps AskStream and extracts the final content/reasoning from the event stream.
type SessionAskAdapter struct {
	mgr           *SessionManager
	log           *logger.Logger
	supervisorsFn func() []*agent.Supervisor // optional: reap supervisor children on cancel
	registry      *agent.Registry            // registry for cancel stop+unregister
}

// NewQQBotAdapter creates a SessionProvider backed by the given SessionManager.
func NewQQBotAdapter(mgr *SessionManager, log *logger.Logger) *SessionAskAdapter {
	return &SessionAskAdapter{mgr: mgr, log: log}
}

// SetSupervisorsFn sets the supervisor accessor for reaping child agents on cancel.
func (a *SessionAskAdapter) SetSupervisorsFn(fn func() []*agent.Supervisor) {
	a.supervisorsFn = fn
}

// SetRegistry sets the agent registry for agent lifecycle management on cancel.
func (a *SessionAskAdapter) SetRegistry(reg *agent.Registry) {
	a.registry = reg
}

// CancelCurrent implements qqbot.SessionProvider.CancelCurrent.
// 1) Cancels the active AskStream context (stops L1 + propagates to delegated agents).
// 2) Stops the L1 agent and unregisters it from the registry (restores initial state).
// 3) As a safety net, reaps any supervisor children still in StateProcessing.
func (a *SessionAskAdapter) CancelCurrent(reason string) error {
	sess := a.mgr.Session()
	if sess == nil {
		return errors.New("no active session")
	}

	err := sess.CancelCurrent(reason)
	if err != nil && !errors.Is(err, ErrNoActiveTask) {
		return err
	}

	// Stop L1 agent and restart to idle state (instead of unregistering).
	// Keeping L1 in the registry lets the frontend show it as a solid-idle node
	// rather than a dashed-border placeholder after cancellation.
	_ = sess.Agent.Stop(5 * time.Second)
	if err := sess.Agent.Start(context.Background()); err != nil {
		a.log.WarnContext(context.Background(), logger.CatApp, "cancel: restart agent failed",
			"session_id", sess.ID,
			"err", err.Error(),
		)
	}

	// Safety net: reap any orphaned supervisor children
	if a.supervisorsFn != nil {
		for _, sv := range a.supervisorsFn() {
			for _, child := range sv.Children() {
				if child.State() == agent.StateProcessing {
					a.log.DebugContext(context.Background(), logger.CatApp, "cancel: reaping supervisor child",
						"instance_id", child.InstanceID,
					)
					if reapErr := sv.ReapChild(child.InstanceID, 10*time.Second); reapErr != nil {
						a.log.WarnContext(context.Background(), logger.CatApp, "cancel: reap child failed",
							"instance_id", child.InstanceID,
							"err", reapErr.Error(),
						)
					}
				}
			}
		}
	}

	return nil
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

	// Re-register agent if it was unregistered by CancelCurrent.
	// This ensures the web UI shows the agent when a new task begins.
	if a.registry != nil {
		_ = a.registry.Register(sess.Agent)
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
		if errors.Is(err, ErrQueued) {
			return nil, qqbot.ErrQueued
		}
		return nil, err
	}

	var contentBuf strings.Builder
	var sentLen int
	var reasoningContent string
	var imageURLs []string
	var mediaList []qqbot.PendingMedia

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
			if (e.Name == "ImageGenerate" || e.Name == "ImageEdit") && e.Result != "" {
				urls := parseImageGenResult(e.Result)
				if len(urls) > 0 {
					imageURLs = append(imageURLs, urls...)
					for _, url := range urls {
						mediaList = append(mediaList, qqbot.PendingMedia{
							FileType: 1, // FileTypeImage
							URL:      url,
						})
					}
				}
			} else if e.Name == "SendFile" && e.Result != "" {
				res := parseSendFileResult(e.Result)
				if res != nil {
					ftype := 4 // Default: file
					switch res.FileType {
					case "image":
						ftype = 1
					case "video":
						ftype = 2
					case "voice":
						ftype = 3
					case "file":
						ftype = 4
					}
					b64 := res.Base64Data
					if b64 == "" && res.Path != "" {
						if data, err := os.ReadFile(res.Path); err == nil {
							b64 = base64.StdEncoding.EncodeToString(data)
						}
					}
					mediaList = append(mediaList, qqbot.PendingMedia{
						FileType:   ftype,
						URL:        res.URL,
						Base64Data: b64,
					})
				}
			}
		case agent.DoneEvent:
			reasoningContent = e.ReasoningContent
		case agent.ErrorEvent:
			_ = sess.isCancelledAndReset()
			return nil, e.Err
		}
	}

	// Check if the event loop exited due to cancellation (forwarder set cancelled flag).
	if sess.isCancelledAndReset() {
		return nil, qqbot.ErrTaskCancelled
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
		MediaList:        mediaList,
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

type sendFileToolResult struct {
	Status     string `json:"status"`
	FileName   string `json:"file_name"`
	FileType   string `json:"file_type"`
	Base64Data string `json:"base64_data"`
	Path       string `json:"path"`
	URL        string `json:"url"`
}

func parseSendFileResult(raw string) *sendFileToolResult {
	var r sendFileToolResult
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return nil
	}
	if r.Status != "success" {
		return nil
	}
	return &r
}
