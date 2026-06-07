package simulation

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// WorldStateChange records a single mutation to the world state.
type WorldStateChange struct {
	Key      string    `json:"key"`
	OldValue any       `json:"old_value,omitempty"`
	NewValue any       `json:"new_value"`
	AgentID  string    `json:"agent_id"`
	Round    int       `json:"round"`
	At       time.Time `json:"at"`
}

// WorldState is a thread-safe key-value store with change history.
type WorldState struct {
	data    map[string]any
	history []WorldStateChange
	mu      sync.RWMutex
}

func NewWorldState(initial map[string]any) *WorldState {
	data := make(map[string]any)
	for k, v := range initial {
		data[k] = v
	}
	return &WorldState{data: data}
}

func (ws *WorldState) Get(key string) (any, bool) {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	v, ok := ws.data[key]
	return v, ok
}

func (ws *WorldState) Set(key string, value any, agentID string, round int) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	old := ws.data[key]
	ws.data[key] = value
	ws.history = append(ws.history, WorldStateChange{
		Key:      key,
		OldValue: old,
		NewValue: value,
		AgentID:  agentID,
		Round:    round,
		At:       time.Now(),
	})
}

func (ws *WorldState) Delete(key string, agentID string, round int) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	old := ws.data[key]
	delete(ws.data, key)
	ws.history = append(ws.history, WorldStateChange{
		Key:      key,
		OldValue: old,
		NewValue: nil,
		AgentID:  agentID,
		Round:    round,
		At:       time.Now(),
	})
}

func (ws *WorldState) Snapshot() map[string]any {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	out := make(map[string]any, len(ws.data))
	for k, v := range ws.data {
		out[k] = v
	}
	return out
}

func (ws *WorldState) History() []WorldStateChange {
	ws.mu.RLock()
	defer ws.mu.RUnlock()
	out := make([]WorldStateChange, len(ws.history))
	copy(out, ws.history)
	return out
}

// FormatForPrompt renders the world state as structured markdown.
func (ws *WorldState) FormatForPrompt() string {
	ws.mu.RLock()
	defer ws.mu.RUnlock()

	if len(ws.data) == 0 {
		return "## Current World State\n(empty)"
	}

	var b strings.Builder
	b.WriteString("## Current World State\n")

	keys := make([]string, 0, len(ws.data))
	for k := range ws.data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := ws.data[k]
		b.WriteString(fmt.Sprintf("- **%s**: %v\n", k, formatValue(v)))
	}
	return b.String()
}

func formatValue(v any) string {
	switch val := v.(type) {
	case []any:
		parts := make([]string, len(val))
		for i, item := range val {
			parts[i] = fmt.Sprintf("%v", item)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case []string:
		return "[" + strings.Join(val, ", ") + "]"
	default:
		return fmt.Sprintf("%v", v)
	}
}
