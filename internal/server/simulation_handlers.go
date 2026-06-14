package server

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/xiaobaitu/soloqueue/internal/memoryengine"
	"github.com/xiaobaitu/soloqueue/internal/logger"
	"github.com/xiaobaitu/soloqueue/internal/simulation"
)

// handleListSimulations lists all simulations.
func (m *Mux) handleListSimulations(w http.ResponseWriter, r *http.Request) {
	if m.simEngine == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "simulation engine not configured"})
		return
	}
	sims := m.simEngine.List()
	m.writeJSON(w, http.StatusOK, sims)
}

// handleCreateSimulation creates a new simulation.
func (m *Mux) handleCreateSimulation(w http.ResponseWriter, r *http.Request) {
	if m.simEngine == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "simulation engine not configured"})
		return
	}
	var config simulation.SimulationConfig
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	id, err := m.simEngine.Create(config)
	if err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	m.writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

// handleGetSimulation returns a simulation by ID.
func (m *Mux) handleGetSimulation(w http.ResponseWriter, r *http.Request) {
	if m.simEngine == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "simulation engine not configured"})
		return
	}
	id := chi.URLParam(r, "id")
	state, err := m.simEngine.Get(id)
	if err != nil {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	m.writeJSON(w, http.StatusOK, state)
}

// handleStartSimulation starts a simulation.
func (m *Mux) handleStartSimulation(w http.ResponseWriter, r *http.Request) {
	if m.simEngine == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "simulation engine not configured"})
		return
	}
	id := chi.URLParam(r, "id")
	_, err := m.simEngine.Start(context.Background(), id)
	if err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	m.writeJSON(w, http.StatusOK, map[string]string{"status": "started", "id": id})
}

// handleStopSimulation stops a running simulation.
func (m *Mux) handleStopSimulation(w http.ResponseWriter, r *http.Request) {
	if m.simEngine == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "simulation engine not configured"})
		return
	}
	id := chi.URLParam(r, "id")
	if err := m.simEngine.Stop(id); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	m.writeJSON(w, http.StatusOK, map[string]string{"status": "stopped", "id": id})
}

// handleDeleteSimulation deletes a simulation.
func (m *Mux) handleDeleteSimulation(w http.ResponseWriter, r *http.Request) {
	if m.simEngine == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "simulation engine not configured"})
		return
	}
	id := chi.URLParam(r, "id")
	if err := m.simEngine.Delete(id); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	m.writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}

type fromSeedRequest struct {
	SeedText        string `json:"seed_text"`
	Topic           string `json:"topic,omitempty"`
	PersonaCount    int    `json:"persona_count"`
	ModelID         string `json:"model_id,omitempty"`
	ProviderID      string `json:"provider_id,omitempty"`
	MaxWallClockMs  int    `json:"max_wall_clock_ms,omitempty"`
	SimulatedHours  int    `json:"simulated_hours,omitempty"`
	TickIntervalMs  int    `json:"tick_interval_ms,omitempty"`
	TimeScale       int    `json:"time_scale,omitempty"`
	EnableReflection bool  `json:"enable_reflection,omitempty"`
}

type fromSeedResponse struct {
	SimulationID string                          `json:"simulation_id"`
	Entities     []memoryengine.EntityExtraction `json:"entities"`
	Personas     []simulation.Persona           `json:"personas"`
	Topic        string                          `json:"topic"`
}

// handleCreateFromSeed creates a simulation from seed text with auto-generated personas.
func (m *Mux) handleCreateFromSeed(w http.ResponseWriter, r *http.Request) {
	if m.simEngine == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "simulation engine not configured"})
		return
	}
	var req fromSeedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.SeedText == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "seed_text is required"})
		return
	}
	if req.PersonaCount != 0 && (req.PersonaCount < 2 || req.PersonaCount > 50) {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "persona_count must be 0 (auto-detect) or between 2 and 50"})
		return
	}

	opts := simulation.CreateFromSeedOptions{
		ModelID:         req.ModelID,
		ProviderID:      req.ProviderID,
		MaxWallClockMs:  req.MaxWallClockMs,
		SimulatedHours:  req.SimulatedHours,
		TickIntervalMs:  req.TickIntervalMs,
		TimeScale:       req.TimeScale,
		EnableReflection: req.EnableReflection,
	}

	m.log.InfoContext(r.Context(), logger.CatSimulation, "create from seed: request received", "seed_text_len", len(req.SeedText))
	simID, extraction, personas, err := m.simEngine.CreateFromSeed(r.Context(), req.SeedText, req.Topic, req.PersonaCount, opts)
	if err != nil {
		m.log.Error(logger.CatSimulation, "create from seed failed", "err", err.Error())
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	topic := req.Topic
	if topic == "" && extraction != nil && len(extraction.KeyTopics) > 0 {
		topic = extraction.KeyTopics[0]
	}

	m.writeJSON(w, http.StatusCreated, fromSeedResponse{
		SimulationID: simID,
		Entities:     extraction.Entities,
		Personas:     personas,
		Topic:        topic,
	})
}

