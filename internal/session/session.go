// Package session provides the "Dialogue Session" abstraction, wrapping agent + context window.
//
// Design principles:
//
//   - A Session corresponds to "a single independent conversation": bound to an *agent.Agent, holding
//     *ctxwin.ContextWindow to manage full conversation history (including intermediate tool call messages).
//   - Requests within the same Session are serial: a new request returns directly if the previous round is not finished
//     ErrSessionBusy (avoids context window out-of-order). The agent itself is also serial, offering double protection.
//   - SessionManager manages the unique active session; globally there is only one session.
package session

import (
	"context"
	"crypto/rand"
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
	toolset "github.com/xiaobaitu/soloqueue/internal/agenttools/tools"
	"github.com/xiaobaitu/soloqueue/internal/channel"
	"github.com/xiaobaitu/soloqueue/internal/dispatch"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
	"github.com/xiaobaitu/soloqueue/internal/infra/telemetry"
	"github.com/xiaobaitu/soloqueue/internal/llm"
	"github.com/xiaobaitu/soloqueue/internal/memory/conversation"
	"github.com/xiaobaitu/soloqueue/internal/memory/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/memory/timeline"
	"github.com/xiaobaitu/soloqueue/internal/runwatch"
)

// ─── Errors ────────────────────────────────────────────────────────────────

var (
	// ErrSessionBusy is returned when the session is busy with another request.
	ErrSessionBusy = errors.New("session: busy (another Ask in flight)")

	// ErrQueued is returned when the message is queued in the pending queue
	ErrQueued = errors.New("session: message queued")

	// ErrSessionClosed is returned when the session is closed
	ErrSessionClosed = errors.New("session: closed")

	// ErrNoActiveTask is returned when there is no active task to cancel
	ErrNoActiveTask = errors.New("session: no active task")
)

// IsSessionBusyError lets scheduler-facing interfaces classify contention
// without importing the concrete session package or matching error strings.
func (s *Session) IsSessionBusyError(err error) bool { return errors.Is(err, ErrSessionBusy) }

type rejectBusyQueueKey struct{}

type temporalExposureKey struct{}

type temporalExposure struct {
	receivedAt time.Time
}

// WithRejectBusyQueue marks a desktop request that must be rejected rather
// than inserted into the session pending queue when the session is busy.
func WithRejectBusyQueue(ctx context.Context) context.Context {
	return context.WithValue(ctx, rejectBusyQueueKey{}, true)
}

func rejectsBusyQueue(ctx context.Context) bool {
	v, _ := ctx.Value(rejectBusyQueueKey{}).(bool)
	return v
}

func withTemporalExposure(ctx context.Context, receivedAt time.Time) context.Context {
	return context.WithValue(ctx, temporalExposureKey{}, temporalExposure{receivedAt: receivedAt})
}

func temporalExposureFromContext(ctx context.Context) (temporalExposure, bool) {
	value, ok := ctx.Value(temporalExposureKey{}).(temporalExposure)
	return value, ok && !value.receivedAt.IsZero()
}

func inputPushOptions(ctx context.Context) []ctxwin.PushOption {
	metadata, ok := temporalExposureFromContext(ctx)
	if !ok {
		return nil
	}
	return []ctxwin.PushOption{
		ctxwin.WithTimestamp(metadata.receivedAt),
		ctxwin.WithExposeTimestamp(true),
	}
}

func (s *Session) enqueuePending(ctx context.Context, prompt string) {
	message := PendingMessage{Prompt: prompt}
	metadata, ok := temporalExposureFromContext(ctx)
	if ok {
		message.ReceivedAt = metadata.receivedAt
		message.ExposeTimestamp = true
	}
	s.pending.EnqueueMessage(message)
}

// Version is the current version of soloqueue. It is set at startup by the main command.
var Version = "0.1.0"

const defaultRequestTimeout time.Duration = 0

// CurrentLevel returns the classification level of the last routed task.
// Returns "" if no task has been routed yet or routing is disabled.
func (s *Session) CurrentLevel() string {
	s.lastLevelMu.RLock()
	defer s.lastLevelMu.RUnlock()
	return s.lastLevel
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

// ChannelMetadataStore persists channel sender metadata across restarts.
type ChannelMetadataStore interface {
	SaveChannelSenderData(targetID, channelType, metadata string) error
	GetChannelSenderData(targetID, channelType string) (string, error)
}

// ─── Session ──────────────────────────────────────────────────────────────

// Session represents a conversation session.
type Session struct {
	TargetID string
	TeamID   string
	Router   TaskRouterFunc // Optional: task routing classifier (nil = no routing, use default model)
	Created  time.Time

	mu              sync.Mutex
	agentMu         sync.RWMutex // serializes generation snapshots and swaps
	rebuildMu       sync.Mutex   // single-flight generation construction/swap
	generation      agentGeneration
	cw              *ctxwin.ContextWindow // Replaces original history, manages full conversation context
	tl              *timeline.Writer      // Timeline writer (can be nil, meaning no persistence)
	dispatchManager *dispatch.Manager
	dispatchInitErr error
	runWatch        *runwatch.Manager
	logger          *logger.Logger       // Session-level logger
	resourceCloser  func() error         // closes the logger handler owned by this Session
	metaStore       ChannelMetadataStore // Optional: for persisting channel sender metadata

	// pending queue: new messages enqueue when session is busy, popped and injected
	// into ContextWindow before the agent's next LLM API call in the tool loop, merging consecutive messages
	pending *PendingQueue

	// inFlight CAS lock for concurrent requests: 0 -> 1 enter; returns ErrSessionBusy on failure
	inFlight    atomic.Int32
	flightSeq   atomic.Uint64
	flightOwner atomic.Uint64

	// closed indicates if the Session has been deleted
	closed      atomic.Bool
	closeOnce   sync.Once
	disposeOnce sync.Once

	// lastActive for reaper cleanup; updated on every request
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
	activeCancels map[string]activeTurnCancel

	// channelSenders maps channel type ("qq"/"wechat") to send functions.
	// Registered by bridges when OnMessage fires. Protected by channelSendersMu.
	channelSenders      map[string]func(context.Context, string) error
	channelMediaSenders map[string]func(context.Context, []channel.OutboundMedia) error
	channelSendersMu    sync.RWMutex

	lastLevel          string       // last classified task type
	lastLevelMu        sync.RWMutex // protects lastLevel and lastRouteResult
	lastRouteResult    RouteResult  // cached route result (model params preserved)
	channelRouteMu     sync.Mutex
	channelRouteKey    string
	channelRouteOwners int

	// L2-only: coordinates persistent meta.json writes for level/git_base_ref/baseline.
	// Empty on L1 sessions; MergeAndSave is only called when metaL2ID is non-empty.
	metaWorkDir string
	metaL2ID    string
	// gitBaseRef captures the HEAD commit hash at session start (git repos).
	// Persisted in meta.json; read by the Changes tab to diff against this ref.
	gitBaseRef   string
	metaBaseline map[string]string // path→sha256 snapshot, non-git projects only

	memoryHook    MemoryHook            // optional callback for short-term memory (nil = disabled)
	memoryManager *conversation.Manager // for dedup cursor; set alongside memoryHook

	// personaStatePath points to the persona state.md used for the nightly
	// reflection; empty disables persona state injection.
	personaStatePath string

	personaLLM  agent.LLMClient // reflection LLM; nil = reflection disabled (L2 sessions)
	personaName func() string   // resolves assistant name from soul.md at call time

	personaProviderID string // fast/classifier model provider for reflection LLM calls
	personaModelID    string // fast/classifier model ID for reflection LLM calls

	VisionDescriber VisionDescriberFunc // optional callback to transcribe images when active model lacks vision

	idleTimeout       time.Duration // 0 = disabled; auto-clear idle sessions
	compactThreshold  int           // 0 = disabled; minimum CW tokens to trigger compact
	requestTimeout    time.Duration // session-scoped so deadline behavior can be tested without a production-length wait
	askStreamHistory  func(context.Context, *ctxwin.ContextWindow, string) (<-chan agent.AgentEvent, error)
	rebuildGeneration func(context.Context) (*agent.Agent, *agent.Supervisor, error)
	publishSupervisor func(*agent.Supervisor)
	removeSupervisor  func(*agent.Supervisor)
	agentRegistry     *agent.Registry
	lastJob           *agent.JobHandle
	isQBot            atomic.Bool
}

type agentGeneration struct {
	agent      *agent.Agent
	supervisor *agent.Supervisor
}

// RequestRoute is the immutable routing decision captured synchronously for a
// specific desktop request before AskStream returns. It does not depend on the
// actor having started and therefore survives queueing, yield, and generation
// replacement timing.
type RequestRoute struct {
	TaskType        string
	ModelID         string
	ProviderID      string
	AgentInstanceID string
}

type requestRouteCapture struct {
	once sync.Once
	ch   chan RequestRoute
}

type requestRouteCaptureKey struct{}

// WithRequestRouteCapture installs a request-owned one-shot route sink. The
// buffered channel is owned by the caller context: no Session map or cleanup
// path can leak if setup fails, a socket disconnects, or no consumer reads it.
func WithRequestRouteCapture(ctx context.Context) (context.Context, <-chan RequestRoute) {
	capture := &requestRouteCapture{ch: make(chan RequestRoute, 1)}
	return context.WithValue(ctx, requestRouteCaptureKey{}, capture), capture.ch
}

var quarantineGraceNanos atomic.Int64

func init() {
	quarantineGraceNanos.Store(int64(agent.JobWatchdogGrace))
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
		TargetID:       id,
		TeamID:         teamID,
		Created:        time.Now(),
		generation:     agentGeneration{agent: a},
		cw:             cw,
		tl:             tl,
		logger:         l,
		resourceCloser: l.Close,
		pending:        &PendingQueue{},
		activeCancels:  make(map[string]activeTurnCancel),
		requestTimeout: defaultRequestTimeout,
	}
	if a != nil {
		s.askStreamHistory = func(ctx context.Context, cw *ctxwin.ContextWindow, prompt string) (<-chan agent.AgentEvent, error) {
			ch, job, err := a.AskStreamWithHistoryTracked(ctx, cw, prompt)
			s.agentMu.Lock()
			s.lastJob = job
			s.agentMu.Unlock()
			return ch, err
		}
	}
	s.lastActive.Store(time.Now().UnixNano())
	if tl != nil {
		manager, err := dispatch.NewManager(tl.Dir(), id)
		if err != nil {
			s.dispatchInitErr = fmt.Errorf("session: dispatch manager initialization failed: %w", err)
			l.Warn(logger.CatApp, "session: dispatch manager unavailable", "err", err.Error())
		} else {
			s.dispatchManager = manager
			if a != nil && !a.HasTool("inspect_delegation") {
				if err := a.RegisterTool(toolset.NewInspectDelegationTool()); err != nil {
					l.Warn(logger.CatTool, "session: register inspect_delegation failed", "err", err.Error())
				}
			}
		}
	}

	// Wire pending message drainer so the agent injects queued messages
	// before each LLM API call.
	if cw != nil {
		cw.SetPendingDrainer(func() ctxwin.PendingInput {
			pending := s.pending.Drain()
			if pending.Content == "" {
				return ctxwin.PendingInput{}
			}
			trimmed := strings.TrimSpace(pending.Content)
			lower := strings.ToLower(trimmed)
			switch lower {
			case "/cancel", "/compact", "/help", "/?", "/clear", "/version":
				s.logger.WarnContext(context.Background(), logger.CatApp, "pending drain: dropping stale slash command",
					"target_id", s.TargetID,
					"command", lower,
				)
				return ctxwin.PendingInput{}
			}
			parts := make([]ctxwin.TemporalPart, len(pending.Parts))
			for i, part := range pending.Parts {
				parts[i] = ctxwin.TemporalPart{
					Content:         part.Prompt,
					Timestamp:       part.ReceivedAt,
					ExposeTimestamp: part.ExposeTimestamp,
				}
			}
			s.logger.InfoContext(context.Background(), logger.CatApp, "pending messages injected into context window",
				"target_id", s.TargetID,
				"prompt_len", len(pending.Content),
			)
			return ctxwin.PendingInput{Content: pending.Content, TemporalParts: parts}
		})
	}

	s.logger.InfoContext(context.Background(), logger.CatApp, "session created",
		"target_id", id,
		"team_id", teamID,
	)

	return s
}

