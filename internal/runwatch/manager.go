// Package runwatch centralizes progress-aware execution supervision.
//
// It uses hierarchical leases instead of total runtime limits so a healthy leaf
// protects its parent while genuinely orphaned work remains cancellable.
package runwatch

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type Kind string

const (
	KindRoot       Kind = "root"
	KindModel      Kind = "model"
	KindTool       Kind = "tool"
	KindDelegation Kind = "delegation"
)

type ProgressKind string

const (
	ProgressTransport  ProgressKind = "transport"
	ProgressSemantic   ProgressKind = "semantic"
	ProgressStructural ProgressKind = "structural"
)

type Code string

const (
	CodeModelTransportStalled     Code = "model_transport_stalled"
	CodeModelFirstProgressStalled Code = "model_first_progress_stalled"
	CodeModelSemanticStalled      Code = "model_semantic_stalled"
	CodeToolStalled               Code = "tool_stalled"
	CodeDelegationOrphaned        Code = "delegation_orphaned"
	CodeRootOrphaned              Code = "root_orphaned"
	CodeCancelledByUser           Code = "cancelled_by_user"
)

type Cause struct {
	Code        Code
	OperationID string
}

func (c *Cause) Error() string {
	if c.OperationID == "" {
		return string(c.Code)
	}
	return fmt.Sprintf("%s: %s", c.Code, c.OperationID)
}

func (c *Cause) Unwrap() error {
	if c != nil && c.Code == CodeCancelledByUser {
		return context.Canceled
	}
	return nil
}

func CodeOf(err error) Code {
	var cause *Cause
	if errors.As(err, &cause) {
		return cause.Code
	}
	return ""
}

type Policy struct {
	ScanInterval  time.Duration
	FirstSemantic time.Duration
	TransportIdle time.Duration
	SemanticIdle  time.Duration
	RootIdle      time.Duration
	OrphanIdle    time.Duration
}

// DefaultPolicy is the fixed production watchdog policy. It is intentionally
// internal runtime behavior rather than user configuration; tests may still
// inject a narrower Policy directly into NewManager.
func DefaultPolicy() Policy {
	return Policy{
		ScanInterval:  5 * time.Second,
		FirstSemantic: 5 * time.Minute,
		TransportIdle: 2 * time.Minute,
		SemanticIdle:  10 * time.Minute,
		RootIdle:      15 * time.Minute,
		OrphanIdle:    15 * time.Minute,
	}
}

type Metadata struct {
	RunID          string
	ParentID       string
	OwnerSessionID string
	Phase          string
}

type Snapshot struct {
	RunID          string
	Phase          string
	LastProgressAt time.Time
	WatchdogDueAt  time.Time
	Terminated     bool
	TerminalCode   Code
}

type clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

type Option func(*Manager)

func WithClock(c clock) Option {
	return func(m *Manager) { m.clock = c }
}

type Manager struct {
	mu      sync.Mutex
	clock   clock
	policy  Policy
	runs    map[string]*state
	closed  bool
	stop    chan struct{}
	wg      sync.WaitGroup
	rootSeq uint64
}

type state struct {
	id            string
	key           string
	kind          Kind
	phase         string
	policy        Policy
	parent        *state
	children      map[string]*state
	root          *state
	cancel        context.CancelCauseFunc
	startedAt     time.Time
	lastTransport time.Time
	lastSemantic  time.Time
	lastProgress  time.Time
	hasSemantic   bool
	terminal      Code
	terminated    bool
	active        bool
}

type Handle struct {
	manager *Manager
	state   *state
}

func NewManager(policy Policy, opts ...Option) *Manager {
	if policy.ScanInterval <= 0 {
		policy.ScanInterval = 5 * time.Second
	}
	m := &Manager{clock: realClock{}, policy: policy, runs: make(map[string]*state), stop: make(chan struct{})}
	for _, opt := range opts {
		opt(m)
	}
	m.wg.Add(1)
	go m.scanLoop(policy.ScanInterval)
	return m
}

func (m *Manager) scanLoop(interval time.Duration) {
	defer m.wg.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.Scan()
		case <-m.stop:
			return
		}
	}
}

