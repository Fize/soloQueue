package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
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