func (s *Session) publishRequestRoute(ctx context.Context, a *agent.Agent, result RouteResult) {
	capture, _ := ctx.Value(requestRouteCaptureKey{}).(*requestRouteCapture)
	if capture == nil || a == nil {
		return
	}
	route := RequestRoute{
		TaskType:        result.Level,
		ModelID:         result.ModelID,
		ProviderID:      result.ProviderID,
		AgentInstanceID: a.InstanceID,
	}
	if override := iface.ModelOverrideFromContext(ctx); override != nil {
		if route.TaskType == "" {
			route.TaskType = override.TaskType
		}
		if route.ModelID == "" {
			route.ModelID = override.ModelID
		}
		if route.ProviderID == "" {
			route.ProviderID = override.ProviderID
		}
	}
	if route.ModelID == "" {
		route.ModelID = a.Def.ModelID
	}
	if route.ProviderID == "" {
		route.ProviderID = a.Def.ProviderID
	}
	capture.once.Do(func() {
		capture.ch <- route
		close(capture.ch)
	})
}

// SetRunWatch attaches the process-owned supervisor after Session construction
// so runtime wiring does not create a dependency cycle.
func (s *Session) SetRunWatch(manager *runwatch.Manager) {
	s.runWatch = manager
}

// SetAgentRebuilder supplies the owner-controlled fresh-generation factory
// used after a quarantined Agent ignored request cancellation.
func (s *Session) SetAgentRebuilder(fn func(context.Context) (*agent.Agent, error)) {
	s.agentMu.Lock()
	defer s.agentMu.Unlock()
	if fn == nil {
		s.rebuildGeneration = nil
		return
	}
	s.rebuildGeneration = func(ctx context.Context) (*agent.Agent, *agent.Supervisor, error) {
		a, err := fn(ctx)
		return a, nil, err
	}
}

// SetGenerationRebuilder installs the owner-controlled factory for a complete
// Agent generation. Persistent L2 sessions use this to replace both the leader
// and its Supervisor/L3 ownership domain.
func (s *Session) SetGenerationRebuilder(fn func(context.Context) (*agent.Agent, *agent.Supervisor, error)) {
	s.agentMu.Lock()
	defer s.agentMu.Unlock()
	s.rebuildGeneration = fn
}

// SetSupervisor publishes the Supervisor belonging to the current Agent.
func (s *Session) SetSupervisor(sv *agent.Supervisor, remove func(*agent.Supervisor)) {
	s.agentMu.Lock()
	s.generation.supervisor = sv
	s.removeSupervisor = remove
	s.agentMu.Unlock()
}

// SetSupervisorPublisher supplies the Runtime ownership hook used only after a
// prepared Agent/Supervisor generation has won the Session publication check.
// Construction must not publish the Supervisor itself.
func (s *Session) SetSupervisorPublisher(publish func(*agent.Supervisor)) {
	s.agentMu.Lock()
	s.publishSupervisor = publish
	s.agentMu.Unlock()
}

// PublishInitialGeneration makes the already constructed first generation
// discoverable only after Session has installed all lifecycle ownership hooks.
func (s *Session) PublishInitialGeneration() {
	s.agentMu.Lock()
	defer s.agentMu.Unlock()
	if s.generation.supervisor != nil && s.publishSupervisor != nil {
		s.publishSupervisor(s.generation.supervisor)
	}
	if s.generation.agent != nil {
		s.generation.agent.ActivateScheduling()
	}
}

// SetAgentRegistry supplies the registry owner so generation replacement can
// remove quarantined agents from the active scheduling/index set.
func (s *Session) SetAgentRegistry(registry *agent.Registry) {
	s.agentMu.Lock()
	s.agentRegistry = registry
	s.agentMu.Unlock()
}

// CurrentAgent returns a consistent snapshot of the active Agent generation.
// Callers must retain the returned pointer for the duration of one operation;
// a later watchdog replacement may publish a different generation.
func (s *Session) CurrentAgent() *agent.Agent {
	if s == nil {
		return nil
	}
	s.agentMu.RLock()
	defer s.agentMu.RUnlock()
	return s.generation.agent
}

// CurrentSupervisor returns the L3 owner paired with CurrentAgent's generation.
func (s *Session) CurrentSupervisor() *agent.Supervisor {
	if s == nil {
		return nil
	}
	s.agentMu.RLock()
	defer s.agentMu.RUnlock()
	return s.generation.supervisor
}

func (s *Session) acquireFlight() (uint64, bool) {
	if !s.inFlight.CompareAndSwap(0, 1) {
		return 0, false
	}
	id := s.flightSeq.Add(1)
	s.flightOwner.Store(id)
	return id, true
}

func (s *Session) releaseFlight(id uint64) {
	if id != 0 && s.flightOwner.CompareAndSwap(id, 0) {
		s.inFlight.Store(0)
	}
}

