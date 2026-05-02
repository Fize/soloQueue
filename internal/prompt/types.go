package prompt

// LeaderInfo describes an available Team Leader.
// The main agent only needs to know what each team can do (Description),
// not what tools they have — tools are implementation details managed internally.
type LeaderInfo struct {
	Name             string     // e.g. "dev"
	Description      string     // e.g. "Full-stack developer, responsible for frontend/backend development"
	Group            string     // e.g. "DevOps"
	GroupDescription string     // Group description (from group file body)
	MatchedWorkspace *Workspace // Workspace matched by cwd, may be nil
}

// ProfileAnswers holds the user's responses to the personalization questionnaire.
type ProfileAnswers struct {
	Name        string // Assistant name, defaults to "SoloQueue"
	Gender      string // Gender, defaults to "female"
	Personality string // Personality, defaults to "playful"
	CommStyle   string // Communication style, defaults to "casual"
	Preset      string // Preset character name; when non-empty, skips the questionnaire and uses the preset profile
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

// ProfilePreset defines a preset character profile.
type ProfilePreset struct {
	Name        string // Character name
	Description string // One-line description
	Gender      string // Gender
}

// PresetProfiles returns all available preset characters.
func PresetProfiles() []ProfilePreset {
	return []ProfilePreset{
		{Name: "韩立", Description: "寡言冷静，谨慎果决的散修", Gender: "male"},
		{Name: "极阴老祖", Description: "霸道威严，千年修为的魔道巨擘", Gender: "male"},
		{Name: "南宫婉", Description: "清冷疏离，道心坚定的掩月宗仙子", Gender: "female"},
		{Name: "玄骨上人", Description: "阴鸷深算，视苍生为药引的老魔", Gender: "male"},
		{Name: "元瑶", Description: "柔婉坚韧，重情重义的修仙者", Gender: "female"},
		{Name: "紫灵", Description: "含蓄精炼，算尽利弊的布局者", Gender: "female"},
	}
}

// ProfileNeededError is returned when profile.md is missing.
// The caller handles the interactive questionnaire flow.
type ProfileNeededError struct {
	RoleID string
}

func (e *ProfileNeededError) Error() string {
	return "profile.md not found for role: " + e.RoleID
}
