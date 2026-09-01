package server

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

// RequestState describes the active phase of a chat request in the global registry.
type RequestState string

const (
	RequestStateStarting   RequestState = "starting"
	RequestStateStreaming  RequestState = "streaming"
	RequestStateDelegating RequestState = "delegating"
	RequestStateCancelling RequestState = "cancelling"
)

var (
	ErrSessionBusy            = errors.New("session already has an active request")
	ErrDuplicateRequestID     = errors.New("request ID is already registered")
	ErrRequestNotFound        = errors.New("request not found")
	ErrRequestSessionMismatch = errors.New("request does not belong to session")
)

// ActiveRequest holds active request metadata independent of WebSocket client connection state.
type ActiveRequest struct {
	SessionID          string
	RequestID          string
	AgentInstanceID    string
	OwnerClientID      string
	State              RequestState
	StartedAt          time.Time
	Delegating         bool
	RunID              string
	Phase              string
	LastProgressAt     time.Time
	WatchdogDueAt      time.Time
	TerminalCode       string
	cancelReady        chan struct{}
	canceller          func() error
	cancelBound        bool
	finalized          bool
	cancelStarted      bool
	cancelDone         chan struct{}
	cancelErr          error
	terminalRecordedAt time.Time
}

type WatchdogState struct {
	RunID          string
	Phase          string
	LastProgressAt time.Time
	WatchdogDueAt  time.Time
	TerminalCode   string
}

// ActiveRequestRegistry manages all active requests across the server.
// It is keyed by both session ID and request ID.
type ActiveRequestRegistry struct {
	mu          sync.RWMutex
	bySession   map[string]map[string]*ActiveRequest // sessionID -> requestID -> request
	byRequest   map[string]*ActiveRequest
	terminals   map[string]map[string]*ActiveRequest // bounded rebuildable terminal snapshots
	now         func() time.Time
	terminalTTL time.Duration
}

const maxTerminalTombstones = 128
const defaultTerminalTombstoneTTL = 30 * time.Second

// NewActiveRequestRegistry creates a new ActiveRequestRegistry instance.
func NewActiveRequestRegistry() *ActiveRequestRegistry {
	return &ActiveRequestRegistry{
		bySession:   make(map[string]map[string]*ActiveRequest),
		byRequest:   make(map[string]*ActiveRequest),
		terminals:   make(map[string]map[string]*ActiveRequest),
		now:         time.Now,
		terminalTTL: defaultTerminalTombstoneTTL,
	}
}

// Reserve registers a new active request for a session.
// L1 sessions allow concurrent requests; L2 and other sessions are single-flight.
// Returns ErrSessionBusy if a single-flight session already has an active request.
// Returns ErrDuplicateRequestID if the request ID is already registered.
func (r *ActiveRequestRegistry) Reserve(sessionID, requestID, ownerClientID string) (ActiveRequest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trimTerminalsLocked()

	// L1 sessions allow concurrent requests.
	// L2 sessions and other sessions are single-flight.
	isL1 := sessionID == "l1"
	if !isL1 {
		if reqs, exists := r.bySession[sessionID]; exists && len(reqs) > 0 {
			return ActiveRequest{}, ErrSessionBusy
		}
	}
	if _, exists := r.byRequest[requestID]; exists {
		return ActiveRequest{}, ErrDuplicateRequestID
	}
	if isL1 {
		if byID := r.terminals[sessionID]; byID != nil {
			delete(byID, requestID)
			if len(byID) == 0 {
				delete(r.terminals, sessionID)
			}
		}
	} else {
		// A single-flight reservation starts a new observable generation. No
		// terminal from an older generation may remain its current runtime.
		delete(r.terminals, sessionID)
	}

	if r.bySession[sessionID] == nil {
		r.bySession[sessionID] = make(map[string]*ActiveRequest)
	}

	now := r.now()
	req := &ActiveRequest{
		SessionID:      sessionID,
		RequestID:      requestID,
		OwnerClientID:  ownerClientID,
		State:          RequestStateStarting,
		StartedAt:      now,
		RunID:          requestID,
		Phase:          string(RequestStateStarting),
		LastProgressAt: now,
		cancelReady:    make(chan struct{}),
		cancelDone:     make(chan struct{}),
	}
	r.bySession[sessionID][requestID] = req
	r.byRequest[requestID] = req

	return r.snapshot(req), nil
}

