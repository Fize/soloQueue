// Package session provides the "Dialogue Session" abstraction, wrapping agent + context window.
//
// Design principles:
//
//   - A Session corresponds to "a single independent conversation": bound to an *agent.Agent, holding
//     *ctxwin.ContextWindow to manage full conversation history (including intermediate tool call messages).
//   - Ask/AskStream within the same Session is serial: new Ask returns directly if the previous round is not finished
//     ErrSessionBusy (avoids context window out-of-order). The agent itself is also serial, offering double protection.
//   - SessionManager manages the unique active session; globally there is only one session.
package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/conversationlog"
	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/telemetry"
	"github.com/xiaobaitu/soloqueue/internal/timeline"
)

// ─── Errors ────────────────────────────────────────────────────────────────

var (
	// ErrSessionBusy is returned when the session is busy with another Ask
	ErrSessionBusy = errors.New("session: busy (another Ask in flight)")

	// ErrQueued is returned when the message is queued in the pending queue
	ErrQueued = errors.New("session: message queued")

	// ErrSessionClosed is returned when the session is closed
	ErrSessionClosed = errors.New("session: closed")

	// ErrNoActiveTask is returned when there is no active task to cancel
	ErrNoActiveTask = errors.New("session: no active task")
)

type rejectBusyQueueKey struct{}

// WithRejectBusyQueue marks a desktop request that must be rejected rather
// than inserted into the session pending queue when the session is busy.
func WithRejectBusyQueue(ctx context.Context) context.Context {
	return context.WithValue(ctx, rejectBusyQueueKey{}, true)
}

func rejectsBusyQueue(ctx context.Context) bool {
	v, _ := ctx.Value(rejectBusyQueueKey{}).(bool)
	return v
}

// Version is the current version of soloqueue. It is set at startup by the main command.
var Version = "0.1.0"

// CurrentLevel returns the classification level of the last routed task.
// Returns "" if no task has been routed yet or routing is disabled.
func (s *Session) CurrentLevel() string {
	s.lastLevelMu.RLock()
	defer s.lastLevelMu.RUnlock()
	return s.lastLevel
}

// LevelLocked returns whether the task level has been locked by the user
// via /l0, /l1, /l2, or /l3 commands.
func (s *Session) LevelLocked() bool {
	s.lastLevelMu.RLock()
	defer s.lastLevelMu.RUnlock()
	return s.levelLocked
}

// SetLastLevel sets the session's last classification level.
// Called during session restore to recover the task level from disk.
func (s *Session) SetLastLevel(level string) {
	s.lastLevelMu.Lock()
	s.lastLevel = level
	s.lastLevelMu.Unlock()
}

// ─── TaskRouter Interface ─────────────────────────────────────────────────────

// RouteResult is a minimal routing decision passed to the session layer.
type RouteResult struct {
	ProviderID        string // LLM provider to use (e.g., "deepseek"); empty = default
	ModelID           string // API model to use (e.g., "deepseek-v4-pro")
	ThinkingEnabled   bool   // whether to enable thinking mode
	ReasoningEffort   string // "high" | "max" | ""
	ThinkingType      string // thinking.type value: "enabled" (default) or "adaptive"
	Level             string // classification level label (e.g., "L1-SimpleSingleFile")
	ContextWindow     int    // model context window capacity (tokens); 0 = unchanged
	Vision            bool   // model supports multimodal image_url content
	ClassifierWarning string // non-empty when classification degraded (for desktop notification)
}

// TaskRouterFunc classifies a user prompt and returns model routing parameters.
// priorLevel is the session's current task level string ("" if none).
// Used to inject the router without creating import cycles.
// Returns error if classification fails; caller proceeds with defaults.
type TaskRouterFunc func(ctx context.Context, prompt string, priorLevel string, history []ctxwin.PayloadMessage) (RouteResult, error)

// MemoryHook is called when conversation context is being discarded (compaction or /clear).
// conversationText is a plain-text representation of the messages being forgotten.
// recordedAt indicates the date of the conversation segment for correct file routing.
type MemoryHook func(ctx context.Context, conversationText string, recordedAt time.Time)

// ─── Session ──────────────────────────────────────────────────────────────

// Session represents a conversation session.
type Session struct {
	ID      string
	TeamID  string
	Agent   *agent.Agent
	Router  TaskRouterFunc // Optional: task routing classifier (nil = no routing, use default model)
	Created time.Time

	mu     sync.Mutex
	cw     *ctxwin.ContextWindow // Replaces original history, manages full conversation context
	tl     *timeline.Writer      // Timeline writer (can be nil, meaning no persistence)
	logger *logger.Logger        // Session-level logger

	// pending queue: new messages enqueue when session is busy, popped and injected
	// into ContextWindow before the agent's next LLM API call in the tool loop, merging consecutive messages
	pending *PendingQueue

	// inFlight CAS lock for concurrent Asks: 0 -> 1 enter; returns ErrSessionBusy on failure
	inFlight atomic.Int32

	// closed indicates if the Session has been deleted
	closed atomic.Bool

	// lastActive for reaper cleanup; updated on every Ask
	lastActive atomic.Int64 // unix nanos

	// delegationPending indicates if an async delegation is in progress
	// Set to true when DelegationStartedEvent arrives, indicating L1 has delegated a task to L2
	// At this point, inFlight is released, allowing the user to send new messages
	// New message CW pushes are delayed until turnDone signal, ensuring correct CW message order
	delegationPending atomic.Bool
	turnMu            sync.Mutex    // protects turnDone creation and closing
	turnDone          chan struct{} // closed when the async delegation turn completes
	turnDoneClosed    bool          // prevents duplicate close of turnDone

	// activeCancels contains every live top-level turn in this session. A turn
	// context is the root of all local, delegated, and cross-team LLM calls, so
	// cancelling it stops the whole request tree without stopping the reusable
	// Session or Agent instances.
	cancelMu      sync.Mutex
	activeCancels map[uint64]activeTurnCancel
	nextCancelID  uint64

	// Supervisor is the L3 child manager for L2 sessions; nil for L1 sessions
	Supervisor *agent.Supervisor

	// channelSenders maps channel type ("qq"/"wechat") to send functions.
	// Registered by bridges when OnMessage fires. Protected by channelSendersMu.
	channelSenders   map[string]func(context.Context, string) error
	channelSendersMu sync.RWMutex

	// cancelled: forwarder sets this when cancelled, consumed and reset by the adapter
	cancelled atomic.Bool

	lastLevel       string       // last classified task level (L0-L3)
	lastLevelMu     sync.RWMutex // protects lastLevel and levelLocked
	levelLocked     bool         // true when user explicitly locked level via /l0-/l3
	lastRouteResult RouteResult  // cached route result for locked mode (model params preserved)

	// L2-only: coordinates persistent meta.json writes for level/git_base_ref/baseline.
	// Empty on L1 sessions; MergeAndSave is only called when metaL2ID is non-empty.
	metaWorkDir string
	metaL2ID    string
	// gitBaseRef captures the HEAD commit hash at session start (git repos).
	// Persisted in meta.json; read by the Changes tab to diff against this ref.
	gitBaseRef   string
	metaBaseline map[string]string // path→sha256 snapshot, non-git projects only

	memoryHook    MemoryHook               // optional callback for short-term memory (nil = disabled)
	memoryManager *conversationlog.Manager // for dedup cursor; set alongside memoryHook

	idleTimeout      time.Duration // 0 = disabled; auto-clear idle sessions
	compactThreshold int           // 0 = disabled; minimum CW tokens to trigger compact
	isQBot           atomic.Bool
}

// NewSession constructs and starts a session (agent should already have started)
//
// cw should already contain system prompt (pushed in factory).
// tl can be nil (no persistence).
// logger is the session-level logger (creates default logger if nil).
func NewSession(id, teamID string, a *agent.Agent, cw *ctxwin.ContextWindow, tl *timeline.Writer, l *logger.Logger) *Session {
	if l == nil {
		var err error
		l, err = logger.System("/tmp", logger.WithConsole(false), logger.WithFile(false))
		if err != nil {
			panic(err)
		}
	}

	s := &Session{
		ID:            id,
		TeamID:        teamID,
		Agent:         a,
		Created:       time.Now(),
		cw:            cw,
		tl:            tl,
		logger:        l,
		pending:       &PendingQueue{},
		activeCancels: make(map[uint64]activeTurnCancel),
	}
	s.lastActive.Store(time.Now().UnixNano())

	// Wire pending message drainer so the agent injects queued messages
	// before each LLM API call.
	if cw != nil {
		cw.SetPendingDrainer(func() string {
			if pending := s.pending.Drain(); pending != "" {
				trimmed := strings.TrimSpace(pending)
				lower := strings.ToLower(trimmed)
				switch lower {
				case "/cancel", "/compact", "/help", "/?", "/clear", "/version":
					s.logger.WarnContext(context.Background(), logger.CatApp, "pending drain: dropping stale slash command",
						"session_id", s.ID,
						"command", lower,
					)
					return ""
				}
				s.logger.InfoContext(context.Background(), logger.CatApp, "pending messages injected into context window",
					"session_id", s.ID,
					"prompt_len", len(pending),
				)
				return pending
			}
			return ""
		})
	}

	s.logger.InfoContext(context.Background(), logger.CatApp, "session created",
		"session_id", id,
		"team_id", teamID,
	)

	return s
}

