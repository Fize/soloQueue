package session

import (
	"errors"
	"strings"

	uuidpkg "github.com/google/uuid"
)

// SessionKind distinguishes L1 from L2 session references.
type SessionKind uint8

const (
	// KindL1 is the L1 (assistant) session — exactly one per backend process.
	KindL1 SessionKind = iota
	// KindL2 is a multi-turn L2 session, identified by a UUID.
	KindL2
)

// SessionRef is a parsed, validated session identifier.
type SessionRef struct {
	Kind SessionKind
	L2ID string // non-empty only when Kind == KindL2
}

var (
	// ErrEmptySessionID is returned when an empty session ID is parsed.
	ErrEmptySessionID = errors.New("empty session ID")
	// ErrMalformedSessionID is returned when a session ID cannot be parsed.
	ErrMalformedSessionID = errors.New("malformed session ID")
	// ErrEmptyL2UUID is returned when the l2: prefix is present but the UUID is empty.
	ErrEmptyL2UUID = errors.New("l2 session ID has empty UUID")
)

// ParseSessionID parses a raw session ID string into a validated SessionRef.
//
// Valid inputs:
//   - "l1"              → KindL1
//   - "l2:<uuid>"       → KindL2 with L2ID = <uuid> (uuid must be non-empty)
//
// Invalid inputs:
//   - ""                → ErrEmptySessionID
//   - bare UUID         → ErrMalformedSessionID
//   - "l2:"             → ErrEmptyL2UUID
//   - any other prefix  → ErrMalformedSessionID
func ParseSessionID(raw string) (SessionRef, error) {
	if raw == "" {
		return SessionRef{}, ErrEmptySessionID
	}
	if raw == "l1" {
		return SessionRef{Kind: KindL1}, nil
	}
	if strings.HasPrefix(raw, "l2:") {
		uuid := raw[3:]
		if uuid == "" {
			return SessionRef{}, ErrEmptyL2UUID
		}
		if _, err := uuidpkg.Parse(uuid); err != nil {
			return SessionRef{}, ErrMalformedSessionID
		}
		return SessionRef{Kind: KindL2, L2ID: uuid}, nil
	}
	return SessionRef{}, ErrMalformedSessionID
}

// String returns the canonical string representation of the session reference.
func (r SessionRef) String() string {
	if r.Kind == KindL1 {
		return "l1"
	}
	return "l2:" + r.L2ID
}

// NormalizeLegacy attempts to normalize a potentially empty or bare-UUID
// session ID into a canonical form. This is intended for documented legacy
// REST endpoints during the one-release compatibility window.
//
//   - ""          → "l1"
//   - "l1"        → "l1" (no change)
//   - "l2:<uuid>" → "l2:<uuid>" (no change)
//   - bare UUID   → "l2:<uuid>"
func NormalizeLegacy(raw string) string {
	if raw == "" {
		return "l1"
	}
	if raw == "l1" || strings.HasPrefix(raw, "l2:") {
		return raw
	}
	// Treat as bare UUID.
	return "l2:" + raw
}
