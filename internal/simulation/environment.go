package simulation

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// EnvObject represents an interactive object in the environment.
type EnvObject struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	IsInteractive bool          `json:"is_interactive"`
	State        map[string]any `json:"state"`
}

// Zone is a named area in the environment where agents can be present.
type Zone struct {
	Name          string              `json:"name"`
	Description   string              `json:"description"`
	Capacity      int                 `json:"capacity"`
	Objects       map[string]*EnvObject `json:"objects"`
	PresentAgents map[string]bool     `json:"-"` // set of agent IDs currently in zone
}

// Environment is the spatial context for the simulation.
// Agents exist in zones, can move between them, and observe their surroundings.
type Environment struct {
	zones    map[string]*Zone
	agentLoc map[string]string // agentID → zoneName
	clock    *SimClock
	history  []EnvironmentEvent
	mu       sync.RWMutex
}

// EnvironmentEvent records a change in the environment.
type EnvironmentEvent struct {
	Type      string    `json:"type"` // "agent_enter", "agent_leave", "object_changed"
	AgentID   string    `json:"agent_id,omitempty"`
	ZoneName  string    `json:"zone_name"`
	ObjectID  string    `json:"object_id,omitempty"`
	Detail    string    `json:"detail"`
	SimTime   time.Time `json:"sim_time"`
}

// NewEnvironment creates a simulation environment with predefined zones.
func NewEnvironment(clock *SimClock) *Environment {
	return &Environment{
		zones:     make(map[string]*Zone),
		agentLoc:  make(map[string]string),
		clock:     clock,
	}
}

// AddZone registers a new zone in the environment.
func (env *Environment) AddZone(name, description string, capacity int) {
	env.mu.Lock()
	defer env.mu.Unlock()
	env.zones[name] = &Zone{
		Name:          name,
		Description:   description,
		Capacity:      capacity,
		Objects:       make(map[string]*EnvObject),
		PresentAgents: make(map[string]bool),
	}
}

// AddObject places an object in a zone.
func (env *Environment) AddObject(zoneName string, obj *EnvObject) {
	env.mu.Lock()
	defer env.mu.Unlock()
	if z, ok := env.zones[zoneName]; ok {
		z.Objects[obj.ID] = obj
	}
}

// PlaceAgent sets an agent's initial location.
func (env *Environment) PlaceAgent(agentID, zoneName string) {
	env.mu.Lock()
	defer env.mu.Unlock()
	env.agentLoc[agentID] = zoneName
	if z, ok := env.zones[zoneName]; ok {
		z.PresentAgents[agentID] = true
	}
}

// MoveAgent moves an agent from one zone to another.
// Returns observations describing what changed.
func (env *Environment) MoveAgent(agentID, targetZone string) ([]Observation, error) {
	env.mu.Lock()

	currentZone, exists := env.agentLoc[agentID]
	if !exists {
		env.mu.Unlock()
		return nil, fmt.Errorf("agent %s has no location", agentID)
	}

	target, ok := env.zones[targetZone]
	if !ok {
		env.mu.Unlock()
		return nil, fmt.Errorf("zone %s does not exist", targetZone)
	}

	if len(target.PresentAgents) >= target.Capacity && target.Capacity > 0 {
		env.mu.Unlock()
		return nil, fmt.Errorf("zone %s is full (capacity %d)", targetZone, target.Capacity)
	}

	// Leave current zone
	if z, ok2 := env.zones[currentZone]; ok2 {
		delete(z.PresentAgents, agentID)
	}

	// Enter target zone
	target.PresentAgents[agentID] = true
	env.agentLoc[agentID] = targetZone

	now := env.clock.Now()

	env.history = append(env.history, EnvironmentEvent{
		Type:     "agent_leave",
		AgentID:  agentID,
		ZoneName: currentZone,
		SimTime:  now,
	})
	env.history = append(env.history, EnvironmentEvent{
		Type:     "agent_enter",
		AgentID:  agentID,
		ZoneName: targetZone,
		SimTime:  now,
	})
	env.mu.Unlock()

	// Build observations for agents in both zones
	var obs []Observation
	obs = append(obs, Observation{
		Type:    "agent_move",
		Content: fmt.Sprintf("%s left %s and entered %s.", agentID, currentZone, targetZone),
		Source:  agentID,
		At:      now,
	})
	return obs, nil
}

// GetAgentZone returns the zone an agent is currently in.
func (env *Environment) GetAgentZone(agentID string) string {
	env.mu.RLock()
	defer env.mu.RUnlock()
	return env.agentLoc[agentID]
}

