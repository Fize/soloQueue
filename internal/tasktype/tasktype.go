// Package tasktype defines the stable, cross-layer taxonomy used for model
// routing. It deliberately describes the nature of work, not its difficulty.
package tasktype

import "fmt"

// TaskType is the type of work a user is asking the system to perform.
type TaskType string

const (
	Unknown     TaskType = ""
	General     TaskType = "general"
	Engineering TaskType = "engineering"
	Research    TaskType = "research"
)

// Valid reports whether t is one of the routable task types.
func (t TaskType) Valid() bool {
	switch t {
	case General, Engineering, Research:
		return true
	default:
		return false
	}
}

func (t TaskType) String() string { return string(t) }

// Parse validates a persisted or transport value.
func Parse(value string) (TaskType, error) {
	t := TaskType(value)
	if !t.Valid() {
		return Unknown, fmt.Errorf("unsupported task type %q", value)
	}
	return t, nil
}