func (m *Manager) Start(ctx context.Context, meta Metadata) (context.Context, *Handle, error) {
	if meta.RunID == "" {
		return nil, nil, errors.New("runwatch: run ID is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, nil, errors.New("runwatch: manager is closed")
	}
	if _, exists := m.runs[meta.RunID]; exists {
		return nil, nil, fmt.Errorf("runwatch: run %q already exists", meta.RunID)
	}
	now := m.clock.Now()
	m.rootSeq++
	runCtx, cancel := context.WithCancelCause(ctx)
	root := &state{
		id: meta.RunID, key: fmt.Sprintf("root:%d", m.rootSeq), kind: KindRoot, phase: meta.Phase, policy: mergePolicy(m.policy, Policy{}),
		children: make(map[string]*state), cancel: cancel, startedAt: now,
		lastTransport: now, lastSemantic: now, lastProgress: now, active: true,
	}
	root.root = root
	m.runs[root.id] = root
	handle := &Handle{manager: m, state: root}
	return ContextWithHandle(runCtx, handle), handle, nil
}

func (h *Handle) BeginOperation(kind Kind, id string, policy Policy) (*Handle, error) {
	if h == nil || h.manager == nil || id == "" {
		return nil, errors.New("runwatch: operation ID is required")
	}
	m := h.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	parent := h.state
	if parent == nil || !parent.active || parent.terminated || parent.root == nil || m.runs[parent.root.id] != parent.root {
		return nil, errors.New("runwatch: parent is not active")
	}
	if parent.children[id] != nil {
		return nil, fmt.Errorf("runwatch: operation %q already exists", id)
	}
	now := m.clock.Now()
	child := &state{
		id: id, key: parent.key + "/" + id, kind: kind, policy: mergePolicy(parent.root.policy, policy), parent: parent, root: parent.root,
		children: make(map[string]*state), cancel: parent.root.cancel, startedAt: now,
		lastTransport: now, lastSemantic: now, lastProgress: now, active: true,
	}
	parent.children[id] = child
	return &Handle{manager: m, state: child}, nil
}

// Kind reports the exact operation kind bound to this path-scoped handle.
func (h *Handle) Kind() Kind {
	if h == nil || h.manager == nil || h.state == nil {
		return ""
	}
	h.manager.mu.Lock()
	defer h.manager.mu.Unlock()
	if !h.state.active {
		return ""
	}
	return h.state.kind
}

func mergePolicy(base, override Policy) Policy {
	merged := base
	if override.ScanInterval > 0 {
		merged.ScanInterval = override.ScanInterval
	}
	if override.FirstSemantic > 0 {
		merged.FirstSemantic = override.FirstSemantic
	}
	if override.TransportIdle > 0 {
		merged.TransportIdle = override.TransportIdle
	}
	if override.SemanticIdle > 0 {
		merged.SemanticIdle = override.SemanticIdle
	}
	if override.RootIdle > 0 {
		merged.RootIdle = override.RootIdle
	}
	if override.OrphanIdle > 0 {
		merged.OrphanIdle = override.OrphanIdle
	}
	return merged
}

func (h *Handle) Pulse(kind ProgressKind, phase string) {
	if h == nil || h.manager == nil {
		return
	}
	m := h.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	s := h.state
	if s == nil || !s.active || s.terminated {
		return
	}
	now := m.clock.Now()
	s.lastProgress = now
	if phase != "" {
		s.phase = phase
	}
	switch kind {
	case ProgressTransport:
		s.lastTransport = now
	case ProgressSemantic:
		s.lastTransport = now
		s.lastSemantic = now
		s.hasSemantic = true
	case ProgressStructural:
		s.lastSemantic = now
		s.hasSemantic = true
	}
}

func (h *Handle) Complete() {
	if h == nil || h.manager == nil {
		return
	}
	m := h.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	s := h.state
	if s == nil || !s.active {
		return
	}
	deactivate(s)
	if s.parent != nil {
		parent := s.parent
		delete(parent.children, s.id)
		// Child completion is structural progress for its parent. Without this
		// refresh an old parent lease can expire in the gap before the next
		// sibling/model operation is registered.
		now := m.clock.Now()
		parent.lastProgress = now
		parent.lastSemantic = now
		parent.hasSemantic = true
		return
	}
	delete(m.runs, s.id)
}

func deactivate(s *state) {
	s.active = false
	for _, child := range s.children {
		deactivate(child)
	}
}

// Fail terminates the owning root exactly once with a typed cancellation cause.
func (h *Handle) Fail(cause error) {
	if h == nil || h.manager == nil || cause == nil {
		return
	}
	m := h.manager
	m.mu.Lock()
	s := h.state
	if s == nil || !s.active || s.root.terminated {
		m.mu.Unlock()
		return
	}
	root := s.root
	root.terminated = true
	root.terminal = CodeOf(cause)
	cancel := root.cancel
	m.mu.Unlock()
	cancel(cause)
}

func (m *Manager) Scan() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	now := m.clock.Now()
	type cancellation struct {
		cancel context.CancelCauseFunc
		cause  error
	}
	var cancellations []cancellation
	for _, root := range m.runs {
		if root.terminated {
			continue
		}
		if failed := firstExpired(root, now); failed != nil {
			code := expiredCode(failed, now)
			root.terminal = code
			root.terminated = true
			cancellations = append(cancellations, cancellation{
				cancel: root.cancel,
				cause:  &Cause{Code: code, OperationID: failed.id},
			})
		}
	}
	m.mu.Unlock()
	for _, cancellation := range cancellations {
		cancellation.cancel(cancellation.cause)
	}
}

func (m *Manager) Snapshot(id string) (Snapshot, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.runs[id]
	if s == nil {
		return Snapshot{}, false
	}
	return snapshotOf(s), true
}

// Snapshot returns this path-scoped handle's state without resolving an
// operation ID globally across unrelated roots.
func (h *Handle) Snapshot() (Snapshot, bool) {
	if h == nil || h.manager == nil || h.state == nil {
		return Snapshot{}, false
	}
	h.manager.mu.Lock()
	defer h.manager.mu.Unlock()
	if !h.state.active {
		return Snapshot{}, false
	}
	return snapshotOf(h.state), true
}

