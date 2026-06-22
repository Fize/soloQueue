package simulation

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/agent"
	"github.com/xiaobaitu/soloqueue/internal/memoryengine"
	"github.com/xiaobaitu/soloqueue/internal/sqlitedb"
	"github.com/xiaobaitu/soloqueue/internal/tools"
)

func TestSimulationConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  SimulationConfig
		wantErr string
	}{
		{
			name:    "empty topic",
			config:  SimulationConfig{Personas: []Persona{{ID: "a"}, {ID: "b"}}},
			wantErr: "topic is required",
		},
		{
			name: "too few personas",
			config: SimulationConfig{
				Topic:    "test",
				Personas: []Persona{{ID: "a"}},
			},
			wantErr: "need at least 2 personas",
		},
		{
			name: "duplicate persona ids",
			config: SimulationConfig{
				Topic:    "test",
				Personas: []Persona{{ID: "a"}, {ID: "a"}},
			},
			wantErr: "duplicate persona id",
		},
		{
			name: "valid config with defaults",
			config: SimulationConfig{
				Topic:    "test",
				Personas: []Persona{{ID: "a"}, {ID: "b"}},
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("expected no error, got: %v", err)
				}
				if tt.config.MaxWallClockMs <= 0 {
					t.Errorf("expected MaxActions default, got %d", tt.config.MaxWallClockMs)
				}
				if tt.config.MaxWallClockMs <= 0 {
				t.Errorf("expected MaxWallClockMs default, got %d", tt.config.MaxWallClockMs)
			}
			return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

func TestWorldState(t *testing.T) {
	ws := NewWorldState(map[string]any{"key1": "val1"})

	v, ok := ws.Get("key1")
	if !ok || v != "val1" {
		t.Errorf("expected key1=val1, got %v, %v", v, ok)
	}

	_, ok = ws.Get("nonexistent")
	if ok {
		t.Error("expected false for nonexistent key")
	}

	ws.Set("key2", "val2", "agent-1", 1)
	v, ok = ws.Get("key2")
	if !ok || v != "val2" {
		t.Errorf("expected key2=val2, got %v, %v", v, ok)
	}

	ws.Delete("key1", "agent-1", 1)
	_, ok = ws.Get("key1")
	if ok {
		t.Error("expected key1 to be deleted")
	}

	history := ws.History()
	if len(history) != 2 {
		t.Errorf("expected 2 history entries, got %d", len(history))
	}

	snap := ws.Snapshot()
	if len(snap) != 1 {
		t.Errorf("expected 1 key in snapshot, got %d", len(snap))
	}

	formatted := ws.FormatForPrompt()
	if !strings.Contains(formatted, "key2") {
		t.Errorf("FormatForPrompt should contain key2: %s", formatted)
	}
}

func TestWorldStateEmpty(t *testing.T) {
	ws := NewWorldState(nil)
	formatted := ws.FormatForPrompt()
	if !strings.Contains(formatted, "(empty)") {
		t.Errorf("empty world state should show (empty): %s", formatted)
	}
}

func TestWorldStateConcurrency(t *testing.T) {
	ws := NewWorldState(nil)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func(n int) {
			defer wg.Done()
			ws.Set("key", n, "agent", 1)
		}(i)
		go func() {
			defer wg.Done()
			ws.Get("key")
			ws.Snapshot()
		}()
	}
	wg.Wait()
}

func TestMessageBusRegisterSendDrain(t *testing.T) {
	bus := NewMessageBus(16)
	bus.Register("alice")
	bus.Register("bob")

	msg := Message{From: "alice", To: "bob", Content: "hello", Round: 1, Type: "statement"}
	bus.Send("bob", msg)

	msgs := bus.DrainAll("bob")
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Content != "hello" {
		t.Errorf("expected 'hello', got %q", msgs[0].Content)
	}

	msgs = bus.DrainAll("bob")
	if len(msgs) != 0 {
		t.Errorf("expected empty after drain, got %d messages", len(msgs))
	}
}

func TestMessageBusBroadcast(t *testing.T) {
	bus := NewMessageBus(16)
	bus.Register("alice")
	bus.Register("bob")
	bus.Register("charlie")

	msg := Message{From: "alice", To: "*", Content: "broadcast", Round: 1, Type: "statement"}
	bus.Broadcast("alice", msg)

	aliceMsgs := bus.DrainAll("alice")
	if len(aliceMsgs) != 0 {
		t.Errorf("sender should not receive own broadcast, got %d messages", len(aliceMsgs))
	}

	for _, id := range []string{"bob", "charlie"} {
		msgs := bus.DrainAll(id)
		if len(msgs) != 1 {
			t.Errorf("%s should have 1 message, got %d", id, len(msgs))
		}
	}
}

func TestMessageBusUnregister(t *testing.T) {
	bus := NewMessageBus(16)
	bus.Register("alice")
	bus.Unregister("alice")

	msg := Message{From: "bob", To: "alice", Content: "hi", Round: 1, Type: "statement"}
	bus.Send("alice", msg)

	msgs := bus.DrainAll("alice")
	if len(msgs) != 0 {
		t.Errorf("unregistered agent should receive nothing, got %d", len(msgs))
	}
}

func TestMessageBusConcurrency(t *testing.T) {
	bus := NewMessageBus(64)
	for i := 0; i < 10; i++ {
		bus.Register(string(rune('a' + i)))
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			from := string(rune('a' + n))
			msg := Message{From: from, To: "*", Content: "msg", Round: 1, Type: "statement"}
			for j := 0; j < 100; j++ {
				bus.Broadcast(from, msg)
			}
		}(i)
	}
	wg.Wait()

	for i := 0; i < 10; i++ {
		bus.DrainAll(string(rune('a' + i)))
	}
}

