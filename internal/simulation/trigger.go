package simulation

import (
	"strings"
	"sync"
	"time"
)

// TriggerPolicy controls when an agent decides to speak.
type TriggerPolicy interface {
	// ShouldSpeak returns true if the agent should respond now.
	// inbox: pending messages for this agent (already drained).
	// worldState: current snapshot of world state.
	// lastSpokeAt: when the agent last spoke (zero if never).
	ShouldSpeak(agentID string, inbox []Message, worldState map[string]any, lastSpokeAt time.Time) bool
}

// ─── ReactiveTrigger ───────────────────────────────────────────────────────────

// ReactiveTrigger responds immediately to every message received.
// Anti-spam: enforces MinInterval between responses.
type ReactiveTrigger struct {
	MinInterval time.Duration
}

func (t *ReactiveTrigger) ShouldSpeak(agentID string, inbox []Message, _ map[string]any, lastSpokeAt time.Time) bool {
	if len(inbox) == 0 {
		return false
	}
	if t.MinInterval > 0 && time.Since(lastSpokeAt) < t.MinInterval {
		return false
	}
	return true
}

// ─── SelectiveTrigger ──────────────────────────────────────────────────────────

// SelectiveTrigger responds only to mentions, questions, proposals, or idle timeout.
type SelectiveTrigger struct {
	RespondToMentions  bool
	RespondToQuestions bool
	RespondToProposals bool
	IdleTimeout        time.Duration // speak even if no trigger, after this long

	mu           sync.Mutex
	lastActivity time.Time // last time ANY message was received
}

func NewSelectiveTrigger(mention, question, proposal bool, idle time.Duration) *SelectiveTrigger {
	return &SelectiveTrigger{
		RespondToMentions:  mention,
		RespondToQuestions: question,
		RespondToProposals: proposal,
		IdleTimeout:        idle,
	}
}

func (t *SelectiveTrigger) ShouldSpeak(agentID string, inbox []Message, worldState map[string]any, lastSpokeAt time.Time) bool {
	t.mu.Lock()
	if len(inbox) > 0 {
		t.lastActivity = time.Now()
	}
	lastAct := t.lastActivity
	t.mu.Unlock()

	// Check each message for a trigger
	for _, msg := range inbox {
		if msg.Type == "system" {
			return true
		}
		if t.RespondToMentions && strings.Contains(msg.Content, "@"+agentID) {
			return true
		}
		if t.RespondToQuestions && strings.HasSuffix(strings.TrimSpace(msg.Content), "?") {
			return true
		}
		if t.RespondToProposals && strings.Contains(msg.Content, "[PROPOSE ") {
			return true
		}
	}

	// Idle timeout: speak if nothing has happened for a while
	if t.IdleTimeout > 0 && !lastAct.IsZero() && time.Since(lastAct) > t.IdleTimeout {
		return true
	}

	// If we have pending messages but no specific trigger, don't respond
	return false
}

// ─── RateLimitedTrigger ────────────────────────────────────────────────────────

// RateLimitedTrigger decorates another policy with per-agent rate limiting.
type RateLimitedTrigger struct {
	Inner     TriggerPolicy
	MaxPerMin int
	Burst     int

	mu       sync.Mutex
	timestamps map[string][]time.Time // agentID → recent speak times
}

func NewRateLimitedTrigger(inner TriggerPolicy, maxPerMin, burst int) *RateLimitedTrigger {
	return &RateLimitedTrigger{
		Inner:      inner,
		MaxPerMin:  maxPerMin,
		Burst:      burst,
		timestamps: make(map[string][]time.Time),
	}
}

func (t *RateLimitedTrigger) ShouldSpeak(agentID string, inbox []Message, worldState map[string]any, lastSpokeAt time.Time) bool {
	if !t.Inner.ShouldSpeak(agentID, inbox, worldState, lastSpokeAt) {
		return false
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	window := now.Add(-time.Minute)

	// Prune old timestamps
	times := t.timestamps[agentID]
	cutoff := 0
	for i, ts := range times {
		if ts.After(window) {
			cutoff = i
			break
		}
	}
	times = times[cutoff:]

	// Check burst
	if len(times) >= t.Burst {
		return false
	}

	// Check rate
	if len(times) >= t.MaxPerMin {
		return false
	}

	times = append(times, now)
	t.timestamps[agentID] = times
	return true
}

// ─── Helpers ────────────────────────────────────────────────────────────────────

// NewTriggerPolicy creates the appropriate policy from a config string.
func NewTriggerPolicy(name string, minInterval time.Duration) TriggerPolicy {
	switch name {
	case "reactive":
		return &ReactiveTrigger{MinInterval: minInterval}
	case "selective":
		fallthrough
	default:
		return NewSelectiveTrigger(true, true, true, 30*time.Second)
	}
}
