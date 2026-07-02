// Package skill implements an executable skill system.
//
// A Skill is a callable skill definition for an Agent, activated by the LLM via built-in Skill tools.
// The refactored design aligns with Claude Code's Skill mechanism:
//
//   - A Skill is an immutable data definition (exported fields struct, not an interface)
//   - SkillTool implements tools.Tool, called by LLM via function calling
//   - Supports two execution modes: inline (injecting instructions into the current conversation) and fork (isolated sub-agent)
//   - Supports allowed-tools whitelist, $ARGUMENTS replacement, !`command` shell execution, @file references
//
// Dependency direction:
//
//	tools does not depend on anyone (defines Tool interface)
//	skill → tools (SkillTool implements Tool interface)
//	agent → skill + tools (holds both Registries)
package skill

import (
	"fmt"
	"sort"
	"sync"
)

// ─── Skill Type ────────────────────────────────────────────────────────────

// SkillCategory distinguishes between built-in and external Skills.
type SkillCategory string

const (
	// SkillBuiltin represents a built-in Skill (registered by Go code).
	SkillBuiltin SkillCategory = "builtin"
	// SkillUser represents a user-defined external Skill (loaded from SKILL.md files).
	SkillUser SkillCategory = "user"
)

// Skill is an immutable skill definition (not modified after construction).
//
// It aligns with Claude Code's Skill mechanism: a Skill is an executable capability callable by an LLM,
// containing instruction content, tool whitelist, execution mode, and other configurations.
// The LLM activates the skill via built-in Skill tools, rather than manually reading SKILL.md.
type Skill struct {
	// ID is a unique identifier (e.g., "commit", "deploy"), must not be empty.
	ID string

	// Name is a human-readable name.
	Name string

	// Description is a natural language description for the LLM (brief explanation).
	Description string

	// WhenToUse describes additional trigger conditions, appended to Description for the LLM to decide when to call.
	WhenToUse string

	// Instructions contains the full instruction content.
	//
	// For skills loaded from SKILL.md, this is the body part;
	// for built-in skills, this is the instruction text defined in Go code.
	Instructions string

	// AllowedTools is a tool whitelist; nil means no restrictions.
	//
	// Supported patterns: Bash(git:*), Edit(src/**/*.ts), mcp__server__tool
	AllowedTools []string

	// DisableModelInvocation, when true, prevents the skill from appearing in the Skill tool description.
	// It can only be manually triggered via the /skill-name slash command.
	DisableModelInvocation bool

	// UserInvocable, when false, prevents the skill from appearing in the / menu.
	// It is only for internal AI calls or references by other skills.
	UserInvocable bool

	// Context is the execution mode: "fork" for an isolated sub-agent, "" for inline.
	Context string

	// Agent is the type of sub-agent when forking (e.g., "general-purpose", "Explore").
	Agent string

	// Category returns the Skill's classification (builtin or user).
	Category SkillCategory

	// FilePath is the absolute path to SKILL.md (empty for built-in skills).
	FilePath string

	// Dir is the directory containing SKILL.md (supports file reference resolution).
	Dir string

	// Triggers are keywords that can trigger the skill.
	Triggers []string

	// Disabled indicates whether the skill is disabled.
	Disabled bool

	// Upstream is the remote Git repository address.
	Upstream string

	// Branch is the remote Git branch name.
	Branch string

	// SubPath is the sub-directory path within the remote Git repository.
	SubPath string

	// RequiredEnv required environment variables for the skill
	RequiredEnv []string
}

// ─── Constructors ──────────────────────────────────────────────────────────────

// SkillOption is an optional configuration for a Skill.
type SkillOption func(*Skill)

// WithAllowedTools sets the tool whitelist.
func WithAllowedTools(tools []string) SkillOption {
	return func(s *Skill) { s.AllowedTools = tools }
}

// WithDisableModelInvocation prevents AI from automatically calling the skill.
func WithDisableModelInvocation() SkillOption {
	return func(s *Skill) { s.DisableModelInvocation = true }
}

// WithUserInvocable sets whether the skill appears in the / menu.
func WithUserInvocable(v bool) SkillOption {
	return func(s *Skill) { s.UserInvocable = v }
}

