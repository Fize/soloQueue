package simulation

import (
	"strings"
	"testing"
	"time"
)

func TestParseActions_Spawn(t *testing.T) {
	// [SPAWN name]: description
	actions, _ := ParseActions("[SPAWN Charlie]: A policy advisor who joins to provide regulatory perspective.")
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Type != ActionSpawn {
		t.Errorf("expected spawn action, got %s", actions[0].Type)
	}
	if actions[0].Target != "Charlie" {
		t.Errorf("expected target 'Charlie', got %q", actions[0].Target)
	}
	if !strings.Contains(actions[0].Content, "policy advisor") {
		t.Errorf("expected description in content, got: %s", actions[0].Content)
	}
}

func TestParseActions_SpawnNoDescription(t *testing.T) {
	actions, _ := ParseActions("[SPAWN Charlie]")
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Type != ActionSpawn {
		t.Errorf("expected spawn action, got %s", actions[0].Type)
	}
	if actions[0].Target != "Charlie" {
		t.Errorf("expected target 'Charlie', got %q", actions[0].Target)
	}
}

func TestParseActions_Die(t *testing.T) {
	actions, _ := ParseActions("[DIE]")
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Type != ActionDie {
		t.Errorf("expected die action, got %s", actions[0].Type)
	}
}

func TestParseActions_Exit(t *testing.T) {
	actions, _ := ParseActions("[EXIT]")
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Type != ActionDie {
		t.Errorf("expected die action for [EXIT], got %s", actions[0].Type)
	}
}

func TestParseActions_MixedWithLifecycle(t *testing.T) {
	content := "[SAY]: I think we need fresh eyes on this.\n[SPAWN Mediator]: A neutral third party to help us reach consensus.\n[PASS]"
	actions, _ := ParseActions(content)

	if len(actions) != 3 {
		t.Fatalf("expected 3 actions, got %d", len(actions))
	}
	if actions[0].Type != ActionSpeak {
		t.Errorf("expected speak, got %s", actions[0].Type)
	}
	if actions[1].Type != ActionSpawn {
		t.Errorf("expected spawn, got %s", actions[1].Type)
	}
	if actions[2].Type != ActionPass {
		t.Errorf("expected pass, got %s", actions[2].Type)
	}
}

func TestAction_String_SpawnDie(t *testing.T) {
	spawn := Action{Type: ActionSpawn, Target: "Charlie", Content: "Policy advisor"}
	if s := spawn.String(); !strings.HasPrefix(s, "[SPAWN") {
		t.Errorf("expected [SPAWN] prefix, got: %s", s)
	}

	die := Action{Type: ActionDie}
	if die.String() != "[DIE]" {
		t.Errorf("expected [DIE], got: %s", die.String())
	}
}

func TestEnvironment_RemoveAgent(t *testing.T) {
	cfg := DefaultClockConfig()
	clock := NewSimClock(cfg)
	env := NewEnvironment(clock)

	env.AddZone("cafe", "A cozy cafe", 10)
	env.PlaceAgent("alice", "cafe")
	env.PlaceAgent("bob", "cafe")

	// Verify both are in cafe
	agents := env.GetAgentsInZone("cafe")
	if len(agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(agents))
	}

	// Remove alice
	env.RemoveAgent("alice")

	// Verify alice is gone
	agents = env.GetAgentsInZone("cafe")
	if len(agents) != 1 {
		t.Errorf("expected 1 agent after removal, got %d", len(agents))
	}
	if env.GetAgentZone("alice") != "" {
		t.Errorf("alice should have no zone after removal")
	}

	// Bob should still be there
	if env.GetAgentZone("bob") != "cafe" {
		t.Errorf("bob should still be in cafe")
	}
}

func TestEnvironment_RemoveAgent_NotExists(t *testing.T) {
	cfg := DefaultClockConfig()
	clock := NewSimClock(cfg)
	env := NewEnvironment(clock)

	// Should not panic
	env.RemoveAgent("nonexistent")
}

func TestGraph_RemoveNode(t *testing.T) {
	g := NewRelationGraph()
	g.AddNode("alice")
	g.AddNode("bob")
	g.AddNode("charlie")
	g.AddEdge("alice", "bob", RelMention, 1, "hello")
	g.AddEdge("bob", "alice", RelRebuttal, 1, "disagree")

	if g.NodeCount() != 3 {
		t.Fatalf("expected 3 nodes, got %d", g.NodeCount())
	}

	g.RemoveNode("alice")

	if g.NodeCount() != 2 {
		t.Errorf("expected 2 nodes after removal, got %d", g.NodeCount())
	}

	// All edges involving alice should be gone
	for _, e := range g.Edges() {
		if e.Source == "alice" || e.Target == "alice" {
			t.Errorf("found edge still referencing alice: %s -> %s", e.Source, e.Target)
		}
	}
}