// History returns a snapshot of the current context window for the REST API.
//
// <recalled_memories> blocks injected by the pre-load mechanism are stripped
// from user messages so the web UI never exposes them.
func (s *Session) History() []agent.LLMMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	payload := s.cw.BuildPayload()
	out := make([]agent.LLMMessage, 0, len(payload))
	for _, p := range payload {
		content := p.Content
		if p.Role == "user" {
			content = StripRecalledMemories(content)
		}
		out = append(out, agent.LLMMessage{
			Role:             p.Role,
			Content:          content,
			ReasoningContent: p.ReasoningContent,
			Name:             p.Name,
			ToolCallID:       p.ToolCallID,
			ToolCalls:        p.ToolCalls,
		})
	}
	return out
}

// StripDesignDirective removes the [CRITICAL DIRECTIVE: ...], [SELECTED DOM ELEMENT: ...] and [USER DRAWINGS/ANNOTATIONS DETECTED: ...] blocks from the end of a message if present.
func StripDesignDirective(s string) string {
	const directiveMarker = "\n\n[CRITICAL DIRECTIVE: Design preview mode is active."
	idx := strings.Index(s, directiveMarker)
	if idx >= 0 {
		s = s[:idx]
	}

	const elementMarker = "\n\n[SELECTED DOM ELEMENT:"
	idx2 := strings.Index(s, elementMarker)
	if idx2 >= 0 {
		s = s[:idx2]
	}

	const drawingsMarker = "\n\n[USER DRAWINGS/ANNOTATIONS DETECTED:"
	idx3 := strings.Index(s, drawingsMarker)
	if idx3 >= 0 {
		s = s[:idx3]
	}

	return s
}

// StripRecalledMemories removes the <recalled_memories>...</recalled_memories>
// block from the beginning of a message if present.
func StripRecalledMemories(s string) string {
	s = StripDesignDirective(s)
	const startTag = "<recalled_memories>"
	const endTag = "</recalled_memories>"
	start := strings.Index(s, startTag)
	if start < 0 {
		return s
	}
	end := strings.Index(s[start+len(startTag):], endTag)
	if end < 0 {
		return s
	}
	end = start + len(startTag) + end + len(endTag)
	// After the end tag, expect "\n\n" separator, then the original prompt
	remainder := strings.TrimLeft(s[end:], "\n ")
	if remainder == "" {
		return s
	}
	return remainder
}

// ContextWindow returns the underlying ContextWindow (for scenarios requiring direct access)
func (s *Session) ContextWindow() *ctxwin.ContextWindow {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cw
}

// Idle returns true if there is no Ask or AskStream currently in flight.
func (s *Session) Idle() bool {
	return s.inFlight.Load() == 0
}

// SetChannelSender registers a send function for the given channel type ("qq"/"wechat").
// Called by the bridge when OnMessage fires. Replaces any existing sender for the same type.
func (s *Session) SetChannelSender(channelType string, fn func(context.Context, string) error) {
	s.channelSendersMu.Lock()
	if s.channelSenders == nil {
		s.channelSenders = make(map[string]func(context.Context, string) error)
	}
	prev := s.channelSenders[channelType] != nil
	s.channelSenders[channelType] = fn
	s.channelSendersMu.Unlock()
	s.logger.InfoContext(context.Background(), logger.CatApp, "session: channelSender registered",
		"session_id", s.ID,
		"channel_type", channelType,
		"had_previous", prev,
	)
}

// HasNotifyChannel reports whether this session's agent has configured a
// notification channel. It does not claim that a live sender is available.
func (s *Session) HasNotifyChannel() bool {
	return s.Agent != nil && s.Agent.Def.NotifyChannel != ""
}

// SendViaChannel sends text through the configured notify channel.
// The channel is determined by the agent's NotifyChannel config (e.g. "qq" or "wechat").
// If notify_channel or its active sender is absent, no notification is sent.
func (s *Session) SendViaChannel(ctx context.Context, text string) error {
	notifyChannel := ""
	if s.Agent != nil {
		notifyChannel = s.Agent.Def.NotifyChannel
	}
	if notifyChannel == "" {
		s.logger.WarnContext(ctx, logger.CatApp, "session: SendViaChannel skipped, no notify_channel configured",
			"session_id", s.ID,
		)
		return nil
	}

	s.channelSendersMu.RLock()
	fn := s.channelSenders[notifyChannel]
	s.channelSendersMu.RUnlock()

	if fn == nil {
		s.logger.WarnContext(ctx, logger.CatApp, "session: SendViaChannel skipped, no sender for notify_channel",
			"session_id", s.ID,
			"notify_channel", notifyChannel,
		)
		return nil
	}

	s.logger.InfoContext(ctx, logger.CatApp, "session: SendViaChannel delivering",
		"session_id", s.ID,
		"notify_channel", notifyChannel,
		"text_len", len(text),
	)
	return fn(ctx, text)
}

// IsQBot returns true if the session is currently serving or was last triggered by QBot.
func (s *Session) IsQBot() bool {
	return s.isQBot.Load()
}

// SetIsQBot sets the QBot status for the session.
func (s *Session) SetIsQBot(val bool) {
	s.isQBot.Store(val)
}

// ClassifierWarning returns the classifier degradation warning, if any.
// Non-empty when the task router encountered an error or LLM fallback.
// Callers should send a desktop notification to inform the user.
func (s *Session) ClassifierWarning() string {
	s.lastLevelMu.RLock()
	defer s.lastLevelMu.RUnlock()
	return s.lastRouteResult.ClassifierWarning
}

// CW returns the underlying ContextWindow pointer without locking.
// Safe for read-only access: the cw pointer is set at construction time
// and never changes. Use this in hot paths (e.g., UI tick) to avoid
// contending with Session.mu.
func (s *Session) CW() *ctxwin.ContextWindow {
	return s.cw
}

// AskIsolated executes a prompt in a clean context: it calls the underlying
// agent directly without pushing to the session's ContextWindow or timeline.
// This is used by the cron scheduler so scheduled tasks run without polluting
// the user's conversation history or being confused by stale context.
// All system logs (actor/llm/tool) are still written normally.
func (s *Session) AskIsolated(ctx context.Context, prompt string) (<-chan iface.AgentEvent, error) {
	// Inject telemetry context
	ctx = telemetry.WithTelemetryContext(ctx, s.TeamID, telemetry.UsageChat)

	if s.closed.Load() {
		return nil, ErrSessionClosed
	}
	ctx = iface.ContextWithIsQBot(ctx, s.IsQBot())
	ch, err := s.Agent.AskStream(ctx, prompt)
	if err != nil {
		return nil, err
	}
	// Wrap AgentEvent channel to iface.AgentEvent channel (they are the same type via embedding)
	out := make(chan iface.AgentEvent, 64)
	go func() {
		defer close(out)
		for ev := range ch {
			out <- ev
		}
	}()
	return out, nil
}

// AskIsolatedWithModel executes an isolated scheduled task with an explicit
// per-run model. The override is cleared when the returned stream closes so it
// cannot leak into the next user request.
// AskStreamWithModel executes a prompt via AskStream (pushing to timeline)
// with an explicit per-run model. The override is cleared when the stream closes.
func (s *Session) AskStreamWithModel(ctx context.Context, prompt string, params *iface.ModelOverrideParams) (<-chan iface.AgentEvent, error) {
	if params == nil {
		return s.AskStream(ctx, prompt)
	}
	s.Agent.SetModelOverride(&agent.ModelParams{
		ProviderID:      params.ProviderID,
		ModelID:         params.ModelID,
		ThinkingEnabled: params.ThinkingEnabled,
		ReasoningEffort: params.ReasoningEffort,
		ThinkingType:    params.ThinkingType,
		TaskType:        params.TaskType,
		ContextWindow:   params.ContextWindow,
		Vision:          params.Vision,
	})
	ch, err := s.AskStream(ctx, prompt)
	if err != nil {
		s.Agent.ClearModelOverride()
		return nil, err
	}
	out := make(chan iface.AgentEvent, 64)
	go func() {
		defer close(out)
		defer s.Agent.ClearModelOverride()
		for ev := range ch {
			out <- ev
		}
	}()
	return out, nil
}

