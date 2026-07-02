package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/xiaobaitu/soloqueue/internal/iface"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/skill"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

// ─── Types ───────────────────────────────────────────────────────────────────

// job is a "unit of work" flowing in the mailbox
//
// It is a closure, encapsulating "what needs to be done this time" (including caller data and reply channel).
// All upper-layer APIs (Ask / Submit / future Session) construct and deliver jobs with different semantics.
// Not exposed externally.
type job func(ctx context.Context)

// ─── Agent ───────────────────────────────────────────────────────────────────

// Agent is a long-running unit bound with LLM + configuration + logs
//
// Lifecycle:
//
//	NewAgent → Start → [ Ask / Submit ]* → Stop → Stopped
//	                                                 ↓
//	                                                 Restartable after Stop
//
// Concurrency safety:
//   - Ask / Submit / State / Done / Err / Stop can be called concurrently by multiple goroutines
//   - Jobs in the mailbox are executed serially within the run goroutine (naturally mutually exclusive)
//   - Start and Stop are mutually exclusive (only one modifies the lifecycle field at a time)
type Agent struct {
	Def Definition
	LLM LLMClient
	Log *logger.Logger

	// Configuration (immutable after construction)
	mailboxCap    int
	tools         *tools.ToolRegistry  // Underlying execution primitives
	skills        *skill.SkillRegistry // Context injection mechanism
	parallelTools bool                 // When true, multiple tool_calls in one turn are executed concurrently using errgroup

	// toolTimeouts specifies the timeout duration for Execute by tool.Name() (0/nil = no single tool timeout)
	// execToolStream wraps ctx with context.WithTimeout; timeout errors are formatted
	// as "error: tool timeout after Xs" and fed back to LLM (without interrupting the loop).
	toolTimeouts map[string]time.Duration

	// runtime fields: allocated at Start, 'done' retained after Stop until next Start overwrites it
	// mu is only for mutual exclusion on Start/Stop paths; hot paths (Ask/Submit) read via snapshot
	mu      sync.Mutex
	ctx     context.Context
	cancel  context.CancelFunc
	mailbox chan job
	done    chan struct{}

	// Confirmation state machine: execToolStream blocks, waiting for external Confirm to inject results
	confirmMu      sync.RWMutex
	pendingConfirm map[string]*confirmSlot

	// bypassConfirm skips all tool confirmations; either from the agent template's permission field or global --bypass.
	bypassConfirm bool

	// confirmStore is a session-level tool permission store; defaults to in-memory implementation, can be replaced via WithConfirmStore.
	confirmStore SessionConfirmStore

	// WorkDir is the working directory for this agent's tool execution.
	// L1 uses the global workDir (~/.soloqueue). L2/L3 use a project-specific
	// directory chosen at delegation time. Tools like Bash default their cwd
	// to this path. Project configs (.claude/, AGENTS.md, CLAUDE.md) are
	// loaded relative to this directory.
	WorkDir string

	// InstanceID is the unique identifier (UUID) for an Agent instance, separate from Def.ID (template/role identifier).
	// Supports multiple Agent instances of the same template coexisting (parallel scheduling).
	InstanceID string

	// Asynchronous delegation tracking (L1 specific)
	turnMu     sync.RWMutex
	asyncTurns map[int]*asyncTurnState // iter → turn asynchronous state

	// Priority mailbox (L1 enabled; nil means a regular job channel)
	priorityMailbox *PriorityMailbox

	// modelOverride is a per-ask model parameter override.
	// Set by the router before submitting an ask job, consumed by streamLoop,
	// and auto-cleared when the ask completes. Thread-safe via atomic pointer.
	modelOverride atomic.Pointer[ModelParams]

	// runtime bundles all mutable runtime observability state under a single
	// RWMutex. Includes lifecycle state, error tracking, circuit breaker,
	// exit error, and work-tracking fields. Read by inspect_agent tool and API;
	// written by the agent's own goroutine (single writer).
	runtimeMu sync.RWMutex
	runtime   agentRuntime

	watchSlots []watchSlot
	watchMu    sync.RWMutex
	watchSeq   int64

	onStateChange func(State) // called after every state transition
}

// confirmSlot is a pending confirmation slot for a single tool_call
type confirmSlot struct {
	done atomic.Bool
	ch   chan string // User-selected option value; "" indicates rejection/cancellation
}