func TestFormatMessages(t *testing.T) {
	msgs := []Message{
		{From: "bob", Type: "statement", Content: "I think Go is better", Round: 1},
		{From: "alice", Type: "rebuttal", Content: "@bob: But Rust is safer", Round: 1},
	}
	formatted := FormatMessages(msgs)
	if !strings.Contains(formatted, "alice") || !strings.Contains(formatted, "bob") {
		t.Errorf("FormatMessages should contain both agents: %s", formatted)
	}
}

func TestFormatMessagesEmpty(t *testing.T) {
	formatted := FormatMessages(nil)
	if !strings.Contains(formatted, "no messages") {
		t.Errorf("empty messages should show indicator: %s", formatted)
	}
}

func TestParseActions(t *testing.T) {
	tests := []struct {
		name          string
		content       string
		wantSpeak     bool
		wantPropCount int
		wantTarget    string
	}{
		{name: "simple statement", content: "I believe Rust is best.", wantSpeak: false, wantPropCount: 0},
		{name: "with say", content: "[SAY]: Hello everyone", wantSpeak: true, wantPropCount: 0, wantTarget: "*"},
		{name: "with private say", content: "[SAY @Alice]: I have a question for you.", wantSpeak: true, wantPropCount: 0, wantTarget: "Alice"},
		{name: "with proposal", content: "OK.\n[PROPOSE consensus: leaning-go]\nDone.", wantSpeak: false, wantPropCount: 1},
		{name: "multiple proposals", content: "[PROPOSE vote: 1]\n[PROPOSE concern: safety]", wantSpeak: false, wantPropCount: 2},
		{name: "with move", content: "[MOVE cafe] Let's go.", wantSpeak: false, wantPropCount: 0},
		{name: "with interact", content: "[INTERACT library_pc: search]", wantSpeak: false, wantPropCount: 0},
		{name: "with wait", content: "[WAIT 30m]", wantSpeak: false, wantPropCount: 0},
		{name: "with pass", content: "[PASS]", wantSpeak: false, wantPropCount: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actions, proposals := ParseActions(tt.content)
			if len(proposals) != tt.wantPropCount {
				t.Errorf("expected %d proposals, got %d", tt.wantPropCount, len(proposals))
			}
			if tt.wantSpeak {
				found := false
				for _, a := range actions {
					if a.Type == ActionSpeak {
						found = true
						if a.Target != tt.wantTarget {
							t.Errorf("expected target %q, got %q", tt.wantTarget, a.Target)
						}
					}
				}
				if !found {
					t.Error("expected a speak action")
				}
			}
		})
	}
}

func TestAgentMemory(t *testing.T) {
	mem := NewAgentMemory("test-agent")
	mem.Record(MemoryRecord{Round: 1, Role: "user", Content: "prompt1"})
	mem.Record(MemoryRecord{Round: 1, Role: "assistant", Content: "response1"})
	mem.Record(MemoryRecord{Round: 2, Role: "user", Content: "prompt2"})
	mem.Record(MemoryRecord{Round: 2, Role: "assistant", Content: "response2 longer"})

	records := mem.Records()
	if len(records) != 4 {
		t.Errorf("expected 4 records, got %d", len(records))
	}

	r1 := mem.ByRound(1)
	if len(r1) != 2 {
		t.Errorf("expected 2 records for round 1, got %d", len(r1))
	}
}

