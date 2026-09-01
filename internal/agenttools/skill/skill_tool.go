package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/xiaobaitu/soloqueue/internal/infra/logger"
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
	stats     InvocationStats // Optional; telemetry + listing ordering
	agentID   string          // Agent identity attached to recorded events
}

// SkillToolOption is an optional configuration for SkillTool
type SkillToolOption func(*SkillTool)

// WithSkillLogger sets the logger instance for SkillTool
func WithSkillLogger(l *logger.Logger) SkillToolOption {
	return func(st *SkillTool) { st.logger = l }
}

// WithInvocationStats wires telemetry: Execute calls are recorded and
// Description() orders skills by recent invocation counts.
func WithInvocationStats(stats InvocationStats) SkillToolOption {
	return func(st *SkillTool) { st.stats = stats }
}

// WithAgentID sets the agent identity attached to recorded invocation events.
func WithAgentID(id string) SkillToolOption {
	return func(st *SkillTool) { st.agentID = id }
}

// statsWindow is the lookback for listing-order counts.
const statsWindow = 30 * 24 * time.Hour

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

// ForkResultPrefix marks Skill tool results produced by fork-mode execution.
// Fork results are legitimate repeatable answers (e.g. re-querying the same
// stock quote), NOT idempotent instructions — dedup (agent/skill_dedup.go) and
// post-compaction restore (memory/ctxwin/skill_restore.go) skip them.
const ForkResultPrefix = "[Fork result] "

// Description dynamically generates the skill listing for the LLM:
// a usage directive prioritizing skills, then a trigger-first one-line index
// (BuildSkillListing). This is the primary proactive-usage mechanism.
//
// The listing has no budget cap (budget 0 = unbounded): every visible skill
// gets a full summary, because ID-only lines carry no trigger signal and are
// effectively invisible. Output is a deterministic function of the catalog
// (counts desc, ID asc ties), so the system prompt stays cache-stable; size is
// governed by catalog count — see README "Skills" for the best-practice range.
func (t *SkillTool) Description() string {
	skills := t.registry.Skills()

	var visible []*Skill
	byID := make(map[string]*Skill, len(skills))
	for _, s := range skills {
		if s.DisableModelInvocation {
			continue
		}
		visible = append(visible, s)
		byID[s.ID] = s
	}
	if len(visible) == 0 {
		return "Invoke a skill by name. No skills are currently available."
	}

	// With telemetry wired, order by recent counts; otherwise ID order.
	var counts map[string]int
	if t.stats != nil {
		counts, _ = t.stats.Counts(context.Background(), time.Now().Add(-statsWindow))
	}

	listing := BuildSkillListing(visible, 0, counts) // 0 = no budget cap
	listing = markForkSkills(listing, byID)

	return "Invoke a skill to load specialized workflows and methodologies. Skills contain mandatory step-by-step instructions for specific domains. When your task matches a skill's description, you MUST call this tool BEFORE using raw tools (Read, Write, Bash, etc.). Treat skills as your highest-priority execution guide — they encode battle-tested workflows. Skipping a matching skill is a protocol violation.\n\nAvailable skills:\n" + listing
}

// markForkSkills appends " [fork]" to listing lines for fork-mode skills so
// the LLM knows the invocation cost profile before calling.
func markForkSkills(listing string, byID map[string]*Skill) string {
	lines := strings.Split(listing, "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		id := strings.TrimPrefix(line, "- ")
		if idx := strings.Index(id, ": "); idx > 0 {
			id = id[:idx]
		}
		if s, ok := byID[id]; ok && s.Context == "fork" && !strings.Contains(line, "[fork]") {
			lines[i] = line + " [fork]"
		}
	}
	return strings.Join(lines, "\n")
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
	started := time.Now()
	var args skillToolArgs
	if err := json.Unmarshal([]byte(rawArgs), &args); err != nil {
		t.record(ctx, args.Skill, "", InvocationError, started)
		return fmt.Sprintf("error: invalid skill arguments: %s", err), nil
	}

	if t.logger != nil {
		t.logger.InfoContext(ctx, logger.CatTool, "skill: executing",
			"skill_id", args.Skill, "has_args", args.Args != "")
	}

	s, ok := t.registry.GetSkill(args.Skill)
	if !ok {
		t.record(ctx, args.Skill, args.Args, InvocationNotFound, started)
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
				t.record(ctx, s.ID, args.Args, InvocationError, started)
				if t.logger != nil {
					t.logger.WarnContext(ctx, logger.CatTool, "skill: fork failed",
						"skill_id", s.ID, "err", err.Error())
				}
				return fmt.Sprintf("error: skill %q fork execution failed: %s", s.ID, err), nil
			}
			t.record(ctx, s.ID, args.Args, InvocationFork, started)
			if t.logger != nil {
				t.logger.InfoContext(ctx, logger.CatTool, "skill: fork completed",
					"skill_id", s.ID, "result_len", len(result))
			}
			return ForkResultPrefix + fmt.Sprintf("skill %q executed in a sub-agent:\n\n%s", s.ID, result), nil
		}
		// forkSpawn not set, degrade to inline
		fallthrough
	default:
		t.record(ctx, s.ID, args.Args, InvocationOK, started)
		if t.logger != nil {
			t.logger.InfoContext(ctx, logger.CatTool, "skill: inline completed",
				"skill_id", s.ID, "content_len", len(content))
		}
		// Inline mode: return the preprocessed skill content
		// The LLM will consume this as a tool result and continue to act based on the skill instructions.
		return content, nil
	}
}

// record persists an invocation event; failures are logged, never returned
// (telemetry must not block skill execution).
func (t *SkillTool) record(ctx context.Context, skillID, args string, result InvocationResult, started time.Time) {
	if t.stats == nil {
		return
	}
	if err := t.stats.Record(ctx, InvocationEvent{
		AgentID:  t.agentID,
		SkillID:  skillID,
		Args:     args,
		Result:   result,
		Duration: time.Since(started),
	}); err != nil && t.logger != nil {
		t.logger.WarnContext(ctx, logger.CatTool, "skill: record invocation failed",
			"skill_id", skillID, "err", err.Error())
	}
}