// Option is a functional option for NewAgent
type Option func(*Agent)

// WithMailboxCap sets the mailbox capacity (default DefaultMailboxCap = 8)
// cap <= 0 will be ignored (uses default value)
func WithMailboxCap(cap int) Option {
	return func(a *Agent) {
		if cap > 0 {
			a.mailboxCap = cap
		}
	}
}

// WithSkills registers a batch of Skills to the agent's SkillRegistry
//
// A Skill is an executable skill definition, called by the LLM via built-in tools.
// Multiple calls to WithSkills accumulate (on the same SkillRegistry); a Skill with the same name will still panic.
func WithSkills(skills ...*skill.Skill) Option {
	return func(a *Agent) {
		if len(skills) == 0 {
			return
		}
		if a.skills == nil {
			a.skills = skill.NewSkillRegistry()
		}
		for _, s := range skills {
			if err := a.skills.Register(s); err != nil {
				panic(fmt.Sprintf("agent: WithSkills: %v", err))
			}
		}
	}
}

// WithTools registers a batch of Tools to the agent's ToolRegistry
//
// A Tool is an underlying execution primitive, called by the LLM via function calling.
// Duplicate name / empty Tool.Name() / nil tool will panic — this is a build-time programming error,
// must fail early. Production code should ensure tool names are unique.
//
// Multiple calls to WithTools accumulate (on the same registry); a tool with the same name will still panic.
func WithTools(ts ...tools.Tool) Option {
	return func(a *Agent) {
		if len(ts) == 0 {
			return
		}
		if a.tools == nil {
			a.tools = tools.NewToolRegistry()
		}
		for _, t := range ts {
			if err := a.tools.Register(t); err != nil {
				panic(fmt.Sprintf("agent: WithTools: %v", err))
			}
		}
	}
}

// WithParallelTools enables/disables concurrent execution of multiple tool_calls in the same turn
//
// Defaults to false (sequential, maintaining current behavior).
//
// When enabled: if len(toolCalls) > 1 in runOnceStream, it uses the errgroup concurrent path;
// a single tool_call still executes sequentially (no concurrency benefit, saves goroutine creation).
//
// Event order guarantee:
//   - In concurrent scenarios, the relative order of `ToolExecStart` / `ToolExecDone` events
//     is determined by goroutine scheduling; for any given `call_id`, `Start` always precedes `Done`.
//   - The `role=tool` messages fed back to the LLM are **strictly in the original LLM tool_calls order**,
//     not disrupted by goroutine completion order.
//
// Error semantics are consistent with sequential execution: tool execution errors (including panic / ctx cancellation) are formatted as
// `"error: ..."` and fed back to the LLM, without interrupting the loop; errgroup never short-circuits.
func WithParallelTools(enabled bool) Option {
	return func(a *Agent) {
		a.parallelTools = enabled
	}
}

// WithToolTimeout sets the timeout duration for Execute for a given tool.Name()
//
// Semantics:
//   - d > 0: wraps ctx with context.WithTimeout inside execToolStream; upon timeout,
//     it is **formatted as** `"error: tool timeout after <d>"` and fed back to LLM,
//     without interrupting the entire Ask loop (consistent with regular tool errors)
//   - d <= 0: removes the timeout configuration for this tool (reverts to no timeout, inherits upstream ctx)
//   - Multiple calls for the same name: the last call takes precedence
//
// Example:
//
//	agent.NewAgent(def, llm, log,
//	    agent.WithTools(tools...),
//	    agent.WithToolTimeout("Bash", 30*time.Second),
//	    agent.WithToolTimeout("WebFetch", 10*time.Second),
//	)
//
// Does not affect ctx cancellation path: caller ctx cancellation (Stop / Ask ctx) still propagates according to original semantics
// immediately to the current tool (WithTimeout is an **additional** deadline, not overwriting the parent ctx).
func WithToolTimeout(name string, d time.Duration) Option {
	return func(a *Agent) {
		if a.toolTimeouts == nil {
			a.toolTimeouts = make(map[string]time.Duration)
		}
		if d <= 0 {
			delete(a.toolTimeouts, name)
			return
		}
		a.toolTimeouts[name] = d
	}
}