// BindCanceller attaches the exact session run only after AskStream has
// registered it. A cancellation requested during RequestStateStarting waits
// on this binding instead of being falsely acknowledged.
func (r *ActiveRequestRegistry) BindCanceller(requestID string, canceller func() error) error {
	if canceller == nil {
		return errors.New("request canceller is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	req, ok := r.byRequest[requestID]
	if !ok || req.finalized {
		return ErrRequestNotFound
	}
	if req.cancelBound {
		return errors.New("request canceller already bound")
	}
	req.canceller = canceller
	req.cancelBound = true
	close(req.cancelReady)
	return nil
}

// CancelAndWait atomically tombstones a request, waits for its exact Session
// run to be bound, invokes cancellation, and returns only after that run has
// stopped (the Session canceller provides the final wait). owner is true only
// for the caller responsible for emitting the request's terminal envelopes.
func (r *ActiveRequestRegistry) CancelAndWait(ctx context.Context, sessionID, requestID string) (ActiveRequest, bool, error) {
	r.mu.Lock()
	req, ok := r.byRequest[requestID]
	if !ok || req.SessionID != sessionID || req.finalized {
		r.mu.Unlock()
		return ActiveRequest{}, false, ErrRequestNotFound
	}
	if req.cancelStarted {
		done := req.cancelDone
		r.mu.Unlock()
		select {
		case <-done:
			r.mu.RLock()
			err := req.cancelErr
			snapshot := r.snapshot(req)
			r.mu.RUnlock()
			return snapshot, false, err
		case <-ctx.Done():
			return ActiveRequest{}, false, context.Cause(ctx)
		}
	}
	req.cancelStarted = true
	req.State = RequestStateCancelling
	ready := req.cancelReady
	r.mu.Unlock()

	select {
	case <-ready:
	case <-ctx.Done():
		r.finishCancel(req, context.Cause(ctx))
		return ActiveRequest{}, true, context.Cause(ctx)
	}

	r.mu.RLock()
	finalized, canceller := req.finalized, req.canceller
	snapshot := r.snapshot(req)
	r.mu.RUnlock()
	if finalized || canceller == nil {
		r.finishCancel(req, ErrRequestNotFound)
		return ActiveRequest{}, true, ErrRequestNotFound
	}
	err := canceller()
	r.finishCancel(req, err)
	return snapshot, true, err
}

func (r *ActiveRequestRegistry) finishCancel(req *ActiveRequest, err error) {
	r.mu.Lock()
	req.cancelErr = err
	select {
	case <-req.cancelDone:
	default:
		close(req.cancelDone)
	}
	r.mu.Unlock()
}

func (r *ActiveRequestRegistry) SetWatchdog(requestID string, state WatchdogState) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	req, ok := r.byRequest[requestID]
	if !ok {
		return false, ErrRequestNotFound
	}
	changed := req.RunID != state.RunID || req.Phase != state.Phase ||
		!req.LastProgressAt.Equal(state.LastProgressAt) || !req.WatchdogDueAt.Equal(state.WatchdogDueAt) ||
		req.TerminalCode != state.TerminalCode
	req.RunID = state.RunID
	req.Phase = state.Phase
	req.LastProgressAt = state.LastProgressAt
	req.WatchdogDueAt = state.WatchdogDueAt
	req.TerminalCode = state.TerminalCode
	return changed, nil
}

// GetBySession returns an active request, or the latest observable terminal
// snapshot when no request remains active.
func (r *ActiveRequestRegistry) GetBySession(sessionID string) (ActiveRequest, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trimTerminalsLocked()
	reqs, ok := r.bySession[sessionID]
	if ok && len(reqs) > 0 {
		var latest *ActiveRequest
		for _, req := range reqs {
			if latest == nil || requestAfter(req.StartedAt, req.RequestID, latest.StartedAt, latest.RequestID) {
				latest = req
			}
		}
		return r.snapshot(latest), true
	}
	var latest *ActiveRequest
	for _, req := range r.terminals[sessionID] {
		if latest == nil || requestAfter(req.terminalRecordedAt, req.RequestID, latest.terminalRecordedAt, latest.RequestID) {
			latest = req
		}
	}
	if latest != nil {
		return r.snapshot(latest), true
	}
	return ActiveRequest{}, false
}

// GetBySessionAll returns all active request snapshots for a session.
func (r *ActiveRequestRegistry) GetBySessionAll(sessionID string) []ActiveRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trimTerminalsLocked()
	reqs := r.bySession[sessionID]
	terminal := r.terminals[sessionID]
	result := make([]ActiveRequest, 0, len(reqs)+len(terminal))
	active := make([]*ActiveRequest, 0, len(reqs))
	for _, req := range reqs {
		active = append(active, req)
	}
	sort.Slice(active, func(i, j int) bool {
		if active[i].StartedAt.Equal(active[j].StartedAt) {
			return active[i].RequestID < active[j].RequestID
		}
		return active[i].StartedAt.Before(active[j].StartedAt)
	})
	for _, req := range active {
		result = append(result, r.snapshot(req))
	}
	history := make([]*ActiveRequest, 0, len(terminal))
	for _, req := range terminal {
		history = append(history, req)
	}
	sort.Slice(history, func(i, j int) bool {
		if history[i].terminalRecordedAt.Equal(history[j].terminalRecordedAt) {
			return history[i].RequestID < history[j].RequestID
		}
		return history[i].terminalRecordedAt.Before(history[j].terminalRecordedAt)
	})
	for _, req := range history {
		result = append(result, r.snapshot(req))
	}
	return result
}

