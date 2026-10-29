// Package tasktype defines the work-nature taxonomy for model routing (not difficulty).
package tasktype

import "fmt"

type TaskType string

const (
	Unknown     TaskType = ""
	General     TaskType = "general"
	Engineering TaskType = "engineering"
	Research    TaskType = "research"
)

// Descriptions are shared by the LLM prompt and the local decision model.
const (
	GeneralDescription     = "conversation, explanation, writing, translation, summarizing supplied content, ordinary planning"
	EngineeringDescription = "code, repositories, debugging, tests, databases, APIs, automation, deployment, or technical implementation"
	ResearchDescription    = "external search, current information, source verification, comparisons, or citations"
)

// Valid reports whether t is a routable task type.
func (t TaskType) Valid() bool {
	switch t {
	case General, Engineering, Research:
		return true
	default:
		return false
	}
}

func (t TaskType) String() string { return string(t) }

// Parse converts a string to a validated TaskType.
func Parse(value string) (TaskType, error) {
	t := TaskType(value)
	if !t.Valid() {
		return Unknown, fmt.Errorf("unsupported task type %q", value)
	}
	return t, nil
}
