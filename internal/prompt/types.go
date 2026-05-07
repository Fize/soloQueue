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

// ProfileAnswers holds the user's responses to the personalization questionnaire.
type ProfileAnswers struct {
	Name        string // Assistant name, defaults to "SoloQueue"
	Gender      string // Gender, defaults to "female"
	Personality string // Personality, defaults to "playful"
	CommStyle   string // Communication style, defaults to "casual"
}

// DefaultProfileAnswers returns ProfileAnswers with all defaults.
func DefaultProfileAnswers() ProfileAnswers {
	return ProfileAnswers{
		Name:        "SoloQueue",
		Gender:      "female",
		Personality: "playful",
		CommStyle:   "casual",
	}
}

// SoulNeededError is returned when soul.md is missing.
// The caller handles the interactive questionnaire flow.
type SoulNeededError struct {
	RoleID string
}

func (e *SoulNeededError) Error() string {
	return "soul.md not found for role: " + e.RoleID
}
