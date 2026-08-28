package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/channel"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/infra/telemetry"
	"github.com/xiaobaitu/soloqueue/internal/memory/conversation"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
)

func withChannelTelemetry(ctx context.Context) context.Context {
	if meta, ok := channel.ChatMetaFromContext(ctx); ok {
		origin := meta.Channel
		if origin == "qqbot" {
			origin = telemetry.OriginQQ
		}
		return telemetry.WithTelemetryMetadata(ctx, telemetry.Metadata{Origin: origin})
	}
	return ctx
}

// channelAdapterBase provides shared logic for messaging channel adapters.
type channelAdapterBase struct {
	log           *logger.Logger
	supervisorsFn func() []*agent.Supervisor
	registry      *agent.Registry
}

// SetSupervisorsFn sets the supervisor accessor for reaping child agents on cancel.
func (b *channelAdapterBase) SetSupervisorsFn(fn func() []*agent.Supervisor) {
	b.supervisorsFn = fn
}

// SetRegistry sets the agent registry for agent lifecycle management on cancel.
func (b *channelAdapterBase) SetRegistry(reg *agent.Registry) {
	b.registry = reg
}

// reapSupervisorChildren stops any orphaned supervisor children that are not
// cleanly idle. Children in StateStopped or StateStopping were previously
// skipped, causing them to permanently leak in the supervisor children map
// and registry after /cancel fired. ReapChild unregisters and stops them
// regardless of current state.
func (b *channelAdapterBase) reapSupervisorChildren(tag string) {
	if b.supervisorsFn == nil {
		return
	}
	for _, sv := range b.supervisorsFn() {
		for _, child := range sv.Children() {
			if child.State() == agent.StateIdle {
				continue
			}
			if reapErr := sv.ReapChild(child.InstanceID, 10*time.Second); reapErr != nil {
				b.log.WarnContext(context.Background(), logger.CatApp, tag+": reap child failed",
					"instance_id", child.InstanceID,
					"state", child.State().String(),
					"err", reapErr.Error(),
				)
			}
		}
	}
}

// cancelCurrent cancels the current request tree while keeping the session and
// its agents reusable. Reaping is defensive cleanup for delegated children
// whose implementations fail to exit promptly after their context is cancelled.
func (b *channelAdapterBase) cancelCurrent(sess *Session, reason string) error {
	err := sess.CancelCurrent(reason)
	b.reapSupervisorChildren("cancel")
	if errors.Is(err, ErrNoActiveTask) {
		return nil
	}
	return err
}

// compactAndReap compacts the session and reaps orphaned supervisor children.
func (b *channelAdapterBase) compactAndReap(ctx context.Context, sess *Session) error {
	_, err := sess.Compact(ctx)
	if err != nil {
		return err
	}
	b.reapSupervisorChildren("compact")
	return nil
}

// askChannelStream preserves the immutable channel route owned by the caller.
// Same-route messages retain the existing pending-merge behavior; a different
// route waits instead of entering Session.pending, whose string-only payload
// cannot remember which bridge must deliver the eventual response.
func (b *channelAdapterBase) askChannelStream(ctx context.Context, sess *Session, prompt string) (<-chan iface.AgentEvent, func(), error) {
	route := channelRouteKey(ctx)
	structuredInput := ctx.Value(ctxwin.FilesContextKey) != nil || ctx.Value(ctxwin.ImageContextKey) != nil
	for {
		sess.channelRouteMu.Lock()
		if sess.channelRouteKey != "" && sess.channelRouteKey != route {
			sess.channelRouteMu.Unlock()
			timer := time.NewTimer(25 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, nil, ctx.Err()
			case <-timer.C:
			}
			continue
		}
		sameRouteActive := sess.channelRouteKey == route && sess.channelRouteOwners > 0
		if sess.channelRouteKey == "" {
			sess.channelRouteKey = route
		}
		sess.channelRouteOwners++
		sess.channelRouteMu.Unlock()

		askCtx := ctx
		if !sameRouteActive || structuredInput {
			askCtx = WithRejectBusyQueue(ctx)
		}
		eventCh, err := sess.AskStream(askCtx, prompt)
		if err != nil {
			releaseChannelRoute(sess, route)
			if errors.Is(err, ErrQueued) && (!sameRouteActive || structuredInput) {
				timer := time.NewTimer(25 * time.Millisecond)
				select {
				case <-ctx.Done():
					timer.Stop()
					return nil, nil, ctx.Err()
				case <-timer.C:
				}
				continue
			}
			return nil, nil, err
		}
		return eventCh, func() { releaseChannelRoute(sess, route) }, nil
	}
}

