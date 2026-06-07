package simulation

import (
	"fmt"
	"sync"
	"time"
)

// Persona defines the personality, goals, and behavioral traits of a simulation agent.
type Persona struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Role         string            `json:"role"`
	SystemPrompt string            `json:"system_prompt"`
	Goals        []string          `json:"goals"`
	Traits       map[string]string `json:"traits"`
	ModelID      string            `json:"model_id,omitempty"`
	ProviderID   string            `json:"provider_id,omitempty"`
	Temperature  float64           `json:"temperature,omitempty"`
}

// InteractionMode is deprecated; all simulations now use event-driven mode.
// Kept for API compatibility; ignored by the engine.
type InteractionMode string

const (
	ModeEventDriven InteractionMode = "event-driven"
)

// SimulationConfig is the complete simulation setup.
// All simulations run in event-driven mode: agents independently decide when to speak.
type SimulationConfig struct {
	ID                 string         `json:"id,omitempty"`
	Topic              string         `json:"topic"`
	Description        string         `json:"description,omitempty"`
	Personas           []Persona      `json:"personas"`
	WorldState         map[string]any `json:"initial_world_state,omitempty"`
	MaxActions         int            `json:"max_actions,omitempty"`
	MaxWallClockMs     int            `json:"max_wall_clock_ms,omitempty"`
	TriggerPolicy      string         `json:"trigger_policy,omitempty"`
	MinSpeakIntervalMs int            `json:"min_speak_interval_ms,omitempty"`
}

// Validate checks the config and applies defaults.
func (c *SimulationConfig) Validate() error {
	if c.Topic == "" {
		return ErrEmptyTopic
	}
	if len(c.Personas) < 2 {
		return ErrTooFewPersonas
	}
	if len(c.Personas) > 5 {
		return ErrTooManyPersonas
	}
	seen := make(map[string]bool)
	for _, p := range c.Personas {
		if p.ID == "" {
			return fmt.Errorf("persona has empty id")
		}
		if seen[p.ID] {
			return fmt.Errorf("%w: %s", ErrDuplicatePersonaID, p.ID)
		}
		seen[p.ID] = true
	}
	if c.MaxActions <= 0 {
		c.MaxActions = 15
	}
	if c.MaxWallClockMs <= 0 {
		c.MaxWallClockMs = 300000 // 5 minutes
	}
	if c.TriggerPolicy == "" {
		c.TriggerPolicy = "selective"
	}
	if c.MinSpeakIntervalMs <= 0 {
		c.MinSpeakIntervalMs = 2000
	}
	return nil
}

// SimulationStatus represents the current state of a simulation.
type SimulationStatus string

const (
	StatusPending   SimulationStatus = "pending"
	StatusRunning   SimulationStatus = "running"
	StatusCompleted SimulationStatus = "completed"
	StatusFailed    SimulationStatus = "failed"
	StatusCancelled SimulationStatus = "cancelled"
)

// SimulationState holds the full mutable state of a simulation.
type SimulationState struct {
	Config      SimulationConfig        `json:"config"`
	Status      SimulationStatus        `json:"status"`
	CurrentRound int                     `json:"current_round"`
	Rounds      []RoundResult           `json:"rounds"`
	WorldState  *WorldState             `json:"-"`
	AgentStates map[string]*AgentState  `json:"agent_states"`
	CreatedAt   time.Time               `json:"created_at"`
	StartedAt   *time.Time              `json:"started_at,omitempty"`
	CompletedAt *time.Time              `json:"completed_at,omitempty"`
	Error       string                  `json:"error,omitempty"`
	RunID       string                  `json:"run_id"`
	Report      string                  `json:"report,omitempty"`
	mu          sync.RWMutex
}

func (s *SimulationState) Lock()   { s.mu.Lock() }
func (s *SimulationState) Unlock() { s.mu.Unlock() }
func (s *SimulationState) RLock()  { s.mu.RLock() }
func (s *SimulationState) RUnlock() { s.mu.RUnlock() }

