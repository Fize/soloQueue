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
	MBTI         string            `json:"mbti,omitempty"`
	Age          int               `json:"age,omitempty"`
	Gender       string            `json:"gender,omitempty"`
	Country      string            `json:"country,omitempty"`
	Profession   string            `json:"profession,omitempty"`
	Bio          string            `json:"bio,omitempty"`
	Persona      string            `json:"persona,omitempty"`
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
type SimulationConfig struct {
	ID              string         `json:"id,omitempty"`
	Topic           string         `json:"topic"`
	Description     string         `json:"description,omitempty"`
	Personas        []Persona      `json:"personas"`
	WorldState      map[string]any `json:"initial_world_state,omitempty"`
	MaxWallClockMs  int            `json:"max_wall_clock_ms,omitempty"`
	InitialEdges    []EdgeDTO      `json:"-"` // populated from seed extraction, not persisted

	// Generative Agents extensions
	SimulatedHours    int  `json:"simulated_hours,omitempty"`
	TickIntervalMs    int  `json:"tick_interval_ms,omitempty"`
	TimeScale         int  `json:"time_scale,omitempty"`
	EnableReflection  bool `json:"enable_reflection,omitempty"`
}

// Validate checks the config and applies defaults.
func (c *SimulationConfig) Validate() error {
	if c.Topic == "" {
		return ErrEmptyTopic
	}
	if len(c.Personas) < 2 {
		return ErrTooFewPersonas
	}
	if len(c.Personas) > 50 {
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
	if c.MaxWallClockMs <= 0 {
		c.MaxWallClockMs = 300000 // 5 minutes
	}
	if c.TickIntervalMs <= 0 {
		c.TickIntervalMs = 500
	}
	if c.TimeScale <= 0 {
		c.TimeScale = 600 // 1s real = 10min simulated
	}
	if c.SimulatedHours <= 0 {
		c.SimulatedHours = 48
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

// SimulationRelationGraph represents the interaction graph schema returned to the UI.
type SimulationRelationGraph struct {
	Nodes []string  `json:"nodes"`
	Edges []EdgeDTO `json:"edges"`
}

// SimulationState holds the full mutable state of a simulation.
type SimulationState struct {
	Config       SimulationConfig         `json:"config"`
	Status       SimulationStatus         `json:"status"`
	CurrentRound int                      `json:"current_round"`
	Rounds       []RoundResult            `json:"rounds"`
	WorldState   *WorldState              `json:"-"`
	AgentStates  map[string]*AgentState   `json:"agent_states"`
	CreatedAt    time.Time                `json:"created_at"`
	StartedAt    *time.Time               `json:"started_at,omitempty"`
	CompletedAt  *time.Time               `json:"completed_at,omitempty"`
	Error        string                   `json:"error,omitempty"`
	RunID        string                   `json:"run_id"`
	Report       string                   `json:"report,omitempty"`
	Graph        *SimulationRelationGraph `json:"graph,omitempty"`
	mu           sync.RWMutex
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

// EdgeDTO is a serializable graph edge for real-time progress updates.
type EdgeDTO struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Type   string `json:"type"`
	Weight int    `json:"weight"`
}

// AgentProgressState tracks per-agent runtime progress for frontend display.
type AgentProgressState struct {
	PersonaID      string `json:"persona_id"`
	Name           string `json:"name"`
	Role           string `json:"role"`
	MessageCount   int    `json:"message_count"`
	LastActionType string `json:"last_action_type"`
	LastActionTime string `json:"last_action_time"`
	Status         string `json:"status"` // "thinking" | "spoke" | "idle"
}

// SimulationProgress is broadcast periodically via WebSocket during simulation.
type SimulationProgress struct {
	SimulationID          string                          `json:"simulation_id"`
	Phase                 string                          `json:"phase"` // "initializing"|"running"|"generating_report"|"completed"|"failed"
	ProgressPercent       float64                         `json:"progress_percent"`
	CurrentActions        int                             `json:"current_actions"`
	MaxActions            int                             `json:"max_actions"`
	ElapsedSeconds        float64                         `json:"elapsed_seconds"`
	EstimatedRemainingSec float64                         `json:"estimated_remaining_seconds"`
	AgentStates           map[string]*AgentProgressState  `json:"agent_states,omitempty"`
	GraphEdges            []EdgeDTO                       `json:"graph_edges,omitempty"`
	RecentLogs            []string                        `json:"recent_logs,omitempty"`
}

// MemoryRecord is a single entry in an agent's memory.
type MemoryRecord struct {
	Round        int            `json:"round"`
	Role         string         `json:"role"`
	Content      string         `json:"content"`
	WorldState   map[string]any `json:"world_state"`
	ReceivedMsgs []Message      `json:"received_msgs"`
	Timestamp    time.Time      `json:"timestamp"`

	// Generative Agents extensions
	RecordType    string    `json:"record_type,omitempty"`    // "observation", "action", "reflection", "plan", "dialogue"
	Importance    float64   `json:"importance,omitempty"`     // 1-10 importance score
	Source        string    `json:"source,omitempty"`         // agentID or objectID origin
	Location      string    `json:"location,omitempty"`       // zone name where event occurred
	SimulatedTime time.Time `json:"simulated_time,omitempty"` // simulated clock time
}

// Observation represents something an agent perceives from the environment.
type Observation struct {
	Type       string    `json:"type"`       // "agent_speak", "agent_move", "agent_enter", "agent_leave", "object", "environment", "nearby_zone", "agent_present", "time_event"
	Content    string    `json:"content"`    // natural language description
	Source     string    `json:"source"`     // agentID or objectID that generated this
	Importance float64   `json:"importance"` // 1-10, for retrieval priority
	At         time.Time `json:"at"`         // simulated time
}

// DailyPlan is an agent's full day schedule.
type DailyPlan struct {
	GeneratedAt time.Time  `json:"generated_at"`
	Schedule    []PlanItem `json:"schedule"`
	AgentID     string     `json:"agent_id"`
}

// PlanItem is a single scheduled activity.
type PlanItem struct {
	StartTime   time.Time `json:"start_time"`
	EndTime     time.Time `json:"end_time"`
	Activity    string    `json:"activity"`
	Location    string    `json:"location"`    // zone name
	Description string    `json:"description"` // short description
	Status      string    `json:"status"`      // "pending", "in_progress", "completed", "cancelled"
}

// ReflectionRecord holds a generated reflection.
type ReflectionRecord struct {
	AgentID     string    `json:"agent_id"`
	Content     string    `json:"content"`     // the reflection text
	GeneratedAt time.Time `json:"generated_at"` // simulated time
	Sources     []int     `json:"sources"`      // memory record rounds that inspired this
	Importance  float64   `json:"importance"`
}

// AgentRelationship captures one agent's internal model of another agent.
type AgentRelationship struct {
	SubjectID   string    `json:"subject_id"`   // the observing agent
	TargetID    string    `json:"target_id"`    // the agent being observed
	Familiarity float64   `json:"familiarity"`  // 0.0-1.0
	Affinity    float64   `json:"affinity"`     // -1.0 (hate) to 1.0 (love)
	Tags        []string  `json:"tags"`         // "reliable", "annoying", etc.
	LastUpdated time.Time `json:"last_updated"`
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

// TruncateTo keeps the last n records and discards the rest.
func (am *AgentMemory) TruncateTo(n int) {
	am.mu.Lock()
	defer am.mu.Unlock()
	if len(am.records) <= n {
		return
	}
	am.records = am.records[len(am.records)-n:]
}

// TruncateByImportance reduces the memory to approximately n records,
// keeping the most recent records but prioritizing high-importance ones.
// This follows the Generative Agents paper's approach of retaining memories
// weighted by recency, importance, and relevance.
func (am *AgentMemory) TruncateByImportance(n int) {
	am.mu.Lock()
	defer am.mu.Unlock()
	if len(am.records) <= n {
		return
	}

	// Keep the most recent 2/3 of target as-is (recency wins for recent memories).
	// For the remaining 1/3, select from older records by importance.
	recentKeep := n * 2 / 3
	importanceKeep := n - recentKeep

	// Always keep the most recent records
	newRecords := make([]MemoryRecord, 0, n)
	split := len(am.records) - recentKeep
	if split < 0 {
		split = 0
	}
	newRecords = append(newRecords, am.records[split:]...)

	// From the older records, keep those with highest importance
	olderRecords := am.records[:split]
	if len(olderRecords) > 0 && importanceKeep > 0 {
		// Sort older records by importance descending (simple insertion sort for small N)
		sorted := make([]MemoryRecord, len(olderRecords))
		copy(sorted, olderRecords)
		for i := 1; i < len(sorted); i++ {
			j := i
			for j > 0 && sorted[j].Importance > sorted[j-1].Importance {
				sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
				j--
			}
		}
		// Take the top importanceKeep from sorted (or fewer if not enough)
		take := importanceKeep
		if take > len(sorted) {
			take = len(sorted)
		}
		// Insert them at the beginning of newRecords, preserving time order among selected
		selected := sorted[:take]
		// Sort selected back by timestamp to maintain chronological order
		for i := 1; i < len(selected); i++ {
			j := i
			for j > 0 && selected[j].Timestamp.Before(selected[j-1].Timestamp) {
				selected[j], selected[j-1] = selected[j-1], selected[j]
				j--
			}
		}
		newRecords = append(selected, newRecords...)
	}

	am.records = newRecords
}