func (s *Session) askStream(ctx context.Context, cw *ctxwin.ContextWindow, prompt string) (<-chan agent.AgentEvent, *agent.JobHandle, error) {
	s.agentMu.RLock()
	legacy := s.askStreamHistory
	s.agentMu.RUnlock()
	s.agentMu.Lock()
	s.lastJob = nil
	s.agentMu.Unlock()
	if legacy == nil {
		return nil, nil, errors.New("session: agent stream unavailable")
	}
	ch, err := legacy(ctx, cw, prompt)
	s.agentMu.RLock()
	job := s.lastJob
	s.agentMu.RUnlock()
	return ch, job, err
}

func (s *Session) rebuildQuarantinedAgent(ctx context.Context, expected ...*agent.Agent) error {
	s.rebuildMu.Lock()
	defer s.rebuildMu.Unlock()
	s.agentMu.RLock()
	current := s.generation.agent
	rebuilder := s.rebuildGeneration
	s.agentMu.RUnlock()
	if len(expected) > 0 && expected[0] != nil && current != expected[0] {
		return nil
	}
	if rebuilder == nil {
		return agent.ErrQuarantined
	}
	newAgent, newSupervisor, err := rebuilder(ctx)
	if err != nil {
		return err
	}
	if newAgent == nil {
		return errors.New("session: agent rebuilder returned nil agent")
	}
	// A conforming factory creates the replacement pending. Keep the lifecycle
	// boundary defensive for legacy/test factories that return a schedulable
	// Agent so no work can be acquired between return and atomic publication.
	newAgent.DeactivateScheduling()
	fresh := agentGeneration{agent: newAgent, supervisor: newSupervisor}
	s.agentMu.Lock()
	// Close may begin while the factory is constructing outside agentMu. The
	// lifecycle owner revalidates both closed state and expected generation
	// before publication; rejected fresh resources are retired symmetrically.
	if s.closed.Load() || (len(expected) > 0 && expected[0] != nil && s.generation.agent != expected[0]) {
		closed := s.closed.Load()
		s.agentMu.Unlock()
		s.retireGeneration(fresh, true)
		if closed {
			return ErrSessionClosed
		}
		return nil
	}
	old := s.generation
	publish := func() {
		s.generation = fresh
		if s.publishSupervisor != nil && fresh.supervisor != nil {
			s.publishSupervisor(fresh.supervisor)
		}
		s.askStreamHistory = func(ctx context.Context, cw *ctxwin.ContextWindow, prompt string) (<-chan agent.AgentEvent, error) {
			ch, job, err := newAgent.AskStreamWithHistoryTracked(ctx, cw, prompt)
			s.agentMu.Lock()
			s.lastJob = job
			s.agentMu.Unlock()
			return ch, err
		}
	}
	if s.agentRegistry != nil && old.agent != nil {
		if err := s.agentRegistry.PublishReplacement(old.agent, newAgent, publish); err != nil {
			s.agentMu.Unlock()
			s.retireGeneration(fresh, true)
			return fmt.Errorf("session: publish replacement generation: %w", err)
		}
	} else {
		if old.agent != nil {
			old.agent.DeactivateScheduling()
		}
		publish()
		newAgent.ActivateScheduling()
	}
	s.agentMu.Unlock()
	s.retireGeneration(old, false)
	return nil
}

func (s *Session) retireGeneration(g agentGeneration, stop bool) {
	s.agentMu.RLock()
	registry := s.agentRegistry
	removeSupervisor := s.removeSupervisor
	s.agentMu.RUnlock()
	if g.agent != nil {
		g.agent.DeactivateScheduling()
	}
	if g.supervisor != nil {
		_ = g.supervisor.ReapAll(5 * time.Second)
		if removeSupervisor != nil {
			removeSupervisor(g.supervisor)
		}
	}
	if registry != nil && g.agent != nil {
		registry.Unregister(g.agent.InstanceID)
	}
	if g.agent != nil {
		if stop {
			_ = g.agent.Stop(5 * time.Second)
		} else {
			g.agent.Quarantine(agent.ErrQuarantined)
		}
	}
}

func (s *Session) WatchdogSnapshot(runID string) (runwatch.Snapshot, bool) {
	if s.runWatch == nil {
		return runwatch.Snapshot{}, false
	}
	return s.runWatch.Snapshot(runID)
}

// quarantineAgentAfterWatchdog prevents a non-cooperative job from keeping an
// Agent mailbox blocked after the request-facing watchdog has terminated.
// Replacement is owned by the SessionManager/Supervisor; this generation is
// deliberately never restarted in place because its late goroutine is unsafe.
func (s *Session) quarantineAgentAfterWatchdog(cause error, a *agent.Agent, job *agent.JobHandle) {
	if s == nil || a == nil || job == nil || !isWatchdogCause(cause) {
		return
	}
	if !job.Fence() {
		return
	}
	go func(a *agent.Agent, job *agent.JobHandle) {
		timer := time.NewTimer(time.Duration(quarantineGraceNanos.Load()))
		defer timer.Stop()
		select {
		case <-job.Done():
			job.ReleaseFence()
			return
		case <-timer.C:
		}
		// Revalidate completion, the exact installed fence, and actor ownership
		// in one Agent-owned transition. A simultaneous Done or async yield wins
		// over this timer and can never quarantine later work.
		job.QuarantineIfStillBlocking(cause)
	}(a, job)
}

func isWatchdogCause(cause error) bool {
	switch runwatch.CodeOf(cause) {
	case runwatch.CodeModelTransportStalled, runwatch.CodeModelFirstProgressStalled,
		runwatch.CodeModelSemanticStalled, runwatch.CodeToolStalled,
		runwatch.CodeDelegationOrphaned, runwatch.CodeRootOrphaned:
		return true
	default:
		return false
	}
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

// StripUploadedFilePrompts removes system-appended file upload blocks and vision transcription blocks.
func StripUploadedFilePrompts(s string) string {
	if idx := strings.Index(s, "\n\n[User has uploaded a file, saved locally at:\n"); idx >= 0 {
		s = s[:idx]
	} else if idx := strings.Index(s, "[User has uploaded a file, saved locally at:\n"); idx == 0 {
		s = ""
	}
	// Legacy WebSocket upload block (pre-format-unification timeline entries).
	if idx := strings.Index(s, "\n\n[Uploaded files:\n"); idx >= 0 {
		s = s[:idx]
	} else if idx := strings.Index(s, "[Uploaded files:\n"); idx == 0 {
		s = ""
	}
	if idx := strings.Index(s, "\n\n[System: The user included "); idx >= 0 {
		s = s[:idx]
	} else if idx := strings.Index(s, "[System: The user included "); idx == 0 {
		s = ""
	}
	return strings.TrimSpace(s)
}

// StripRecalledMemories removes directives and system metadata blocks from a message string.
func StripRecalledMemories(s string) string {
	s = StripDesignDirective(s)
	s = StripUploadedFilePrompts(s)
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

// Idle returns true if there is no request currently in flight.
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
		"target_id", s.TargetID,
		"channel_type", channelType,
		"had_previous", prev,
	)
}

// SetChannelSenderData saves the channel sender closure and persists its metadata to the DB if a store is configured.
func (s *Session) SetChannelSenderData(channelType string, metadata []byte, fn func(context.Context, string) error) {
	s.SetChannelSender(channelType, fn)
	if s.metaStore != nil {
		if err := s.metaStore.SaveChannelSenderData(s.TargetID, channelType, string(metadata)); err != nil {
			s.logger.WarnContext(context.Background(), logger.CatApp, "session: failed to save channel metadata",
				"target_id", s.TargetID,
				"channel_type", channelType,
				"err", err.Error(),
			)
		}
	}
}

// SetChannelMediaSender registers media delivery for one channel route.
func (s *Session) SetChannelMediaSender(channelType string, fn func(context.Context, []channel.OutboundMedia) error) {
	if channelType == "" || fn == nil {
		return
	}
	s.channelSendersMu.Lock()
	if s.channelMediaSenders == nil {
		s.channelMediaSenders = make(map[string]func(context.Context, []channel.OutboundMedia) error)
	}
	s.channelMediaSenders[channelType] = fn
	s.channelSendersMu.Unlock()
}

// SendMediaViaChannel sends media only through the configured notify channel.
func (s *Session) SendMediaViaChannel(ctx context.Context, media []channel.OutboundMedia) error {
	if len(media) == 0 {
		return nil
	}
	notifyChannel := ""
	if a := s.CurrentAgent(); a != nil {
		notifyChannel = a.Def.NotifyChannel
	}
	if notifyChannel == "" {
		return nil
	}
	s.channelSendersMu.RLock()
	fn := s.channelMediaSenders[notifyChannel]
	s.channelSendersMu.RUnlock()
	if fn != nil {
		return fn(ctx, media)
	}
	if s.metaStore != nil {
		metadata, err := s.metaStore.GetChannelSenderData(s.TargetID, notifyChannel)
		if err != nil {
			return err
		}
		if metadata != "" {
			if factory := channel.GetMediaSenderFactory(notifyChannel); factory != nil {
				return factory(ctx, []byte(metadata), media)
			}
		}
	}
	return nil
}