func (s *Session) AskIsolatedWithModel(ctx context.Context, prompt string, params *iface.ModelOverrideParams) (<-chan iface.AgentEvent, error) {
	if params == nil {
		return s.AskIsolated(ctx, prompt)
	}
	s.Agent.SetModelOverride(&agent.ModelParams{
		ProviderID:      params.ProviderID,
		ModelID:         params.ModelID,
		ThinkingEnabled: params.ThinkingEnabled,
		ReasoningEffort: params.ReasoningEffort,
		ThinkingType:    params.ThinkingType,
		TaskType:        params.TaskType,
		ContextWindow:   params.ContextWindow,
		Vision:          params.Vision,
	})
	ch, err := s.AskIsolated(ctx, prompt)
	if err != nil {
		s.Agent.ClearModelOverride()
		return nil, err
	}
	out := make(chan iface.AgentEvent, 64)
	go func() {
		defer close(out)
		defer s.Agent.ClearModelOverride()
		for ev := range ch {
			out <- ev
		}
	}()
	return out, nil
}

// QueueMessage enqueues a user message into the pending queue without blocking.
// The message will be injected into the agent's context window before the next
// LLM API call, merged with any other pending messages into a single user turn.
func (s *Session) QueueMessage(prompt string) {
	s.pending.Enqueue(prompt)
	s.logger.InfoContext(context.Background(), logger.CatApp, "message queued via QueueMessage",
		"session_id", s.ID,
		"prompt_len", len(prompt),
	)
}

// HasPending reports whether user messages are waiting in the pending queue.
func (s *Session) HasPending() bool {
	return s.pending.HasPending()
}

// SetMemoryHook sets the optional callback for short-term memory recording.
// The hook is called when conversation context is discarded via compaction or /clear.
func (s *Session) SetMemoryHook(hook MemoryHook) {
	s.memoryHook = hook
}

// SetMemoryManager sets the memory manager for dedup cursor tracking.
// Must be set alongside SetMemoryHook for dedup to work.
func (s *Session) SetMemoryManager(mm *conversationlog.Manager) {
	s.memoryManager = mm
}

// Clear performs a soft clear: appends /clear control event to timeline, resets ContextWindow
//
// Does not delete any persistent data. ContextWindow only retains the system prompt.
func (s *Session) Clear() error {
	s.mu.Lock()

	// Snapshot messages for memory recording before clearing.
	// Filter by dedup cursor and group by date.
	var dateGroups []payloadDateGroup
	if s.memoryHook != nil {
		payload := s.cw.BuildPayload()
		cursor := time.Time{}
		if s.memoryManager != nil {
			cursor = s.memoryManager.LastRecordedAt()
		}
		filtered := filterPayloadSince(payload, cursor)
		if len(filtered) > 0 {
			dateGroups = groupPayloadByDate(filtered)
		}
	}

	// Append /clear control event to timeline
	if s.tl != nil {
		if err := s.tl.AppendControl(&timeline.ControlPayload{
			Action: "clear",
			Reason: "user_command",
		}); err != nil {
			s.mu.Unlock()
			s.logger.LogError(context.Background(), logger.CatApp, "session clear failed", err)
			return fmt.Errorf("session: clear: %w", err)
		}
	}

	// Reset ContextWindow (retaining system prompt)
	s.cw.Reset()
	s.mu.Unlock()

	// Call memory hook for each date group (outside lock)
	if s.memoryHook != nil && len(dateGroups) > 0 {
		var latest time.Time
		for _, g := range dateGroups {
			text := formatPayloadForMemory(g.msgs)
			s.memoryHook(context.Background(), text, g.date)
			for _, m := range g.msgs {
				if m.Timestamp.After(latest) {
					latest = m.Timestamp
				}
			}
		}
		if s.memoryManager != nil {
			s.memoryManager.AdvanceLastRecordedAt(latest)
		}
	}

	s.logger.InfoContext(context.Background(), logger.CatApp, "session cleared",
		"session_id", s.ID,
	)

	return nil
}

// Compact compacts the context window by summarizing older messages
// into a condensed representation using the compactor. Unlike Clear,
// it preserves the recent context and does NOT save to conversationlog.
func (s *Session) Compact(ctx context.Context) (string, error) {
	compactCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	summary, err := s.cw.CompactAndReplace(compactCtx)
	if err != nil {
		s.logger.LogError(context.Background(), logger.CatApp, "session compact failed", err)
		return "", fmt.Errorf("session: compact: %w", err)
	}

	s.logger.InfoContext(context.Background(), logger.CatApp, "session compacted",
		"session_id", s.ID,
	)

	return summary, nil
}

// LastMessageTime returns the timestamp of the last non-system message.
// Returns zero time if no messages exist or only system prompt is present.
func (s *Session) LastMessageTime() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	payload := s.cw.BuildPayload()
	for i := len(payload) - 1; i >= 0; i-- {
		if payload[i].Role != "system" {
			return payload[i].Timestamp
		}
	}
	return time.Time{}
}

// ShouldClearContext returns true if (a) the last message is older than idleTimeout,
// AND (b) currentTokens >= minTokens. This prevents wasting tokens on short sessions.
func (s *Session) ShouldClearContext(idleTimeout time.Duration, minTokens int) bool {
	last := s.LastMessageTime()
	if last.IsZero() {
		return false
	}
	if time.Since(last) < idleTimeout {
		return false
	}
	// Time condition met; now check token threshold
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cw.CurrentTokens() >= minTokens
}

// ClearSilent performs a "silent" clear of the context window.
// Unlike Clear(), it does NOT write a control event to the timeline.
// It triggers the memory hook (if set) for short-term memory storage.
// Rewind truncates the context window and timeline from the target timestamp onwards.
func (s *Session) Rewind(targetTs time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var closest time.Time
	var minDiff time.Duration = 2 * time.Second
	for i := 0; i < s.cw.Len(); i++ {
		msg, ok := s.cw.MessageAt(i)
		if !ok || msg.Timestamp.IsZero() {
			continue
		}
		diff := msg.Timestamp.Sub(targetTs)
		if diff < 0 {
			diff = -diff
		}
		if diff < minDiff {
			minDiff = diff
			closest = msg.Timestamp
		}
	}

	resolvedTs := targetTs
	if !closest.IsZero() {
		resolvedTs = closest
	}

	// Append /rewind control event to timeline
	if s.tl != nil {
		if err := s.tl.AppendControl(&timeline.ControlPayload{
			Action:   "rewind",
			TargetTs: []string{resolvedTs.Format(time.RFC3339Nano)},
		}); err != nil {
			s.logger.LogError(context.Background(), logger.CatApp, "session rewind failed", err)
			return fmt.Errorf("session: rewind: %w", err)
		}
	}

	// Truncate in-memory ContextWindow
	s.cw.Rewind(resolvedTs)
	return nil
}

// DeleteMessages drops specific messages from the context window and timeline.
func (s *Session) DeleteMessages(targetTsList []time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(targetTsList) == 0 {
		return nil
	}

	// Resolve fuzzy matching for targetTsList
	var resolvedTsList []time.Time
	for _, ts := range targetTsList {
		var closest time.Time
		var minDiff time.Duration = 2 * time.Second
		for i := 0; i < s.cw.Len(); i++ {
			msg, ok := s.cw.MessageAt(i)
			if !ok || msg.Timestamp.IsZero() {
				continue
			}
			diff := msg.Timestamp.Sub(ts)
			if diff < 0 {
				diff = -diff
			}
			if diff < minDiff {
				minDiff = diff
				closest = msg.Timestamp
			}
		}
		if !closest.IsZero() {
			resolvedTsList = append(resolvedTsList, closest)
		} else {
			// fallback to original if not found
			resolvedTsList = append(resolvedTsList, ts)
		}
	}

	var strList []string
	for _, ts := range resolvedTsList {
		strList = append(strList, ts.Format(time.RFC3339Nano))
	}

	// Append /delete control event to timeline
	if s.tl != nil {
		if err := s.tl.AppendControl(&timeline.ControlPayload{
			Action:   "delete",
			TargetTs: strList,
		}); err != nil {
			s.logger.LogError(context.Background(), logger.CatApp, "session delete failed", err)
			return fmt.Errorf("session: delete: %w", err)
		}
	}

	// Remove from in-memory ContextWindow
	s.cw.DeleteMessages(resolvedTsList)
	return nil
}

// ClearSilent performs a silent clear (resets ContextWindow without appending timeline event)
func (s *Session) ClearSilent() error {
	s.mu.Lock()

	// Snapshot messages for memory recording.
	// Filter by dedup cursor and group by date.
	var dateGroups []payloadDateGroup
	if s.memoryHook != nil {
		payload := s.cw.BuildPayload()
		cursor := time.Time{}
		if s.memoryManager != nil {
			cursor = s.memoryManager.LastRecordedAt()
		}
		filtered := filterPayloadSince(payload, cursor)
		if len(filtered) > 0 {
			dateGroups = groupPayloadByDate(filtered)
		}
	}

	// Reset context window (preserves system prompt)
	s.cw.Reset()
	s.mu.Unlock()

	// Call memory hook for each date group (outside lock)
	if s.memoryHook != nil && len(dateGroups) > 0 {
		var latest time.Time
		for _, g := range dateGroups {
			text := formatPayloadForMemory(g.msgs)
			s.memoryHook(context.Background(), text, g.date)
			for _, m := range g.msgs {
				if m.Timestamp.After(latest) {
					latest = m.Timestamp
				}
			}
		}
		if s.memoryManager != nil {
			s.memoryManager.AdvanceLastRecordedAt(latest)
		}
	}

	return nil
}