// WithPriorityMailbox enables priority mailbox (L1 specific)
//
// When enabled:
//   - High-priority jobs (delegation callbacks, timeout events) are delivered via highCh
//   - Normal-priority jobs (user Ask/Submit) are delivered via normalCh
//   - The run goroutine prioritizes consuming from highCh to ensure delegation results are not blocked by new messages
func WithPriorityMailbox() Option {
	return func(a *Agent) {
		a.priorityMailbox = NewPriorityMailbox()
	}
}

// WithInstanceID overrides the auto-generated UUID instance ID.
// Primarily for deterministic testing.
func WithInstanceID(id string) Option {
	return func(a *Agent) {
		a.InstanceID = id
	}
}

// WithAgentWorkDir sets the working directory for this agent's tool execution.
//
// When set, tools like Bash will default their cwd to this directory.
// Project configs (.claude/, AGENTS.md, CLAUDE.md) are loaded relative to
// this path. L1 uses the global ~/.soloqueue; L2/L3 use a project-specific
// directory chosen at delegation time.
func WithAgentWorkDir(dir string) Option {
	return func(a *Agent) {
		a.WorkDir = dir
	}
}

// SetDelegateSpawnFn replaces the SpawnFn on the DelegateTool with the given
// leaderID. This is used after Supervisor creation to wire L2→L3 delegation
// through the Supervisor so spawned L3 children are tracked.
//
// Returns true if a DelegateTool with that name was found and updated.
func (a *Agent) SetDelegateSpawnFn(leaderID string, spawnFn func(ctx context.Context, task string, workDir string) (iface.Locatable, error)) bool {
	if a.tools == nil {
		return false
	}
	t, ok := a.tools.Get("delegate_" + strings.ReplaceAll(leaderID, " ", "_"))
	if !ok {
		return false
	}
	dt, ok := t.(*tools.DelegateTool)
	if !ok {
		return false
	}
	dt.SpawnFn = spawnFn
	return true
}

// RegisterTool registers a tool into the agent's ToolRegistry at runtime.
func (a *Agent) RegisterTool(t tools.Tool) error {
	if a.tools == nil {
		return fmt.Errorf("agent %q: ToolRegistry is nil", a.Def.ID)
	}
	return a.tools.Register(t)
}

// PendingDelegations returns the number of pending asynchronous delegation turns
func (a *Agent) PendingDelegations() int {
	a.turnMu.RLock()
	defer a.turnMu.RUnlock()
	return len(a.asyncTurns)
}

// MailboxDepth returns the mailbox queue depth
//
// For PriorityMailbox, returns (high, normal); for a regular mailbox, returns (0, len(mailbox)).
// The values are approximate (channel length is not precisely locked).
func (a *Agent) MailboxDepth() (high, normal int) {
	a.mu.Lock()
	pm := a.priorityMailbox
	mb := a.mailbox
	a.mu.Unlock()

	if pm != nil {
		return pm.Len()
	}
	if mb != nil {
		return 0, len(mb)
	}
	return 0, 0
}

// SetModelOverride sets per-ask model parameters that take precedence over
// Definition defaults. Called by the router BEFORE AskStream/Ask.
// The override is automatically cleared when the ask completes.
//
// If the agent has an explicitly configured model (from template), this is
// a no-op — template model takes precedence over task-level routing.
//
// Thread-safe (atomic pointer store). Calling with nil clears the override.
func (a *Agent) SetModelOverride(params *ModelParams) {
	if a.Def.ExplicitModel {
		return
	}
	a.modelOverride.Store(params)
}

// ClearModelOverride removes the per-ask override, reverting to Definition defaults.
func (a *Agent) ClearModelOverride() {
	a.modelOverride.Store(nil)
}

// ModelOverride returns the current per-ask model override, or nil if none is active.
// Thread-safe (atomic pointer load).
func (a *Agent) ModelOverride() *ModelParams {
	return a.modelOverride.Load()
}

// EffectiveModelID returns the model ID actually in use.
// It prefers the per-ask override (set by the router) and falls back to the
// Definition default. Thread-safe (atomic pointer load).
func (a *Agent) EffectiveModelID() string {
	if mp := a.modelOverride.Load(); mp != nil && mp.ModelID != "" {
		return mp.ModelID
	}
	return a.Def.ModelID
}