func snapshotOf(s *state) Snapshot {
	return Snapshot{
		RunID:          s.root.id,
		Phase:          s.phase,
		LastProgressAt: s.lastProgress,
		WatchdogDueAt:  effectiveWatchdogDueAt(s),
		Terminated:     s.terminated,
		TerminalCode:   s.terminal,
	}
}

func (m *Manager) Cancel(runID string, cause error) error {
	if cause == nil {
		return errors.New("runwatch: cancellation cause is required")
	}
	m.mu.Lock()
	root := m.runs[runID]
	if root == nil || root.terminated {
		m.mu.Unlock()
		return fmt.Errorf("runwatch: active run %q not found", runID)
	}
	root.terminal = CodeOf(cause)
	root.terminated = true
	cancel := root.cancel
	m.mu.Unlock()
	cancel(cause)
	return nil
}

func watchdogDueAt(s *state) time.Time {
	var due time.Time
	consider := func(base time.Time, idle time.Duration) {
		if idle <= 0 {
			return
		}
		candidate := base.Add(idle)
		if due.IsZero() || candidate.Before(due) {
			due = candidate
		}
	}
	if s.kind == KindModel {
		consider(s.lastTransport, s.policy.TransportIdle)
		if s.hasSemantic {
			consider(s.lastSemantic, s.policy.SemanticIdle)
		} else {
			consider(s.startedAt, s.policy.FirstSemantic)
		}
	}
	if s.kind == KindRoot {
		consider(s.lastProgress, s.policy.RootIdle)
	} else {
		consider(s.lastProgress, s.policy.OrphanIdle)
	}
	return due
}

func effectiveWatchdogDueAt(s *state) time.Time {
	if s == nil || s.terminated {
		return time.Time{}
	}
	var childDue time.Time
	activeChildren := 0
	for _, child := range s.children {
		if child.terminated {
			continue
		}
		activeChildren++
		candidate := effectiveWatchdogDueAt(child)
		if candidate.IsZero() {
			continue
		}
		if childDue.IsZero() || candidate.Before(childDue) {
			childDue = candidate
		}
	}
	if activeChildren > 0 {
		return childDue
	}
	return watchdogDueAt(s)
}

func firstExpired(s *state, now time.Time) *state {
	if s.terminated {
		return nil
	}
	for _, child := range s.children {
		if failed := firstExpired(child, now); failed != nil {
			return failed
		}
	}
	if hasHealthyChild(s, now) {
		return nil
	}
	if s.kind == KindModel {
		if s.policy.TransportIdle > 0 && now.Sub(s.lastTransport) >= s.policy.TransportIdle {
			return s
		}
		if !s.hasSemantic && s.policy.FirstSemantic > 0 && now.Sub(s.startedAt) >= s.policy.FirstSemantic {
			return s
		}
		if s.hasSemantic && s.policy.SemanticIdle > 0 && now.Sub(s.lastSemantic) >= s.policy.SemanticIdle {
			return s
		}
	}
	idle := s.policy.OrphanIdle
	if s.kind == KindRoot {
		idle = s.policy.RootIdle
	}
	if idle > 0 && now.Sub(s.lastProgress) >= idle {
		return s
	}
	return nil
}

func hasHealthyChild(s *state, now time.Time) bool {
	for _, child := range s.children {
		if !child.terminated && firstExpired(child, now) == nil {
			return true
		}
	}
	return false
}

func expiredCode(s *state, now time.Time) Code {
	if s.kind == KindModel {
		if s.policy.TransportIdle > 0 && now.Sub(s.lastTransport) >= s.policy.TransportIdle {
			return CodeModelTransportStalled
		}
		if !s.hasSemantic {
			return CodeModelFirstProgressStalled
		}
		return CodeModelSemanticStalled
	}
	if s.kind == KindTool {
		return CodeToolStalled
	}
	if s.kind == KindDelegation {
		return CodeDelegationOrphaned
	}
	return CodeRootOrphaned
}

func (m *Manager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	cancels := make([]context.CancelCauseFunc, 0, len(m.runs))
	for id, root := range m.runs {
		deactivate(root)
		cancels = append(cancels, root.cancel)
		delete(m.runs, id)
	}
	close(m.stop)
	m.mu.Unlock()
	for _, cancel := range cancels {
		cancel(context.Canceled)
	}
	m.wg.Wait()
}

type handleContextKey struct{}

func ContextWithHandle(ctx context.Context, h *Handle) context.Context {
	return context.WithValue(ctx, handleContextKey{}, h)
}

func HandleFromContext(ctx context.Context) *Handle {
	if ctx == nil {
		return nil
	}
	h, _ := ctx.Value(handleContextKey{}).(*Handle)
	return h
}

func Pulse(ctx context.Context, kind ProgressKind, phase string) {
	if h := HandleFromContext(ctx); h != nil {
		h.Pulse(kind, phase)
	}
}