// FlushMemory snapshots all unpersisted messages from the context window and
// writes them to short-term memory files. Unlike ClearSilent, it does NOT
// reset the context window — this is a read-only flush for periodic persistence
// (e.g. daily midnight task).
func (s *Session) FlushMemory(ctx context.Context) {
	if s.memoryHook == nil || s.memoryManager == nil {
		return
	}

	s.mu.Lock()
	payload := s.cw.BuildPayload()
	cursor := s.memoryManager.LastRecordedAt()
	s.mu.Unlock()

	filtered := filterPayloadSince(payload, cursor)
	if len(filtered) == 0 {
		return
	}

	var latest time.Time
	groups := groupPayloadByDate(filtered)
	for _, g := range groups {
		text := formatPayloadForMemory(g.msgs)
		s.memoryHook(ctx, text, g.date)
		for _, m := range g.msgs {
			if m.Timestamp.After(latest) {
				latest = m.Timestamp
			}
		}
	}
	if !latest.IsZero() {
		s.memoryManager.AdvanceLastRecordedAt(latest)
	}
}

// checkAutoClear checks if the session has been idle for long enough and
// the context window is large enough to warrant automatic compression.
// If both conditions are met, it compresses the conversation history into
// a summary and replaces the CW content with system_prompt + summary.
//
// Must be called while inFlight is held (no concurrent Ask).
func (s *Session) checkAutoClear() {
	timeout := s.idleTimeout
	threshold := s.compactThreshold
	if timeout <= 0 || threshold <= 0 {
		return
	}

	lastNano := s.lastActive.Load()
	if lastNano == 0 {
		return
	}
	if time.Since(time.Unix(0, lastNano)) < timeout {
		return
	}

	s.mu.Lock()
	tokens := s.cw.CurrentTokens()
	s.mu.Unlock()

	if tokens < threshold {
		s.logger.InfoContext(context.Background(), logger.CatApp, "auto-clear: idle but tokens below compact threshold",
			"tokens", tokens, "threshold", threshold)
		return
	}

	s.logger.InfoContext(context.Background(), logger.CatApp, "auto-clear: compressing idle session context",
		"tokens", tokens, "threshold", threshold)

	compactCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	summary, err := s.cw.CompactAndReplace(compactCtx)
	if err != nil {
		s.logger.WarnContext(context.Background(), logger.CatApp, "auto-clear: compact failed, keeping context",
			"err", err.Error())
		return
	}

	s.logger.InfoContext(context.Background(), logger.CatApp, "auto-clear: context compressed and replaced",
		"summary_len", len(summary))
}

// Ask sends a round of prompt and returns the final content
//
// Semantics:
//   - Ask is serialized within the same session (inFlight CAS 0→1, otherwise ErrSessionBusy)
//   - First push user prompt to ContextWindow
//   - Call Agent.AskWithHistory (with full history)
//   - Success: push assistant reply to ContextWindow
//   - Failure: PopLast removes the user prompt just pushed
//   - ctx cancellation propagates to agent; Session does not manage timeout.
func (s *Session) Ask(ctx context.Context, prompt string) (string, error) {
	// Inject telemetry context
	ctx = telemetry.WithTelemetryContext(ctx, s.TeamID, telemetry.UsageChat)

	if s.closed.Load() {
		s.logger.DebugContext(ctx, logger.CatApp, "ask rejected: session closed")
		return "", ErrSessionClosed
	}
	if !s.inFlight.CompareAndSwap(0, 1) {
		s.logger.InfoContext(ctx, logger.CatApp, "ask rejected: session busy, message queued",
			"session_id", s.ID,
			"prompt_len", len(prompt),
		)
		s.pending.Enqueue(prompt)
		return "", ErrQueued
	}
	defer s.inFlight.Store(0)
	defer s.touch()

	s.checkAutoClear()

	start := time.Now()

	ctx = iface.ContextWithIsQBot(ctx, s.IsQBot())

	// Resize to default model's context window and push user prompt
	effectiveCW := s.Agent.Def.ContextWindow
	if effectiveCW <= 0 {
		effectiveCW = agent.DefaultContextWindow
	}
	s.mu.Lock()
	s.cw.Resize(effectiveCW, 0, 0)
	cwLenBeforeTurn := s.cw.Len()
	s.cw.Push(ctxwin.RoleUser, prompt)
	s.mu.Unlock()

	s.logger.DebugContext(ctx, logger.CatApp, "ask: prompt pushed to context window",
		"session_id", s.ID,
		"prompt_len", len(prompt),
	)

	reply, reasoningContent, err := s.Agent.AskWithHistory(ctx, s.cw, prompt)
	duration := time.Since(start).Milliseconds()

	if err != nil {
		s.mu.Lock()
		s.cw.Truncate(cwLenBeforeTurn)
		s.mu.Unlock()

		s.logger.WarnContext(ctx, logger.CatApp, "ask failed, user prompt removed",
			"session_id", s.ID,
			"duration_ms", duration,
			"err", err.Error(),
		)
		return "", err
	}

	// Empty assistant reply with no tool calls is invalid for LLM API.
	// Skip the push but keep the user prompt so LLM retains context.
	if reply == "" {
		s.logger.WarnContext(ctx, logger.CatApp, "ask: empty assistant reply skipped",
			"session_id", s.ID,
			"duration_ms", duration,
			"reasoning_len", len(reasoningContent),
		)
		return "", fmt.Errorf("session: assistant returned empty reply")
	}

	s.mu.Lock()
	opts := []ctxwin.PushOption{ctxwin.WithReasoningContent(reasoningContent)}
	s.cw.Push(ctxwin.RoleAssistant, reply, opts...)
	s.mu.Unlock()

	s.logger.DebugContext(ctx, logger.CatApp, "ask complete",
		"session_id", s.ID,
		"reply_len", len(reply),
		"reasoning_len", len(reasoningContent),
		"duration_ms", duration,
	)

	return reply, nil
}