func TestSimulationStore(t *testing.T) {
	store := NewSimulationStore()

	config := SimulationConfig{
		Topic:    "test topic",
		Personas: []Persona{{ID: "alice", Name: "Alice"}, {ID: "bob", Name: "Bob"}},
	}

	id, err := store.Create(config)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty id")
	}

	state, err := store.Get(id)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if state.Status != StatusPending {
		t.Errorf("expected pending status, got %s", state.Status)
	}

	list := store.List()
	if len(list) != 1 {
		t.Errorf("expected 1 simulation, got %d", len(list))
	}

	err = store.Delete(id)
	if err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	_, err = store.Get(id)
	if err != ErrSimNotFound {
		t.Errorf("expected ErrSimNotFound, got %v", err)
	}
}


func TestBuildSimulationSystemPrompt(t *testing.T) {
	persona := Persona{
		ID:    "alice",
		Name:  "Alice",
		Role:  "Engineer",
		Goals: []string{"Advocate for Rust"},
		Traits: map[string]string{"openness": "high"},
	}
	allPersonas := []Persona{
		persona,
		{ID: "bob", Name: "Bob", Role: "Manager", SystemPrompt: "Prefers practicality"},
	}

	prompt := BuildSimulationSystemPrompt(persona, "Test", allPersonas)
	if !strings.Contains(prompt, "Alice") {
		t.Error("should contain persona name")
	}
	if !strings.Contains(prompt, "Bob") {
		t.Error("should mention other participants")
	}
	if !strings.Contains(prompt, "PROPOSE") {
		t.Error("should contain PROPOSE instruction")
	}
}

func TestBuildReportPrompt(t *testing.T) {
	ws := NewWorldState(map[string]any{"consensus": "leaning-go"})
	mem1 := NewAgentMemory("alice")
	mem1.Record(MemoryRecord{Round: 1, Role: "assistant", Content: "Rust is better"})
	mem2 := NewAgentMemory("bob")
	mem2.Record(MemoryRecord{Round: 1, Role: "assistant", Content: "Go is simpler"})

	memories := map[string]*AgentMemory{"alice": mem1, "bob": mem2}
	graph := NewRelationGraph()
	graph.AddEdge("alice", "bob", RelRebuttal, 1, "I disagree")
	prompt := BuildReportPrompt("Test", memories, graph, ws, "", "en")

	if !strings.Contains(prompt, "alice") {
		t.Error("should contain agent id")
	}
	if !strings.Contains(prompt, "Stance evolution") {
		t.Error("should contain instructions")
	}
}

// ─── Phase 2: GA Integration Tests ────────────────────────────────────────────

func TestCreateFromSeed(t *testing.T) {
	fakeLLM := &agent.FakeLLM{
		Responses: []string{
			// First call: seed extraction
			`{
				"entities": [{"name": "Rust", "type": "technology", "confidence": 0.9, "relations": [{"target_name": "Go", "rel_type": "rebuttal", "weight": 0.8}]}, {"name": "Go", "type": "technology", "confidence": 0.9}],
				"world_state": {"era": "2025", "context": "systems programming"},
				"key_topics": ["Rust vs Go for systems programming"],
				"conflict_areas": ["memory safety vs simplicity"]
			}`,
			// Second call: persona generation
			`{
				"personas": [
					{"id": "rust-advocate", "name": "Alice", "role": "Rust advocate", "goals": ["Promote Rust"], "traits": {"technical": "expert"}, "stance_per_entity": {"Rust": "pro", "Go": "con"}},
					{"id": "go-advocate", "name": "Bob", "role": "Go advocate", "goals": ["Defend simplicity"], "traits": {"practical": "high"}, "stance_per_entity": {"Go": "pro", "Rust": "con"}}
				]
			}`,
		},
	}

	registry := agent.NewRegistry(nil)
	factory := createTestFactory(registry, fakeLLM)
	engine := NewSimulationEngine(
		factory, registry, fakeLLM,
		tools.Config{WorkDir: "/tmp"},
		SimulationConfigFile{DefaultMaxWallClockMs: 2000},
		nil,
	)

	ctx := context.Background()
	simID, extraction, personas, err := engine.CreateFromSeed(ctx, "Rust is memory safe, Go is simple.", "", 2, CreateFromSeedOptions{})
	if err != nil {
		t.Fatalf("CreateFromSeed: %v", err)
	}
	if simID == "" {
		t.Error("expected non-empty simulation ID")
	}
	if extraction == nil {
		t.Fatal("expected non-nil extraction")
	}
	if len(personas) != 2 {
		t.Errorf("expected 2 personas, got %d", len(personas))
	}
	if len(extraction.Entities) != 2 {
		t.Errorf("expected 2 entities, got %d", len(extraction.Entities))
	}
	if extraction.WorldState["era"] != "2025" {
		t.Errorf("expected world_state era=2025, got %v", extraction.WorldState["era"])
	}

	// Verify simulation is created and can be started
	events, err := engine.Start(ctx, simID)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	msgCount := 0
	for ev := range events {
		if ev.Type == "agent_message" {
			msgCount++
		}
	}
	t.Logf("seed simulation messages: %d", msgCount)
}

