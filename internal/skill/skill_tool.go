package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xiaobaitu/soloqueue/internal/logger"
)

// ─── SkillTool ─────────────────────────────────────────────────────────────

// SkillTool enables LLMs to invoke Skills via function calling.
//
// Aligns with Claude Code's Skill mechanism: When an LLM calls a Skill (e.g., skill="commit", args="..."),
// SkillTool looks up the skill from the SkillRegistry, executes the preprocessing pipeline,
// and then decides whether to execute it inline or fork based on skill.Context.
//
// SkillTool's Description dynamically compiles a list of all skills not marked for disable-model-invocation,
// letting the LLM know when to use which skill.
type SkillTool struct {
	registry  *SkillRegistry
	forkSpawn SkillForkSpawnFn // If nil, fork mode degrades to inline
	logger    *logger.Logger
}

// SkillToolOption is an optional configuration for SkillTool
type SkillToolOption func(*SkillTool)

// WithSkillLogger sets the logger instance for SkillTool
func WithSkillLogger(l *logger.Logger) SkillToolOption {
	return func(st *SkillTool) { st.logger = l }
}

// NewSkillTool constructs a SkillTool
//
// registry cannot be nil. forkSpawn can be nil (in which case fork mode degrades to inline).
func NewSkillTool(registry *SkillRegistry, forkSpawn SkillForkSpawnFn, opts ...SkillToolOption) *SkillTool {
	st := &SkillTool{
		registry:  registry,
		forkSpawn: forkSpawn,
	}
	for _, opt := range opts {
		opt(st)
	}
	return st
}

// skillToolArgs is the argument structure for SkillTool
type skillToolArgs struct {
	Skill string `json:"skill"`
	Args  string `json:"args,omitempty"`
}

func (SkillTool) Name() string { return "Skill" }

// Description dynamically generates a list of skills for the LLM to decide when to use
func (t *SkillTool) Description() string {
	skills := t.registry.Skills()
	if len(skills) == 0 {
		return "Invoke a skill by name. No skills are currently available."
	}

	var b strings.Builder
	b.WriteString("Invoke a skill by name. Available skills:\n")
	for _, s := range skills {
		if s.DisableModelInvocation {
			continue
		}
		fmt.Fprintf(&b, "- %s: %s", s.ID, s.CombinedDescription())
		if s.Context == "fork" {
			b.WriteString(" [fork]")
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func (SkillTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"skill": {"type": "string", "description": "Name of the skill to invoke"},
			"args":  {"type": "string", "description": "Optional arguments to pass to the skill"}
		},
		"required": ["skill"]
	}`)
}

// Execute performs a skill invocation
//
// Process:
//  1. Parse arguments (skill name + optional args)
//  2. Look up the skill from the registry
//  3. Execute the preprocessing pipeline (e.g., $ARGUMENTS replacement)
//  4. Determine execution mode based on skill.Context:
//     inline → Return the preprocessed content, LLM continues to call tools based on this.
//     fork   → Create a sub-agent for execution, return the sub-agent's result.
func (t *SkillTool) Execute(ctx context.Context, rawArgs string) (string, error) {
	var args skillToolArgs
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		return fmt.Sprintf("error: invalid skill arguments: %s", err), nil
	}

	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "skill: executing",
			"skill_id", args.Skill, "has_args", args.Args != "")
	}

	s, ok := t.registry.GetSkill(args.Skill)
	if !ok {
		if t.logger != nil {
			t.logger.WarnContext(ctx, logger.CatTool, "skill: not found",
				"skill_id", args.Skill)
		}
		return fmt.Sprintf("error: skill %q not found", args.Skill), nil
	}

	// Preprocessing pipeline
	content := PreprocessContent(s.Instructions, args.Args, s.Dir)

	// Execution mode
	switch s.Context {
	case "fork":
		if t.forkSpawn != nil {
			result, err := ExecuteFork(ctx, s, content, args.Args, t.forkSpawn)
			if err != nil {
				if t.logger != nil {
					t.logger.WarnContext(ctx, logger.CatTool, "skill: fork failed",
						"skill_id", s.ID, "err", err.Error())
				}
				return fmt.Sprintf("error: skill %q fork execution failed: %s", s.ID, err), nil
			}
			if t.logger != nil {
				t.logger.InfoContext(ctx, logger.CatTool, "skill: fork completed",
					"skill_id", s.ID, "result_len", len(result))
			}
			return result, nil
		}
		// forkSpawn not set, degrade to inline
		fallthrough
	default:
		if t.logger != nil {
			t.logger.InfoContext(ctx, logger.CatTool, "skill: inline completed",
				"skill_id", s.ID, "content_len", len(content))
		}
		// Inline mode: return the preprocessed skill content
		// The LLM will consume this as a tool result and continue to act based on the skill instructions.
		return content, nil
	}
}