// GetObservations collects what an agent can perceive in their current zone.
func (env *Environment) GetObservations(agentID string, personaName string) []Observation {
	env.mu.RLock()
	defer env.mu.RUnlock()

	zoneName := env.agentLoc[agentID]
	zone := env.zones[zoneName]
	if zone == nil {
		return nil
	}

	now := env.clock.Now()
	var obs []Observation

	// Zone description
	obs = append(obs, Observation{
		Type:    "environment",
		Content: fmt.Sprintf("You are in %s: %s.", zone.Name, zone.Description),
		Source:  "",
		At:      now,
	})

	// Objects in zone
	for _, obj := range zone.Objects {
		obs = append(obs, Observation{
			Type:    "object",
			Content: fmt.Sprintf("You see %s: %s.", obj.Name, obj.Description),
			Source:  obj.ID,
			At:      now,
		})
	}

	// Other agents present
	for otherID := range zone.PresentAgents {
		if otherID == agentID {
			continue
		}
		obs = append(obs, Observation{
			Type:    "agent_present",
			Content: fmt.Sprintf("%s is here.", otherID),
			Source:  otherID,
			At:      now,
		})
	}

	// Nearby zones (accessible)
	for name, z := range env.zones {
		if name != zoneName {
			obs = append(obs, Observation{
				Type:    "nearby_zone",
				Content: fmt.Sprintf("You can go to %s: %s.", z.Name, z.Description),
				Source:  name,
				At:      now,
			})
		}
	}

	return obs
}

// GetAgentsInZone returns the IDs of agents currently in a zone.
func (env *Environment) GetAgentsInZone(zoneName string) []string {
	env.mu.RLock()
	defer env.mu.RUnlock()
	z := env.zones[zoneName]
	if z == nil {
		return nil
	}
	ids := make([]string, 0, len(z.PresentAgents))
	for id := range z.PresentAgents {
		ids = append(ids, id)
	}
	return ids
}

// Interact allows an agent to interact with an object in their current zone.
func (env *Environment) Interact(agentID, objectID, action string) (string, error) {
	env.mu.Lock()
	defer env.mu.Unlock()

	zoneName := env.agentLoc[agentID]
	zone := env.zones[zoneName]
	if zone == nil {
		return "", fmt.Errorf("agent %s location unknown", agentID)
	}

	obj, ok := zone.Objects[objectID]
	if !ok {
		return "", fmt.Errorf("object %s not found in zone %s", objectID, zoneName)
	}

	if !obj.IsInteractive {
		return "", fmt.Errorf("object %s is not interactive", objectID)
	}

	detail := fmt.Sprintf("%s %s %s in %s.", agentID, action, obj.Name, zoneName)
	env.history = append(env.history, EnvironmentEvent{
		Type:     "object_changed",
		AgentID:  agentID,
		ZoneName: zoneName,
		ObjectID: objectID,
		Detail:   detail,
		SimTime:  env.clock.Now(),
	})

	return detail, nil
}

// ZoneNames returns all zone names.
func (env *Environment) ZoneNames() []string {
	env.mu.RLock()
	defer env.mu.RUnlock()
	names := make([]string, 0, len(env.zones))
	for name := range env.zones {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ZoneCount returns the number of zones.
func (env *Environment) ZoneCount() int {
	env.mu.RLock()
	defer env.mu.RUnlock()
	return len(env.zones)
}

// FormatForPrompt renders the environment layout for system prompt injection.
func (env *Environment) FormatForPrompt() string {
	env.mu.RLock()
	defer env.mu.RUnlock()

	var b strings.Builder
	b.WriteString("## Environment Layout\n\n")
	b.WriteString("Available locations:\n")
	for name, z := range env.zones {
		b.WriteString(fmt.Sprintf("- **%s**: %s (capacity: %d)\n", name, z.Description, z.Capacity))
		if len(z.Objects) > 0 {
			for _, obj := range z.Objects {
				b.WriteString(fmt.Sprintf("  - %s: %s\n", obj.Name, obj.Description))
			}
		}
	}
	return b.String()
}

// History returns recent environment events.
func (env *Environment) History() []EnvironmentEvent {
	env.mu.RLock()
	defer env.mu.RUnlock()
	out := make([]EnvironmentEvent, len(env.history))
	copy(out, env.history)
	return out
}

// AgentPositions returns a map of agentID → zoneName.
func (env *Environment) AgentPositions() map[string]string {
	env.mu.RLock()
	defer env.mu.RUnlock()
	out := make(map[string]string, len(env.agentLoc))
	for k, v := range env.agentLoc {
		out[k] = v
	}
	return out
}