// RoundResult captures a single round of simulation.
type RoundResult struct {
	RoundNumber int            `json:"round_number"`
	Messages    []RoundMessage `json:"messages"`
	Summary     string         `json:"summary"`
	StartedAt   time.Time      `json:"started_at"`
	CompletedAt time.Time      `json:"completed_at"`
}

// RoundMessage is a single agent output in a round.
type RoundMessage struct {
	AgentID   string `json:"agent_id"`
	AgentName string `json:"agent_name"`
	Content   string `json:"content"`
	Reasoning string `json:"reasoning,omitempty"`
	To        string `json:"to"`
	Type      string `json:"type"`
	Round     int    `json:"round"`
	SeqNum    int    `json:"seq_num"`
}

// AgentState tracks per-agent runtime statistics.
type AgentState struct {
	PersonaID     string         `json:"persona_id"`
	InstanceID    string         `json:"instance_id"`
	TotalMessages int            `json:"total_messages"`
	TotalTokens   int            `json:"total_tokens"`
	IsActive      bool           `json:"is_active"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

// SimulationEvent is emitted during simulation for streaming to API consumers.
type SimulationEvent struct {
	Type         string    `json:"type"`
	SimulationID string    `json:"simulation_id"`
	Round        int       `json:"round"`
	Data         any       `json:"data,omitempty"`
	Error        string    `json:"error,omitempty"`
	Timestamp    time.Time `json:"timestamp"`
}

// MemoryRecord is a single entry in an agent's memory.
type MemoryRecord struct {
	Round        int            `json:"round"`
	Role         string         `json:"role"`
	Content      string         `json:"content"`
	WorldState   map[string]any `json:"world_state"`
	ReceivedMsgs []Message      `json:"received_msgs"`
	Timestamp    time.Time      `json:"timestamp"`
}

// AgentMemory accumulates all rounds for a single agent. Append-only, thread-safe.
type AgentMemory struct {
	personaID string
	records   []MemoryRecord
	mu        sync.RWMutex
}

func NewAgentMemory(personaID string) *AgentMemory {
	return &AgentMemory{
		personaID: personaID,
		records:   make([]MemoryRecord, 0),
	}
}

func (am *AgentMemory) Record(rec MemoryRecord) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.records = append(am.records, rec)
}

func (am *AgentMemory) Records() []MemoryRecord {
	am.mu.RLock()
	defer am.mu.RUnlock()
	out := make([]MemoryRecord, len(am.records))
	copy(out, am.records)
	return out
}

func (am *AgentMemory) ByRound(round int) []MemoryRecord {
	am.mu.RLock()
	defer am.mu.RUnlock()
	var out []MemoryRecord
	for _, r := range am.records {
		if r.Round == round {
			out = append(out, r)
		}
	}
	return out
}

func (am *AgentMemory) TokenCount() int {
	am.mu.RLock()
	defer am.mu.RUnlock()
	total := 0
	for _, r := range am.records {
		total += len(r.Content) / 4 // rough estimate
	}
	return total
}

// StancePoint represents an agent's position at a specific round.
type StancePoint struct {
	Round   int    `json:"round"`
	Stance  string `json:"stance"`
	Summary string `json:"summary"`
}

// StanceEvolution extracts the agent's stance progression across rounds.
func (am *AgentMemory) StanceEvolution() []StancePoint {
	am.mu.RLock()
	defer am.mu.RUnlock()

	var points []StancePoint
	for _, r := range am.records {
		if r.Role != "assistant" {
			continue
		}
		// Use first 200 chars as stance summary
		summary := r.Content
		if len(summary) > 200 {
			summary = summary[:200]
		}
		points = append(points, StancePoint{
			Round:   r.Round,
			Stance:  am.personaID,
			Summary: summary,
		})
	}
	return points
}
