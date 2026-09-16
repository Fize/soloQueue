package prompt

// LeaderInfo describes an available Team Leader.
// The main agent only needs to know what each team can do (Description),
// not what tools they have — tools are implementation details managed internally.
type LeaderInfo struct {
	Name             string // e.g. "dev"
	Description      string // e.g. "Full-stack developer, responsible for frontend/backend development"
	Group            string // e.g. "DevOps"
	GroupDescription string // Group description (from group file body)
}

// SoulNeededError is returned when soul.md is missing.
type SoulNeededError struct {
	RoleID string // Kept for backward compatibility, now refers to "default" role
}

func (e *SoulNeededError) Error() string {
	return "soul.md not found in roles directory"
}
