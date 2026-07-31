package server

import (
	"errors"
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
	ErrSessionBusy              = errors.New("session already has an active request")
	ErrDuplicateRequestID       = errors.New("request ID is already registered")
	ErrRequestNotFound          = errors.New("request not found")
	ErrRequestSessionMismatch   = errors.New("request does not belong to session")
	ErrToolConfirmationMismatch = errors.New("tool confirmation call ID does not belong to request")
)

// ActiveRequest holds active request metadata independent of WebSocket client connection state.
type ActiveRequest struct {
	SessionID       string
	RequestID       string
	AgentInstanceID string
	OwnerClientID   string
	State           RequestState
	StartedAt       time.Time
	Delegating      bool
	PendingCallIDs  map[string]struct{}
}

// ActiveRequestRegistry manages all active requests across the server.
// It is keyed by both session ID and request ID.
type ActiveRequestRegistry struct {
	mu        sync.RWMutex
	bySession map[string]map[string]*ActiveRequest // sessionID -> requestID -> request
	byRequest map[string]*ActiveRequest
}

// NewActiveRequestRegistry creates a new ActiveRequestRegistry instance.
func NewActiveRequestRegistry() *ActiveRequestRegistry {
	return &ActiveRequestRegistry{
		bySession: make(map[string]map[string]*ActiveRequest),
		byRequest: make(map[string]*ActiveRequest),
	}
}

// Reserve registers a new active request for a session.
// L1 sessions allow concurrent requests; L2 and other sessions are single-flight.
// Returns ErrSessionBusy if a single-flight session already has an active request.
// Returns ErrDuplicateRequestID if the request ID is already registered.
func (r *ActiveRequestRegistry) Reserve(sessionID, requestID, ownerClientID string) (ActiveRequest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

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

	if r.bySession[sessionID] == nil {
		r.bySession[sessionID] = make(map[string]*ActiveRequest)
	}

	req := &ActiveRequest{
		SessionID:      sessionID,
		RequestID:      requestID,
		OwnerClientID:  ownerClientID,
		State:          RequestStateStarting,
		StartedAt:      time.Now(),
		PendingCallIDs: make(map[string]struct{}),
	}
	r.bySession[sessionID][requestID] = req
	r.byRequest[requestID] = req

	return r.snapshot(req), nil
}

// GetBySession returns an active request snapshot for a session, if any.
// For multi-request sessions (L1), any active request is returned.
func (r *ActiveRequestRegistry) GetBySession(sessionID string) (ActiveRequest, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	reqs, ok := r.bySession[sessionID]
	if !ok || len(reqs) == 0 {
		return ActiveRequest{}, false
	}
	for _, req := range reqs {
		return r.snapshot(req), true
	}
	return ActiveRequest{}, false
}

// GetBySessionAll returns all active request snapshots for a session.
func (r *ActiveRequestRegistry) GetBySessionAll(sessionID string) []ActiveRequest {
	r.mu.RLock()
	defer r.mu.RUnlock()
	reqs, ok := r.bySession[sessionID]
	if !ok {
		return nil
	}
	result := make([]ActiveRequest, 0, len(reqs))
	for _, req := range reqs {
		result = append(result, r.snapshot(req))
	}
	return result
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

// RegisterPendingCall adds a pending tool call ID to an active request.
func (r *ActiveRequestRegistry) RegisterPendingCall(requestID, callID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	req, ok := r.byRequest[requestID]
	if !ok {
		return ErrRequestNotFound
	}
	req.PendingCallIDs[callID] = struct{}{}
	return nil
}

// ResolvePendingCall removes a pending tool call ID from an active request.
func (r *ActiveRequestRegistry) ResolvePendingCall(requestID, callID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	req, ok := r.byRequest[requestID]
	if !ok {
		return ErrRequestNotFound
	}
	if _, exists := req.PendingCallIDs[callID]; !exists {
		return ErrToolConfirmationMismatch
	}
	delete(req.PendingCallIDs, callID)
	return nil
}

// Finalize removes an active request from both session and request indexes.
// It is idempotent and releases all pending call IDs.
func (r *ActiveRequestRegistry) Finalize(sessionID, requestID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	req, ok := r.byRequest[requestID]
	if !ok || req.SessionID != sessionID {
		return false
	}

	delete(r.byRequest, requestID)
	delete(r.bySession[sessionID], requestID)
	if len(r.bySession[sessionID]) == 0 {
		delete(r.bySession, sessionID)
	}
	return true
}

func (r *ActiveRequestRegistry) snapshot(req *ActiveRequest) ActiveRequest {
	pending := make(map[string]struct{}, len(req.PendingCallIDs))
	for k, v := range req.PendingCallIDs {
		pending[k] = v
	}
	return ActiveRequest{
		SessionID:       req.SessionID,
		RequestID:       req.RequestID,
		AgentInstanceID: req.AgentInstanceID,
		OwnerClientID:   req.OwnerClientID,
		State:           req.State,
		StartedAt:       req.StartedAt,
		Delegating:      req.Delegating,
		PendingCallIDs:  pending,
	}
}