// EffectiveProviderID returns the provider ID actually in use.
// It prefers the per-ask override (set by the router) and falls back to the
// Definition default. Thread-safe (atomic pointer load).
func (a *Agent) EffectiveProviderID() string {
	if mp := a.modelOverride.Load(); mp != nil && mp.ProviderID != "" {
		return mp.ProviderID
	}
	return a.Def.ProviderID
}

// EffectiveContextWindow returns the context window capacity actually in use.
// It prefers the per-ask override (set by the router) and falls back to the
// Definition default. Thread-safe (atomic pointer load).
func (a *Agent) EffectiveContextWindow() int {
	if mp := a.modelOverride.Load(); mp != nil && mp.ContextWindow > 0 {
		return mp.ContextWindow
	}
	return a.Def.ContextWindow
}

// EffectiveTaskLevel returns the task classification level from the current
// per-ask override. Returns "" if no override is active or no level is set.
// Thread-safe (atomic pointer load).
func (a *Agent) EffectiveTaskLevel() string {
	if mp := a.modelOverride.Load(); mp != nil {
		return mp.Level
	}
	return ""
}

// RecordError increments the error counter and stores the error message.
// Called from streamLoop (LLM errors) and watchDelegatedTask (delegation failures).
func (a *Agent) RecordError(err error) {
	a.runtimeMu.Lock()
	a.runtime.errCount++
	a.runtime.lastErr = err.Error()
	a.runtimeMu.Unlock()
}

// ResetErrors clears the per-job error counters. Called at the start of
// each new job in run.go.
func (a *Agent) ResetErrors() {
	a.runtimeMu.Lock()
	a.runtime.errCount = 0
	a.runtime.lastErr = ""
	a.runtimeMu.Unlock()
}

// ErrorCount returns the number of errors recorded in the current job.
func (a *Agent) ErrorCount() int32 {
	a.runtimeMu.RLock()
	defer a.runtimeMu.RUnlock()
	return a.runtime.errCount
}

// LastError returns the most recent error message, or "" if none.
func (a *Agent) LastError() string {
	a.runtimeMu.RLock()
	defer a.runtimeMu.RUnlock()
	return a.runtime.lastErr
}

// ─── Circuit breaker ────────────────────────────────────────────────────

// IncrementConsecutiveFailures increments the circuit breaker counter and
// returns the new value. Called by streamLoop on fatal errors.
func (a *Agent) IncrementConsecutiveFailures() int32 {
	a.runtimeMu.Lock()
	a.runtime.consecutiveFailures++
	v := a.runtime.consecutiveFailures
	a.runtimeMu.Unlock()
	return v
}

func (a *Agent) ResetConsecutiveFailures() {
	a.runtimeMu.Lock()
	a.runtime.consecutiveFailures = 0
	a.runtimeMu.Unlock()
}

func (a *Agent) ConsecutiveFailures() int32 {
	a.runtimeMu.RLock()
	defer a.runtimeMu.RUnlock()
	return a.runtime.consecutiveFailures
}

// CurrentWork returns a consistent snapshot of the agent's current runtime
// observability state including lifecycle state, work tracking, errors, and
// delegation count.
func (a *Agent) CurrentWork() WorkStatus {
	a.runtimeMu.RLock()
	defer a.runtimeMu.RUnlock()
	var elapsed string
	if !a.runtime.startedAt.IsZero() {
		elapsed = time.Since(a.runtime.startedAt).Truncate(time.Millisecond).String()
	}
	return WorkStatus{
		State:               a.runtime.state,
		Prompt:              a.runtime.prompt,
		Iteration:           int(a.runtime.iter),
		CurrentTool:         a.runtime.tool,
		CurrentToolArgs:     a.runtime.toolArgs,
		Elapsed:             elapsed,
		ErrorCount:          int(a.runtime.errCount),
		LastError:           a.runtime.lastErr,
		ConsecutiveFailures: int(a.runtime.consecutiveFailures),
		PendingDelegations:  a.pendingDelegationsLocked(),
	}
}

func (a *Agent) pendingDelegationsLocked() int {
	return len(a.asyncTurns)
}

// ─── Runtime state helpers (agent goroutine only) ────────────────────────

