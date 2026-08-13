package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

var (
	ErrMemoryAccessDenied       = errors.New("memory_access_denied")
	ErrMemoryOwnerInvalid       = errors.New("memory_owner_invalid")
	ErrMemorySubjectConflict    = errors.New("memory_subject_conflict")
	ErrMemoryReplacementInvalid = errors.New("memory_replacement_invalid")
)

// Access is an immutable, server-bound capability for one memory owner and
// scope. Agent tools receive this interface instead of the privileged Engine.
type Access interface {
	Ingest(context.Context, MemoryCandidate) (IngestResult, error)
	Search(context.Context, SearchQuery) (*SearchResultSet, error)
	Timeline(context.Context, string, string, int) ([]MemoryEntry, error)
	RecallEntity(context.Context, string, int, int) ([]SearchResult, error)
}

type boundAccess struct {
	engine        *Engine
	ownerType     string
	ownerID       string
	scopeType     string
	scopeID       string
	includeGlobal bool
	writable      bool
}

// BindL1 creates an L1 capability with the existing scope behavior.
func (e *Engine) BindL1(scopeType, scopeID string, includeGlobal bool) Access {
	if scopeType == "" {
		scopeType = ScopeGlobal
	}
	return &boundAccess{
		engine: e, ownerType: OwnerL1,
		scopeType: scopeType, scopeID: scopeID,
		includeGlobal: includeGlobal, writable: true,
	}
}

// BindL2Group creates a capability for exactly one persisted group UUID.
func (e *Engine) BindL2Group(ownerID string) (Access, error) {
	ownerID = strings.TrimSpace(ownerID)
	parsed, err := uuid.Parse(ownerID)
	if err != nil || parsed.String() != strings.ToLower(ownerID) {
		return nil, ErrMemoryOwnerInvalid
	}
	return &boundAccess{
		engine: e, ownerType: OwnerL2Group, ownerID: ownerID,
		scopeType: ScopeTeam, scopeID: ownerID,
		includeGlobal: false, writable: true,
	}, nil
}

func (a *boundAccess) validate() error {
	if a == nil || a.engine == nil {
		return ErrMemoryAccessDenied
	}
	if a.ownerType == OwnerL1 && a.ownerID == "" {
		return nil
	}
	if a.ownerType == OwnerL2Group {
		parsed, err := uuid.Parse(a.ownerID)
		if err == nil && parsed.String() == strings.ToLower(a.ownerID) {
			return nil
		}
	}
	return ErrMemoryOwnerInvalid
}

func (a *boundAccess) Ingest(ctx context.Context, candidate MemoryCandidate) (IngestResult, error) {
	if err := a.validate(); err != nil {
		return IngestResult{}, err
	}
	if !a.writable {
		return IngestResult{}, ErrMemoryAccessDenied
	}
	candidate.OwnerType = a.ownerType
	candidate.OwnerID = a.ownerID
	candidate.ScopeType = a.scopeType
	candidate.ScopeID = a.scopeID
	return a.engine.Ingest(ctx, candidate)
}

func (a *boundAccess) Search(ctx context.Context, query SearchQuery) (*SearchResultSet, error) {
	if err := a.validate(); err != nil {
		return nil, err
	}
	query.OwnerType = a.ownerType
	query.OwnerID = a.ownerID
	query.ScopeType = a.scopeType
	query.ScopeID = a.scopeID
	query.IncludeGlobal = a.includeGlobal
	return a.engine.Search(ctx, query)
}

func (a *boundAccess) Timeline(ctx context.Context, from, to string, limit int) ([]MemoryEntry, error) {
	if err := a.validate(); err != nil {
		return nil, err
	}
	return a.engine.store.TimelineOwned(ctx, from, to, limit, a.ownerType, a.ownerID, a.scopeType, a.scopeID, a.includeGlobal)
}

func (a *boundAccess) RecallEntity(ctx context.Context, entity string, maxHops, limit int) ([]SearchResult, error) {
	if strings.TrimSpace(entity) == "" {
		return nil, fmt.Errorf("memory_invalid_arguments")
	}
	return a.SearchEntity(ctx, entity, limit)
}

func (a *boundAccess) SearchEntity(ctx context.Context, entity string, limit int) ([]SearchResult, error) {
	result, err := a.Search(ctx, SearchQuery{
		Entities:            []string{entity},
		Limit:               limit,
		IncludeGraphContext: true,
	})
	if err != nil {
		return nil, err
	}
	return result.Results, nil
}

func normalizeOwner(ownerType, ownerID string) (string, string) {
	if ownerType == "" {
		return OwnerL1, ""
	}
	return ownerType, ownerID
}

func validOwner(ownerType, ownerID string) bool {
	ownerType, ownerID = normalizeOwner(ownerType, ownerID)
	if ownerType == OwnerL1 {
		return ownerID == ""
	}
	if ownerType != OwnerL2Group {
		return false
	}
	parsed, err := uuid.Parse(ownerID)
	return err == nil && parsed.String() == strings.ToLower(ownerID)
}
