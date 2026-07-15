package session

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/conversationlog"
	"github.com/xiaobaitu/soloqueue/internal/qqbot"
)

// qqbotAdapterBase provides shared logic for QQ Bot session adapters.
// Both SessionAskAdapter and L2QQBotAdapter embed it to avoid code duplication.
type qqbotAdapterBase struct {
	log           *logger.Logger
	supervisorsFn func() []*agent.Supervisor
	registry      *agent.Registry
}

// SetSupervisorsFn sets the supervisor accessor for reaping child agents on cancel.
func (b *qqbotAdapterBase) SetSupervisorsFn(fn func() []*agent.Supervisor) {
	b.supervisorsFn = fn
}

// SetRegistry sets the agent registry for agent lifecycle management on cancel.
func (b *qqbotAdapterBase) SetRegistry(reg *agent.Registry) {
	b.registry = reg
}

// reapSupervisorChildren stops any orphaned supervisor children still in Processing state.
func (b *qqbotAdapterBase) reapSupervisorChildren(tag string) {
	if b.supervisorsFn == nil {
		return
	}
	for _, sv := range b.supervisorsFn() {
		for _, child := range sv.Children() {
			if child.State() == agent.StateProcessing {
				if reapErr := sv.ReapChild(child.InstanceID, 10*time.Second); reapErr != nil {
					b.log.WarnContext(context.Background(), logger.CatApp, tag+": reap child failed",
						"instance_id", child.InstanceID,
						"err", reapErr.Error(),
					)
				}
			}
		}
	}
}

// cancelAndRestart force-kills the session, stops the agent, and restarts it to idle.
func (b *qqbotAdapterBase) cancelAndRestart(sess *Session, reason string) {
	sess.ForceKill(reason)
	_ = sess.Agent.Stop(5 * time.Second)
	if err := sess.Agent.Start(context.Background()); err != nil {
		b.log.WarnContext(context.Background(), logger.CatApp, "cancel: restart agent failed",
			"session_id", sess.ID,
			"err", err.Error(),
		)
	}
	b.reapSupervisorChildren("cancel")
}

// compactAndReap compacts the session and reaps orphaned supervisor children.
func (b *qqbotAdapterBase) compactAndReap(ctx context.Context, sess *Session) error {
	_, err := sess.Compact(ctx)
	if err != nil {
		return err
	}
	b.reapSupervisorChildren("compact")
	return nil
}