func requestAfter(at time.Time, requestID string, otherAt time.Time, otherRequestID string) bool {
	if at.Equal(otherAt) {
		return requestID > otherRequestID
	}
	return at.After(otherAt)
}

// ActiveCount reports the number of requests still owned by the registry.
// It is used by runtime status refreshers to avoid periodic work when idle.
func (r *ActiveRequestRegistry) ActiveCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.byRequest)
}

// Validate checks that a request ID exists and belongs to the specified session ID.
func (r *ActiveRequestRegistry) Validate(sessionID, requestID string) (ActiveRequest, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	req, ok := r.byRequest[requestID]
	if !ok {
		return ActiveRequest{}, ErrRequestNotFound
	}
	if req.SessionID != sessionID {
		return ActiveRequest{}, ErrRequestSessionMismatch
	}
	return r.snapshot(req), nil
}

// SetRoute binds the agent instance ID to the active request.
func (r *ActiveRequestRegistry) SetRoute(requestID, agentInstanceID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	req, ok := r.byRequest[requestID]
	if !ok {
		return ErrRequestNotFound
	}
	req.AgentInstanceID = agentInstanceID
	if req.State == RequestStateStarting {
		req.State = RequestStateStreaming
	}
	return nil
}

// SetState updates the state of an active request.
func (r *ActiveRequestRegistry) SetState(requestID string, state RequestState) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	req, ok := r.byRequest[requestID]
	if !ok {
		return ErrRequestNotFound
	}
	req.State = state
	return nil
}