// AskStream streaming version; caller must range over the returned channel until closed
//
// Context window pushes user + assistant upon receiving DoneEvent;
// removes user prompt via PopLast upon receiving ErrorEvent.
// caller abandoning range must cancel ctx.
//
// Async delegation support: when L1 delegates a task to L2, DelegationStartedEvent releases inFlight,
// allowing the user to send new messages during this time. The CW push of new messages will wait until the delegation round is completed,
// to guarantee the correct message order in ContextWindow (delegation reply finishes first, then new user message appears).
func (s *Session) AskStream(ctx context.Context, prompt string) (<-chan iface.AgentEvent, error) {
	if s.closed.Load() {
		s.logger.DebugContext(ctx, logger.CatApp, "askstream rejected: session closed")
		return nil, ErrSessionClosed
	}

	trimmed := strings.TrimSpace(prompt)
	lowerTrimmed := strings.ToLower(trimmed)

	// Inject telemetry context
	ctx = telemetry.WithTelemetryContext(ctx, s.TeamID, telemetry.UsageChat)

	// ── Pre-inFlight slash command intercept (always immediate, never queued) ──
	switch lowerTrimmed {
	case "/compact":
		// Acquire inFlight to serialize with forwarder goroutine.
		// Compact modifies CW state and must not run concurrently with AskStream.
		if !s.inFlight.CompareAndSwap(0, 1) {
			s.logger.InfoContext(ctx, logger.CatApp, "compact rejected: session busy, message queued",
				"session_id", s.ID,
			)
			if !rejectsBusyQueue(ctx) {
				s.pending.Enqueue(prompt)
			}
			return nil, ErrQueued
		}
		s.touch()
		// Record the "/compact" prompt in CW + timeline so it survives the
		// post-completion loadHistory. Without this the user's prompt is
		// silently dropped from the chat UI.
		s.cw.Push(ctxwin.RoleUser, prompt)
		out := make(chan iface.AgentEvent, 2)
		go func() {
			defer close(out)
			defer s.inFlight.Store(0)
			defer s.touch()
			if summary, err := s.Compact(ctx); err != nil {
				out <- agent.ContentDeltaEvent{Delta: "Compact failed: " + err.Error()}
				out <- agent.DoneEvent{Content: "Compact failed: " + err.Error()}
			} else {
				if summary == "" {
					summary = "Context window compacted (no content to summarize)"
				}
				out <- agent.ContentDeltaEvent{Delta: summary}
				out <- agent.DoneEvent{Content: summary}
			}
		}()
		return out, nil

	case "/help", "/?":
		out := make(chan iface.AgentEvent, 2)
		go func() {
			defer close(out)
			text := "Available commands:\n" +
				"- `/help` or `/?` — View available commands\n" +
				"- `/cancel` — Cancel current task\n" +
				"- `/clear` — Clear dialogue history\n" +
				"- `/compact` — Compact context window (no memory save)\n" +
				"- `/init` — Create/update AGENTS.md in the project directory (L2 sessions only)\n" +
				"- `/version` — View version number\n" +
				"- `/l0` — Lock routing level to L0 (conversational)\n" +
				"- `/l1` — Lock routing level to L1 (single file modification)\n" +
				"- `/l2` — Lock routing level to L2 (multi-file modification)\n" +
				"- `/l3` — Lock routing level to L3 (complex architecture refactoring)"
			out <- agent.ContentDeltaEvent{Delta: text}
			out <- agent.DoneEvent{Content: text}
		}()
		return out, nil

	case "/clear":
		// Acquire inFlight to serialize with forwarder goroutine.
		// Clear modifies CW state and must not run concurrently with AskStream.
		if !s.inFlight.CompareAndSwap(0, 1) {
			s.logger.InfoContext(ctx, logger.CatApp, "clear rejected: session busy, message queued",
				"session_id", s.ID,
			)
			if !rejectsBusyQueue(ctx) {
				s.pending.Enqueue(prompt)
			}
			return nil, ErrQueued
		}
		s.touch()
		out := make(chan iface.AgentEvent, 2)
		go func() {
			defer close(out)
			defer s.inFlight.Store(0)
			defer s.touch()
			if err := s.Clear(); err != nil {
				out <- agent.ContentDeltaEvent{Delta: "Clear failed: " + err.Error()}
				out <- agent.DoneEvent{Content: "Clear failed: " + err.Error()}
			} else {
				out <- agent.ContentDeltaEvent{Delta: "Dialogue history cleared and saved to conversationlog."}
				out <- agent.DoneEvent{Content: "Session history cleared."}
			}
		}()
		return out, nil

	case "/version":
		out := make(chan iface.AgentEvent, 2)
		go func() {
			defer close(out)
			v := Version
			if v == "" {
				v = "SoloQueue"
			} else {
				v = "SoloQueue " + v
			}
			out <- agent.ContentDeltaEvent{Delta: v}
			out <- agent.DoneEvent{Content: v}
		}()
		return out, nil

	case "/init":
		if s.Agent.WorkDir == "" {
			out := make(chan iface.AgentEvent, 1)
			go func() {
				defer close(out)
				msg := "Init failed: no project directory set — /init is only available in L2 sessions with a project workDir"
				out <- agent.ContentDeltaEvent{Delta: msg}
				out <- agent.DoneEvent{Content: msg}
			}()
			return out, nil
		}
		// Replace prompt with LLM init instructions; fall through to normal processing.
		// The agent will explore the project and create/update AGENTS.md using tools.
		prompt = BuildInitPrompt(s.Agent.WorkDir)
		// Intentionally no return — let it reach the LLM agent below.

	}

	// Reset cancelled flag to prevent leakage of the residual flag from previous AskStream to this call.
	// See isCancelledAndReset - forwarder goroutine sets this flag when askCtx is cancelled,
	// consumed by the adapter (e.g. qqbot_adapter) after the event loop. If the adapter
	// returns early due to ErrorEvent and does not consume it, the residual flag causes the next AskStream to incorrectly report cancellation.
	s.cancelled.Store(false)

	// L1 async delegation does not block new messages. Agent mailbox guarantees serial execution of jobs:
	// resumeTurn (high priority) executes before new message jobs, keeping CW order naturally correct.

	if !s.inFlight.CompareAndSwap(0, 1) {
		s.logger.InfoContext(ctx, logger.CatApp, "askstream rejected: session busy, message queued",
			"session_id", s.ID,
			"prompt_len", len(prompt),
		)
		if !rejectsBusyQueue(ctx) {
			s.pending.Enqueue(prompt)
		}
		return nil, ErrQueued
	}
	clientCtx := ctx
	askCtx, askCancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Minute)
	askCtx = iface.ContextWithIsQBot(askCtx, s.IsQBot())
	cancelID := s.registerActiveCancel(askCancel)
	// Routing, memory recall, the leader LLM, local children, and cross-team
	// helpers all use this same cancellation root.
	ctx = askCtx
	// Note: the release of inFlight is handled by the forwarder goroutine below
	// checkAutoClear must happen before touch (here lastActive is the end time of the previous Ask)
	s.checkAutoClear()
	s.touch()

	start := time.Now()

	// ── Task routing: classify prompt and set model override ──
	effectiveCW := s.Agent.Def.ContextWindow
	if effectiveCW <= 0 {
		effectiveCW = agent.DefaultContextWindow
	}
	if s.Router != nil {
		// Check for explicit level lock/unlock commands (/l0, /l1, /l2, /l3)
		if newLevel, isLock := parseLevelLockCommand(prompt); isLock {
			s.lastLevelMu.Lock()
			s.levelLocked = true
			s.lastLevel = newLevel
			s.lastLevelMu.Unlock()
			s.logger.DebugContext(ctx, logger.CatApp, "task level locked by user",
				"session_id", s.ID,
				"level", newLevel,
			)
		}

		s.lastLevelMu.RLock()
		locked := s.levelLocked
		priorLevel := s.lastLevel
		cachedResult := s.lastRouteResult
		s.lastLevelMu.RUnlock()

		var result RouteResult
		var err error

		if locked && !isLevelLockCommand(prompt) {
			// Locked: skip routing, reuse cached model params
			result = cachedResult
			if result.ModelID == "" {
				// If we have a locked level but no cached result (e.g. restart),
				// fallback to the agent's default model and the locked level.
				result.Level = s.lastLevel
				result.ProviderID = s.Agent.Def.ProviderID
				result.ModelID = s.Agent.Def.ModelID
				result.ThinkingEnabled = s.Agent.Def.ThinkingEnabled
				result.ReasoningEffort = s.Agent.Def.ReasoningEffort
				result.ContextWindow = s.Agent.Def.ContextWindow
			}
			s.logger.DebugContext(ctx, logger.CatApp, "task routing skipped (level locked)",
				"session_id", s.ID,
				"level", result.Level,
			)
		} else {
			routerCtx := telemetry.WithTelemetryContext(ctx, s.TeamID, telemetry.UsageRouter)
			result, err = s.Router(routerCtx, prompt, priorLevel, s.cw.BuildPayload())
			if err != nil {
				s.logger.WarnContext(ctx, logger.CatApp, "task router failed, using default model",
					"session_id", s.ID,
					"err", err.Error(),
				)
				// Don't return — proceed with defaults (no model override)
				result = RouteResult{
					ClassifierWarning: "Task classification degraded: " + err.Error(),
				}
			}
		}

		if result.Level != "" {
			s.logger.DebugContext(ctx, logger.CatApp, "task router applied model override",
				"session_id", s.ID,
				"provider_id", result.ProviderID,
				"model_id", result.ModelID,
				"thinking_enabled", result.ThinkingEnabled,
				"reasoning_effort", result.ReasoningEffort,
				"level", result.Level,
			)
			s.Agent.SetModelOverride(&agent.ModelParams{
				ProviderID:      result.ProviderID,
				ModelID:         result.ModelID,
				ThinkingEnabled: result.ThinkingEnabled,
				ReasoningEffort: result.ReasoningEffort,
				ThinkingType:    result.ThinkingType,
				TaskType:        result.Level,
				ContextWindow:   result.ContextWindow,
				Vision:          result.Vision,
			})
			s.lastLevelMu.Lock()
			s.lastLevel = result.Level
			s.lastRouteResult = result
			s.lastLevelMu.Unlock()

			// Persist lastLevel to meta.json so it survives restarts.
			// Without this, a restarted session loses its task level context
			// and follow-up questions get misclassified as L0 conversation.
			// L1 sessions leave metaL2ID empty and skip this write.
			if s.metaL2ID != "" {
				if err := MergeAndSave(s.metaWorkDir, s.metaL2ID, func(m *SessionMeta) {
					m.Level = result.Level
				}); err != nil {
					s.logger.WarnContext(ctx, logger.CatApp, "persist lastLevel to meta.json failed",
						"session_id", s.ID,
						"err", err.Error(),
					)
				}
			}

			if result.ContextWindow > 0 {
				effectiveCW = result.ContextWindow
			}
		}
	}

	// ── Resize context window to match effective model ──

	// Resize and push user prompt atomically (both hold cw.Lock)
	s.mu.Lock()
	s.cw.Resize(effectiveCW, 0, 0)
	cwLenBeforeTurn := s.cw.Len()
	// Extract images from context if present (e.g., from qbot image uploads).
	// Images are passed as []llm.ImageContent via context.WithValue.
	var pushOpts []ctxwin.PushOption
	if images, ok := ctx.Value(ctxwin.ImageContextKey).([]llm.ImageContent); ok && len(images) > 0 {
		pushOpts = append(pushOpts, ctxwin.WithImages(images))
	}
	s.cw.Push(ctxwin.RoleUser, prompt, pushOpts...)
	s.mu.Unlock()

	s.logger.DebugContext(ctx, logger.CatApp, "askstream: prompt pushed to context window",
		"session_id", s.ID,
		"prompt_len", len(prompt),
	)

	srcCh, err := s.Agent.AskStreamWithHistory(askCtx, s.cw, prompt)
	if err != nil {
		// Agent stopped: attempt to restart and retry once
		if errors.Is(err, agent.ErrStopped) || errors.Is(err, agent.ErrNotStarted) {
			s.logger.InfoContext(ctx, logger.CatApp, "askstream: agent not running, attempting restart",
				"session_id", s.ID,
				"err", err.Error(),
			)
			if startErr := s.Agent.Start(context.Background()); startErr != nil {
				s.logger.WarnContext(ctx, logger.CatApp, "askstream: agent restart failed",
					"session_id", s.ID,
					"err", startErr.Error(),
				)
			} else {
				// Retry once
				srcCh, err = s.Agent.AskStreamWithHistory(askCtx, s.cw, prompt)
				if err == nil {
					goto enqueued
				}
			}
		}

		// Enqueue failure: cleanup
		s.unregisterActiveCancel(cancelID)
		askCancel()

		s.mu.Lock()
		s.cw.Truncate(cwLenBeforeTurn)
		s.mu.Unlock()
		s.inFlight.Store(0)

		s.logger.WarnContext(ctx, logger.CatApp, "askstream: agent stream setup failed",
			"session_id", s.ID,
			"err", err.Error(),
		)
		return nil, err
	}