// consumeAskStreamEvents drains the event channel and builds the AskStreamResult.
// This is the shared event loop used by both SessionAskAdapter and L2QQBotAdapter.
func (b *qqbotAdapterBase) consumeAskStreamEvents(
	ctx context.Context,
	sess *Session,
	eventCh <-chan iface.AgentEvent,
	onIntermediate qqbot.OnIntermediateFunc,
) (*qqbot.AskStreamResult, error) {
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
			if onIntermediate != nil && contentBuf.Len() > sentLen {
				intermediate := contentBuf.String()[sentLen:]
				onIntermediate(ctx, intermediate)
				sentLen = contentBuf.Len()
			}

		case agent.ToolNeedsConfirmEvent:
			b.log.InfoContext(ctx, logger.CatApp, "qqbot adapter: auto-approving tool",
				"session_id", sess.ID,
				"tool_name", e.Name,
				"call_id", e.CallID,
			)
			if err := sess.Agent.Confirm(e.CallID, "approve"); err != nil {
				b.log.WarnContext(ctx, logger.CatApp, "qqbot adapter: auto-approve failed",
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
							FileType: 1,
							URL:      url,
						})
					}
				}
			} else if e.Name == "SendFile" && e.Result != "" {
				res := parseSendFileResult(e.Result)
				if res != nil {
					ftype := 4
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
					b64 := ""
					if res.Path != "" {
						if data, err := os.ReadFile(res.Path); err == nil {
							b64 = base64.StdEncoding.EncodeToString(data)
						}
					}
					mediaList = append(mediaList, qqbot.PendingMedia{
						FileType:   ftype,
						URL:        res.URL,
						Base64Data: b64,
						FileName:   res.FileName,
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

	if sess.isCancelledAndReset() {
		return nil, qqbot.ErrTaskCancelled
	}

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

// saveUploadedFileToSession saves an uploaded file to the session's downloads directory.
func (b *qqbotAdapterBase) saveUploadedFileToSession(sess *Session, filename string, content []byte) (string, error) {
	workDir := sess.Agent.WorkDir
	if workDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		workDir = filepath.Join(home, ".soloqueue")
	}
	downloadsDir := filepath.Join(workDir, "downloads")
	if err := os.MkdirAll(downloadsDir, 0o755); err != nil {
		return "", err
	}
	destPath := filepath.Join(downloadsDir, filename)
	if err := os.WriteFile(destPath, content, 0o644); err != nil {
		return "", err
	}
	return destPath, nil
}

// ─── SessionAskAdapter (L1) ──────────────────────────────────────────────────

// SessionAskAdapter adapts *SessionManager to the qqbot.SessionProvider interface.
type SessionAskAdapter struct {
	qqbotAdapterBase
	mgr *SessionManager
}

// NewQQBotAdapter creates a SessionProvider backed by the given SessionManager.
func NewQQBotAdapter(mgr *SessionManager, log *logger.Logger) *SessionAskAdapter {
	return &SessionAskAdapter{
		qqbotAdapterBase: qqbotAdapterBase{log: log},
		mgr:              mgr,
	}
}

// CancelCurrent implements qqbot.SessionProvider.CancelCurrent.
func (a *SessionAskAdapter) CancelCurrent(reason string) error {
	sess := a.mgr.Session()
	if sess == nil {
		return errors.New("no active session")
	}
	a.cancelAndRestart(sess, reason)
	return nil
}

// Clear implements qqbot.SessionProvider.Clear.
func (a *SessionAskAdapter) Clear(ctx context.Context) error {
	sess := a.mgr.Session()
	if sess == nil {
		return errors.New("no active session")
	}
	return sess.Clear()
}

// Compact implements qqbot.SessionProvider.Compact.
func (a *SessionAskAdapter) Compact(ctx context.Context) error {
	sess := a.mgr.Session()
	if sess == nil {
		return errors.New("no active session")
	}
	return a.compactAndReap(ctx, sess)
}

// AskStream implements qqbot.SessionProvider.
func (a *SessionAskAdapter) AskStream(ctx context.Context, prompt string, onIntermediate qqbot.OnIntermediateFunc) (*qqbot.AskStreamResult, error) {
	sess := a.mgr.Session()
	if sess == nil {
		return nil, errors.New("no active session")
	}
	sess.SetIsQBot(true)

	if a.registry != nil {
		_ = a.registry.Register(sess.Agent)
	}

	cw := sess.CW()
	if cw != nil {
		tokens, _, _ := cw.TokenUsage()
		a.log.InfoContext(ctx, logger.CatApp, "qqbot adapter: session CW state",
			"session_id", sess.ID,
			"cw_tokens", tokens,
			"cw_msgs", cw.Len(),
		)
	}

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

	return a.consumeAskStreamEvents(ctx, sess, eventCh, onIntermediate)
}

// SaveUploadedFile saves an uploaded file to the session's workspace downloads folder.
func (a *SessionAskAdapter) SaveUploadedFile(ctx context.Context, filename string, content []byte) (string, error) {
	sess := a.mgr.Session()
	if sess == nil {
		return "", errors.New("no active session")
	}
	return a.saveUploadedFileToSession(sess, filename, content)
}

// ─── L2QQBotAdapter (L2) ─────────────────────────────────────────────────────

// L2QQBotAdapter adapts *L2SessionStore to the qqbot.SessionProvider interface.
type L2QQBotAdapter struct {
	qqbotAdapterBase
	l2Store       *L2SessionStore
	botAppID      string
	bindAgent     string
	memoryManager *conversationlog.Manager
}

// NewL2QQBotAdapter creates a SessionProvider backed by an L2 session.
func NewL2QQBotAdapter(l2Store *L2SessionStore, botAppID, bindAgent string, log *logger.Logger, mm *conversationlog.Manager) *L2QQBotAdapter {
	return &L2QQBotAdapter{
		qqbotAdapterBase: qqbotAdapterBase{log: log},
		l2Store:          l2Store,
		botAppID:         botAppID,
		bindAgent:        bindAgent,
		memoryManager:    mm,
	}
}

// getSession returns the underlying L2 session, creating it if it doesn't exist.
func (a *L2QQBotAdapter) getSession(ctx context.Context) (*Session, error) {
	sessionID := "qqbot-" + a.botAppID
	sess, err := a.l2Store.Get(ctx, sessionID)
	if err != nil {
		_, createErr := a.l2Store.Create(ctx, sessionID, a.bindAgent, "", a.l2Store.WorkDir())
		if createErr != nil {
			return nil, fmt.Errorf("create L2 session: %w", createErr)
		}
		sess, err = a.l2Store.Activate(ctx, sessionID)
		if err != nil {
			return nil, fmt.Errorf("activate L2 session: %w", err)
		}
	}

	if a.memoryManager != nil {
		sess.SetMemoryManager(a.memoryManager)
	}

	return sess, nil
}

// CancelCurrent implements qqbot.SessionProvider.CancelCurrent.
func (a *L2QQBotAdapter) CancelCurrent(reason string) error {
	sess, err := a.getSession(context.Background())
	if err != nil {
		return err
	}
	a.cancelAndRestart(sess, reason)
	return nil
}

// Clear implements qqbot.SessionProvider.Clear.
func (a *L2QQBotAdapter) Clear(ctx context.Context) error {
	sess, err := a.getSession(ctx)
	if err != nil {
		return err
	}
	return sess.Clear()
}

// Compact implements qqbot.SessionProvider.Compact.
func (a *L2QQBotAdapter) Compact(ctx context.Context) error {
	sess, err := a.getSession(ctx)
	if err != nil {
		return err
	}
	return a.compactAndReap(ctx, sess)
}

// AskStream implements qqbot.SessionProvider.
func (a *L2QQBotAdapter) AskStream(ctx context.Context, prompt string, onIntermediate qqbot.OnIntermediateFunc) (*qqbot.AskStreamResult, error) {
	sess, err := a.getSession(ctx)
	if err != nil {
		return nil, err
	}
	sess.SetIsQBot(true)

	if a.registry != nil {
		_ = a.registry.Register(sess.Agent)
	}

	cw := sess.CW()
	if cw != nil {
		tokens, _, _ := cw.TokenUsage()
		a.log.InfoContext(ctx, logger.CatApp, "qqbot L2 adapter: session CW state",
			"session_id", sess.ID,
			"cw_tokens", tokens,
			"cw_msgs", cw.Len(),
		)
	}

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

	return a.consumeAskStreamEvents(ctx, sess, eventCh, onIntermediate)
}

// SaveUploadedFile saves an uploaded file to the session's workspace downloads folder.
func (a *L2QQBotAdapter) SaveUploadedFile(ctx context.Context, filename string, content []byte) (string, error) {
	sess, err := a.getSession(ctx)
	if err != nil {
		return "", err
	}
	return a.saveUploadedFileToSession(sess, filename, content)
}

// ─── ErrorQQBotAdapter ───────────────────────────────────────────────────────

// ErrorQQBotAdapter adapts a configuration error to the qqbot.SessionProvider interface.
type ErrorQQBotAdapter struct {
	errMsg string
}

// NewErrorQQBotAdapter creates a SessionProvider that immediately reports the given error.
func NewErrorQQBotAdapter(errMsg string) *ErrorQQBotAdapter {
	return &ErrorQQBotAdapter{errMsg: errMsg}
}

func (a *ErrorQQBotAdapter) CancelCurrent(reason string) error { return nil }

func (a *ErrorQQBotAdapter) Clear(ctx context.Context) error { return nil }

func (a *ErrorQQBotAdapter) Compact(ctx context.Context) error { return nil }

func (a *ErrorQQBotAdapter) AskStream(ctx context.Context, prompt string, onIntermediate qqbot.OnIntermediateFunc) (*qqbot.AskStreamResult, error) {
	return &qqbot.AskStreamResult{Content: a.errMsg}, nil
}

func (a *ErrorQQBotAdapter) SaveUploadedFile(ctx context.Context, filename string, content []byte) (string, error) {
	return "", errors.New(a.errMsg)
}

// ─── Parsers ─────────────────────────────────────────────────────────────────

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

// parseSendFileResult extracts metadata from a SendFile tool result JSON.
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

type sendFileToolResult struct {
	Status   string `json:"status"`
	FileName string `json:"file_name"`
	FileType string `json:"file_type"`
	Path     string `json:"path"`
	URL      string `json:"url"`
}