func TestCreateFromSeedWithSuggestedAgents(t *testing.T) {
	fakeLLM := &agent.FakeLLM{
		Responses: []string{
			// First call: seed extraction with suggested agents
			`{
				"entities": [{"name": "Rust", "type": "technology", "confidence": 0.9}],
				"world_state": {"context": "systems programming"},
				"key_topics": ["Rust vs Go"],
				"conflict_areas": ["simplicity"],
				"suggested_agents": [
					{
						"name": "Alice",
						"role": "Rust advocate",
						"description": "Prefers borrow checker",
						"traits": ["smart", "opinionated"]
					},
					{
						"name": "Bob",
						"role": "Go advocate",
						"description": "Prefers simplicity",
						"traits": ["practical"]
					}
				]
			}`,
			// Second call: persona generation
			`{
				"personas": [
					{
						"id": "alice",
						"name": "Alice",
						"role": "Rust advocate",
						"goals": ["Advocate for borrow checker"],
						"traits": {"personality": "opinionated"},
						"stance_per_entity": {"Rust": "pro"}
					},
					{
						"id": "bob",
						"name": "Bob",
						"role": "Go advocate",
						"goals": ["Advocate for simplicity"],
						"traits": {"personality": "practical"},
						"stance_per_entity": {"Rust": "con"}
					}
				]
			}`,
		},
	}

	registry := agent.NewRegistry(nil)
	factory := createTestFactory(registry, fakeLLM)
	engine := NewSimulationEngine(
		factory, registry, fakeLLM,
		tools.Config{WorkDir: "/tmp"},
		SimulationConfigFile{DefaultMaxWallClockMs: 2000},
		nil,
	)

	ctx := context.Background()
	// Pass 0 (auto-detect) first to verify suggested agents are deduced
	simID, extraction, personas, err := engine.CreateFromSeed(ctx, "Rust vs Go discussion with Alice and Bob.", "", 0, CreateFromSeedOptions{})
	if err != nil {
		t.Fatalf("CreateFromSeed: %v", err)
	}
	if simID == "" {
		t.Error("expected non-empty simulation ID")
	}
	if extraction == nil {
		t.Fatal("expected non-nil extraction")
	}
	if len(extraction.SuggestedAgents) != 2 {
		t.Errorf("expected 2 suggested agents, got %d", len(extraction.SuggestedAgents))
	}
	if len(personas) != 2 {
		t.Errorf("expected 2 personas, got %d", len(personas))
	}
	if personas[0].Name != "Alice" || personas[1].Name != "Bob" {
		t.Errorf("expected Alice and Bob, got %s and %s", personas[0].Name, personas[1].Name)
	}

	// Verify simulation is created and can be started
	events, err := engine.Start(ctx, simID)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	msgCount := 0
	for ev := range events {
		if ev.Type == "agent_message" {
			msgCount++
		}
	}
	t.Logf("suggested agents simulation messages: %d", msgCount)
}

