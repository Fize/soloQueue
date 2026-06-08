package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/xiaobaitu/soloqueue/internal/memoryengine"
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
	_, err := m.simEngine.Start(r.Context(), id)
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
	SeedText     string `json:"seed_text"`
	Topic        string `json:"topic,omitempty"`
	PersonaCount int    `json:"persona_count"`
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
	if req.PersonaCount <= 0 {
		req.PersonaCount = 3
	}
	if req.PersonaCount < 2 || req.PersonaCount > 5 {
		m.writeJSON(w, http.StatusBadRequest, map[string]string{"error": "persona_count must be between 2 and 5"})
		return
	}

	simID, extraction, personas, err := m.simEngine.CreateFromSeed(r.Context(), req.SeedText, req.Topic, req.PersonaCount)
	if err != nil {
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