enqueued:

	out := make(chan iface.AgentEvent, 64)
	go func() {
		// Cleanup: unregister this turn and release askCtx when the goroutine ends.
		defer func() {
			s.unregisterActiveCancel(cancelID)
			askCancel()
		}()
		defer close(out)
		defer s.inFlight.Store(0)
		defer s.touch()
		defer s.closeTurnDone()
		defer func() {
			if r := recover(); r != nil {
				s.logger.ErrorContext(ctx, logger.CatApp, "session event processor panic recovered",
					"session_id", s.ID,
					"panic", fmt.Sprintf("%v", r),
				)
			}
		}()

		var finalContent string
		var finalReasoning string
		var gotDone bool
		var eventCount int
		var accContent strings.Builder // accumulates streamed content for partial flush on non-normal exit
		var accReasoning strings.Builder
		var lastPushedContent string // tracks last content pushed to cw (avoids duplicate partial flush)
		// persistedAssistantDump captures the concatenation of every assistant
		// content already written to the timeline via the CW push hook (agent's
		// postIteration pushes assistant(tool_calls) per iteration). Captured
		// BEFORE rollback truncates the CW, so the partial-flush defer can skip
		// content that was already persisted (prevents duplicate timeline rows
		// when a turn is cancelled mid-delegation or after tool execution).
		var persistedAssistantDump string
		// Only scans assistant rows pushed AFTER this turn's user message
		// (index cwLenBeforeTurn). Assistant content from previous turns lives
		// before that index — including it would make the prefix comparison
		// fail whenever a previous reply shares text with this turn's output,
		// silently defeating the duplicate-flush guard.
		assistantDump := func() string {
			var b strings.Builder
			for i := cwLenBeforeTurn; i < s.cw.Len(); i++ {
				if m, ok := s.cw.MessageAt(i); ok && m.Role == ctxwin.RoleAssistant {
					b.WriteString(m.Content)
				}
			}
			return b.String()
		}
		var rollbackOnce sync.Once
		rollbackTurn := func() {
			rollbackOnce.Do(func() {
				persistedAssistantDump = assistantDump()
				s.mu.Lock()
				s.cw.Truncate(cwLenBeforeTurn)
				s.mu.Unlock()
			})
		}

		// Flush partial content on any non-normal exit (Stop, panic, srcCh close).
		// Declared after the recover defer so it runs first (LIFO), before panic recovery.
		// Skipped on normal completion (gotDone=true) because PushHook already wrote the assistant message.
		// Also skipped if accContent matches the last content we already pushed to cw
		// (this happens when the L2 ran multiple turns and the last turn produced no new content).
		defer func() {
			if !gotDone && accContent.Len() > 0 && s.tl != nil {
				pending := accContent.String()
				// Skip if this content is identical to what we already pushed to cw
				// in a previous turn's DoneEvent. This prevents duplicate entries when
				// the LLM returns a multi-turn response (e.g., content + tool_call + empty
				// content) and the session is force-killed before the last DoneEvent.
				if pending == lastPushedContent {
					return
				}
				// The accumulated content may already be persisted by the agent's
				// per-iteration CW pushes (postIteration → pushHook → timeline).
				// When a turn is cancelled after tool execution / async delegation,
				// the agent has already written the earlier iterations' content;
				// flushing the whole buffer again produces duplicate timeline rows.
				// Strip the already-persisted prefix and flush only the increment —
				// this preserves content produced by the cancelled in-flight turn
				// while avoiding the duplicate row for what was already written.
				dump := persistedAssistantDump
				if dump == "" {
					// rollbackTurn never ran (e.g. srcCh closed without a DoneEvent):
					// scan the CW directly — it still holds the persisted content.
					dump = assistantDump()
				}
				if dump != "" {
					pending = partialFlushRemainder(pending, dump)
					if pending == "" {
						s.logger.DebugContext(ctx, logger.CatApp, "askstream: partial flush skipped (content already persisted by push hook)",
							"session_id", s.ID,
							"content_len", accContent.Len(),
							"persisted_len", len(dump),
						)
						return
					}
				}
				_ = s.tl.AppendMessage(&timeline.MessagePayload{
					Role:             "assistant",
					Content:          pending,
					ReasoningContent: accReasoning.String(),
					AgentID:          s.Agent.InstanceID,
				})
				s.logger.DebugContext(ctx, logger.CatApp, "askstream: partial assistant content flushed to timeline",
					"session_id", s.ID,
					"content_len", accContent.Len(),
				)
			}
		}()

		clientDisconnected := false
		for {
			var ev agent.AgentEvent
			if clientDisconnected {
				select {
				case e, ok := <-srcCh:
					if !ok {
						goto done
					}
					ev = e
				case <-askCtx.Done():
					s.cancelled.Store(true)
					rollbackTurn()

					s.logger.DebugContext(ctx, logger.CatApp, "askstream cancelled (read)",
						"session_id", s.ID,
						"events_processed", eventCount,
						"duration_ms", time.Since(start).Milliseconds(),
					)
					return
				}
			} else {
				select {
				case e, ok := <-srcCh:
					if !ok {
						goto done
					}
					ev = e
				case <-askCtx.Done():
					s.cancelled.Store(true)
					rollbackTurn()

					s.logger.DebugContext(ctx, logger.CatApp, "askstream cancelled (read)",
						"session_id", s.ID,
						"events_processed", eventCount,
						"duration_ms", time.Since(start).Milliseconds(),
					)
					return
				case <-clientCtx.Done():
					clientDisconnected = true
					continue
				}
			}
			if clientDisconnected {
				select {
				case out <- ev:
					eventCount++
				default:
				}
			} else {
				select {
				case out <- ev:
					eventCount++
				case <-askCtx.Done():
					s.cancelled.Store(true)
					rollbackTurn()

					s.logger.DebugContext(ctx, logger.CatApp, "askstream cancelled (write)",
						"session_id", s.ID,
						"events_processed", eventCount,
						"duration_ms", time.Since(start).Milliseconds(),
					)
					return
				case <-clientCtx.Done():
					clientDisconnected = true
					select {
					case out <- ev:
						eventCount++
					default:
					}
				}
			}

			switch e := ev.(type) {
			case agent.ToolNeedsConfirmEvent:
				s.logger.InfoContext(ctx, logger.CatApp, "session-forwarder: confirm event received and forwarded",
					"session_id", s.ID,
					"call_id", e.CallID,
					"tool_name", e.Name,
				)
			case agent.DelegationStartedEvent:
				// Async delegation started: release inFlight, allowing user to send new messages
				s.logger.DebugContext(ctx, logger.CatApp, "delegation started",
					"session_id", s.ID,
				)
				s.newTurnDone()
				s.inFlight.Store(0)
			case agent.ContentDeltaEvent:
				accContent.WriteString(e.Delta)
			case agent.ReasoningDeltaEvent:
				accReasoning.WriteString(e.Delta)
			case agent.DoneEvent:
				finalContent = e.Content
				finalReasoning = e.ReasoningContent
				gotDone = true
				s.logger.DebugContext(ctx, logger.CatApp, "askstream done event received",
					"session_id", s.ID,
					"content_len", len(e.Content),
					"reasoning_len", len(e.ReasoningContent),
				)
			case agent.ErrorEvent:
				// Error: roll back the entire incomplete turn.
				rollbackTurn()

				s.logger.WarnContext(ctx, logger.CatApp, "askstream error event, user prompt removed",
					"session_id", s.ID,
					"err", e.Err.Error(),
				)
			}
		}
	done:
		// Check if cancellation occurred between goto done and this label (narrow race window)
		if askCtx.Err() != nil {
			s.cancelled.Store(true)
			rollbackTurn()
		} else if gotDone {
			if finalContent != "" {
				s.mu.Lock()
				opts := []ctxwin.PushOption{ctxwin.WithReasoningContent(finalReasoning)}
				s.cw.Push(ctxwin.RoleAssistant, finalContent, opts...)
				// Track what we just pushed so the partial-flush defer can skip duplicates.
				lastPushedContent = finalContent
				s.mu.Unlock()

				s.logger.DebugContext(ctx, logger.CatApp, "askstream: assistant reply pushed to context window",
					"session_id", s.ID,
				)
			} else {
				// Empty assistant reply — invalid for LLM API.
				// Skip the push but keep the user prompt for context.
				s.logger.WarnContext(ctx, logger.CatApp, "askstream: empty assistant reply skipped",
					"session_id", s.ID,
					"reasoning_len", len(finalReasoning),
				)
			}
		}
		// Delegation round completed: close turnDone channel, notify waiting new messages
		s.closeTurnDone()

		s.logger.DebugContext(ctx, logger.CatApp, "askstream complete",
			"session_id", s.ID,
			"events_processed", eventCount,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}()
	return out, nil
}