func TestGraph_RemoveNode_NotExists(t *testing.T) {
	g := NewRelationGraph()
	g.AddNode("alice")
	// Should not panic
	g.RemoveNode("nonexistent")
	if g.NodeCount() != 1 {
		t.Errorf("expected 1 node, got %d", g.NodeCount())
	}
}

func TestRelationshipManager_RemoveSubject(t *testing.T) {
	rm := NewRelationshipManager()
	rm.Set("alice", "bob", 0.5, 0.3, []string{"friend"})
	rm.Set("bob", "alice", 0.7, 0.4, []string{"colleague"})
	rm.Set("alice", "charlie", 0.2, 0.0, nil)

	// Verify relationships exist
	if rm.Get("alice", "bob") == nil {
		t.Fatal("expected alice->bob relationship")
	}

	rm.RemoveSubject("alice")

	// All relationships involving alice should be gone
	if rm.Get("alice", "bob") != nil {
		t.Error("alice->bob should be removed")
	}
	if rm.Get("bob", "alice") != nil {
		t.Error("bob->alice should be removed")
	}
	if rm.Get("alice", "charlie") != nil {
		t.Error("alice->charlie should be removed")
	}
}

func TestSanitizeSpawnID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Alice", "alice"},
		{"Bob Smith", "bob_smith"},
		{"Dr. Jane Doe", "dr__jane_doe"},
		{"VeryLongNameThatExceeds", "verylongnamethatexce"},
		{"with-dashes_and_underscores", "with-dashes_and_unde"}, // truncated to 20 chars
	}

	for _, tt := range tests {
		got := sanitizeSpawnID(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeSpawnID(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestLifecycleManager_CheckSimTimeTrigger(t *testing.T) {
	cfg := DefaultClockConfig()
	clock := NewSimClock(cfg)

	lm := &lifecycleManager{clock: clock}

	// Test duration trigger
	if !lm.checkSimTimeTrigger("0h") {
		t.Error("0h should trigger immediately (elapsed >= 0)")
	}
	if lm.checkSimTimeTrigger("100h") {
		t.Error("100h should not trigger at start (elapsed < 100h)")
	}

	// Test clock time trigger (start is 7:00)
	// 06:00 has already passed → should trigger (current time 07:00 >= trigger 06:00)
	if !lm.checkSimTimeTrigger("06:00") {
		t.Error("06:00 should trigger (current 07:00 >= trigger 06:00)")
	}
	// 07:00 is now → should trigger
	if !lm.checkSimTimeTrigger("07:00") {
		t.Error("07:00 should trigger (current time equals trigger)")
	}
}

func TestLifecycleManager_CheckWallTimeTrigger(t *testing.T) {
	lm := &lifecycleManager{}
	simStart := time.Now().Add(-10 * time.Second)

	if !lm.checkWallTimeTrigger("5s", simStart) {
		t.Error("5s should trigger after 10s elapsed")
	}
	if lm.checkWallTimeTrigger("60s", simStart) {
		t.Error("60s should not trigger after only 10s")
	}
	if lm.checkWallTimeTrigger("invalid", simStart) {
		t.Error("invalid duration should not trigger")
	}
}

func TestLifecycleManager_CheckConditionTrigger(t *testing.T) {
	ws := NewWorldState(map[string]any{"temperature": "hot"})
	lm := &lifecycleManager{worldState: ws}

	// Simple world state key check
	if !lm.checkConditionTrigger("temperature") {
		t.Error("temperature key exists, should trigger")
	}
	if lm.checkConditionTrigger("nonexistent") {
		t.Error("nonexistent key should not trigger")
	}
}

func TestSeedLifecycleEvent_Fields(t *testing.T) {
	ev := SeedLifecycleEvent{
		Type:         "agent_death",
		AgentName:    "Alice",
		Trigger:      "sim_time",
		TriggerValue: "3h",
		Reason:       "Alice must leave for a flight",
	}

	if ev.Type != "agent_death" {
		t.Errorf("expected agent_death, got %s", ev.Type)
	}
	if ev.Triggered {
		t.Error("event should not be triggered by default")
	}

	// Mark as triggered
	ev.Triggered = true
	if !ev.Triggered {
		t.Error("event should be triggered after setting")
	}
}

func TestSpawnInfo_Fields(t *testing.T) {
	info := SpawnInfo{
		Name:        "Charlie",
		Description: "A policy expert",
		RequestedBy: "alice",
	}

	if info.Name != "Charlie" {
		t.Errorf("expected Charlie, got %s", info.Name)
	}
	if info.RequestedBy != "alice" {
		t.Errorf("expected alice, got %s", info.RequestedBy)
	}
}