// HasNotifyChannel reports whether this session's agent has configured a
// notification channel. It does not claim that a live sender is available.
func (s *Session) HasNotifyChannel() bool {
	a := s.CurrentAgent()
	return a != nil && a.Def.NotifyChannel != ""
}

// SendViaChannel sends text through the configured notify channel.
// The channel is determined by the agent's NotifyChannel config (e.g. "qq" or "wechat").
// If notify_channel or its active sender is absent, no notification is sent.
func (s *Session) SendViaChannel(ctx context.Context, text string) error {
	notifyChannel := ""
	if a := s.CurrentAgent(); a != nil {
		notifyChannel = a.Def.NotifyChannel
	}
	if notifyChannel == "" {
		s.logger.WarnContext(ctx, logger.CatApp, "session: SendViaChannel skipped, no notify_channel configured",
			"target_id", s.TargetID,
		)
		return nil
	}

	s.channelSendersMu.RLock()
	fn := s.channelSenders[notifyChannel]
	s.channelSendersMu.RUnlock()

	if fn != nil {
		s.logger.InfoContext(ctx, logger.CatApp, "session: SendViaChannel delivering via memory",
			"target_id", s.TargetID,
			"notify_channel", notifyChannel,
			"text_len", len(text),
		)
		return fn(ctx, text)
	}

	if s.metaStore != nil {
		metadata, err := s.metaStore.GetChannelSenderData(s.TargetID, notifyChannel)
		if err != nil {
			s.logger.WarnContext(ctx, logger.CatApp, "session: failed to load channel metadata",
				"target_id", s.TargetID,
				"notify_channel", notifyChannel,
				"err", err.Error(),
			)
		} else if metadata != "" {
			factory := channel.GetSenderFactory(notifyChannel)
			if factory != nil {
				s.logger.InfoContext(ctx, logger.CatApp, "session: SendViaChannel delivering via DB fallback",
					"target_id", s.TargetID,
					"notify_channel", notifyChannel,
					"text_len", len(text),
				)
				if err := factory(ctx, []byte(metadata), text); err != nil {
					s.logger.WarnContext(ctx, logger.CatApp, "session: factory send failed",
						"target_id", s.TargetID,
						"notify_channel", notifyChannel,
						"err", err.Error(),
					)
					return err // Bubble up error just like fn(ctx, text) would
				}
				return nil
			} else {
				s.logger.WarnContext(ctx, logger.CatApp, "session: SendViaChannel skipped, no factory for notify_channel",
					"target_id", s.TargetID,
					"notify_channel", notifyChannel,
				)
			}
		}
	}

	s.logger.WarnContext(ctx, logger.CatApp, "session: SendViaChannel skipped, no sender for notify_channel",
		"target_id", s.TargetID,
		"notify_channel", notifyChannel,
	)
	return nil
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

func (s *Session) withSessionTelemetry(ctx context.Context) context.Context {
	metadata := telemetry.MetadataFromContext(ctx)
	if metadata.Origin == "" {
		metadata.Origin = telemetry.OriginSystem
	}
	if metadata.SessionID == "" {
		metadata.SessionID = s.TargetID
	}
	if metadata.AgentID == "" {
		if a := s.CurrentAgent(); a != nil {
			metadata.AgentID = a.InstanceID
		}
	}
	return telemetry.WithTelemetryMetadata(ctx, metadata)
}

func effectiveRunID(ctx context.Context) string {
	metadata := telemetry.MetadataFromContext(ctx)
	if metadata.RunID != "" {
		return metadata.RunID
	}
	if metadata.RequestID != "" {
		return metadata.RequestID
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(fmt.Sprintf("session: generate run ID: %v", err))
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16])
}

func (s *Session) withDispatchScope(ctx context.Context) context.Context {
	if s.dispatchManager == nil {
		return ctx
	}
	return dispatch.WithScope(ctx, dispatch.Scope{Manager: s.dispatchManager})
}

// beginRunLifecycle attaches one Runtime-owned watchdog root to a public
// Session execution entrypoint. Nested helpers reuse an existing handle so
// model overrides and adapters cannot accidentally create duplicate roots.
func (s *Session) beginRunLifecycle(ctx context.Context, phase string) (context.Context, *runwatch.Handle, string, error) {
	runID := effectiveRunID(ctx)
	metadata := telemetry.MetadataFromContext(ctx)
	metadata.RunID = runID
	ctx = telemetry.WithTelemetryMetadata(ctx, metadata)
	if runwatch.HandleFromContext(ctx) != nil || s.runWatch == nil {
		return ctx, nil, runID, nil
	}
	watchCtx, handle, err := s.runWatch.Start(ctx, runwatch.Metadata{
		RunID: runID, OwnerSessionID: s.TargetID, Phase: phase,
	})
	return watchCtx, handle, runID, err
}

// applyWatchdogEvent updates supervision before the event becomes externally
// observable, so progress is recorded before it is delivered to callers.
func (s *Session) applyWatchdogEvent(runHandle *runwatch.Handle, ev iface.AgentEvent) {
	if runHandle == nil {
		return
	}
	switch ev.(type) {
	case agent.DelegationStartedEvent:
		runHandle.Pulse(runwatch.ProgressStructural, "delegating")
	case agent.ContentDeltaEvent:
		runHandle.Pulse(runwatch.ProgressSemantic, "streaming")
	case agent.ReasoningDeltaEvent:
		runHandle.Pulse(runwatch.ProgressSemantic, "reasoning")
	case agent.ToolCallDeltaEvent:
		runHandle.Pulse(runwatch.ProgressSemantic, "tool_call")
	case agent.ToolExecStartEvent:
		runHandle.Pulse(runwatch.ProgressStructural, "tool_execution")
	case agent.ToolExecDoneEvent, agent.IterationDoneEvent, agent.DelegationCompletedEvent:
		runHandle.Pulse(runwatch.ProgressStructural, "streaming")
	case agent.DoneEvent:
		runHandle.Pulse(runwatch.ProgressSemantic, "completed")
	}
}

// AskIsolated executes a prompt in a clean context: it calls the underlying
// agent directly without pushing to the session's ContextWindow or timeline.
// This is used by the cron scheduler so scheduled tasks run without polluting
// the user's conversation history or being confused by stale context.
// All system logs (actor/llm/tool) are still written normally.
func (s *Session) AskIsolated(ctx context.Context, prompt string) (<-chan iface.AgentEvent, error) {
	// Inject telemetry context
	ctx = telemetry.WithTelemetryContext(ctx, s.TeamID, telemetry.UsageChat)
	ctx = s.withSessionTelemetry(ctx)
	ctx = s.withDispatchScope(ctx)

	if s.closed.Load() {
		return nil, ErrSessionClosed
	}
	if s.dispatchInitErr != nil {
		return nil, s.dispatchInitErr
	}
	flightID, acquired := s.acquireFlight()
	if !acquired {
		return nil, ErrSessionBusy
	}
	askCtx, askCancel := context.WithCancelCause(ctx)
	var runHandle *runwatch.Handle
	var runID string
	var err error
	askCtx, runHandle, runID, err = s.beginRunLifecycle(askCtx, "isolated")
	if err != nil {
		askCancel(context.Canceled)
		s.releaseFlight(flightID)
		return nil, err
	}
	cancelID := s.registerActiveCancel(runID, lifecycleCancel(runHandle, askCancel))
	askCtx = iface.ContextWithIsQBot(askCtx, s.IsQBot())
	askCtx = iface.ContextWithMediaDelivery(askCtx, s.HasNotifyChannel())
	a := s.CurrentAgent()
	if a == nil {
		s.unregisterActiveCancel(cancelID)
		askCancel(context.Canceled)
		if runHandle != nil {
			runHandle.Complete()
		}
		s.releaseFlight(flightID)
		return nil, errors.New("session: no active agent")
	}
	ch, job, err := a.AskStreamTracked(askCtx, prompt)
	if errors.Is(err, agent.ErrQuarantined) && s.rebuildGeneration != nil {
		if rebuildErr := s.rebuildQuarantinedAgent(context.Background(), a); rebuildErr == nil {
			a = s.CurrentAgent()
			ch, job, err = a.AskStreamTracked(askCtx, prompt)
		} else {
			err = rebuildErr
		}
	}
	if err != nil {
		s.unregisterActiveCancel(cancelID)
		askCancel(context.Canceled)
		if runHandle != nil {
			runHandle.Complete()
		}
		s.releaseFlight(flightID)
		return nil, err
	}
	// Wrap AgentEvent channel to iface.AgentEvent channel (they are the same type via embedding)
	out := make(chan iface.AgentEvent, 64)
	go func() {
		defer close(out)
		defer s.releaseFlight(flightID)
		defer s.unregisterActiveCancel(cancelID)
		defer askCancel(context.Canceled)
		if runHandle != nil {
			defer runHandle.Complete()
		}
		for {
			select {
			case <-askCtx.Done():
				s.quarantineAgentAfterWatchdog(context.Cause(askCtx), a, job)
				if cause := context.Cause(askCtx); cause != nil && (!errors.Is(cause, context.Canceled) || runwatch.CodeOf(cause) != "") {
					select {
					case out <- agent.ErrorEvent{Err: cause}:
					default:
					}
				}
				return
			case ev, ok := <-ch:
				if !ok {
					return
				}
				s.applyWatchdogEvent(runHandle, ev)
				select {
				case out <- ev:
				case <-askCtx.Done():
					s.quarantineAgentAfterWatchdog(context.Cause(askCtx), a, job)
					return
				}
			}
		}
	}()
	return out, nil
}