func channelRouteKey(ctx context.Context) string {
	meta, ok := channel.ChatMetaFromContext(ctx)
	if !ok {
		return "channel"
	}
	return strings.Join([]string{meta.Channel, meta.AccountID, meta.ConversationID, meta.UserID}, "\x00")
}

func releaseChannelRoute(sess *Session, route string) {
	sess.channelRouteMu.Lock()
	defer sess.channelRouteMu.Unlock()
	if sess.channelRouteKey != route || sess.channelRouteOwners == 0 {
		return
	}
	sess.channelRouteOwners--
	if sess.channelRouteOwners == 0 {
		sess.channelRouteKey = ""
	}
}

// consumeAskStreamEvents drains the event channel and builds the AskStreamResult.
// This is the shared event loop used by both L1 and L2 channel adapters.
func (b *channelAdapterBase) consumeAskStreamEvents(
	ctx context.Context,
	sess *Session,
	eventCh <-chan iface.AgentEvent,
	onIntermediate channel.OnIntermediateFunc,
) (*channel.AskStreamResult, error) {
	return b.consumeAskStreamEventsWithDelegation(ctx, sess, eventCh, onIntermediate, nil)
}

func (b *channelAdapterBase) consumeAskStreamEventsWithDelegation(
	ctx context.Context,
	sess *Session,
	eventCh <-chan iface.AgentEvent,
	onIntermediate channel.OnIntermediateFunc,
	onDelegationStarted func(),
) (*channel.AskStreamResult, error) {
	var contentBuf strings.Builder
	var sentLen int
	var reasoningContent string
	var imageURLs []string
	var mediaList []channel.PendingMedia

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

		case agent.DelegationStartedEvent:
			if onDelegationStarted != nil {
				onDelegationStarted()
			}

		case agent.ToolNeedsConfirmEvent:
			b.log.InfoContext(ctx, logger.CatApp, "channel adapter: auto-approving tool",
				"target_id", sess.TargetID,
				"tool_name", e.Name,
				"call_id", e.CallID,
			)
			if err := sess.Agent.Confirm(e.CallID, "approve"); err != nil {
				b.log.WarnContext(ctx, logger.CatApp, "channel adapter: auto-approve failed",
					"target_id", sess.TargetID,
					"call_id", e.CallID,
					"err", err.Error(),
				)
			}

		case agent.ToolExecDoneEvent:
			if e.Name == "ImageTool" && e.Result != "" {
				urls := parseImageGenResult(e.Result)
				if len(urls) > 0 {
					imageURLs = append(imageURLs, urls...)
					for _, url := range urls {
						mediaList = append(mediaList, channel.PendingMedia{
							Kind: channel.MediaImage,
							URL:  url,
						})
					}
				}
			} else if e.Name == "SendFile" && e.Result != "" {
				res := parseSendFileResult(e.Result)
				if res != nil {
					kind := channel.MediaFile
					switch res.FileType {
					case "image":
						kind = channel.MediaImage
					case "video":
						kind = channel.MediaVideo
					case "voice":
						kind = channel.MediaVoice
					case "file":
						kind = channel.MediaFile
					}
					mediaList = append(mediaList, channel.PendingMedia{
						Kind:     kind,
						Path:     res.Path,
						URL:      res.URL,
						FileName: res.FileName,
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
		return nil, channel.ErrTaskCancelled
	}

	finalContent := contentBuf.String()[sentLen:]
	if finalContent == "" && reasoningContent != "" {
		finalContent = reasoningContent
	}

	return &channel.AskStreamResult{
		Content:          finalContent,
		ReasoningContent: reasoningContent,
		ImageURLs:        imageURLs,
		MediaList:        mediaList,
	}, nil
}

// saveUploadedFileToSession saves an uploaded file to the session's downloads directory.
func (b *channelAdapterBase) saveUploadedFileToSession(sess *Session, filename string, content []byte) (string, error) {
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

// SessionAskAdapter adapts *SessionManager to channel.SessionProvider.
type SessionAskAdapter struct {
	channelAdapterBase
	mgr *SessionManager
}

// NewQQBotAdapter creates a SessionProvider backed by the given SessionManager.
func NewQQBotAdapter(mgr *SessionManager, log *logger.Logger) *SessionAskAdapter {
	return NewChannelAdapter(mgr, log)
}

// NewChannelAdapter creates a transport-neutral session adapter.
func NewChannelAdapter(mgr *SessionManager, log *logger.Logger) *SessionAskAdapter {
	return &SessionAskAdapter{
		channelAdapterBase: channelAdapterBase{log: log},
		mgr:                mgr,
	}
}

// CancelCurrent implements channel.SessionProvider.CancelCurrent.
func (a *SessionAskAdapter) CancelCurrent(reason string) error {
	sess := a.mgr.Session()
	if sess == nil {
		return errors.New("no active session")
	}
	return a.cancelCurrent(sess, reason)
}

// Clear implements channel.SessionProvider.Clear.
func (a *SessionAskAdapter) Clear(ctx context.Context) error {
	sess := a.mgr.Session()
	if sess == nil {
		return errors.New("no active session")
	}
	return sess.Clear()
}

// Compact implements channel.SessionProvider.Compact.
func (a *SessionAskAdapter) Compact(ctx context.Context) error {
	sess := a.mgr.Session()
	if sess == nil {
		return errors.New("no active session")
	}
	return a.compactAndReap(ctx, sess)
}

// SetChannelSender stores the function used to send text through the session's
// active channel bridge. channelType is "qq" or "wechat".
func (a *SessionAskAdapter) SetChannelSender(channelType string, fn func(context.Context, string) error) {
	if sess := a.mgr.Session(); sess != nil {
		sess.SetChannelSender(channelType, fn)
	}
}

// SetChannelSenderData saves the sender closure and persists its metadata.
func (a *SessionAskAdapter) SetChannelSenderData(channelType string, metadata []byte, fn func(context.Context, string) error) {
	if sess := a.mgr.Session(); sess != nil {
		sess.SetChannelSenderData(channelType, metadata, fn)
	}
}

func (a *SessionAskAdapter) SetChannelMediaSender(channelType string, fn func(context.Context, []channel.OutboundMedia) error) {
	if sess := a.mgr.Session(); sess != nil {
		sess.SetChannelMediaSender(channelType, fn)
	}
}

// AskStream implements channel.SessionProvider.
func (a *SessionAskAdapter) AskStream(ctx context.Context, prompt string, onIntermediate channel.OnIntermediateFunc) (*channel.AskStreamResult, error) {
	receivedAt := time.Now()
	ctx = withTemporalExposure(ctx, receivedAt)
	ctx = withChannelTelemetry(ctx)
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
		a.log.InfoContext(ctx, logger.CatApp, "channel adapter: session CW state",
			"target_id", sess.TargetID,
			"cw_tokens", tokens,
			"cw_msgs", cw.Len(),
		)
	}

	ctx = agent.WithBypassConfirmCtx(ctx)
	ctx = iface.ContextWithMediaDelivery(ctx, true)

	eventCh, releaseRoute, err := a.askChannelStream(ctx, sess, prompt)
	if err != nil {
		if errors.Is(err, ErrSessionBusy) {
			return nil, channel.ErrSessionBusy
		}
		if errors.Is(err, ErrQueued) {
			return nil, channel.ErrQueued
		}
		return nil, err
	}
	// A yielded L1 turn no longer owns the actor execution slot. Releasing its
	// channel route at the same boundary lets another bridge submit work while
	// this caller keeps consuming its request-scoped response stream.
	releaseRouteOnce := sync.OnceFunc(releaseRoute)
	defer releaseRouteOnce()

	return a.consumeAskStreamEventsWithDelegation(ctx, sess, eventCh, onIntermediate, releaseRouteOnce)
}

// SaveUploadedFile saves an uploaded file to the session's workspace downloads folder.
func (a *SessionAskAdapter) SaveUploadedFile(ctx context.Context, filename string, content []byte) (string, error) {
	sess := a.mgr.Session()
	if sess == nil {
		return "", errors.New("no active session")
	}
	return a.saveUploadedFileToSession(sess, filename, content)
}

// ─── L2ChannelAdapter (L2) ───────────────────────────────────────────────────

// L2ChannelAdapter adapts *L2SessionStore to channel.SessionProvider.
type L2ChannelAdapter struct {
	channelAdapterBase
	l2Store       *L2SessionStore
	channelID     string
	botAppID      string
	bindAgent     string
	memoryManager *conversation.Manager
}

// NewL2QQBotAdapter creates a SessionProvider backed by an L2 session.
func NewL2QQBotAdapter(l2Store *L2SessionStore, botAppID, bindAgent string, log *logger.Logger, mm *conversation.Manager) *L2ChannelAdapter {
	return NewL2ChannelAdapter(l2Store, "qqbot", botAppID, bindAgent, log, mm)
}

// NewL2ChannelAdapter creates an L2 session isolated by channel and account.
func NewL2ChannelAdapter(l2Store *L2SessionStore, channelID, accountID, bindAgent string, log *logger.Logger, mm *conversation.Manager) *L2ChannelAdapter {
	return &L2ChannelAdapter{
		channelAdapterBase: channelAdapterBase{log: log},
		l2Store:            l2Store,
		channelID:          channelID,
		botAppID:           accountID,
		bindAgent:          bindAgent,
		memoryManager:      mm,
	}
}

// L2QQBotAdapter is retained as a source-compatible alias.
type L2QQBotAdapter = L2ChannelAdapter

// getSession returns the underlying L2 session, creating it if it doesn't exist.
func (a *L2ChannelAdapter) getSession(ctx context.Context) (*Session, error) {
	sessionID := a.channelID + "-" + a.botAppID
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

// CancelCurrent implements channel.SessionProvider.CancelCurrent.
func (a *L2ChannelAdapter) CancelCurrent(reason string) error {
	sess, err := a.getSession(context.Background())
	if err != nil {
		return err
	}
	return a.cancelCurrent(sess, reason)
}

// Clear implements channel.SessionProvider.Clear.
func (a *L2ChannelAdapter) Clear(ctx context.Context) error {
	sess, err := a.getSession(ctx)
	if err != nil {
		return err
	}
	return sess.Clear()
}

// Compact implements channel.SessionProvider.Compact.
func (a *L2ChannelAdapter) Compact(ctx context.Context) error {
	sess, err := a.getSession(ctx)
	if err != nil {
		return err
	}
	return a.compactAndReap(ctx, sess)
}

// SetChannelSender stores the function used to send text through the session's
// channel bridge. channelType is "qq" or "wechat".
func (a *L2ChannelAdapter) SetChannelSender(channelType string, fn func(context.Context, string) error) {
	if sess, err := a.getSession(context.Background()); err == nil {
		sess.SetChannelSender(channelType, fn)
		a.l2Store.SetChannelSenderForGroup(a.bindAgent, channelType, fn)
	}
}

// SetChannelSenderData saves the sender closure and persists its metadata.
func (a *L2ChannelAdapter) SetChannelSenderData(channelType string, metadata []byte, fn func(context.Context, string) error) {
	if sess, err := a.getSession(context.Background()); err == nil {
		sess.SetChannelSenderData(channelType, metadata, fn)
		a.l2Store.SetChannelSenderDataForGroup(a.bindAgent, channelType, metadata, fn)
	}
}

func (a *L2ChannelAdapter) SetChannelMediaSender(channelType string, fn func(context.Context, []channel.OutboundMedia) error) {
	if sess, err := a.getSession(context.Background()); err == nil {
		sess.SetChannelMediaSender(channelType, fn)
		a.l2Store.SetChannelMediaSenderForGroup(a.bindAgent, channelType, fn)
	}
}

// AskStream implements channel.SessionProvider.
func (a *L2ChannelAdapter) AskStream(ctx context.Context, prompt string, onIntermediate channel.OnIntermediateFunc) (*channel.AskStreamResult, error) {
	ctx = withChannelTelemetry(ctx)
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
		a.log.InfoContext(ctx, logger.CatApp, "channel L2 adapter: session CW state",
			"target_id", sess.TargetID,
			"cw_tokens", tokens,
			"cw_msgs", cw.Len(),
		)
	}

	ctx = agent.WithBypassConfirmCtx(ctx)
	ctx = iface.ContextWithMediaDelivery(ctx, true)

	eventCh, releaseRoute, err := a.askChannelStream(ctx, sess, prompt)
	if err != nil {
		if errors.Is(err, ErrSessionBusy) {
			return nil, channel.ErrSessionBusy
		}
		if errors.Is(err, ErrQueued) {
			return nil, channel.ErrQueued
		}
		return nil, err
	}
	defer releaseRoute()

	return a.consumeAskStreamEvents(ctx, sess, eventCh, onIntermediate)
}

// SaveUploadedFile saves an uploaded file to the session's workspace downloads folder.
func (a *L2ChannelAdapter) SaveUploadedFile(ctx context.Context, filename string, content []byte) (string, error) {
	sess, err := a.getSession(ctx)
	if err != nil {
		return "", err
	}
	return a.saveUploadedFileToSession(sess, filename, content)
}

// ─── ErrorChannelAdapter ─────────────────────────────────────────────────────

// ErrorChannelAdapter adapts a configuration error to channel.SessionProvider.
type ErrorChannelAdapter struct {
	errMsg string
}

// NewErrorQQBotAdapter creates a SessionProvider that immediately reports the given error.
func NewErrorQQBotAdapter(errMsg string) *ErrorChannelAdapter {
	return NewErrorChannelAdapter(errMsg)
}

// NewErrorChannelAdapter creates a provider that reports a configuration error.
func NewErrorChannelAdapter(errMsg string) *ErrorChannelAdapter {
	return &ErrorChannelAdapter{errMsg: errMsg}
}

// ErrorQQBotAdapter is retained as a source-compatible alias.
type ErrorQQBotAdapter = ErrorChannelAdapter

func (a *ErrorChannelAdapter) CancelCurrent(reason string) error { return nil }

func (a *ErrorChannelAdapter) Clear(ctx context.Context) error { return nil }

func (a *ErrorChannelAdapter) Compact(ctx context.Context) error { return nil }

func (a *ErrorChannelAdapter) AskStream(ctx context.Context, prompt string, onIntermediate channel.OnIntermediateFunc) (*channel.AskStreamResult, error) {
	return &channel.AskStreamResult{Content: a.errMsg}, nil
}

func (a *ErrorChannelAdapter) SaveUploadedFile(ctx context.Context, filename string, content []byte) (string, error) {
	return "", errors.New(a.errMsg)
}

func (a *ErrorChannelAdapter) SetChannelSenderData(channelType string, metadata []byte, fn func(context.Context, string) error) {
}

// ─── Parsers ─────────────────────────────────────────────────────────────────

// parseImageGenResult extracts image URLs from an ImageTool result JSON.
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
