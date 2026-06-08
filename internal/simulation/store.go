package simulation

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

// Store is the CRUD interface for simulation persistence.
// Both SimulationStore (in-memory) and SQLiteStore implement it.
type Store interface {
	Create(config SimulationConfig) (string, error)
	Get(id string) (*SimulationState, error)
	List() []*SimulationState
	Update(id string, state *SimulationState) error
	Delete(id string) error
	SaveAgentMemories(simID string, personaID string, records []MemoryRecord) error
	GetAgentMemories(simID string, personaID string) ([]MemoryRecord, error)
}

// SimulationStore provides in-memory CRUD for simulations.
type SimulationStore struct {
	simulations map[string]*SimulationState
	agentMem    map[string]map[string][]MemoryRecord // simID → personaID → records
	mu          sync.RWMutex
}

func NewSimulationStore() *SimulationStore {
	return &SimulationStore{
		simulations: make(map[string]*SimulationState),
		agentMem:    make(map[string]map[string][]MemoryRecord),
	}
}

// Create stores a new simulation and returns its ID.
func (s *SimulationStore) Create(config SimulationConfig) (string, error) {
	if err := config.Validate(); err != nil {
		return "", err
	}

	id := uuid.NewString()
	if config.ID != "" {
		id = config.ID
	}

	state := &SimulationState{
		Config:      config,
		Status:      StatusPending,
		CurrentRound: 0,
		Rounds:      make([]RoundResult, 0),
		WorldState:  NewWorldState(config.WorldState),
		AgentStates: make(map[string]*AgentState),
		CreatedAt:   time.Now(),
		RunID:       id,
	}

	for _, p := range config.Personas {
		state.AgentStates[p.ID] = &AgentState{
			PersonaID: p.ID,
			IsActive:  true,
		}
	}

	s.mu.Lock()
	s.simulations[id] = state
	s.mu.Unlock()

	return id, nil
}

// Get retrieves a simulation by ID.
func (s *SimulationStore) Get(id string) (*SimulationState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, ok := s.simulations[id]
	if !ok {
		return nil, ErrSimNotFound
	}
	return state, nil
}

// List returns all simulations.
func (s *SimulationStore) List() []*SimulationState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*SimulationState, 0, len(s.simulations))
	for _, st := range s.simulations {
		out = append(out, st)
	}
	return out
}

// Update replaces the stored state.
func (s *SimulationStore) Update(id string, state *SimulationState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.simulations[id]; !ok {
		return ErrSimNotFound
	}
	s.simulations[id] = state
	return nil
}

// Delete removes a simulation.
func (s *SimulationStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.simulations[id]; !ok {
		return ErrSimNotFound
	}
	delete(s.simulations, id)
	delete(s.agentMem, id)
	return nil
}

// SaveAgentMemories stores or appends agent memories for a simulation.
func (s *SimulationStore) SaveAgentMemories(simID string, personaID string, records []MemoryRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	memMap, ok := s.agentMem[simID]
	if !ok {
		memMap = make(map[string][]MemoryRecord)
		s.agentMem[simID] = memMap
	}
	memMap[personaID] = append(memMap[personaID], records...)
	return nil
}

// GetAgentMemories retrieves agent memories for a simulation agent.
func (s *SimulationStore) GetAgentMemories(simID string, personaID string) ([]MemoryRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	memMap, ok := s.agentMem[simID]
	if !ok {
		return nil, ErrSimNotFound
	}
	records, ok := memMap[personaID]
	if !ok || len(records) == 0 {
		return nil, nil
	}
	out := make([]MemoryRecord, len(records))
	copy(out, records)
	return out, nil
}
