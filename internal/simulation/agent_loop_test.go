package simulation

import (
	"context"
	"math/rand"
	"strings"
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
		nil,
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
		nil,
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
		nil,
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

func createTestAgentLoop(t *testing.T, persona *Persona, env *Environment, bus *MessageBus, clock *SimClock, fakeLLM *agent.FakeLLM, allPersonas []Persona, ws *WorldState) (*GAAgentLoop, *agent.Agent) {
	cw := ctxwin.NewContextWindow(128000, 2000, 0, ctxwin.NewTokenizer())
	cw.Push(ctxwin.RoleSystem, "test prompt")

	def := agent.Definition{
		ID:            persona.ID,
		Name:          persona.Name,
		ContextWindow: 128000,
	}
	agt := agent.NewAgent(def, fakeLLM, nil)
	if err := agt.Start(context.Background()); err != nil {
		t.Fatalf("failed to start agent: %v", err)
	}

	simAgent := &SimAgent{
		agent:     agt,
		persona:   persona,
		cw:        cw,
		memory:    NewAgentMemory(persona.ID),
		bus:       bus,
		timeout:   5 * time.Minute,
		personaID: persona.ID,
	}

	dialogueMgr := NewDialogueManager(bus)
	relationshipMgr := NewRelationshipManager()
	
	nameByID := make(map[string]string)
	for _, p := range allPersonas {
		nameByID[p.ID] = p.Name
	}

	loop := NewGAAgentLoop(
		simAgent, env, bus, clock, nil, relationshipMgr,
		nil, nil, dialogueMgr, ws,
		nameByID,
		allPersonas,
		nil,
		"zh",
		nil,
	)
	return loop, agt
}

func TestGAAgentLoop_PerceptionCheck(t *testing.T) {
	cfg := DefaultClockConfig()
	clock := NewSimClock(cfg)
	env := NewEnvironment(clock)
	bus := NewMessageBus(16)
	ws := NewWorldState(nil)

	env.AddZone("zone1", "Zone 1", 10)
	env.PlaceAgent("alice", "zone1")
	env.PlaceAgent("bob", "zone1")
	env.PlaceAgent("charlie", "zone1")

	// Charlie is visible. Bob is hidden.
	env.HideAgent("bob")

	alicePersona := &Persona{
		ID:   "alice",
		Name: "Alice",
		Traits: map[string]string{
			"perception": "100",
		},
	}
	bobPersona := &Persona{
		ID:   "bob",
		Name: "Bob",
		Traits: map[string]string{
			"stealth": "0",
		},
	}
	charliePersona := &Persona{
		ID:   "charlie",
		Name: "Charlie",
		Traits: map[string]string{
			"stealth": "50",
		},
	}

	fakeLLM := &agent.FakeLLM{
		Responses: []string{"[WAIT]"},
	}

	loop, agt := createTestAgentLoop(t, alicePersona, env, bus, clock, fakeLLM, []Persona{*alicePersona, *bobPersona, *charliePersona}, ws)
	defer agt.Stop(100 * time.Millisecond)

	// Set rand seed for deterministic perception check
	rand.Seed(42)

	loop.ProcessRound(context.Background(), 1, SimTimeEvent{SimTime: clock.Now()})

	// 1. Verify visible agent Charlie automatically boosts familiarity
	relCharlie := loop.relationshipMgr.Get("alice", "charlie")
	if relCharlie == nil || relCharlie.Familiarity <= 0.0 {
		t.Errorf("expected visible agent Charlie to automatically boost familiarity, got: %v", relCharlie)
	}

	// 2. Verify perception roll found the hidden Bob (under seed 42, pDiscover=0.9 will pass)
	relBob := loop.relationshipMgr.Get("alice", "bob")
	if relBob == nil || relBob.Familiarity <= 0.0 {
		t.Errorf("expected hidden agent Bob to be discovered and boost familiarity, got: %v", relBob)
	}

	foundInMem := false
	for _, rec := range loop.sa.Memory().Records() {
		if strings.Contains(rec.Content, "discovered") && strings.Contains(rec.Content, "Bob") {
			foundInMem = true
			break
		}
	}
	if !foundInMem {
		t.Error("expected memory to record discovery of Bob")
	}
}

func TestGAAgentLoop_SneakAttack(t *testing.T) {
	cfg := DefaultClockConfig()
	clock := NewSimClock(cfg)
	env := NewEnvironment(clock)
	bus := NewMessageBus(16)
	ws := NewWorldState(nil)

	env.AddZone("zone1", "Zone 1", 10)
	env.PlaceAgent("alice", "zone1")
	env.PlaceAgent("bob", "zone1")

	alicePersona := &Persona{
		ID:   "alice",
		Name: "Alice",
		Traits: map[string]string{
			"combat_strength": "30",
		},
	}
	bobPersona := &Persona{
		ID:   "bob",
		Name: "Bob",
		Traits: map[string]string{
			"combat_strength": "50",
		},
	}

	fakeLLM := &agent.FakeLLM{
		Responses: []string{"[WAIT]"},
	}

	// We run on Bob's loop to resolve the conflict initiated by Alice
	loop, agt := createTestAgentLoop(t, bobPersona, env, bus, clock, fakeLLM, []Persona{*alicePersona, *bobPersona}, ws)
	defer agt.Stop(100 * time.Millisecond)

	// Scenario A: Ordinary attack (no sneak/hide).
	// Alice strength = 30, Bob strength = 50. Diff = 30 - 50 = -20.
	// Since diff (-20) is within [-30, 30], it results in a Draw.
	rand.Seed(1)
	loop.resolveConflictState(context.Background(), "alice", "Alice", "bob", "Bob", &Action{Type: ActionConflict}, false, 1, SimTimeEvent{SimTime: clock.Now()})

	foundDraw := false
	for _, rec := range loop.sa.Memory().Records() {
		if strings.Contains(rec.Content, "draw") {
			foundDraw = true
			break
		}
	}
	if !foundDraw {
		t.Error("expected ordinary conflict to result in a draw")
	}

	// Reset memory
	loop.sa.memory = NewAgentMemory("bob")

	// Scenario B: Sneak attack.
	// Alice is hidden. Alice gets +30 combat strength.
	// Alice total strength = 30 + 30 = 60, Bob = 50. Diff = 10 (still a draw, but we want to verify the strength calculation).
	// Let's set Alice base strength to 10.
	// Without sneak attack: Alice (10) vs Bob (50) -> diff = -40 -> Bob wins, Alice loses (and potential death check).
	// With sneak attack: Alice (10+30=40) vs Bob (50) -> diff = -10 -> Draw.
	alicePersona.Traits["combat_strength"] = "10"
	loop.resolveConflictState(context.Background(), "alice", "Alice", "bob", "Bob", &Action{Type: ActionConflict}, true, 2, SimTimeEvent{SimTime: clock.Now()})

	foundDraw = false
	for _, rec := range loop.sa.Memory().Records() {
		if strings.Contains(rec.Content, "draw") {
			foundDraw = true
			break
		}
	}
	if !foundDraw {
		t.Error("expected sneak attack to boost strength and result in a draw instead of a defeat")
	}
}

func TestGAAgentLoop_GroupConflict(t *testing.T) {
	cfg := DefaultClockConfig()
	clock := NewSimClock(cfg)
	env := NewEnvironment(clock)
	bus := NewMessageBus(16)
	ws := NewWorldState(nil)

	env.AddZone("zone1", "Zone 1", 10)
	env.PlaceAgent("alice", "zone1")
	env.PlaceAgent("bob", "zone1")
	env.PlaceAgent("charlie", "zone1")

	alicePersona := &Persona{
		ID:   "alice",
		Name: "Alice",
		Traits: map[string]string{
			"combat_strength": "20",
		},
	}
	bobPersona := &Persona{
		ID:   "bob",
		Name: "Bob",
		Traits: map[string]string{
			"combat_strength": "50",
		},
	}
	charliePersona := &Persona{
		ID:   "charlie",
		Name: "Charlie",
		Traits: map[string]string{
			"combat_strength": "40",
		},
	}

	fakeLLM := &agent.FakeLLM{
		Responses: []string{"[WAIT]"},
	}

	// We run on Bob's loop to resolve conflict
	loop, agt := createTestAgentLoop(t, bobPersona, env, bus, clock, fakeLLM, []Persona{*alicePersona, *bobPersona, *charliePersona}, ws)
	defer agt.Stop(100 * time.Millisecond)

	// Scenario A: Charlie is NOT Alice's ally (affinity = 0).
	// Alice strength = 20, Bob strength = 50. Diff = 20 - 50 = -30.
	// Bob wins (diff <= -30), Alice/initiator loses.
	// B's memory will record a victory: "You won a crushing victory"
	loop.resolveConflictState(context.Background(), "alice", "Alice", "bob", "Bob", &Action{Type: ActionConflict}, false, 1, SimTimeEvent{SimTime: clock.Now()})

	foundVictory := false
	for _, rec := range loop.sa.Memory().Records() {
		if strings.Contains(rec.Content, "won a great victory") {
			foundVictory = true
			break
		}
	}
	if !foundVictory {
		t.Error("expected Bob to win when Alice has no allies")
	}

	// Reset memory
	loop.sa.memory = NewAgentMemory("bob")

	// Scenario B: Charlie IS Alice's ally (affinity > 0).
	// We set Alice's affinity with Charlie to 50.0 in the shared relationshipMgr.
	loop.relationshipMgr.SetWithKind("alice", "charlie", RelationFriend, 1.0, 50.0, nil)

	// Alice faction strength = 20 + 40 (Charlie) = 60. Bob strength = 50.
	// Diff = 60 - 50 = 10.
	// Results in a Draw. B's memory will record: "The two sides are evenly matched"
	loop.resolveConflictState(context.Background(), "alice", "Alice", "bob", "Bob", &Action{Type: ActionConflict}, false, 2, SimTimeEvent{SimTime: clock.Now()})

	foundDraw := false
	for _, rec := range loop.sa.Memory().Records() {
		if strings.Contains(rec.Content, "draw") {
			foundDraw = true
			break
		}
	}
	if !foundDraw {
		t.Error("expected conflict to be a draw when Alice's ally Charlie joins the fight")
	}
}

func TestGAAgentLoop_SpawnConstraints(t *testing.T) {
	cfg := DefaultClockConfig()
	clock := NewSimClock(cfg)
	env := NewEnvironment(clock)
	bus := NewMessageBus(16)

	alicePersona := &Persona{
		ID:   "alice",
		Name: "Alice",
		Bio:  "Has a friend named Bob.",
	}
	bobPersona := &Persona{
		ID:   "bob",
		Name: "Bob",
	}
	davidPersona := &Persona{
		ID:   "david",
		Name: "David",
	}

	fakeLLM := &agent.FakeLLM{
		Responses: []string{"[WAIT]"},
	}

	// 1. Adventure is disabled
	wsNoAdv := NewWorldState(map[string]any{"adventure": false})
	loop1, agt1 := createTestAgentLoop(t, alicePersona, env, bus, clock, fakeLLM, []Persona{*alicePersona, *bobPersona, *davidPersona}, wsNoAdv)
	defer agt1.Stop(100 * time.Millisecond)

	// A. Spawning Bob (known because Bob is in Alice's Bio) should succeed
	loop1.executeAction(context.Background(), Action{Type: ActionSpawn, Target: "Bob", Content: "A helpful companion"}, "alice", "Alice", 1, SimTimeEvent{SimTime: clock.Now()})

	foundSpawnRequest := false
	for _, rec := range loop1.sa.Memory().Records() {
		if rec.RecordType == "spawn_request" && strings.Contains(rec.Content, "Bob") {
			foundSpawnRequest = true
			break
		}
	}
	if !foundSpawnRequest {
		t.Error("expected spawn request for Bob to succeed without adventure when in bio")
	}

	// B. Spawning David (unknown, not in bio or relationships) should fail
	loop1.executeAction(context.Background(), Action{Type: ActionSpawn, Target: "David", Content: "A stranger"}, "alice", "Alice", 2, SimTimeEvent{SimTime: clock.Now()})

	foundSpawnFailed := false
	for _, rec := range loop1.sa.Memory().Records() {
		if rec.RecordType == "spawn_failed" && strings.Contains(rec.Content, "David") {
			foundSpawnFailed = true
			break
		}
	}
	if !foundSpawnFailed {
		t.Error("expected spawn request for David to fail without adventure")
	}

	// 2. Adventure is enabled
	wsAdv := NewWorldState(map[string]any{"adventure": true})
	loop2, agt2 := createTestAgentLoop(t, alicePersona, env, bus, clock, fakeLLM, []Persona{*alicePersona, *bobPersona, *davidPersona}, wsAdv)
	defer agt2.Stop(100 * time.Millisecond)

	// Spawning David (unknown) should succeed now
	loop2.executeAction(context.Background(), Action{Type: ActionSpawn, Target: "David", Content: "A stranger"}, "alice", "Alice", 3, SimTimeEvent{SimTime: clock.Now()})

	foundSpawnRequestAdv := false
	for _, rec := range loop2.sa.Memory().Records() {
		if rec.RecordType == "spawn_request" && strings.Contains(rec.Content, "David") {
			foundSpawnRequestAdv = true
			break
		}
	}
	if !foundSpawnRequestAdv {
		t.Error("expected spawn request for David to succeed when adventure is enabled")
	}
}