// AskStreamWithModel and AskIsolatedWithModel carry an explicit model route in
// the request context. They never mutate reusable Agent state.
func (s *Session) AskStreamWithModel(ctx context.Context, prompt string, params *iface.ModelOverrideParams) (<-chan iface.AgentEvent, error) {
	if params == nil {
		return s.AskStream(ctx, prompt)
	}
	return s.AskStream(iface.ContextWithModelOverride(ctx, params), prompt)
}

func (s *Session) AskIsolatedWithModel(ctx context.Context, prompt string, params *iface.ModelOverrideParams) (<-chan iface.AgentEvent, error) {
	if params == nil {
		return s.AskIsolated(ctx, prompt)
	}
	return s.AskIsolated(iface.ContextWithModelOverride(ctx, params), prompt)
}

// QueueMessage enqueues a user message into the pending queue without blocking.
// The message will be injected into the agent's context window before the next
// LLM API call, merged with any other pending messages into a single user turn.
func (s *Session) QueueMessage(prompt string) {
	s.pending.Enqueue(prompt)
	s.logger.InfoContext(context.Background(), logger.CatApp, "message queued via QueueMessage",
		"target_id", s.TargetID,
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
func (s *Session) SetMemoryManager(mm *conversation.Manager) {
	s.memoryManager = mm
}

// SetPersonaStatePath sets the path to the persona state.md for injection into
// the context window. Empty disables persona state injection.
func (s *Session) SetPersonaStatePath(p string) {
	s.personaStatePath = p
}

// SetPersonaReflection enables persona reflection for this session. L1-only:
// leave llm nil to disable reflection (L2 sessions). nameFn resolves the
// assistant name from soul.md at call time; may be nil. providerID/modelID are
// the fast/classifier model used for reflection LLM calls.
func (s *Session) SetPersonaReflection(llm agent.LLMClient, providerID, modelID string, nameFn func() string) {
	s.personaLLM = llm
	s.personaProviderID = providerID
	s.personaModelID = modelID
	s.personaName = nameFn
}

// maybeInjectPersonaState pushes the persona state block as a system message
// if it is enabled, the file exists, and the block is not already present.
// The [persona_state] marker disappears on compaction/clear, so the block is
// re-injected on the next user turn.
func (s *Session) maybeInjectPersonaState() {
	if s.personaStatePath == "" || s.cw == nil {
		return
	}
	for i := 0; i < s.cw.Len(); i++ {
		if m, ok := s.cw.MessageAt(i); ok && strings.Contains(m.Content, "[persona_state]") {
			return
		}
	}
	data, err := os.ReadFile(s.personaStatePath)
	if err != nil {
		return // missing state.md: silently skip
	}
	content := strings.TrimSpace(string(data))
	if content == "" {
		return
	}
	s.cw.Push(ctxwin.RoleSystem, "[persona_state]\n"+content)
}

// runPersonaReflection runs the state.md reflection asynchronously on the
// provided raw conversation. L1-only: disabled unless personaLLM is set.
func (s *Session) runPersonaReflection(ctx context.Context, raw string) {
	if s.personaLLM == nil || s.personaStatePath == "" || strings.TrimSpace(raw) == "" {
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.logger.Error(logger.CatApp, "persona reflection: panic recovered", "panic", fmt.Sprintf("%v", r))
			}
		}()
		name := "assistant"
		if s.personaName != nil {
			if n := s.personaName(); n != "" {
				name = n
			}
		}
		var daily string
		if s.memoryManager != nil {
			daily, _ = s.memoryManager.ReadRecentMemory(1)
		}
		if err := UpdatePersonaState(ctx, s.logger, s.personaLLM, s.personaStatePath, name, raw, daily, time.Now(), s.personaProviderID, s.personaModelID); err != nil {
			s.logger.Error(logger.CatApp, "persona reflection failed", "err", err.Error())
		}
	}()
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

	// Trigger persona reflection on the cleared conversation (L1-only).
	if len(dateGroups) > 0 {
		var parts []string
		for _, g := range dateGroups {
			parts = append(parts, formatPayloadForMemory(g.msgs))
		}
		s.runPersonaReflection(context.Background(), strings.Join(parts, "\n"))
	}

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
		"target_id", s.TargetID,
	)

	return nil
}

