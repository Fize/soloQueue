package simulation

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// DialogueSession manages a private, multi-turn conversation between two agents.
// Both agents are locked into the session until one ends it or a timeout expires.
type DialogueSession struct {
	InitiatorID string    `json:"initiator_id"`
	TargetID    string    `json:"target_id"`
	StartedAt   time.Time `json:"started_at"`
	MaxTurns    int       `json:"max_turns"` // max turns per agent before auto-end

	mu      sync.Mutex
	turns   int       // total turns so far
	active  bool
	endAt   time.Time // set when ended
	history []Message
}

// NewDialogueSession creates a new private conversation.
func NewDialogueSession(initiatorID, targetID string, maxTurns int) *DialogueSession {
	if maxTurns <= 0 {
		maxTurns = 10
	}
	return &DialogueSession{
		InitiatorID: initiatorID,
		TargetID:    targetID,
		StartedAt:   time.Now(),
		MaxTurns:    maxTurns,
		active:      true,
		history:     make([]Message, 0, maxTurns*2),
	}
}

// Active returns whether the session is still ongoing.
func (ds *DialogueSession) Active() bool {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	return ds.active
}

// AddTurn records a message in this dialogue.
func (ds *DialogueSession) AddTurn(msg Message) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.turns++
	ds.history = append(ds.history, msg)
	if ds.turns >= ds.MaxTurns {
		ds.active = false
		ds.endAt = time.Now()
	}
}

// End terminates the dialogue session manually.
func (ds *DialogueSession) End() {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.active = false
	ds.endAt = time.Now()
}

// History returns all messages exchanged in this session.
func (ds *DialogueSession) History() []Message {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	out := make([]Message, len(ds.history))
	copy(out, ds.history)
	return out
}

// Turns returns the number of turns so far.
func (ds *DialogueSession) Turns() int {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	return ds.turns
}

// InSession returns whether the given agent is part of this dialogue.
func (ds *DialogueSession) InSession(agentID string) bool {
	return ds.InitiatorID == agentID || ds.TargetID == agentID
}

// Other returns the other participant's ID.
func (ds *DialogueSession) Other(agentID string) string {
	if ds.InitiatorID == agentID {
		return ds.TargetID
	}
	return ds.InitiatorID
}

// DialogueManager tracks all active private conversations in the simulation.
type DialogueManager struct {
	sessions   map[string]*DialogueSession // key: "initiatorID:targetID"
	agentSess  map[string]string           // agentID → sessionKey (which session they're in)
	mu         sync.RWMutex
	bus        *MessageBus
}

// NewDialogueManager creates a dialogue manager.
func NewDialogueManager(bus *MessageBus) *DialogueManager {
	return &DialogueManager{
		sessions:  make(map[string]*DialogueSession),
		agentSess: make(map[string]string),
		bus:       bus,
	}
}

// sessionKey creates a deterministic key for two agent IDs.
func sessionKey(a, b string) string {
	if a < b {
		return a + ":" + b
	}
	return b + ":" + a
}

// Request starts or resumes a private dialogue between two agents.
// The initiator sends a request to the target via the message bus.
func (dm *DialogueManager) Request(initiatorID, targetID string) error {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	key := sessionKey(initiatorID, targetID)

	// Check if either agent is already in a dialogue with someone else
	if ek, ok := dm.agentSess[initiatorID]; ok && ek != key {
		if sess, exists := dm.sessions[ek]; exists && sess.Active() {
			return fmt.Errorf("%s is already in a dialogue", initiatorID)
		}
	}
	if ek, ok := dm.agentSess[targetID]; ok && ek != key {
		if sess, exists := dm.sessions[ek]; exists && sess.Active() {
			return fmt.Errorf("%s is already in a dialogue", targetID)
		}
	}

	if sess, exists := dm.sessions[key]; exists && sess.Active() {
		return fmt.Errorf("dialogue between %s and %s is already active", initiatorID, targetID)
	}

	sess := NewDialogueSession(initiatorID, targetID, 10)
	dm.sessions[key] = sess
	dm.agentSess[initiatorID] = key
	dm.agentSess[targetID] = key

	// Send a dialogue request notification to the target
	dm.bus.Send(targetID, Message{
		From:    initiatorID,
		To:      targetID,
		Content: fmt.Sprintf("%s would like to speak with you privately.", initiatorID),
		Type:    "dialogue_request",
		Round:   0,
	})

	return nil
}

// SendMessage delivers a private message within an active dialogue.
func (dm *DialogueManager) SendMessage(senderID string, content string) error {
	dm.mu.RLock()
	key, inSession := dm.agentSess[senderID]
	if !inSession {
		dm.mu.RUnlock()
		return fmt.Errorf("agent %s is not in any dialogue", senderID)
	}
	sess, exists := dm.sessions[key]
	dm.mu.RUnlock()

	if !exists || !sess.Active() {
		return fmt.Errorf("dialogue is not active")
	}

	recipientID := sess.Other(senderID)

	msg := Message{
		From:    senderID,
		To:      recipientID,
		Content: content,
		Type:    "dialogue_response",
	}

	sess.AddTurn(msg)

	dm.bus.Send(recipientID, msg)

	// Check if this message ends the dialogue
	if strings.Contains(strings.ToLower(content), "goodbye") ||
		strings.Contains(strings.ToLower(content), "farewell") ||
		strings.Contains(strings.ToLower(content), "talk later") {
		sess.End()
		dm.cleanup(key)
	}

	return nil
}

// EndDialogue terminates a dialogue session for the given agent.
func (dm *DialogueManager) EndDialogue(agentID string) {
	dm.mu.Lock()
	defer dm.mu.Unlock()

	key, ok := dm.agentSess[agentID]
	if !ok {
		return
	}

	if sess, exists := dm.sessions[key]; exists {
		sess.End()
	}

	delete(dm.agentSess, agentID)
	other := sessKeyOther(key, agentID)
	delete(dm.agentSess, other)
	delete(dm.sessions, key)
}

// IsAgentInDialogue returns true if the agent is currently in an active private conversation.
func (dm *DialogueManager) IsAgentInDialogue(agentID string) bool {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	key, ok := dm.agentSess[agentID]
	if !ok {
		return false
	}
	sess, exists := dm.sessions[key]
	return exists && sess.Active()
}

// GetDialoguePartner returns the other participant's ID if the agent is in a dialogue.
func (dm *DialogueManager) GetDialoguePartner(agentID string) string {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	key, ok := dm.agentSess[agentID]
	if !ok {
		return ""
	}
	sess, exists := dm.sessions[key]
	if !exists || !sess.Active() {
		return ""
	}
	return sess.Other(agentID)
}

// GetSession returns the dialogue session for the given agent, if any.
func (dm *DialogueManager) GetSession(agentID string) *DialogueSession {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	key, ok := dm.agentSess[agentID]
	if !ok {
		return nil
	}
	return dm.sessions[key]
}

func (dm *DialogueManager) cleanup(key string) {
	// Save session reference BEFORE deleting from map.
	sess := dm.sessions[key]
	if sess == nil {
		return
	}
	delete(dm.sessions, key)
	delete(dm.agentSess, sess.InitiatorID)
	delete(dm.agentSess, sess.TargetID)
}

func sessKeyOther(key, agentID string) string {
	parts := strings.SplitN(key, ":", 2)
	if len(parts) != 2 {
		return ""
	}
	if parts[0] == agentID {
		return parts[1]
	}
	return parts[0]
}