func TestSQLiteStore(t *testing.T) {
	store, err := NewSQLiteStore(t.TempDir() + "/test_sim.db")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()

	config := SimulationConfig{
		Topic:    "test",
		Personas: []Persona{{ID: "a", Name: "A"}, {ID: "b", Name: "B"}},
	}

	id, err := store.Create(config)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	state, err := store.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if state.Config.Topic != "test" {
		t.Errorf("expected topic 'test', got %q", state.Config.Topic)
	}

	// Save results
	rounds := []RoundResult{
		{RoundNumber: 1, Messages: []RoundMessage{{AgentID: "a", Content: "hello"}}, CompletedAt: time.Now()},
	}
	if err := store.SaveResults(id, rounds, "final report text"); err != nil {
		t.Fatalf("save results: %v", err)
	}

	// Re-read to verify persistence
	state2, err := store.Get(id)
	if err != nil {
		t.Fatalf("re-get: %v", err)
	}
	if state2.Report != "final report text" {
		t.Errorf("expected report persisted, got %q", state2.Report)
	}

	// List
	list := store.List()
	if len(list) != 1 {
		t.Errorf("expected 1 simulation, got %d", len(list))
	}

	if err := store.Delete(id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, err = store.Get(id)
	if err != ErrSimNotFound {
		t.Errorf("expected ErrSimNotFound, got %v", err)
	}
}

func TestAgentMemoryPersistence(t *testing.T) {
	store := NewSimulationStore()
	records := []MemoryRecord{
		{Round: 1, Role: "user", Content: "round 1 prompt"},
		{Round: 1, Role: "assistant", Content: "round 1 response"},
	}

	if err := store.SaveAgentMemories("sim1", "alice", records); err != nil {
		t.Fatalf("SaveAgentMemories: %v", err)
	}

	got, err := store.GetAgentMemories("sim1", "alice")
	if err != nil {
		t.Fatalf("GetAgentMemories: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 records, got %d", len(got))
	}
	if got[0].Content != "round 1 prompt" {
		t.Errorf("expected 'round 1 prompt', got '%s'", got[0].Content)
	}
}

func TestAgentMemoryPersistence_Empty(t *testing.T) {
	store := NewSimulationStore()
	got, err := store.GetAgentMemories("nonexistent", "alice")
	if err != ErrSimNotFound {
		t.Errorf("expected ErrSimNotFound, got %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}



func TestReplayAsk_InvalidPersona(t *testing.T) {
	engine := NewSimulationEngine(nil, nil, &agent.FakeLLM{}, tools.Config{}, SimulationConfigFile{}, nil)

	id, err := engine.Create(SimulationConfig{
		Topic:    "test",
		Personas: []Persona{{ID: "alice", Name: "Alice"}, {ID: "bob", Name: "Bob"}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err = engine.ReplayAsk(context.Background(), id, "nonexistent", "question")
	if err == nil {
		t.Fatal("expected error for invalid persona")
	}
}

func TestReplayAsk_NotCompleted(t *testing.T) {
	engine := NewSimulationEngine(nil, nil, &agent.FakeLLM{}, tools.Config{}, SimulationConfigFile{}, nil)

	_, err := engine.ReplayAsk(context.Background(), "nonexistent", "alice", "question")
	if err == nil {
		t.Fatal("expected error for nonexistent sim")
	}
}

func TestBuildReplayPrompt(t *testing.T) {
	persona := &Persona{Name: "Alice", Role: "Rust advocate"}
	records := []MemoryRecord{
		{Round: 1, Role: "user", Content: "Round 1: discuss Rust vs Go"},
		{Round: 1, Role: "assistant", Content: "Rust is memory safe."},
		{Round: 2, Role: "user", Content: "Round 2: Go is simpler"},
		{Round: 2, Role: "assistant", Content: "But safety matters more."},
	}

	prompt := BuildReplayPrompt(persona, "Rust vs Go", records, "What do you think now?", "en")
	if !strings.Contains(prompt, "Alice") {
		t.Error("expected prompt to contain persona name")
	}
	if !strings.Contains(prompt, "Rust advocate") {
		t.Error("expected prompt to contain role")
	}
	if !strings.Contains(prompt, "Rust vs Go") {
		t.Error("expected prompt to contain topic")
	}
	if !strings.Contains(prompt, "What do you think now?") {
		t.Error("expected prompt to contain question")
	}
	if !strings.Contains(prompt, "Rust is memory safe") {
		t.Error("expected prompt to contain memory")
	}
}

func TestTruncateFunctions(t *testing.T) {
	if s := truncateStr("hello world", 5); s != "hello..." {
		t.Errorf("expected 'hello...', got %q", s)
	}
	if s := truncateStr("short", 10); s != "short" {
		t.Errorf("expected 'short', got %q", s)
	}
	if s := truncateStr("long content here", 10); s != "long conte..." {
		t.Errorf("expected 'long conte...', got %q", s)
	}
}

// ─── Phase 3: Graph & Notification Tests ──────────────────────────────────────

func TestRelationGraph(t *testing.T) {
	g := NewRelationGraph()

	g.AddEdge("alice", "bob", RelMention, 1, "hey @bob")
	g.AddEdge("alice", "bob", RelMention, 2, "@bob again")
	g.AddEdge("bob", "alice", RelRebuttal, 2, "I disagree @alice")
	g.AddEdge("charlie", "alice", RelAgree, 3, "I agree with @alice")

	if g.NodeCount() != 3 {
		t.Errorf("expected 3 nodes, got %d", g.NodeCount())
	}

	edges := g.Edges()
	// 3 unique edges: alice->bob:mention (weight 2 after merging), bob->alice:rebuttal, charlie->alice:agree
	if len(edges) != 3 {
		t.Errorf("expected 3 unique edges, got %d", len(edges))
	}

	// alice->bob mention should have weight 2
	aliceEdges := g.EdgesFrom("alice")
	if len(aliceEdges) < 1 {
		t.Fatal("expected edges from alice")
	}
	found := false
	for _, e := range aliceEdges {
		if e.Target == "bob" && e.Type == RelMention && e.Weight == 2 {
			found = true
		}
	}
	if !found {
		t.Error("expected alice->bob mention edge with weight 2")
	}

	// Top edges
	top := g.TopEdges(2)
	if len(top) != 2 {
		t.Errorf("expected 2 top edges, got %d", len(top))
	}
}

func TestRelationGraphFormat(t *testing.T) {
	g := NewRelationGraph()
	g.AddEdge("alice", "bob", RelRebuttal, 1, "I disagree with Bob")

	report := g.FormatForReport()
	if !strings.Contains(report, "alice") {
		t.Error("should contain source agent")
	}
	if !strings.Contains(report, "bob") {
		t.Error("should contain target agent")
	}
	if !strings.Contains(report, "rebuttal") {
		t.Error("should contain relation type")
	}
}

func TestGraphEmpty(t *testing.T) {
	g := NewRelationGraph()
	if g.NodeCount() != 0 {
		t.Error("empty graph should have 0 nodes")
	}
	report := g.FormatForReport()
	if !strings.Contains(report, "no interactions") {
		t.Error("empty graph should indicate no interactions")
	}
}

func TestMessageBusNotification(t *testing.T) {
	bus := NewMessageBus(16)
	bus.Register("alice")
	bus.Register("bob")

	notifyCh := bus.NotifyCh("alice")
	if notifyCh == nil {
		t.Fatal("expected notify channel")
	}

	// Send should trigger notification
	bus.Send("alice", Message{From: "bob", Content: "hello", Round: 1, Type: "statement"})

	select {
	case <-notifyCh:
		// got notification ✓
	case <-time.After(time.Second):
		t.Error("expected notification within 1 second")
	}

	// Drain should clear accumulated notifications
	msgs := bus.DrainAll("alice")
	if len(msgs) != 1 {
		t.Errorf("expected 1 message, got %d", len(msgs))
	}

	// Notification channel should be empty after drain
	select {
	case <-notifyCh:
		t.Error("notification channel should be empty after drain")
	default:
	}
}

func TestMessageBusBroadcastNotify(t *testing.T) {
	bus := NewMessageBus(16)
	bus.Register("alice")
	bus.Register("bob")
	bus.Register("charlie")

	bus.Broadcast("alice", Message{From: "alice", Content: "hello", Round: 1, Type: "statement"})

	// Bob and Charlie should get notifications; Alice should not
	for _, id := range []string{"bob", "charlie"} {
		select {
		case <-bus.NotifyCh(id):
		case <-time.After(time.Second):
			t.Errorf("%s should have received notification", id)
		}
	}

	// Alice (sender) should not get notification
	select {
	case <-bus.NotifyCh("alice"):
		t.Error("sender should not receive notification")
	default:
	}
}





func createTestFactory(registry *agent.Registry, llm agent.LLMClient) *agent.DefaultFactory {
	return agent.NewDefaultFactory(registry, llm, tools.Config{WorkDir: "/tmp"}, nil)
}

func TestFork_Basic(t *testing.T) {
	engine := NewSimulationEngine(nil, nil, &agent.FakeLLM{}, tools.Config{}, SimulationConfigFile{}, nil)

	srcID, err := engine.Create(SimulationConfig{
		Topic:    "Rust vs Go",
		Personas: []Persona{{ID: "alice", Name: "Alice"}, {ID: "bob", Name: "Bob"}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Mark source as completed (since we don't actually run it)
	state, err := engine.Get(srcID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	state.Lock()
	state.Status = StatusCompleted
	state.Unlock()

	newID, err := engine.Fork(context.Background(), srcID, ForkRequest{
		NewTopic:      "Rust vs Go v2",
		NewMaxWallClockMs: 20,
		NewWorldState: map[string]any{"era": "2026"},
	})
	if err != nil {
		t.Fatalf("Fork: %v", err)
	}
	if newID == "" || newID == srcID {
		t.Errorf("expected new ID different from source, got %q", newID)
	}

	newState, err := engine.Get(newID)
	if err != nil {
		t.Fatalf("get forked: %v", err)
	}
	if newState.Config.Topic != "Rust vs Go v2" {
		t.Errorf("expected topic 'Rust vs Go v2', got %q", newState.Config.Topic)
	}
	if newState.Config.MaxWallClockMs != 20 {
		t.Errorf("expected max_wall_clock_ms 20, got %d", newState.Config.MaxWallClockMs)
	}
	if newState.Config.WorldState["era"] != "2026" {
		t.Errorf("expected world_state.era='2026', got %v", newState.Config.WorldState["era"])
	}
	if len(newState.Config.Personas) != 2 {
		t.Errorf("expected 2 personas, got %d", len(newState.Config.Personas))
	}
}

func TestFork_WithExtraPersonas(t *testing.T) {
	engine := NewSimulationEngine(nil, nil, &agent.FakeLLM{}, tools.Config{}, SimulationConfigFile{}, nil)

	srcID, err := engine.Create(SimulationConfig{
		Topic:    "Rust vs Go",
		Personas: []Persona{{ID: "alice", Name: "Alice"}, {ID: "bob", Name: "Bob"}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	state, err := engine.Get(srcID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	state.Lock()
	state.Status = StatusCompleted
	state.Unlock()

	newID, err := engine.Fork(context.Background(), srcID, ForkRequest{
		ExtraPersonas: []Persona{{ID: "charlie", Name: "Charlie", Role: "mediator"}},
	})
	if err != nil {
		t.Fatalf("Fork: %v", err)
	}

	newState, err := engine.Get(newID)
	if err != nil {
		t.Fatalf("get forked: %v", err)
	}
	if len(newState.Config.Personas) != 3 {
		t.Errorf("expected 3 personas (2 original + 1 extra), got %d", len(newState.Config.Personas))
	}
}

func TestFork_DuplicatePersonaID(t *testing.T) {
	engine := NewSimulationEngine(nil, nil, &agent.FakeLLM{}, tools.Config{}, SimulationConfigFile{}, nil)

	srcID, err := engine.Create(SimulationConfig{
		Topic:    "test",
		Personas: []Persona{{ID: "a", Name: "A"}, {ID: "b", Name: "B"}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	state, _ := engine.Get(srcID)
	state.Lock()
	state.Status = StatusCompleted
	state.Unlock()

	_, err = engine.Fork(context.Background(), srcID, ForkRequest{
		ExtraPersonas: []Persona{{ID: "a", Name: "Duplicate"}},
	})
	if err == nil {
		t.Fatal("expected error for duplicate persona ID")
	}
}

func TestFork_NotFinished(t *testing.T) {
	engine := NewSimulationEngine(nil, nil, &agent.FakeLLM{}, tools.Config{}, SimulationConfigFile{}, nil)

	srcID, err := engine.Create(SimulationConfig{
		Topic:    "test",
		Personas: []Persona{{ID: "a", Name: "A"}, {ID: "b", Name: "B"}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err = engine.Fork(context.Background(), srcID, ForkRequest{})
	if err == nil {
		t.Fatal("expected error for unfinished simulation (status=pending)")
	}
}

func TestFork_Nonexistent(t *testing.T) {
	engine := NewSimulationEngine(nil, nil, &agent.FakeLLM{}, tools.Config{}, SimulationConfigFile{}, nil)
	_, err := engine.Fork(context.Background(), "nonexistent", ForkRequest{})
	if err == nil {
		t.Fatal("expected error for nonexistent sim")
	}
}

func TestFork_NotFound(t *testing.T) {
	engine := NewSimulationEngine(nil, nil, &agent.FakeLLM{}, tools.Config{}, SimulationConfigFile{}, nil)
	_, err := engine.Fork(context.Background(), "nonexistent", ForkRequest{})
	if err == nil {
		t.Fatal("expected error for nonexistent sim")
	}
}

func TestFork_InheritsOriginalTopic(t *testing.T) {
	engine := NewSimulationEngine(nil, nil, &agent.FakeLLM{}, tools.Config{}, SimulationConfigFile{}, nil)

	srcID, err := engine.Create(SimulationConfig{
		Topic:    "original topic",
		Personas: []Persona{{ID: "a", Name: "A"}, {ID: "b", Name: "B"}},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	state, _ := engine.Get(srcID)
	state.Lock()
	state.Status = StatusCompleted
	state.Unlock()

	// Fork with no topic override
	newID, err := engine.Fork(context.Background(), srcID, ForkRequest{})
	if err != nil {
		t.Fatalf("Fork: %v", err)
	}
	newState, err := engine.Get(newID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if newState.Config.Topic != "original topic" {
		t.Errorf("expected topic 'original topic', got %q", newState.Config.Topic)
	}
}

func TestReplayAsk_ReportAgent(t *testing.T) {
	var capturedPrompt string
	fakeLLM := &agent.FakeLLM{
		Responses: []string{
			"The analyst response regarding the report.",
		},
		Hook: func(req agent.LLMRequest) {
			if len(req.Messages) > 0 {
				capturedPrompt = req.Messages[0].Content
			}
		},
	}
	engine := NewSimulationEngine(nil, nil, fakeLLM, tools.Config{}, SimulationConfigFile{DefaultModelID: "test-model"}, nil)

	// Inject a completed simulation state with a report
	simID := "test-sim-id"
	_, err := engine.store.Create(SimulationConfig{
		ID:       simID,
		Topic:    "Topic A",
		Personas: []Persona{{ID: "alice", Name: "Alice"}, {ID: "bob", Name: "Bob"}},
		Language: "en",
	})
	if err != nil {
		t.Fatalf("failed to create sim state: %v", err)
	}
	state, _ := engine.store.Get(simID)
	state.Status = StatusCompleted
	state.Report = "This is the final summary report of Topic A."

	// Query replay for report
	answer, err := engine.ReplayAsk(context.Background(), simID, "report", "What is the summary?")
	if err != nil {
		t.Fatalf("ReplayAsk report: %v", err)
	}
	if answer != "The analyst response regarding the report." {
		t.Errorf("expected analyst response, got: %s", answer)
	}

	// Also verify prompt structure
	if capturedPrompt == "" {
		t.Fatal("expected LLM chat to trigger Hook")
	}
	if !strings.Contains(capturedPrompt, "Simulation Topic: Topic A") {
		t.Errorf("expected prompt to contain topic, got: %s", capturedPrompt)
	}
	if !strings.Contains(capturedPrompt, "This is the final summary report of Topic A.") {
		t.Errorf("expected prompt to contain report, got: %s", capturedPrompt)
	}
}

func TestBuildSimulationSystemPrompt_Moderator(t *testing.T) {
	persona := Persona{
		ID:     "host",
		Name:   "Host",
		Role:   "mediator/moderator",
		Traits: map[string]string{"role_type": "mediator"},
	}
	prompt := BuildSimulationSystemPrompt(persona, "Rust vs Go", nil)
	if !strings.Contains(prompt, "You are the Moderator/Host") {
		t.Error("expected moderator rules in prompt")
	}
	if !strings.Contains(prompt, "Do not take a strong personal stance") {
		t.Error("expected prompt to instruct moderator not to take a stance")
	}

	// Normal agent should not have it
	normal := Persona{ID: "alice", Name: "Alice", Role: "Developer"}
	normalPrompt := BuildSimulationSystemPrompt(normal, "Rust vs Go", nil)
	if strings.Contains(normalPrompt, "You are the Moderator/Host") {
		t.Error("unexpected moderator rules in normal prompt")
	}
}

func TestIndexSimulationToKG_Namespacing(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_entries.db")
	db, err := sqlitedb.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite db: %v", err)
	}
	defer db.Close()

	// Build a memory engine with nil embedder and nil vecstore (provider "none")
	memEngine := memoryengine.New(db.DB, &db.WMu, nil, nil, nil)

	engine := NewSimulationEngine(nil, nil, nil, tools.Config{}, SimulationConfigFile{}, nil)
	engine.SetMemoryEngine(memEngine)

	simID := "sim-uuid-1234"
	simAgents := []*SimAgent{
		{
			personaID: "alice",
			persona: &Persona{
				ID:   "alice",
				Name: "Alice",
				Role: "Developer",
				Traits: map[string]string{
					"stance:Go": "pro",
				},
			},
		},
		{
			personaID: "bob",
			persona: &Persona{
				ID:   "bob",
				Name: "Bob",
				Role: "Manager",
				Traits: map[string]string{
					"stance:Go": "con",
				},
			},
		},
	}

	graph := NewRelationGraph()
	graph.AddEdge("alice", "bob", RelMention, 1, "Hey @bob")

	ws := NewWorldState(nil)
	ws.Set("Go", "good", "alice", 1)

	ctx := context.Background()
	engine.indexSimulationToKG(ctx, simID, "Go language debate", simAgents, graph, ws, "This is a summary report.")

	// Query database directly to verify namespacing
	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM kg_nodes WHERE name = ?`, "sim_sim-uuid-1234_alice").Scan(&count)
	if err != nil || count != 1 {
		t.Errorf("expected namespaced node for alice, got count: %d, err: %v", count, err)
	}

	err = db.QueryRow(`SELECT COUNT(*) FROM kg_nodes WHERE name = ?`, "sim_sim-uuid-1234_bob").Scan(&count)
	if err != nil || count != 1 {
		t.Errorf("expected namespaced node for bob, got count: %d, err: %v", count, err)
	}

	// Topic "Go" should not be prefixed
	err = db.QueryRow(`SELECT COUNT(*) FROM kg_nodes WHERE name = ?`, "Go").Scan(&count)
	if err != nil || count != 1 {
		t.Errorf("expected global node for Go, got count: %d, err: %v", count, err)
	}

	// Relations should also use namespaced nodes
	var source, target, relType string
	err = db.QueryRow(`
		SELECT s.name, t.name, e.rel_type
		FROM kg_edges e
		JOIN kg_nodes s ON e.source = s.id
		JOIN kg_nodes t ON e.target = t.id
		WHERE e.rel_type = ?`, "mention").Scan(&source, &target, &relType)
	if err != nil {
		t.Errorf("failed to query mention relation: %v", err)
	}
	if source != "sim_sim-uuid-1234_alice" || target != "sim_sim-uuid-1234_bob" {
		t.Errorf("expected namespaced relation, got source=%q, target=%q", source, target)
	}
}
