package prompt

// LeaderInfo describes an available Team Leader.
// The main agent only needs to know what each team can do (GroupDescription),
// not what tools they have — tools are implementation details managed internally.
type LeaderInfo struct {
	ID               string // canonical target ID used in delegate(target=...)
	Name             string // e.g. "dev"
	Description      string // Leader role description; not exposed in the main agent routing table
	Group            string // e.g. "DevOps"
	GroupDescription string // Team capabilities exposed to the main agent (from group file body)
}

// SoulNeededError is returned when soul.md is missing.
type SoulNeededError struct{}

func (e *SoulNeededError) Error() string {
	return "soul.md not found in roles directory"
}