// SetOnStateChange registers a callback invoked (outside any lock) after every
// state transition. Must be set before Start. The callback receives the new state.
func (a *Agent) SetOnStateChange(fn func(State)) {
	a.mu.Lock()
	a.onStateChange = fn
	a.mu.Unlock()
}

func (a *Agent) setRuntimeState(s State) {
	a.runtimeMu.Lock()
	a.runtime.state = s
	a.runtimeMu.Unlock()
	if fn := a.onStateChange; fn != nil {
		fn(s)
	}
}

func (a *Agent) setRuntimeExitErr(err error) {
	a.runtimeMu.Lock()
	a.runtime.exitErr = err
	a.runtimeMu.Unlock()
}

func (a *Agent) setWorkStart(prompt string) {
	a.runtimeMu.Lock()
	a.runtime.state = StateProcessing
	a.runtime.prompt = prompt
	a.runtime.startedAt = time.Now()
	a.runtimeMu.Unlock()
}

func (a *Agent) setWorkIter(iter int) {
	a.runtimeMu.Lock()
	a.runtime.iter = int32(iter)
	a.runtimeMu.Unlock()
}

func (a *Agent) setWorkTool(name, args string) {
	a.runtimeMu.Lock()
	a.runtime.tool = name
	a.runtime.toolArgs = args
	a.runtimeMu.Unlock()
}

func (a *Agent) clearWorkTool() {
	a.runtimeMu.Lock()
	a.runtime.tool = ""
	a.runtime.toolArgs = ""
	a.runtimeMu.Unlock()
}

func (a *Agent) clearWork() {
	a.runtimeMu.Lock()
	a.runtime.prompt = ""
	a.runtime.iter = 0
	a.runtime.tool = ""
	a.runtime.toolArgs = ""
	a.runtime.state = StateIdle
	a.runtimeMu.Unlock()
}

// ─── Event subscription (Watch mode) ────────────────────────────────────

type watchSlot struct {
	ch chan AgentEvent
	id int64
}

// Watch returns a buffered channel that receives a copy of all AgentEvent
// produced by this agent's current (or next) job execution. Returns a cancel
// function to unsubscribe and close the channel.
//
// The channel has a 64-slot buffer. If the watcher falls behind, events are
// silently dropped (non-blocking fan-out) to avoid backpressure on the
// primary consumer.
//
// Only events from the current/next streamLoop are broadcast; idle agents
// produce no events. Call after Ask/AskStream has started.
func (a *Agent) Watch() (<-chan AgentEvent, func()) {
	ch := make(chan AgentEvent, 64)
	a.watchMu.Lock()
	defer a.watchMu.Unlock()
	a.watchSeq++
	id := a.watchSeq
	a.watchSlots = append(a.watchSlots, watchSlot{ch: ch, id: id})
	cancel := func() {
		a.watchMu.Lock()
		defer a.watchMu.Unlock()
		for i, s := range a.watchSlots {
			if s.id == id {
				a.watchSlots = append(a.watchSlots[:i], a.watchSlots[i+1:]...)
				close(ch)
				return
			}
		}
	}
	return ch, cancel
}

// emitToWatchers fans out an event to all registered watchers. Non-blocking;
// slow watchers silently drop events.
func (a *Agent) emitToWatchers(ev AgentEvent) {
	a.watchMu.RLock()
	defer a.watchMu.RUnlock()
	for _, s := range a.watchSlots {
		select {
		case s.ch <- ev:
		default:
		}
	}
}

// NewAgent constructs an agent that is not yet Started
//
// log can be nil (in which case log calls are skipped).
// Start must be called before it can begin receiving Ask / Submit.
func NewAgent(def Definition, llm LLMClient, log *logger.Logger, opts ...Option) *Agent {
	a := &Agent{
		Def:            def,
		LLM:            llm,
		Log:            log,
		InstanceID:     uuid.NewString(),
		mailboxCap:     DefaultMailboxCap,
		asyncTurns:     make(map[int]*asyncTurnState),
		pendingConfirm: make(map[string]*confirmSlot),
		confirmStore:   NewMemoryConfirmStore(),
		bypassConfirm:  def.BypassConfirm,
	}
	for _, opt := range opts {
		opt(a)
	}
	a.runtime.state = StateStopped
	return a
}