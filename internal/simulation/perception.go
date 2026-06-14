package simulation

import (
	"fmt"
	"time"
)

// PerceptionSystem collects observations from the environment and formats
// them for injection into each agent's decision prompt.
type PerceptionSystem struct {
	env    *Environment
	bus    *MessageBus
	clock  *SimClock
	logFn  func(format string, args ...any)
}

// NewPerceptionSystem creates a perception system.
func NewPerceptionSystem(env *Environment, bus *MessageBus, clock *SimClock) *PerceptionSystem {
	return &PerceptionSystem{
		env:   env,
		bus:   bus,
		clock: clock,
	}
}

// SetLogFunc sets a logging function for diagnostic output.
func (ps *PerceptionSystem) SetLogFunc(fn func(format string, args ...any)) {
	ps.logFn = fn
}

// CollectObservations gathers all new observations for an agent since their last tick.
// This includes: environment perceptions (who is nearby, what objects), incoming
// messages (from the bus), and any pending dialogue requests.
func (ps *PerceptionSystem) CollectObservations(agentID, personaName string) []Observation {
	now := ps.clock.Now()
	var observations []Observation

	// 1. Environment observations (zone, objects, nearby agents, accessible zones)
	envObs := ps.env.GetObservations(agentID, personaName)
	observations = append(observations, envObs...)

	// 2. Incoming messages (drain the message bus)
	inbox := ps.bus.DrainAll(agentID)
	for _, msg := range inbox {
		msgContent := msg.Content
		if msg.Type == "dialogue_request" {
			msgContent = fmt.Sprintf("%s wants to speak with you privately. Say [SAY @%s]: ... to respond, or [PASS] to decline.", msg.From, msg.From)
		}

		obsType := "agent_speak"
		if msg.Type == "system" {
			obsType = "system"
		} else if msg.Type == "dialogue_request" {
			obsType = "dialogue_request"
		} else if msg.Type == "dialogue_response" {
			obsType = "dialogue_response"
		}

		observations = append(observations, Observation{
			Type:    obsType,
			Content: fmt.Sprintf("%s: %s", msg.From, msgContent),
			Source:  msg.From,
			At:      now,
		})
	}

	// 3. Time awareness
	observations = append(observations, Observation{
		Type:    "time_event",
		Content: fmt.Sprintf("Current time: %s (Day %d).", ps.clock.TimeString(), ps.clock.Day()),
		Source:  "",
		At:      now,
	})

	if ps.logFn != nil {
		ps.logFn("perception: %s received %d observations", agentID, len(observations))
	}

	return observations
}

// FormatObservations renders observations as a structured markdown block for prompt injection.
func FormatObservations(observations []Observation) string {
	if len(observations) == 0 {
		return "## Current Perceptions\n(no new observations this tick)"
	}

	result := "## Current Perceptions\n\n"
	result += fmt.Sprintf("### Environment\n")
	hasEnv := false
	hasMessages := false
	var msgBlock string
	hasTime := false
	var timeBlock string

	var otherBlock string

	for _, o := range observations {
		switch o.Type {
		case "environment", "object", "agent_present", "nearby_zone":
			if !hasEnv {
				hasEnv = true
			}
			result += fmt.Sprintf("- %s\n", o.Content)
		case "agent_speak", "system", "dialogue_request", "dialogue_response":
			if !hasMessages {
				hasMessages = true
				msgBlock += "### Recent Messages\n"
			}
			msgBlock += fmt.Sprintf("- %s\n", o.Content)
		case "time_event":
			hasTime = true
			timeBlock = fmt.Sprintf("### Time\n%s\n", o.Content)
		default:
			otherBlock += fmt.Sprintf("- [%s] %s\n", o.Type, o.Content)
		}
	}

	if hasTime {
		result = timeBlock + result
	}

	if hasMessages {
		result += msgBlock
	}

	if otherBlock != "" {
		result += otherBlock
	}

	return result
}

// FormatObservationsForCW renders observations as context window content.
func FormatObservationsForCW(observations []Observation) string {
	if len(observations) == 0 {
		return ""
	}
	return FormatObservations(observations)
}

// ObservationToMemory converts an observation to a MemoryRecord for storage.
func ObservationToMemory(obs Observation, personaID string) MemoryRecord {
	return MemoryRecord{
		Role:          "observation",
		Content:       obs.Content,
		RecordType:    obs.Type,
		Importance:    obs.Importance,
		Source:        obs.Source,
		SimulatedTime: obs.At,
		Timestamp:     time.Now(),
		WorldState:    nil,
	}
}
