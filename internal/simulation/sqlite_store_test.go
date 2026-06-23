package simulation

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/tools"
)

func TestSqliteStoreDeleteCascade(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sim_store_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "sims.db")
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	defer store.Close()

	// 1. Create a simulation
	config := SimulationConfig{
		ID:    "test-sim-id",
		Topic: "Test Cascade Delete",
		Personas: []Persona{
			{ID: "p1", Name: "Alice"},
			{ID: "p2", Name: "Bob"},
		},
	}
	simID, err := store.Create(config)
	if err != nil {
		t.Fatalf("failed to create simulation: %v", err)
	}

	// 2. Add round results
	rounds := []RoundResult{
		{
			RoundNumber: 1,
			Messages: []RoundMessage{
				{AgentID: "p1", AgentName: "Alice", Content: "Hello", To: "p2", Type: "speak", Round: 1},
			},
			StartedAt:   time.Now(),
			CompletedAt: time.Now(),
		},
	}
	if err := store.SaveResults(simID, rounds, "Final Report Summary"); err != nil {
		t.Fatalf("failed to save round results: %v", err)
	}

	// 3. Add agent memories
	memories := []MemoryRecord{
		{
			Round:      1,
			Role:       "observation",
			Content:    "Alice saw Bob",
			RecordType: "observation",
			Timestamp:  time.Now(),
		},
	}
	if err := store.SaveAgentMemories(simID, "p1", memories); err != nil {
		t.Fatalf("failed to save agent memories: %v", err)
	}

	// Verify they exist in DB
	state, err := store.Get(simID)
	if err != nil {
		t.Fatalf("failed to get simulation: %v", err)
	}
	if len(state.Rounds) != 1 {
		t.Errorf("expected 1 round, got %d", len(state.Rounds))
	}
	mems, err := store.GetAgentMemories(simID, "p1")
	if err != nil {
		t.Fatalf("failed to get agent memories: %v", err)
	}
	if len(mems) != 1 {
		t.Errorf("expected 1 memory record, got %d", len(mems))
	}

	// 4. Delete the simulation
	if err := store.Delete(simID); err != nil {
		t.Fatalf("failed to delete simulation: %v", err)
	}

	// 5. Verify cascade deletion has cleared rounds and memories
	_, err = store.Get(simID)
	if err != ErrSimNotFound {
		t.Errorf("expected ErrSimNotFound, got %v", err)
	}

	// Query rounds directly from DB to verify cascade
	var count int
	err = store.db.QueryRow(`SELECT count(*) FROM simulation_rounds WHERE simulation_id = ?`, simID).Scan(&count)
	if err != nil {
		t.Fatalf("failed to query simulation_rounds count: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 simulation_rounds, got %d", count)
	}

	// Query memories directly from DB to verify cascade
	err = store.db.QueryRow(`SELECT count(*) FROM agent_memories WHERE simulation_id = ?`, simID).Scan(&count)
	if err != nil {
		t.Fatalf("failed to query agent_memories count: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 agent_memories, got %d", count)
	}
}

func TestSimulationPauseResumeStepTransitions(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "sim_control_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "sims.db")
	engine := NewSimulationEngine(nil, nil, nil, tools.Config{}, SimulationConfigFile{DBPath: dbPath}, nil)

	config := SimulationConfig{
		Topic: "Test Control transitions",
		Personas: []Persona{
			{ID: "p1", Name: "Alice"},
			{ID: "p2", Name: "Bob"},
		},
		PacingIntervalMs: 123,
	}

	simID, err := engine.Create(config)
	if err != nil {
		t.Fatalf("failed to create: %v", err)
	}

	// 1. Verify pacing interval persisted
	state, err := engine.Get(simID)
	if err != nil {
		t.Fatalf("failed to get: %v", err)
	}
	if state.Config.PacingIntervalMs != 123 {
		t.Errorf("expected pacing interval 123, got %d", state.Config.PacingIntervalMs)
	}

	// 2. Pause simulation (should fail if not running)
	err = engine.Pause(simID)
	if err == nil {
		t.Error("expected error pausing pending simulation")
	}

	// Set status to running manually to test control transitions
	state.Lock()
	state.Status = StatusRunning
	state.Unlock()
	if err := engine.store.Update(simID, state); err != nil {
		t.Fatalf("failed to update status: %v", err)
	}

	// Now Pause should succeed
	err = engine.Pause(simID)
	if err != nil {
		t.Fatalf("failed to pause: %v", err)
	}

	state, _ = engine.Get(simID)
	if state.Status != StatusPaused {
		t.Errorf("expected StatusPaused, got %s", state.Status)
	}

	// Step channel initialization verification
	engine.pausesMu.Lock()
	stepCh := make(chan struct{}, 1)
	engine.stepChs[simID] = stepCh
	engine.pausesMu.Unlock()

	// 3. Step simulation
	err = engine.Step(simID)
	if err != nil {
		t.Fatalf("failed to step: %v", err)
	}

	// Verify stepCh received signal
	select {
	case <-stepCh:
		// success
	default:
		t.Error("expected step signal, got none")
	}

	// 4. Resume simulation
	err = engine.Resume(simID)
	if err != nil {
		t.Fatalf("failed to resume: %v", err)
	}

	state, _ = engine.Get(simID)
	if state.Status != StatusRunning {
		t.Errorf("expected StatusRunning, got %s", state.Status)
	}
}
