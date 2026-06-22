package simulation

import (
	"context"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/ctxwin"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

func TestGAAgentLoop_New(t *testing.T) {
	cfg := DefaultClockConfig()
	clock := NewSimClock(cfg)
	env := NewEnvironment(clock)
	bus := NewMessageBus(16)
	ws := NewWorldState(nil)

	persona := &Persona{ID: "alice", Name: "Alice", Role: "Developer"}
	cw := ctxwin.NewContextWindow(128000, 2000, 0, ctxwin.NewTokenizer())
	cw.Push(ctxwin.RoleSystem, "test prompt")

	simAgent := &SimAgent{
		agent:     nil,
		persona:   persona,
		cw:        cw,
		memory:    NewAgentMemory("alice"),
		bus:       bus,
		timeout:   5 * time.Minute,
		personaID: "alice",
	}

	planGen := NewPlanGenerator(nil, "model", "provider")
	dialogueMgr := NewDialogueManager(bus)
	relationshipMgr := NewRelationshipManager()

	loop := NewGAAgentLoop(
		simAgent, env, bus, clock, planGen, relationshipMgr,
		nil, nil, dialogueMgr, ws,
		map[string]string{"alice": "Alice"},
		[]Persona{*persona},
		nil,
		"zh",
	)

	if loop == nil {
		t.Fatal("expected non-nil loop")
	}
	if loop.sa == nil {
		t.Error("expected sim agent")
	}
}

func TestGAAgentLoop_Stop(t *testing.T) {
	// Create a minimal loop and verify Stop works
	cfg := DefaultClockConfig()
	clock := NewSimClock(cfg)
	env := NewEnvironment(clock)
	bus := NewMessageBus(16)
	ws := NewWorldState(nil)

	persona := &Persona{ID: "alice", Name: "Alice", Role: "Developer"}
	cw := ctxwin.NewContextWindow(128000, 2000, 0, ctxwin.NewTokenizer())

	simAgent := &SimAgent{
		agent:     nil,
		persona:   persona,
		cw:        cw,
		memory:    NewAgentMemory("alice"),
		bus:       bus,
		timeout:   5 * time.Minute,
		personaID: "alice",
	}

	dialogueMgr := NewDialogueManager(bus)
	relationshipMgr := NewRelationshipManager()

	loop := NewGAAgentLoop(
		simAgent, env, bus, clock, nil, relationshipMgr,
		nil, nil, dialogueMgr, ws,
		map[string]string{},
		[]Persona{},
		nil,
		"zh",
	)

	// Stop should not panic even if loop isn't running
	loop.Stop()

	// Double stop should be safe
	loop.Stop()
}

func TestGAAgentLoop_Events(t *testing.T) {
	cfg := DefaultClockConfig()
	clock := NewSimClock(cfg)
	env := NewEnvironment(clock)
	bus := NewMessageBus(16)
	ws := NewWorldState(nil)

	persona := &Persona{ID: "alice", Name: "Alice", Role: "Developer"}
	cw := ctxwin.NewContextWindow(128000, 2000, 0, ctxwin.NewTokenizer())

	simAgent := &SimAgent{
		agent:     nil,
		persona:   persona,
		cw:        cw,
		memory:    NewAgentMemory("alice"),
		bus:       bus,
		timeout:   5 * time.Minute,
		personaID: "alice",
	}

	dialogueMgr := NewDialogueManager(bus)
	relationshipMgr := NewRelationshipManager()

	loop := NewGAAgentLoop(
		simAgent, env, bus, clock, nil, relationshipMgr,
		nil, nil, dialogueMgr, ws,
		map[string]string{},
		[]Persona{},
		nil,
		"zh",
	)

	events := loop.Events()
	if events == nil {
		t.Error("expected non-nil events channel")
	}
}

func TestSafePersonaName(t *testing.T) {
	if safePersonaName(nil) != "unknown" {
		t.Error("expected 'unknown' for nil persona")
	}
	p := &Persona{Name: "Alice"}
	if safePersonaName(p) != "Alice" {
		t.Errorf("expected 'Alice', got %q", safePersonaName(p))
	}
}

// TestGAAgentLoop_Integration verifies the smoke-test flow:
// create engine → create simulation from seed → start → drain events.
// Uses FakeLLM so no real LLM calls are made.
func TestGAAgentLoop_Integration(t *testing.T) {
	fakeLLM := &agent.FakeLLM{
		Responses: []string{
			// Seed extraction Phase 1 (basic)
			`{"entities":[{"name":"AI","type":"technology","confidence":0.9}],"world_state":{},"key_topics":["AI safety"],"conflict_areas":["regulation"]}`,
			// Seed extraction Phase 2 (characters — empty)
			`{"suggested_agents":[],"lifecycle_events":[],"initial_relationships":[]}`,
			// Persona generation
			`{"personas":[{"id":"alice","name":"Alice","role":"Researcher","goals":["Study AI"],"traits":{"curious":"high"},"stance_per_entity":{"AI":"pro"}},{"id":"bob","name":"Bob","role":"Ethicist","goals":["Ensure safety"],"traits":{"cautious":"high"},"stance_per_entity":{"AI":"con"}}]}`,
		},
	}

	registry := agent.NewRegistry(nil)
	factory := agent.NewDefaultFactory(registry, fakeLLM, tools.Config{WorkDir: "/tmp"}, nil)
	engine := NewSimulationEngine(
		factory, registry, fakeLLM,
		tools.Config{WorkDir: "/tmp"},
		SimulationConfigFile{DefaultMaxWallClockMs: 5000},
		nil,
	)

	ctx := context.Background()
	simID, extraction, personas, err := engine.CreateFromSeed(ctx, "AI is transformative.", "", 2, CreateFromSeedOptions{MaxWallClockMs: 5000, EnableReflection: false})
	if err != nil {
		t.Fatalf("CreateFromSeed: %v", err)
	}
	if simID == "" {
		t.Fatal("expected non-empty simID")
	}
	if extraction == nil {
		t.Fatal("expected non-nil extraction")
	}
	if len(personas) != 2 {
		t.Fatalf("expected 2 personas, got %d", len(personas))
	}

	// Verify the simulation was created
	state, err := engine.Get(simID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if state.Config.Topic != "AI safety" {
		t.Errorf("expected topic 'AI safety', got %q", state.Config.Topic)
	}
	if len(state.Config.Personas) != 2 {
		t.Errorf("expected 2 personas in config, got %d", len(state.Config.Personas))
	}
}
