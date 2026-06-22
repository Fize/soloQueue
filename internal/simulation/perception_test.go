package simulation

import (
	"strings"
	"testing"
	"time"
)

func TestPerceptionSystem_CollectObservations(t *testing.T) {
	cfg := DefaultClockConfig()
	clock := NewSimClock(cfg)
	env := NewEnvironment(clock)
	bus := NewMessageBus(16)

	env.AddZone("cafe", "A cozy cafe", 10)
	env.PlaceAgent("alice", "cafe")
	bus.Register("alice")

	ps := NewPerceptionSystem(env, bus, clock)
	obs := ps.CollectObservations("alice", "Alice")

	if len(obs) == 0 {
		t.Fatal("expected at least time observation")
	}

	// Should have a time_event observation
	hasTime := false
	for _, o := range obs {
		if o.Type == "time_event" {
			hasTime = true
		}
	}
	if !hasTime {
		t.Error("expected time_event observation")
	}
}

func TestPerceptionSystem_DrainsMessages(t *testing.T) {
	cfg := DefaultClockConfig()
	clock := NewSimClock(cfg)
	env := NewEnvironment(clock)
	bus := NewMessageBus(16)

	env.AddZone("cafe", "A cozy cafe", 10)
	env.PlaceAgent("alice", "cafe")
	bus.Register("alice")
	bus.Register("bob")

	// Send a message to alice
	bus.Send("alice", Message{
		From:    "bob",
		To:      "alice",
		Content: "Hello alice!",
		Type:    "speak",
		Round:   1,
	})

	ps := NewPerceptionSystem(env, bus, clock)
	obs := ps.CollectObservations("alice", "Alice")

	// Should find the message observation
	hasMessage := false
	for _, o := range obs {
		if o.Type == "agent_speak" && strings.Contains(o.Content, "Hello alice") {
			hasMessage = true
		}
	}
	if !hasMessage {
		t.Error("expected observation with bob's message")
	}

	// Second collection should not have the message (already drained)
	obs2 := ps.CollectObservations("alice", "Alice")
	for _, o := range obs2 {
		if o.Type == "agent_speak" {
			t.Error("message should have been drained")
		}
	}
}

func TestPerceptionSystem_DialogueRequest(t *testing.T) {
	cfg := DefaultClockConfig()
	clock := NewSimClock(cfg)
	env := NewEnvironment(clock)
	bus := NewMessageBus(16)

	env.AddZone("cafe", "A cozy cafe", 10)
	env.PlaceAgent("alice", "cafe")
	bus.Register("alice")
	bus.Register("bob")

	bus.Send("alice", Message{
		From:    "bob",
		To:      "alice",
		Content: "bob wants to speak with you privately. Say [SAY @bob]: ... to respond, or [PASS] to decline.",
		Type:    "dialogue_request",
	})

	ps := NewPerceptionSystem(env, bus, clock)
	obs := ps.CollectObservations("alice", "Alice")

	hasDialogue := false
	for _, o := range obs {
		if o.Type == "dialogue_request" {
			hasDialogue = true
			// Should NOT double-prefix with sender name
			if strings.HasPrefix(o.Content, "bob: bob") {
				t.Error("dialogue_request should not double-prefix sender name")
			}
		}
	}
	if !hasDialogue {
		t.Error("expected dialogue_request observation")
	}
}

func TestPerceptionSystem_DialogueResponse(t *testing.T) {
	cfg := DefaultClockConfig()
	clock := NewSimClock(cfg)
	env := NewEnvironment(clock)
	bus := NewMessageBus(16)

	env.AddZone("cafe", "A cozy cafe", 10)
	env.PlaceAgent("alice", "cafe")
	bus.Register("alice")

	bus.Send("alice", Message{
		From:    "bob",
		To:      "alice",
		Content: "I agree with your point.",
		Type:    "dialogue_response",
	})

	ps := NewPerceptionSystem(env, bus, clock)
	obs := ps.CollectObservations("alice", "Alice")

	hasResponse := false
	for _, o := range obs {
		if o.Type == "dialogue_response" {
			hasResponse = true
			if !strings.Contains(o.Content, "privately") {
				t.Error("dialogue_response should be marked as private")
			}
		}
	}
	if !hasResponse {
		t.Error("expected dialogue_response observation")
	}
}

func TestFormatObservations(t *testing.T) {
	obs := []Observation{
		{Type: "time_event", Content: "Current time: 12:00 (Day 1)."},
		{Type: "environment", Content: "You are in the cafe."},
		{Type: "agent_present", Content: "Bob is here."},
		{Type: "agent_speak", Content: "bob: Hello everyone!"},
	}

	result := FormatObservations(obs, "en")

	if !strings.Contains(result, "Time") {
		t.Error("should contain Time section")
	}
	if !strings.Contains(result, "Environment") {
		t.Error("should contain Environment section")
	}
	if !strings.Contains(result, "Messages") {
		t.Error("should contain Messages section")
	}
}

func TestFormatObservationsEmpty(t *testing.T) {
	result := FormatObservations(nil, "en")
	if !strings.Contains(result, "no new observations") {
		t.Error("should indicate no observations")
	}
}

func TestObservationToMemory(t *testing.T) {
	obs := Observation{
		Type:       "agent_speak",
		Content:    "Bob: hello",
		Source:     "bob",
		Importance: 5.0,
		At:         time.Now(),
	}

	rec := ObservationToMemory(obs, "alice")

	if rec.Role != "observation" {
		t.Errorf("expected role 'observation', got %q", rec.Role)
	}
	if rec.RecordType != "agent_speak" {
		t.Errorf("expected record_type 'agent_speak', got %q", rec.RecordType)
	}
	if rec.Source != "bob" {
		t.Errorf("expected source 'bob', got %q", rec.Source)
	}
}

func TestFormatMessageObservation(t *testing.T) {
	tests := []struct {
		name     string
		msg      Message
		wantType string
		checkFn  func(t *testing.T, content string)
	}{
		{
			name:     "system message",
			msg:      Message{From: "system", Content: "Simulation started.", Type: "system"},
			wantType: "system",
			checkFn: func(t *testing.T, content string) {
				if !strings.HasPrefix(content, "[System]") {
					t.Errorf("system message should start with [System], got: %s", content)
				}
			},
		},
		{
			name:     "dialogue request",
			msg:      Message{From: "bob", Content: "bob wants to speak with you privately.", Type: "dialogue_request"},
			wantType: "dialogue_request",
			checkFn: func(t *testing.T, content string) {
				if strings.HasPrefix(content, "bob: bob") {
					t.Error("dialogue_request should not double-prefix")
				}
			},
		},
		{
			name:     "dialogue response",
			msg:      Message{From: "bob", Content: "I agree.", Type: "dialogue_response"},
			wantType: "dialogue_response",
			checkFn: func(t *testing.T, content string) {
				if !strings.Contains(content, "privately") {
					t.Error("dialogue_response should indicate privacy")
				}
			},
		},
		{
			name:     "regular speak",
			msg:      Message{From: "bob", Content: "Hello!", Type: "speak"},
			wantType: "agent_speak",
			checkFn: func(t *testing.T, content string) {
				if !strings.HasPrefix(content, "bob:") {
					t.Errorf("speak should prefix sender: %s", content)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obsType, content := formatMessageObservation(tt.msg)
			if obsType != tt.wantType {
				t.Errorf("expected type %q, got %q", tt.wantType, obsType)
			}
			tt.checkFn(t, content)
		})
	}
}