// partialFlushRemainder computes what a non-normal-exit flush should actually
// write, given the accumulated content buffer and the assistant content that
// was already persisted this turn by the agent's per-iteration CW pushes.
//
// Returns "" when everything pending was already persisted (pure duplicate —
// flush should be skipped), otherwise the un-persisted increment.
func partialFlushRemainder(pending, persisted string) string {
	if persisted == "" {
		return pending
	}
	if strings.HasPrefix(pending, persisted) {
		return strings.TrimPrefix(pending, persisted)
	}
	// Accumulated content does not start with the persisted content (e.g.
	// previous turns share text with this turn's output): cannot safely
	// slice — write the whole buffer rather than dropping anything.
	return pending
}

// Close marks session as closed, preventing new Asks; does not stop agent
func (s *Session) Close() {
	s.closed.Store(true)

	s.logger.InfoContext(context.Background(), logger.CatApp, "session closed",
		"session_id", s.ID,
		"lifetime_sec", time.Since(s.Created).Seconds(),
	)

	// Close timeline Writer, flush to disk and release file handle
	if s.tl != nil {
		s.tl.Close()
	}

	// Close session logger
	if err := s.logger.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "session close: logger close error: %v\n", err)
	}
}

// closeTurnDone safely closes turnDone channel and cleans up state.
// Can be safely called multiple times (idempotent).
func (s *Session) closeTurnDone() {
	s.turnMu.Lock()
	defer s.turnMu.Unlock()
	if s.turnDone != nil && !s.turnDoneClosed {
		close(s.turnDone)
		s.turnDoneClosed = true
		s.logger.DebugContext(context.Background(), logger.CatApp, "delegation turn completed",
			"session_id", s.ID,
		)
	}
	s.delegationPending.Store(false)
}

// newTurnDone creates a new turnDone channel.
func (s *Session) newTurnDone() {
	s.turnMu.Lock()
	defer s.turnMu.Unlock()
	s.turnDone = make(chan struct{})
	s.turnDoneClosed = false
	s.delegationPending.Store(true)
}

func (s *Session) touch() {
	s.lastActive.Store(time.Now().UnixNano())
}

// LastActive returns the last active time of the session.
func (s *Session) LastActive() time.Time {
	return time.Unix(0, s.lastActive.Load())
}

type activeTurnCancel struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// registerActiveCancel adds a top-level turn cancellation root to the session.
func (s *Session) registerActiveCancel(cancel context.CancelFunc) uint64 {
	s.cancelMu.Lock()
	defer s.cancelMu.Unlock()
	s.nextCancelID++
	id := s.nextCancelID
	s.activeCancels[id] = activeTurnCancel{cancel: cancel, done: make(chan struct{})}
	return id
}

func (s *Session) unregisterActiveCancel(id uint64) {
	s.cancelMu.Lock()
	turn, ok := s.activeCancels[id]
	if ok {
		delete(s.activeCancels, id)
		close(turn.done)
	}
	s.cancelMu.Unlock()
}

// CancelCurrent cancels every live top-level turn in this session. Delegated
// and cross-team calls derive their contexts from these roots, so cancellation
// propagates through the complete request tree. Session and Agent lifecycles
// are intentionally left untouched and can serve later messages normally.
func (s *Session) CancelCurrent(reason string) error {
	s.cancelMu.Lock()
	turns := make([]activeTurnCancel, 0, len(s.activeCancels))
	for _, turn := range s.activeCancels {
		turns = append(turns, turn)
	}
	s.cancelMu.Unlock()

	if len(turns) == 0 {
		return ErrNoActiveTask
	}
	for _, turn := range turns {
		turn.cancel()
	}

	// WebSocket messages are handled serially. Waiting for the forwarding
	// goroutines to release inFlight ensures a chat_send received immediately
	// after chat_cancel starts a fresh turn instead of being stranded in the
	// cancelled turn's pending queue.
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for _, turn := range turns {
		select {
		case <-turn.done:
		case <-deadline.C:
			s.logger.WarnContext(context.Background(), logger.CatApp, "session cancellation cleanup timed out",
				"session_id", s.ID,
				"active_turns", len(turns),
			)
			return nil
		}
	}

	s.logger.InfoContext(context.Background(), logger.CatApp, "session task tree cancelled",
		"session_id", s.ID,
		"active_turns", len(turns),
		"reason", reason,
	)
	return nil
}

// isCancelledAndReset checks if the forwarder exited due to cancellation.
// Consumes the one-time cancelled flag and resets it to false, ensuring ErrCancelled is returned only once.
// Called by SessionAskAdapter after the AskStream event loop.
func (s *Session) isCancelledAndReset() bool {
	return s.cancelled.CompareAndSwap(true, false)
}

// ─── SessionManager ──────────────────────────────────────────────────────

// AgentFactory constructs and starts a new agent given teamID, returning ContextWindow and optional TimelineWriter
//
// **Important**: the passed ctx should NOT be directly passed to agent.Start -- agent lifecycle is independent of
// a single Init call. The factory should use context.Background() as the agent's parent ctx.
// Here ctx is only for brief internal use within the factory (e.g. loading network configs, timeout checks).
//
// The returned *timeline.Writer is closed automatically when Session.Close; can return nil if not needed.
type AgentFactory func(ctx context.Context, teamID string) (*agent.Agent, *ctxwin.ContextWindow, *timeline.Writer, error)

// SessionManager manages the unique active session
type SessionManager struct {
	factory       AgentFactory
	routerFunc    TaskRouterFunc
	memoryHook    MemoryHook
	memoryManager *conversationlog.Manager
	logger        *logger.Logger

	idleTimeout      time.Duration // 0 = disabled; for auto-clear idle sessions
	compactThreshold int           // 0 = disabled; minimum tokens to trigger compact

	mu      sync.Mutex
	session *Session
	closed  atomic.Bool
}

// NewSessionManager constructs the manager
func NewSessionManager(factory AgentFactory, l *logger.Logger) *SessionManager {
	if l == nil {
		var err error
		l, err = logger.System("/tmp", logger.WithConsole(false), logger.WithFile(false))
		if err != nil {
			panic(err)
		}
	}
	return &SessionManager{
		factory: factory,
		logger:  l,
	}
}

// SetRouter sets the task router function for the session.
// Must be called before Init(). Not thread-safe for setup.
func (m *SessionManager) SetRouter(fn TaskRouterFunc) {
	m.routerFunc = fn
}

// SetMemoryHook sets the callback for short-term memory recording.
// Must be called before Init(). Not thread-safe for setup.
func (m *SessionManager) SetMemoryHook(hook MemoryHook) {
	m.memoryHook = hook
}

// SetMemoryManager sets the memory manager for dedup cursor tracking.
// Must be set alongside SetMemoryHook. Not thread-safe for setup.
func (m *SessionManager) SetMemoryManager(mm *conversationlog.Manager) {
	m.memoryManager = mm
}

// SetIdleReaper enables automatic context compression for idle sessions.
// When the session has been idle for longer than idleTimeout AND the context
// window exceeds compactThreshold tokens, the old context is compressed into
// a summary and injected back as a system message.
//
// Must be called before Init(). Not thread-safe for setup.
// Pass idleTimeout <= 0 or compactThreshold <= 0 to disable.
func (m *SessionManager) SetIdleReaper(idleTimeout time.Duration, compactThreshold int) {
	m.idleTimeout = idleTimeout
	m.compactThreshold = compactThreshold
}