// SetDelegating updates the delegation status of an active request.
func (r *ActiveRequestRegistry) SetDelegating(requestID string, delegating bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	req, ok := r.byRequest[requestID]
	if !ok {
		return ErrRequestNotFound
	}
	req.Delegating = delegating
	if delegating {
		req.State = RequestStateDelegating
	} else if req.State == RequestStateDelegating {
		req.State = RequestStateStreaming
	}
	return nil
}

// Finalize removes an active request from both session and request indexes.
// It is idempotent.
func (r *ActiveRequestRegistry) Finalize(sessionID, requestID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	req, ok := r.byRequest[requestID]
	if !ok || req.SessionID != sessionID {
		return false
	}
	req.finalized = true
	if !req.cancelBound {
		close(req.cancelReady)
		req.cancelBound = true
	}

	delete(r.byRequest, requestID)
	delete(r.bySession[sessionID], requestID)
	if len(r.bySession[sessionID]) == 0 {
		delete(r.bySession, sessionID)
	}
	if req.TerminalCode != "" {
		if r.terminals[sessionID] == nil {
			r.terminals[sessionID] = make(map[string]*ActiveRequest)
		}
		terminal := *req
		terminal.canceller = nil
		terminal.cancelReady = nil
		terminal.cancelDone = nil
		terminal.cancelErr = nil
		terminal.terminalRecordedAt = r.now()
		r.terminals[sessionID][requestID] = &terminal
		r.trimTerminalsLocked()
	} else if sessionID != "l1" {
		// A normal single-flight completion supersedes any older failed
		// generation, even if it was retained briefly for observers.
		delete(r.terminals, sessionID)
	}
	return true
}

// ExpireTerminals removes terminal snapshots whose observation window elapsed.
// The return value lets the Hub publish one final state that clears stale
// terminal runtime from already-connected clients.
func (r *ActiveRequestRegistry) ExpireTerminals() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.trimTerminalsLocked()
}

func (r *ActiveRequestRegistry) trimTerminalsLocked() bool {
	changed := false
	count := 0
	var oldestSession, oldestRequest string
	var oldest time.Time
	cutoff := r.now().Add(-r.terminalTTL)
	for sessionID, requests := range r.terminals {
		for requestID, req := range requests {
			if r.terminalTTL > 0 && !req.terminalRecordedAt.After(cutoff) {
				delete(requests, requestID)
				changed = true
				continue
			}
			count++
			if oldest.IsZero() || req.terminalRecordedAt.Before(oldest) {
				oldest = req.terminalRecordedAt
				oldestSession, oldestRequest = sessionID, requestID
			}
		}
		if len(requests) == 0 {
			delete(r.terminals, sessionID)
		}
	}
	for count > maxTerminalTombstones && oldestSession != "" {
		delete(r.terminals[oldestSession], oldestRequest)
		changed = true
		if len(r.terminals[oldestSession]) == 0 {
			delete(r.terminals, oldestSession)
		}
		count--
		oldestSession, oldestRequest, oldest = "", "", time.Time{}
		for sessionID, requests := range r.terminals {
			for requestID, req := range requests {
				if oldest.IsZero() || req.terminalRecordedAt.Before(oldest) {
					oldest = req.terminalRecordedAt
					oldestSession, oldestRequest = sessionID, requestID
				}
			}
		}
	}
	return changed
}

func (r *ActiveRequestRegistry) snapshot(req *ActiveRequest) ActiveRequest {
	return ActiveRequest{
		SessionID:       req.SessionID,
		RequestID:       req.RequestID,
		AgentInstanceID: req.AgentInstanceID,
		OwnerClientID:   req.OwnerClientID,
		State:           req.State,
		StartedAt:       req.StartedAt,
		Delegating:      req.Delegating,
		RunID:           req.RunID,
		Phase:           req.Phase,
		LastProgressAt:  req.LastProgressAt,
		WatchdogDueAt:   req.WatchdogDueAt,
		TerminalCode:    req.TerminalCode,
	}
}
