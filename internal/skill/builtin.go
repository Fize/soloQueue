package skill

// RegisterBuiltinSkills registers all built-in skills into the global registry.
//
// Built-in skills are defined in Go code and provide standard capabilities
// available to all agents. They are registered BEFORE user skills from
// ~/.soloqueue/skills/, so user skills with the same ID will override them.
func RegisterBuiltinSkills(reg *SkillRegistry) {
	// Builtin skills are defined here.
	// Example:
	//   reg.Register(NewBuiltinSkill("commit",
	//       "Generate conventional commit messages from staged git changes",
	//       "## Commit Skill\n\n...",
	//       WithContext("fork"),
	//   ))
	_ = reg // reserved for future builtin skill definitions
}