// Compact compacts the context window by summarizing older messages
// into a condensed representation using the compactor. Unlike Clear,
// it preserves the recent context and does NOT save to conversation.
func (s *Session) Compact(ctx context.Context) (string, error) {
	compactCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	summary, err := s.cw.CompactAndReplace(compactCtx)
	if err != nil {
		s.logger.LogError(context.Background(), logger.CatApp, "session compact failed", err)
		return "", fmt.Errorf("session: compact: %w", err)
	}
	if cause := context.Cause(compactCtx); cause != nil {
		return "", cause
	}

	s.logger.InfoContext(context.Background(), logger.CatApp, "session compacted",
		"target_id", s.TargetID,
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

	var parts []string
	for _, g := range groups {
		parts = append(parts, formatPayloadForMemory(g.msgs))
	}
	s.runPersonaReflection(ctx, strings.Join(parts, "\n"))

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
// Must be called while inFlight is held (no concurrent request).
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
	if s.dispatchInitErr != nil {
		return nil, s.dispatchInitErr
	}

	trimmed := strings.TrimSpace(prompt)
	lowerTrimmed := strings.ToLower(trimmed)

	// Inject telemetry context
	ctx = telemetry.WithTelemetryContext(ctx, s.TeamID, telemetry.UsageChat)
	ctx = s.withSessionTelemetry(ctx)
	ctx = s.withDispatchScope(ctx)

	// ── Pre-inFlight slash command intercept (always immediate, never queued) ──
	switch lowerTrimmed {
	case "/compact":
		// Acquire inFlight to serialize with forwarder goroutine.
		// Compact modifies CW state and must not run concurrently with AskStream.
		if !s.inFlight.CompareAndSwap(0, 1) {
			s.logger.InfoContext(ctx, logger.CatApp, "compact rejected: session busy, message queued",
				"target_id", s.TargetID,
			)
			if !rejectsBusyQueue(ctx) {
				s.enqueuePending(ctx, prompt)
			}
			return nil, ErrQueued
		}
		s.touch()
		compactBase, compactCancel := context.WithCancelCause(context.WithoutCancel(ctx))
		compactCtx, runHandle, runID, watchErr := s.beginRunLifecycle(compactBase, "compact")
		if watchErr != nil {
			compactCancel(context.Canceled)
			s.inFlight.Store(0)
			return nil, watchErr
		}
		cancelID := s.registerActiveCancel(runID, lifecycleCancel(runHandle, compactCancel))
		// Record the "/compact" prompt in CW + timeline so it survives the
		// post-completion loadHistory. Without this the user's prompt is
		// silently dropped from the chat UI.
		s.maybeInjectPersonaState()
		s.cw.Push(ctxwin.RoleUser, prompt, inputPushOptions(ctx)...)
		out := make(chan iface.AgentEvent, 2)
		go func() {
			defer close(out)
			defer func() {
				s.unregisterActiveCancel(cancelID)
				compactCancel(context.Canceled)
				if runHandle != nil {
					runHandle.Complete()
				}
			}()
			defer s.inFlight.Store(0)
			defer s.touch()
			if summary, err := s.Compact(compactCtx); err != nil {
				if cause := context.Cause(compactCtx); cause != nil {
					out <- agent.ErrorEvent{Err: cause}
					return
				}
				out <- agent.ErrorEvent{Err: err}
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
				"target_id", s.TargetID,
			)
			if !rejectsBusyQueue(ctx) {
				s.enqueuePending(ctx, prompt)
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
				out <- agent.ErrorEvent{Err: err}
			} else {
				out <- agent.ContentDeltaEvent{Delta: "Dialogue history cleared and saved to conversation."}
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
		initAgent := s.CurrentAgent()
		if initAgent == nil || initAgent.WorkDir == "" {
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
		prompt = BuildInitPrompt(initAgent.WorkDir)
		// Intentionally no return — let it reach the LLM agent below.

	}

	// L1 async delegation does not block new messages. Agent mailbox guarantees serial execution of jobs:
	// resumeTurn (high priority) executes before new message jobs, keeping CW order naturally correct.

	flightID, acquired := s.acquireFlight()
	if !acquired {
		s.logger.InfoContext(ctx, logger.CatApp, "askstream rejected: session busy, message queued",
			"target_id", s.TargetID,
			"prompt_len", len(prompt),
		)
		if !rejectsBusyQueue(ctx) {
			s.enqueuePending(ctx, prompt)
		}
		return nil, ErrQueued
	}
	clientCtx := ctx
	var askCtx context.Context
	var askCancel context.CancelCauseFunc
	if s.requestTimeout > 0 {
		var deadlineCancel context.CancelFunc
		askCtx, deadlineCancel = context.WithTimeout(context.WithoutCancel(ctx), s.requestTimeout)
		askCancel = func(error) { deadlineCancel() }
	} else {
		askCtx, askCancel = context.WithCancelCause(context.WithoutCancel(ctx))
	}
	askCtx = iface.ContextWithIsQBot(askCtx, s.IsQBot())
	var runHandle *runwatch.Handle
	var runID string
	var watchErr error
	var askAgent *agent.Agent
	var jobHandle *agent.JobHandle
	askCtx, runHandle, runID, watchErr = s.beginRunLifecycle(askCtx, "routing")
	if watchErr != nil {
		askCancel(context.Canceled)
		s.releaseFlight(flightID)
		return nil, watchErr
	}
	cancelID := s.registerActiveCancel(runID, lifecycleCancel(runHandle, askCancel))
	// Routing, memory recall, the leader LLM, local children, and cross-team
	// helpers all use this same cancellation root.
	ctx = askCtx
	// Note: the release of inFlight is handled by the forwarder goroutine below
	// checkAutoClear must happen before touch (here lastActive is the end time of the previous Ask)
	s.checkAutoClear()
	s.touch()

	start := time.Now()

	// ── Task routing: classify prompt and set model override ──
	askAgent = s.CurrentAgent()
	if askAgent == nil {
		askCancel(context.Canceled)
		s.unregisterActiveCancel(cancelID)
		if runHandle != nil {
			runHandle.Complete()
		}
		s.releaseFlight(flightID)
		return nil, errors.New("session: no active agent")
	}
	effectiveCW := askAgent.Def.ContextWindow
	if effectiveCW <= 0 {
		effectiveCW = agent.DefaultContextWindow
	}
	var activeRouteResult RouteResult
	if s.Router != nil {
		s.lastLevelMu.RLock()
		priorLevel := s.lastLevel
		s.lastLevelMu.RUnlock()

		routerCtx := telemetry.WithTelemetryContext(ctx, s.TeamID, telemetry.UsageRouter)
		result, err := s.Router(routerCtx, prompt, priorLevel, s.cw.BuildPayload())
		if err != nil {
			s.logger.WarnContext(ctx, logger.CatApp, "task router failed, using default model",
				"target_id", s.TargetID,
				"err", err.Error(),
			)
			// Don't return — proceed with defaults (no model override)
			result = RouteResult{
				ClassifierWarning: "Task classification degraded: " + err.Error(),
			}
		}

		if result.Level != "" {
			activeRouteResult = result
			s.logger.DebugContext(ctx, logger.CatApp, "task router applied model override",
				"target_id", s.TargetID,
				"provider_id", result.ProviderID,
				"model_id", result.ModelID,
				"thinking_enabled", result.ThinkingEnabled,
				"reasoning_effort", result.ReasoningEffort,
				"level", result.Level,
			)
			askCtx = iface.ContextWithModelOverride(askCtx, &iface.ModelOverrideParams{
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
						"target_id", s.TargetID,
						"err", err.Error(),
					)
				}
			}

			if result.ContextWindow > 0 {
				effectiveCW = result.ContextWindow
			}
			askCtx = telemetry.WithTelemetryMetadata(askCtx, telemetry.Metadata{TaskType: result.Level})
			ctx = askCtx
		}
	}

	// ── Resize context window to match effective model ──

	// Resize and push user prompt atomically (both hold cw.Lock)
	s.mu.Lock()
	s.cw.Resize(effectiveCW, 0, 0)
	cwLenBeforeTurn := s.cw.Len()
	// Extract images from context if present (e.g., from qbot image uploads).
	// Images are passed as []llm.ImageContent via context.WithValue.
	pushOpts := inputPushOptions(ctx)
	if files, ok := ctx.Value(ctxwin.FilesContextKey).([]ctxwin.FileAttachment); ok && len(files) > 0 {
		pushOpts = append(pushOpts, ctxwin.WithFiles(files))
	}
	if images, ok := ctx.Value(ctxwin.ImageContextKey).([]llm.ImageContent); ok && len(images) > 0 {
		pushOpts = append(pushOpts, ctxwin.WithImages(images))

		effectiveVision := askAgent.Def.Vision
		if activeRouteResult.ModelID != "" {
			effectiveVision = activeRouteResult.Vision
		}

		if !effectiveVision && s.VisionDescriber != nil {
			s.logger.InfoContext(ctx, logger.CatApp, "askstream: model lacks vision, invoking vision describer",
				"target_id", s.TargetID,
				"image_count", len(images),
			)
			desc, err := s.VisionDescriber(ctx, images)
			if err != nil {
				s.logger.WarnContext(ctx, logger.CatApp, "askstream: vision describer failed",
					"target_id", s.TargetID,
					"err", err.Error(),
				)
			} else if desc != "" {
				prompt += fmt.Sprintf("\n\n[System: The user included %d image(s). As the current model lacks vision, here is a detailed visual information transcription provided by the vision model:\n%s]", len(images), desc)
			}
		}
	}
	s.maybeInjectPersonaState()
	s.cw.Push(ctxwin.RoleUser, prompt, pushOpts...)
	s.mu.Unlock()

	s.logger.DebugContext(ctx, logger.CatApp, "askstream: prompt pushed to context window",
		"target_id", s.TargetID,
		"prompt_len", len(prompt),
	)

	srcCh, jobHandle, err := s.askStream(askCtx, s.cw, prompt)
	if err != nil {
		// A stopped Agent may be restarted; a quarantined generation must be
		// replaced because its old job may still be running outside the actor.
		if errors.Is(err, agent.ErrQuarantined) && s.rebuildGeneration != nil {
			if rebuildErr := s.rebuildQuarantinedAgent(context.Background(), askAgent); rebuildErr == nil {
				askAgent = s.CurrentAgent()
				srcCh, jobHandle, err = s.askStream(askCtx, s.cw, prompt)
				if err == nil {
					goto enqueued
				}
			} else {
				s.logger.WarnContext(ctx, logger.CatApp, "askstream: quarantined agent replacement failed",
					"target_id", s.TargetID, "err", rebuildErr.Error())
			}
		}
		if errors.Is(err, agent.ErrStopped) || errors.Is(err, agent.ErrNotStarted) {
			s.logger.InfoContext(ctx, logger.CatApp, "askstream: agent not running, attempting restart",
				"target_id", s.TargetID,
				"err", err.Error(),
			)
			if startErr := askAgent.Start(context.Background()); startErr != nil {
				s.logger.WarnContext(ctx, logger.CatApp, "askstream: agent restart failed",
					"target_id", s.TargetID,
					"err", startErr.Error(),
				)
			} else {
				// Retry once
				srcCh, jobHandle, err = s.askStream(askCtx, s.cw, prompt)
				if err == nil {
					goto enqueued
				}
			}
		}

		// Enqueue failure: cleanup
		s.unregisterActiveCancel(cancelID)
		askCancel(context.Canceled)
		if runHandle != nil {
			runHandle.Complete()
		}

		s.mu.Lock()
		s.cw.Truncate(cwLenBeforeTurn)
		s.mu.Unlock()
		s.releaseFlight(flightID)

		s.logger.WarnContext(ctx, logger.CatApp, "askstream: agent stream setup failed",
			"target_id", s.TargetID,
			"err", err.Error(),
		)
		return nil, err
	}

enqueued:
	// Capture the request route before returning to the WebSocket layer. Actor
	// execution is asynchronous, so Agent.activeModelOverride is not an
	// authoritative source for the just-accepted request.
	s.publishRequestRoute(clientCtx, askAgent, activeRouteResult)

	out := make(chan iface.AgentEvent, 64)
	go func() {
		// Cleanup: unregister this turn and release askCtx when the goroutine ends.
		defer close(out)
		defer func() {
			s.unregisterActiveCancel(cancelID)
			askCancel(context.Canceled)
			if runHandle != nil {
				runHandle.Complete()
			}
		}()
		defer s.releaseFlight(flightID)
		defer s.touch()
		defer s.closeTurnDone()
		defer func() {
			if r := recover(); r != nil {
				s.logger.ErrorContext(ctx, logger.CatApp, "session event processor panic recovered",
					"target_id", s.TargetID,
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
							"target_id", s.TargetID,
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
					AgentID:          askAgent.InstanceID,
				})
				s.logger.DebugContext(ctx, logger.CatApp, "askstream: partial assistant content flushed to timeline",
					"target_id", s.TargetID,
					"content_len", accContent.Len(),
				)
			}
		}()

		clientDisconnected := false
		terminalErrorEmitted := false
		deadlineError := fmt.Errorf("Session request timed out after %s", formatRequestTimeout(s.requestTimeout))
		emitTerminalError := func(terminalErr error) {
			if terminalErrorEmitted {
				return
			}
			terminalErrorEmitted = true
			terminal := agent.ErrorEvent{Err: terminalErr}
			select {
			case out <- terminal:
				eventCount++
				return
			default:
			}
			// A terminal event must never wait for a stalled consumer. If the
			// output buffer is saturated, discard one incomplete streaming event
			// to reserve the slot that closes the request with its timeout cause.
			select {
			case <-out:
			default:
			}
			out <- terminal
			eventCount++
		}
		terminalErrorForCause := func(cause error) error {
			if errors.Is(cause, context.DeadlineExceeded) {
				return deadlineError
			}
			return cause
		}
		shouldExposeTerminal := func(cause error) bool {
			return !errors.Is(cause, context.Canceled) || runwatch.CodeOf(cause) != ""
		}
		terminateContext := func() bool {
			cause := context.Cause(askCtx)
			if cause == nil {
				return false
			}
			s.quarantineAgentAfterWatchdog(cause, askAgent, jobHandle)
			rollbackTurn()
			if shouldExposeTerminal(cause) {
				emitTerminalError(terminalErrorForCause(cause))
			}
			return true
		}
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
					terminateContext()

					s.logger.DebugContext(ctx, logger.CatApp, "askstream cancelled (read)",
						"target_id", s.TargetID,
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
					terminateContext()

					s.logger.DebugContext(ctx, logger.CatApp, "askstream cancelled (read)",
						"target_id", s.TargetID,
						"events_processed", eventCount,
						"duration_ms", time.Since(start).Milliseconds(),
					)
					return
				case <-clientCtx.Done():
					clientDisconnected = true
					continue
				}
			}
			sourceTerminal := false
			errorEvent := false
			if e, ok := ev.(agent.ErrorEvent); ok {
				errorEvent = true
				// The agent uses the same request context, so its context error is
				// another observation of this request's terminal state only when it
				// matches askCtx. An upstream context error while askCtx is still
				// live remains an ordinary source error and must be forwarded intact.
				cause := context.Cause(askCtx)
				if sameTerminalCause(e.Err, cause) {
					ev = agent.ErrorEvent{Err: terminalErrorForCause(cause)}
					sourceTerminal = true
				}
				// ErrorEvent terminates AskStream. Roll back before publishing it so
				// callers that return from the stream cannot observe a partial turn
				// in history while the forwarder is still unwinding.
				rollbackTurn()
				s.logger.WarnContext(ctx, logger.CatApp, "askstream error event, user prompt removed",
					"target_id", s.TargetID,
					"err", e.Err,
				)
			}
			s.applyWatchdogEvent(runHandle, ev)
			if sourceTerminal {
				if shouldExposeTerminal(context.Cause(askCtx)) {
					emitTerminalError(ev.(agent.ErrorEvent).Err)
				}
				return
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
					terminateContext()

					s.logger.DebugContext(ctx, logger.CatApp, "askstream cancelled (write)",
						"target_id", s.TargetID,
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
			if errorEvent {
				return
			}
			switch e := ev.(type) {
			case agent.DelegationStartedEvent:
				// Async delegation started: release inFlight, allowing user to send new messages
				s.logger.DebugContext(ctx, logger.CatApp, "delegation started",
					"target_id", s.TargetID,
				)
				s.newTurnDone()
				s.releaseFlight(flightID)
			case agent.ContentDeltaEvent:
				accContent.WriteString(e.Delta)
			case agent.ReasoningDeltaEvent:
				accReasoning.WriteString(e.Delta)
			case agent.DoneEvent:
				finalContent = e.Content
				finalReasoning = e.ReasoningContent
				gotDone = true
				s.logger.DebugContext(ctx, logger.CatApp, "askstream done event received",
					"target_id", s.TargetID,
					"content_len", len(e.Content),
					"reasoning_len", len(e.ReasoningContent),
				)
			}
		}
	done:
		// Check if cancellation occurred between goto done and this label (narrow race window)
		if terminateContext() {
		} else if gotDone {
			if finalContent != "" {
				s.mu.Lock()
				opts := []ctxwin.PushOption{ctxwin.WithReasoningContent(finalReasoning)}
				s.cw.Push(ctxwin.RoleAssistant, finalContent, opts...)
				// Track what we just pushed so the partial-flush defer can skip duplicates.
				lastPushedContent = finalContent
				s.mu.Unlock()

				s.logger.DebugContext(ctx, logger.CatApp, "askstream: assistant reply pushed to context window",
					"target_id", s.TargetID,
				)
			} else {
				// Empty assistant reply — invalid for LLM API.
				// Skip the push but keep the user prompt for context.
				s.logger.WarnContext(ctx, logger.CatApp, "askstream: empty assistant reply skipped",
					"target_id", s.TargetID,
					"reasoning_len", len(finalReasoning),
				)
			}
		}
		// Delegation round completed: close turnDone channel, notify waiting new messages
		s.closeTurnDone()

		s.logger.DebugContext(ctx, logger.CatApp, "askstream complete",
			"target_id", s.TargetID,
			"events_processed", eventCount,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}()
	return out, nil
}

func formatRequestTimeout(timeout time.Duration) string {
	switch {
	case timeout%time.Hour == 0:
		return fmt.Sprintf("%d hours", timeout/time.Hour)
	case timeout%time.Minute == 0:
		return fmt.Sprintf("%d minutes", timeout/time.Minute)
	case timeout%time.Second == 0:
		return fmt.Sprintf("%d seconds", timeout/time.Second)
	default:
		return timeout.String()
	}
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

// Close marks session as closed, preventing new requests; does not stop agent
func (s *Session) beginClose() {
	s.closed.Store(true)
	// Synchronize with generation construction/publication. A factory already
	// in progress observes closed before publish and retires its fresh domain.
	s.rebuildMu.Lock()
	s.rebuildMu.Unlock()
}

func (s *Session) Close() {
	s.beginClose()

	s.closeOnce.Do(func() {
		s.logger.InfoContext(context.Background(), logger.CatApp, "session closed",
			"target_id", s.TargetID,
			"lifetime_sec", time.Since(s.Created).Seconds(),
		)

		// Close timeline Writer, flush to disk and release file handle.
		if s.tl != nil {
			s.tl.Close()
		}

		// The Session owns the generation logger handler. L1 rebuilds reuse
		// this handler, so one close releases every generation's log writers.
		if err := s.resourceCloser(); err != nil {
			fmt.Fprintf(os.Stderr, "session close: logger close error: %v\n", err)
		}
	})
}

// DisposeGeneration is the lifecycle-owner cleanup for a Session's complete
// Agent generation. Unlike Close, it makes the generation undiscoverable,
// reaps its Supervisor domain, unregisters and stops the leader, and then
// closes Session-owned resources. Calls are idempotent.
func (s *Session) DisposeGeneration(timeout time.Duration) {
	if s == nil {
		return
	}
	s.disposeOnce.Do(func() {
		// Fence a concurrent rebuild before detaching the generation snapshot.
		s.beginClose()
		_ = s.CancelCurrent("session generation disposed")

		s.agentMu.Lock()
		generation := s.generation
		s.generation = agentGeneration{}
		s.askStreamHistory = nil
		s.lastJob = nil
		s.agentMu.Unlock()

		if generation.agent != nil {
			generation.agent.DeactivateScheduling()
		}
		if generation.supervisor != nil {
			_ = generation.supervisor.ReapAll(timeout)
			s.agentMu.RLock()
			removeSupervisor := s.removeSupervisor
			s.agentMu.RUnlock()
			if removeSupervisor != nil {
				removeSupervisor(generation.supervisor)
			}
		}
		if generation.agent != nil {
			s.agentMu.RLock()
			registry := s.agentRegistry
			s.agentMu.RUnlock()
			if registry != nil {
				registry.Unregister(generation.agent.InstanceID)
			}
			_ = generation.agent.Stop(timeout)
		}
		s.Close()
	})
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
			"target_id", s.TargetID,
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
	cancel context.CancelCauseFunc
	done   chan struct{}
}

func lifecycleCancel(handle *runwatch.Handle, cancel context.CancelCauseFunc) context.CancelCauseFunc {
	return func(cause error) {
		if handle != nil && runwatch.CodeOf(cause) != "" {
			handle.Fail(cause)
		}
		cancel(cause)
	}
}

// registerActiveCancel adds a top-level turn cancellation root to the session.
func (s *Session) registerActiveCancel(runID string, cancel context.CancelCauseFunc) string {
	s.cancelMu.Lock()
	defer s.cancelMu.Unlock()
	s.activeCancels[runID] = activeTurnCancel{cancel: cancel, done: make(chan struct{})}
	return runID
}

func (s *Session) unregisterActiveCancel(id string) {
	s.cancelMu.Lock()
	turn, ok := s.activeCancels[id]
	if ok {
		delete(s.activeCancels, id)
		close(turn.done)
	}
	s.cancelMu.Unlock()
}

func (s *Session) hasActiveRun(runID string) bool {
	s.cancelMu.Lock()
	_, ok := s.activeCancels[runID]
	s.cancelMu.Unlock()
	return ok
}

// CancelRun targets one request tree because L1 can own multiple independent
// runs whose cancellation scopes must never leak into each other.
func (s *Session) CancelRun(runID, reason string) error {
	s.cancelMu.Lock()
	turn, ok := s.activeCancels[runID]
	s.cancelMu.Unlock()
	if !ok {
		return ErrNoActiveTask
	}
	turn.cancel(&runwatch.Cause{Code: runwatch.CodeCancelledByUser, OperationID: runID})
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-turn.done:
	case <-timer.C:
		return fmt.Errorf("session: timed out waiting for run %q cancellation", runID)
	}
	s.logger.InfoContext(context.Background(), logger.CatApp, "session run cancelled",
		"target_id", s.TargetID,
		"run_id", runID,
		"reason", reason,
	)
	return nil
}

// CancelCurrent cancels every live top-level turn in this session. Delegated
// and cross-team calls derive their contexts from these roots, so cancellation
// propagates through the complete request tree. Session and Agent lifecycles
// are intentionally left untouched and can serve later messages normally.
func (s *Session) CancelCurrent(reason string) error {
	return s.cancelCurrent(reason, false)
}

func (s *Session) cancelCurrent(reason string, byUser bool) error {
	s.cancelMu.Lock()
	type identifiedTurn struct {
		id string
		activeTurnCancel
	}
	turns := make([]identifiedTurn, 0, len(s.activeCancels))
	for id, turn := range s.activeCancels {
		turns = append(turns, identifiedTurn{id: id, activeTurnCancel: turn})
	}
	s.cancelMu.Unlock()

	if len(turns) == 0 {
		return ErrNoActiveTask
	}
	for _, turn := range turns {
		cause := error(context.Canceled)
		if byUser {
			cause = &runwatch.Cause{Code: runwatch.CodeCancelledByUser, OperationID: turn.id}
		}
		turn.cancel(cause)
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
				"target_id", s.TargetID,
				"active_turns", len(turns),
			)
			return nil
		}
	}

	s.logger.InfoContext(context.Background(), logger.CatApp, "session task tree cancelled",
		"target_id", s.TargetID,
		"active_turns", len(turns),
		"reason", reason,
	)
	return nil
}

func sameTerminalCause(source, cause error) bool {
	if source == nil || cause == nil {
		return false
	}
	if errors.Is(source, cause) || errors.Is(cause, source) {
		return true
	}
	sourceCode, causeCode := runwatch.CodeOf(source), runwatch.CodeOf(cause)
	return sourceCode != "" && sourceCode == causeCode
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
	factory          AgentFactory
	routerFunc       TaskRouterFunc
	memoryHook       MemoryHook
	memoryManager    *conversation.Manager
	metaStore        ChannelMetadataStore
	visionDescriber  VisionDescriberFunc
	personaStatePath string          // state.md path for persona state injection; empty = disabled
	personaLLM       agent.LLMClient // reflection LLM for /clear persona reflection; nil = disabled (L2)
	personaName      func() string   // resolves assistant name from soul.md at call time

	personaProviderID string // fast/classifier model provider for reflection LLM calls
	personaModelID    string // fast/classifier model ID for reflection LLM calls

	logger        *logger.Logger
	agentRegistry *agent.Registry

	idleTimeout      time.Duration // 0 = disabled; for auto-clear idle sessions
	compactThreshold int           // 0 = disabled; minimum tokens to trigger compact
	runWatch         *runwatch.Manager

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

// SetVisionDescriber sets the vision describer function for the session.
func (m *SessionManager) SetVisionDescriber(fn VisionDescriberFunc) {
	m.visionDescriber = fn
}

// SetMemoryManager sets the memory manager for dedup cursor tracking.
// Must be set alongside SetMemoryHook. Not thread-safe for setup.
func (m *SessionManager) SetMemoryManager(mm *conversation.Manager) {
	m.memoryManager = mm
}

// SetRunWatch wires the process-owned manager into Sessions created later.
func (m *SessionManager) SetRunWatch(manager *runwatch.Manager) {
	m.runWatch = manager
}

// SetAgentRegistry wires the active-agent registry used for generation
// replacement cleanup. Must be set before Init.
func (m *SessionManager) SetAgentRegistry(registry *agent.Registry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.session == nil {
		// Store through a setup-only field added below.
		m.agentRegistry = registry
	}
}

// SetPersonaStatePath sets the state.md path for persona state injection.
// Must be called before Init(). Empty disables injection. Not thread-safe for setup.
func (m *SessionManager) SetPersonaStatePath(p string) {
	m.personaStatePath = p
}

// SetPersonaReflection enables persona reflection on /clear for the L1 session.
// Must be called before Init(). Pass nil llm to disable (L2 sessions).
// Not thread-safe for setup.
func (m *SessionManager) SetPersonaReflection(llm agent.LLMClient, providerID, modelID string, nameFn func() string) {
	m.personaLLM = llm
	m.personaProviderID = providerID
	m.personaModelID = modelID
	m.personaName = nameFn
}

// SetChannelMetadataStore configures the DB store for channel metadata.
// Must be called before Init(). Not thread-safe for setup.
func (m *SessionManager) SetChannelMetadataStore(store ChannelMetadataStore) {
	m.metaStore = store
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
	if a != nil && a.Log != nil {
		sessionLogger = a.Log.Child()
	}
	s := NewSession(id, teamID, a, cw, tl, sessionLogger)
	s.SetAgentRegistry(m.agentRegistry)
	s.SetAgentRebuilder(func(rebuildCtx context.Context) (*agent.Agent, error) {
		fresh, _, freshTimeline, err := m.factory(rebuildCtx, teamID)
		if freshTimeline != nil {
			_ = freshTimeline.Close()
		}
		return fresh, err
	})
	s.SetRunWatch(m.runWatch)
	s.SetPersonaStatePath(m.personaStatePath)
	s.SetPersonaReflection(m.personaLLM, m.personaProviderID, m.personaModelID, m.personaName)

	if m.routerFunc != nil {
		s.Router = m.routerFunc
	}
	if m.memoryHook != nil {
		s.memoryHook = m.memoryHook
	}
	if m.memoryManager != nil {
		s.memoryManager = m.memoryManager
	}
	if m.visionDescriber != nil {
		s.VisionDescriber = m.visionDescriber
	}
	if m.metaStore != nil {
		s.metaStore = m.metaStore
	}
	if m.idleTimeout > 0 {
		s.idleTimeout = m.idleTimeout
		s.compactThreshold = m.compactThreshold
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed.Load() {
		s.beginClose()
		_ = a.Stop(time.Second)
		if m.agentRegistry != nil {
			m.agentRegistry.Unregister(a.InstanceID)
		}
		s.Close()
		return nil, ErrSessionClosed
	}
	s.PublishInitialGeneration()
	m.session = s

	m.logger.InfoContext(ctx, logger.CatApp, "session initialized",
		"target_id", id,
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
		// Fence generation publication before choosing the Agent to stop.
		s.beginClose()
		if a := s.CurrentAgent(); a != nil {
			_ = a.Stop(stopTimeout)
		}
		s.Close()
	}

	m.logger.InfoContext(context.Background(), logger.CatApp, "session manager shutdown completed")
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