// Init creates the unique session; repeated calls return the existing session
func (m *SessionManager) Init(ctx context.Context, teamID string) (*Session, error) {
	initStart := time.Now()
	m.mu.Lock()
	if m.session != nil {
		s := m.session
		m.mu.Unlock()
		m.logger.DebugContext(ctx, logger.CatApp, "session init: reusing existing session",
			"duration", time.Since(initStart).String())
		return s, nil
	}
	m.mu.Unlock()

	if m.closed.Load() {
		m.logger.DebugContext(ctx, logger.CatApp, "session init rejected: manager closed")
		return nil, ErrSessionClosed
	}

	m.logger.InfoContext(ctx, logger.CatApp, "session init: calling factory")
	factoryStart := time.Now()
	a, cw, tl, err := m.factory(ctx, teamID)
	m.logger.InfoContext(ctx, logger.CatApp, "session init: factory returned",
		"duration", time.Since(factoryStart).String(), "err", fmt.Sprintf("%v", err))
	if err != nil {
		m.logger.WarnContext(ctx, logger.CatApp, "session factory failed",
			"team_id", teamID,
			"err", err.Error(),
		)
		return nil, fmt.Errorf("agent factory: %w", err)
	}
	// L1 orchestrator uses a fixed session ID so it is always the same session
	// regardless of how many times the server restarts.
	id := "l1-session"

	sessionLogger := m.logger.Child()
	s := NewSession(id, teamID, a, cw, tl, sessionLogger)

	if m.routerFunc != nil {
		s.Router = m.routerFunc
	}
	if m.memoryHook != nil {
		s.memoryHook = m.memoryHook
	}
	if m.memoryManager != nil {
		s.memoryManager = m.memoryManager
	}
	if m.idleTimeout > 0 {
		s.idleTimeout = m.idleTimeout
		s.compactThreshold = m.compactThreshold
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed.Load() {
		_ = a.Stop(time.Second)
		return nil, ErrSessionClosed
	}
	m.session = s

	m.logger.InfoContext(ctx, logger.CatApp, "session initialized",
		"session_id", id,
		"team_id", teamID,
		"total_duration", time.Since(initStart).String(),
	)

	return s, nil
}

// Session returns the current session; returns nil if uninitialized
func (m *SessionManager) Session() *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.session
}

// Shutdown closes the session and blocks new Init
func (m *SessionManager) Shutdown(stopTimeout time.Duration) {
	m.closed.Store(true)
	m.mu.Lock()
	s := m.session
	m.session = nil
	m.mu.Unlock()

	if s != nil {
		_ = s.Agent.Stop(stopTimeout)
		s.Close()
	}

	m.logger.InfoContext(context.Background(), logger.CatApp, "session manager shutdown completed")
}

// ─── Level lock helpers ─────────────────────────────────────────────────────

// levelLockCommands maps slash commands to level labels.
var levelLockCommands = map[string]string{
	"l0": "L0-Conversation",
	"l1": "L1-SimpleSingleFile",
	"l2": "L2-MediumMultiFile",
	"l3": "L3-ComplexRefactoring",
}

// parseLevelLockCommand checks if prompt starts with a level-lock command.
// Returns (levelLabel, true) if found, ("", false) otherwise.
func parseLevelLockCommand(prompt string) (string, bool) {
	trimmed := strings.TrimSpace(prompt)
	for cmd, label := range levelLockCommands {
		prefix := "/" + cmd
		if strings.HasPrefix(trimmed, prefix+" ") || trimmed == prefix {
			return label, true
		}
	}
	return "", false
}

// isLevelLockCommand returns true if prompt is a level-lock command
// (including when followed by content, e.g., "/l2 analyze this").
func isLevelLockCommand(prompt string) bool {
	_, ok := parseLevelLockCommand(prompt)
	return ok
}

// formatPayloadForMemory converts ctxwin payload messages to a plain-text string
// suitable for short-term memory summarization. Skips system messages.
// Emits [timestamp] headers at each role transition so the LLM can group events
// chronologically by when they actually occurred.
func formatPayloadForMemory(payload []ctxwin.PayloadMessage) string {
	var b strings.Builder
	var lastTS time.Time
	for _, m := range payload {
		if m.Role == "system" {
			continue
		}
		// Emit timestamp when it changes (new turn boundary), regardless of role.
		if !m.Timestamp.IsZero() && !m.Timestamp.Equal(lastTS) {
			b.WriteString("[" + m.Timestamp.Format("2006-01-02 15:04") + "]\n")
			lastTS = m.Timestamp
		}
		b.WriteString(roleLabel(m.Role))
		if m.Role == "tool" {
			b.WriteString("(" + m.Name + ")")
		}
		b.WriteString(": ")
		content := m.Content
		if len(content) > 2000 {
			content = content[:2000] + "...(truncated)"
		}
		b.WriteString(content)
		b.WriteString("\n\n")
	}
	return b.String()
}

// roleLabel returns a capitalized display label for a message role.
func roleLabel(role string) string {
	switch role {
	case "user":
		return "User"
	case "assistant":
		return "Assistant"
	case "tool":
		return "Tool"
	default:
		return role
	}
}

// payloadDateGroup is a group of payload messages sharing the same date.
type payloadDateGroup struct {
	date time.Time
	msgs []ctxwin.PayloadMessage
}

// filterPayloadSince returns messages whose Timestamp is strictly after cursor.
// Returns the full slice when cursor is zero (never recorded).
func filterPayloadSince(payload []ctxwin.PayloadMessage, cursor time.Time) []ctxwin.PayloadMessage {
	if cursor.IsZero() {
		return payload
	}
	var out []ctxwin.PayloadMessage
	for _, m := range payload {
		if m.Timestamp.After(cursor) {
			out = append(out, m)
		}
	}
	return out
}

// groupPayloadByDate groups payload messages by the calendar date of their Timestamp.
func groupPayloadByDate(payload []ctxwin.PayloadMessage) []payloadDateGroup {
	byDate := make(map[string][]ctxwin.PayloadMessage)
	for _, m := range payload {
		date := m.Timestamp.Format("2006-01-02")
		byDate[date] = append(byDate[date], m)
	}
	result := make([]payloadDateGroup, 0, len(byDate))
	for dateStr, msgs := range byDate {
		t, _ := time.Parse("2006-01-02", dateStr)
		result = append(result, payloadDateGroup{date: t, msgs: msgs})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].date.Before(result[j].date) })
	return result
}

// filterMessagesSince returns messages whose Timestamp is strictly after cursor.
func filterMessagesSince(msgs []ctxwin.Message, cursor time.Time) []ctxwin.Message {
	if cursor.IsZero() {
		return msgs
	}
	var out []ctxwin.Message
	for _, m := range msgs {
		if m.Timestamp.After(cursor) {
			out = append(out, m)
		}
	}
	return out
}

// ─── Project init (/init) ─────────────────────────────────────────────────

// BuildInitPrompt returns an LLM prompt for initializing or updating the
// project's AGENTS.md. If the file already exists, it requests an update
// preserving existing content while correcting outdated sections. If the
// file does not exist, it requests a full project analysis and file creation.
// The returned prompt is designed to be passed through the normal LLM agent
// flow (AskStream), letting the agent explore the project with tools and
// produce a context-aware, project-specific result — matching the behavior of
// Claude Code's init command.
func BuildInitPrompt(workDir string) string {
	agentsPath := filepath.Join(workDir, "AGENTS.md")
	if _, err := os.Stat(agentsPath); err == nil {
		// Exists — update mode: refresh stale content, keep what's still correct.
		return fmt.Sprintf(
			`Read the existing AGENTS.md at %s, then explore the project at %s to identify what has changed.

Steps:
1. Read the existing AGENTS.md to understand current documentation.
2. Explore the project: check build files (Makefile, go.mod, package.json, etc.), directory structure, new dependencies, and recent changes.
3. Update AGENTS.md: remove outdated sections, add missing sections, correct stale commands/paths. Keep the same overall format — it should remain a tactical guide for AI coding agents.

IMPORTANT: Only write the AGENTS.md file. Do NOT output markdown explanations — use the Write tool to save the updated file.`,
			agentsPath, workDir,
		)
	}
	// Create mode — full project analysis.
	return fmt.Sprintf(
		`Analyze the project at %s and create an AGENTS.md file at %s.

Steps:
1. Explore the project to identify: build system and package manager (check for Makefile, go.mod, package.json, Cargo.toml, etc.), main languages/frameworks, directory layout, how to build/test/run.
2. Create AGENTS.md as a tactical guide for AI coding agents. Follow this format:
   - Build & Development (actual commands, not placeholders)
   - Running Locally (actual commands)
   - Tech Stack (with versions if discoverable)
   - Project Structure (key directories and their purpose)
   - Code Style & Conventions
   - Architecture (high-level overview)
   - Key Patterns

IMPORTANT: Only write the AGENTS.md file. Do NOT output markdown explanations — use the Write tool to save the file.`,
		workDir, agentsPath,
	)
}