// WithContext sets the execution mode ("fork" or "").
func WithContext(ctx string) SkillOption {
	return func(s *Skill) { s.Context = ctx }
}

// WithAgent sets the sub-agent type when forking.
func WithAgent(agent string) SkillOption {
	return func(s *Skill) { s.Agent = agent }
}

// NewBuiltinSkill constructs a built-in Skill.
//
// id must not be empty (will cause an error during registration).
// instructions is the full instruction for the skill.
func NewBuiltinSkill(id, desc, instructions string, opts ...SkillOption) *Skill {
	s := &Skill{
		ID:            id,
		Name:          id,
		Description:   desc,
		Instructions:  instructions,
		UserInvocable: true,
		Category:      SkillBuiltin,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// maxSkillDescriptionChars is the maximum character count for the combined description, aligning with Claude Code.
const maxSkillDescriptionChars = 1536

// CombinedDescription merges Description and WhenToUse, truncated to maxSkillDescriptionChars.
//
// Aligns with Claude Code behavior:
//   - when_to_use is appended after description
//   - Combined text limit is 1536 characters
//   - Prioritizes preserving initial content (key use cases are placed at the beginning of the description)
func (s *Skill) CombinedDescription() string {
	if s.WhenToUse == "" {
		return s.Description
	}
	combined := s.Description + "\n" + s.WhenToUse
	if len(combined) <= maxSkillDescriptionChars {
		return combined
	}
	// Truncate: prioritize preserving the full description, remaining quota for the beginning of when_to_use
	descLen := len(s.Description)
	if descLen >= maxSkillDescriptionChars {
		return s.Description[:maxSkillDescriptionChars]
	}
	whenToUseBudget := maxSkillDescriptionChars - descLen - 1 // 1 for '\n'
	if whenToUseBudget <= 0 {
		return s.Description
	}
	return s.Description + "\n" + s.WhenToUse[:whenToUseBudget]
}

// ─── SkillRegistry ─────────────────────────────────────────────────────────

// Skill related errors.
var (
	// ErrSkillIDEmpty indicates Skill.ID is empty during registration.
	ErrSkillIDEmpty = fmt.Errorf("skill: skill id is empty")
	// ErrSkillAlreadyRegistered indicates a skill with the same ID is already registered.
	ErrSkillAlreadyRegistered = fmt.Errorf("skill: skill already registered")
	// ErrSkillNil indicates Register(nil) was called.
	ErrSkillNil = fmt.Errorf("skill: skill is nil")
)

// SkillRegistry manages Skill registration and lookup.
type SkillRegistry struct {
	mu     sync.RWMutex
	skills map[string]*Skill // skillID → Skill
}

// NewSkillRegistry constructs an empty SkillRegistry.
func NewSkillRegistry() *SkillRegistry {
	return &SkillRegistry{
		skills: make(map[string]*Skill),
	}
}

// Register registers a Skill.
//
// Errors:
//   - s == nil      → ErrSkillNil
//   - s.ID == ""    → ErrSkillIDEmpty
//   - duplicate ID  → ErrSkillAlreadyRegistered
func (r *SkillRegistry) Register(s *Skill) error {
	if s == nil {
		return ErrSkillNil
	}
	if s.ID == "" {
		return ErrSkillIDEmpty
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.skills[s.ID]; ok {
		return fmt.Errorf("%w: %s", ErrSkillAlreadyRegistered, s.ID)
	}

	r.skills[s.ID] = s
	return nil
}

// GetSkill looks up a skill by its ID.
func (r *SkillRegistry) GetSkill(id string) (*Skill, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.skills[id]
	return s, ok
}

// Skills returns a snapshot of all registered Skills (sorted by ID alphabetically).
func (r *SkillRegistry) Skills() []*Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Skill, 0, len(r.skills))
	for _, s := range r.skills {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

// Rebuild clears the registry and reloads all skills from the given directories.
func (r *SkillRegistry) Rebuild(dirs map[string]string) error {
	userSkills, err := LoadSkillsFromDirs(dirs)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.skills = make(map[string]*Skill, len(userSkills))
	for _, s := range userSkills {
		r.skills[s.ID] = s
	}
	r.mu.Unlock()
	return nil
}

// Len returns the current number of skills.
func (r *SkillRegistry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.skills)
}