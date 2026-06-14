package simulation

import (
	"strings"
	"testing"
	"time"
)

func TestEnvironment_AddZone(t *testing.T) {
	cfg := DefaultClockConfig()
	clock := NewSimClock(cfg)
	env := NewEnvironment(clock)

	env.AddZone("cafe", "A cozy cafe", 10)
	env.AddZone("library", "A quiet library", 30)

	zoneNames := env.ZoneNames()
	if len(zoneNames) != 2 {
		t.Fatalf("expected 2 zones, got %d", len(zoneNames))
	}

	foundCafe := false
	for _, z := range zoneNames {
		if z == "cafe" {
			foundCafe = true
			break
		}
	}
	if !foundCafe {
		t.Error("cafe zone should exist")
	}
}

func TestEnvironment_PlaceAndMoveAgent(t *testing.T) {
	cfg := DefaultClockConfig()
	clock := NewSimClock(cfg)
	env := NewEnvironment(clock)

	env.AddZone("cafe", "A cozy cafe", 2)
	env.AddZone("library", "A quiet library", 2)

	env.PlaceAgent("alice", "cafe")
	if env.GetAgentZone("alice") != "cafe" {
		t.Errorf("expected alice in cafe, got %s", env.GetAgentZone("alice"))
	}

	agents := env.GetAgentsInZone("cafe")
	if len(agents) != 1 || agents[0] != "alice" {
		t.Errorf("expected [alice] in cafe, got %v", agents)
	}

	// Move alice to library
	_, err := env.MoveAgent("alice", "library")
	if err != nil {
		t.Fatalf("MoveAgent: %v", err)
	}
	if env.GetAgentZone("alice") != "library" {
		t.Errorf("expected alice in library, got %s", env.GetAgentZone("alice"))
	}

	// Cafe should be empty now
	agents = env.GetAgentsInZone("cafe")
	if len(agents) != 0 {
		t.Errorf("expected empty cafe, got %v", agents)
	}
}

func TestEnvironment_CapacityLimit(t *testing.T) {
	cfg := DefaultClockConfig()
	clock := NewSimClock(cfg)
	env := NewEnvironment(clock)

	env.AddZone("cafe", "A cozy cafe", 1)

	env.PlaceAgent("alice", "cafe")
	_, err := env.MoveAgent("bob", "cafe")
	if err == nil {
		t.Error("expected capacity error when moving bob to full cafe")
	}
}

func TestEnvironment_AddAndInteractWithObject(t *testing.T) {
	cfg := DefaultClockConfig()
	clock := NewSimClock(cfg)
	env := NewEnvironment(clock)

	env.AddZone("cafe", "A cozy cafe", 10)
	env.AddObject("cafe", &EnvObject{
		ID:            "menu",
		Name:          "Menu",
		Description:   "A coffee menu",
		IsInteractive: true,
		State:         map[string]any{"specials": "latte"},
	})

	// Agent must be placed in the zone before interacting
	env.PlaceAgent("alice", "cafe")

	result, err := env.Interact("alice", "menu", "read")
	if err != nil {
		t.Fatalf("Interact: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty interaction result")
	}
}

func TestEnvironment_InteractNonexistentObject(t *testing.T) {
	cfg := DefaultClockConfig()
	clock := NewSimClock(cfg)
	env := NewEnvironment(clock)

	_, err := env.Interact("alice", "nonexistent", "read")
	if err == nil {
		t.Error("expected error for nonexistent object")
	}
}

func TestEnvironment_GetObservations(t *testing.T) {
	cfg := DefaultClockConfig()
	clock := NewSimClock(cfg)
	env := NewEnvironment(clock)

	env.AddZone("cafe", "A cozy cafe", 10)
	env.AddZone("library", "A quiet library", 30)
	env.AddObject("cafe", &EnvObject{
		ID:          "menu",
		Name:        "Menu",
		Description: "A coffee menu",
	})

	env.PlaceAgent("alice", "cafe")
	env.PlaceAgent("bob", "cafe")
	env.PlaceAgent("charlie", "library")

	obs := env.GetObservations("alice", "Alice")
	if len(obs) == 0 {
		t.Fatal("expected observations for alice")
	}

	// Should see bob in zone
	hasBob := false
	for _, o := range obs {
		if strings.Contains(o.Content, "bob") || strings.Contains(o.Content, "Bob") {
			hasBob = true
		}
	}
	if !hasBob {
		t.Error("alice should observe bob in the same zone")
	}
}

func TestEnvironment_FormatForPrompt(t *testing.T) {
	cfg := DefaultClockConfig()
	clock := NewSimClock(cfg)
	env := NewEnvironment(clock)

	env.AddZone("cafe", "A cozy cafe", 10)
	prompt := env.FormatForPrompt()

	if !strings.Contains(prompt, "cafe") {
		t.Error("prompt should contain zone name")
	}
}

func TestEnvironment_Concurrency(t *testing.T) {
	cfg := DefaultClockConfig()
	clock := NewSimClock(cfg)
	env := NewEnvironment(clock)

	env.AddZone("cafe", "A cozy cafe", 100)

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func(n int) {
			id := string(rune('a' + n))
			env.PlaceAgent(id, "cafe")
			env.GetAgentZone(id)
			env.GetAgentsInZone("cafe")
		}(i)
	}
	_ = done
	time.Sleep(100 * time.Millisecond)
}