type agentAskRequest struct {
	Question string `json:"question"`
}

// handleAgentAsk allows querying a simulation agent after the simulation ends.
func (m *Mux) handleAgentAsk(w http.ResponseWriter, r *http.Request) {
	if m.simEngine == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "simulation engine not configured"})
		return
	}

	simID := chi.URLParam(r, "id")
	personaID := chi.URLParam(r, "personaId")

	var req agentAskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Question == "" {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "question is required"})
		return
	}

	answer, err := m.simEngine.ReplayAsk(r.Context(), simID, personaID, req.Question)
	if err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	m.writeJSON(w, http.StatusOK, map[string]string{
		"simulation_id": simID,
		"persona_id":    personaID,
		"question":      req.Question,
		"answer":        answer,
	})
}

// handleForkSimulation forks an existing simulation with modified parameters.
func (m *Mux) handleForkSimulation(w http.ResponseWriter, r *http.Request) {
	if m.simEngine == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "simulation engine not configured"})
		return
	}
	id := chi.URLParam(r, "id")

	var req simulation.ForkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	newID, err := m.simEngine.Fork(r.Context(), id, req)
	if err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	m.writeJSON(w, http.StatusCreated, map[string]string{
		"source_simulation_id": id,
		"new_simulation_id":    newID,
		"status":               "forked",
	})
}

// handleUpdateSimulation updates a pending/idle simulation's configuration.
func (m *Mux) handleUpdateSimulation(w http.ResponseWriter, r *http.Request) {
	if m.simEngine == nil {
		m.writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "simulation engine not configured"})
		return
	}
	id := chi.URLParam(r, "id")
	state, err := m.simEngine.Get(id)
	if err != nil {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	state.Lock()
	defer state.Unlock()

	if state.Status != simulation.StatusPending {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "only pending simulations can be configured"})
		return
	}

	var newConfig simulation.SimulationConfig
	if err := json.NewDecoder(r.Body).Decode(&newConfig); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if err := newConfig.Validate(); err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	state.Config = newConfig
	// Update in store
	if err := m.simEngine.Update(id, state); err != nil {
		m.writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	m.writeJSON(w, http.StatusOK, state)
}

// handleGetEnvironment returns the environment state for a simulation.
func (m *Mux) handleGetEnvironment(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	state, err := m.simEngine.Get(id)
	if err != nil {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	m.writeJSON(w, http.StatusOK, map[string]any{
		"simulation_id": id,
		"world_state":   state.WorldState.Snapshot(),
		"status":        state.Status,
	})
}

// handleGetAgentPlan returns the current plan for a simulation agent.
func (m *Mux) handleGetAgentPlan(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	personaID := chi.URLParam(r, "personaId")
	state, err := m.simEngine.Get(id)
	if err != nil {
		m.writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	if as, ok := state.AgentStates[personaID]; ok {
		m.writeJSON(w, http.StatusOK, map[string]any{
			"simulation_id": id,
			"persona_id":    personaID,
			"agent_state":   as,
		})
		return
	}
	m.writeJSON(w, http.StatusNotFound, map[string]string{"error": "agent not found"})
}

// handleGetAgentMemory returns the memory records for a simulation agent.
func (m *Mux) handleGetAgentMemory(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	personaID := chi.URLParam(r, "personaId")
	records, err := m.simEngine.GetAgentMemories(id, personaID)
	if err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	m.writeJSON(w, http.StatusOK, map[string]any{
		"simulation_id": id,
		"persona_id":    personaID,
		"memories":      records,
		"count":         len(records),
	})
}

// handleGetAgentReflections returns the reflections for a simulation agent.
func (m *Mux) handleGetAgentReflections(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	personaID := chi.URLParam(r, "personaId")
	records, err := m.simEngine.GetAgentMemories(id, personaID)
	if err != nil {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	var reflections []simulation.MemoryRecord
	for _, rec := range records {
		if rec.RecordType == "reflection" {
			reflections = append(reflections, rec)
		}
	}
	m.writeJSON(w, http.StatusOK, map[string]any{
		"simulation_id": id,
		"persona_id":    personaID,
		"reflections":   reflections,
		"count":         len(reflections),
	})
}
