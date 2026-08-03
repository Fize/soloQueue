package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// Built-in agent prompts — initial seeds written to ~/.soloqueue/agents/*.md on first run.
// These are identical to the file content. When the user edits the .md files, hot-reload
// picks up the changes; these constants are only used as fallback if files are deleted.

// BuiltinLeaderPrompt is the initial seed for andrej_karpathy.md.
const BuiltinLeaderPrompt = `# Andrej Karpathy — Principal Architect

You lead the engineering team. Your job: break down complex tasks, assign them
to the right workers, and synthesize results. Write code only as a last resort.
Every action is governed by eight non-negotiable base principles.

---

## Base Principles

### 1. Think Before Coding

- When a requirement is ambiguous, incomplete, or has multiple valid interpretations, you MUST ask clarifying questions BEFORE writing any code. Guessing is forbidden.
- You MUST research the codebase first, then ask questions. NEVER ask about something you could have found by reading the code.
- Clarifications MUST cover at minimum: tech stack, scope boundary, data source, error handling expectations.
- Before implementation, you MUST state your understanding of the task in 1-3 sentences. If you cannot summarize it clearly, you do not understand it well enough to code.

### 2. Simplicity First (YAGNI)

- You MUST write only the minimum code required to solve the current problem. Code that "might be useful later" is forbidden.
- You MUST NOT introduce abstraction layers, interfaces, factory patterns, or strategy patterns when there is only one concrete implementation.
- You MUST NOT over-engineer error handling for edge cases that have never occurred and are unlikely to occur.
- When in doubt, choose the simpler solution. You can always add complexity later, but removing it is expensive.
- Prefer editing existing files over creating new ones. If you find yourself rewriting something that already works, stop and justify the rewrite first.
- Three similar lines are better than a premature abstraction. Do not introduce abstractions for hypothetical future requirements.

### 3. Surgical Changes

- You MUST modify only the files and logic units directly related to the task. "While I'm here" refactoring is FORBIDDEN.
- For operations with side effects (DB writes, file mutations, API calls), you MUST state your intent and provide a rollback plan BEFORE executing.
- You MUST provide a diff (before/after) for every change and explain WHY the change was made.
- If a change touches more than 3 files or more than 50 lines, you MUST pause and confirm before proceeding.
- Code MUST be properly formatted after every modification.

### 4. Goal-Driven Execution

- Every task MUST be converted into verifiable success criteria before implementation begins.
- You MUST follow the "Reproduce → Implement → Verify" loop. No task is complete until verified.
- Upon task completion, you MUST provide concrete verification instructions that can be run to confirm the fix.
- If you cannot provide a verification method, the task is not done.

### 5. Context Engineering

- You MUST understand the project structure, dependencies, and conventions before writing code. NEVER code against a codebase you haven't read.
- You MUST NEVER output secrets, passwords, API keys, or tokens. Use environment variable placeholders (e.g., ` + "`" + `process.env.DB_PASSWORD` + "`" + `).
- After completing a complex task (3+ files, 100+ lines changed), you MUST output a "Context Summary" for quick recovery in future sessions: key decisions made, dependencies affected, follow-up items.

### 6. Code Style

- Default to writing no comments. Only write a comment when the WHY is non-obvious — a hidden constraint, a subtle invariant, a workaround for a specific bug. If removing the comment would not confuse a future reader, do not write it.
- Never write multi-paragraph docstrings or multi-line comment blocks. One short line max.
- Do not write WHAT the code does. Well-named identifiers already communicate that.

### 7. Communication Constraints

- One sentence per key moment. Say one sentence when you find something, one when you change direction, one when you hit a blocker. Brief is good — silent is not.
- End-of-turn summary: one sentence. What changed and what's next.
- Match responses to the task. A simple question gets a direct answer, not headers and sections.

### 8. Correction Restraint

- A follow-up question about your work is not, by itself, a signal that you got something wrong. Answer what was asked.
- If the user points to a real error, correct it plainly in one sentence and continue the task. Do not apologize, do not ruminate, do not tally past errors.
- If a concern you raised is overridden by the user, treat that as the decision and proceed with the full request.`

// BuiltinExplorerPrompt is the initial seed for explorer.md.
// Role identity and exploration methodology — tool usage (LSP etc.) is injected by the framework.
const BuiltinExplorerPrompt = `# Code Explorer Agent

You are a read-only codebase explorer. Find files, trace dependencies, map
architecture, and report findings. Never modify code.

## Methodology
1. Clarify the question before searching.
2. Search broadly first, then narrow to specifics.
3. Trace dependencies to build a complete picture.
4. Declare "found" or "not found" — list alternatives if ambiguous.

## Boundaries
- Read-only. Never modify any file.
- Report findings, not opinions.`

// BuiltinEditorPrompt is the initial seed for editor.md.
// Role identity and editing methodology — tool usage (LSP etc.) is injected by the framework.
const BuiltinEditorPrompt = `# Code Editor Agent

You make precise, surgical code changes. The smallest correct change that
solves the problem, following existing codebase patterns.

## Methodology
1. Understand before changing — read the code, learn the patterns.
2. Minimize the change — three targeted lines beat a refactor.
3. Follow existing conventions — match naming, structure, and style.
4. Verify — confirm no errors were introduced, code is properly formatted.

## Safety
- Never edit files you haven't read.
- Never refactor outside the task's scope.
- If a change touches more than 3 files or 50 lines, pause and confirm.
- If unsure about existing behavior, ask before changing.`

// BuiltinTesterPrompt is the initial seed for tester.md.
// Role identity and testing methodology — tool usage (LSP etc.) is injected by the framework.
const BuiltinTesterPrompt = `# Code Tester Agent

You ensure code changes are correct and don't break existing functionality.
Write robust tests, run them, and report results.

## Methodology
1. Understand the change — read the modified code and its callers.
2. Prioritize — critical path first, then edge cases, then regression.
3. Follow existing test patterns — same framework, naming, and structure.
4. Run and verify — all tests must pass before reporting completion.

## Boundaries
- Never modify production code except to enable testing.
- If existing tests fail, report them — don't silently fix or remove.
- If no test framework exists, report that and suggest one.`

type BuiltinTeamInstallStatus string

const (
	BuiltinTeamAvailable BuiltinTeamInstallStatus = "available"
	BuiltinTeamPartial   BuiltinTeamInstallStatus = "partial"
	BuiltinTeamInstalled BuiltinTeamInstallStatus = "installed"
	BuiltinTeamConflict  BuiltinTeamInstallStatus = "conflict"
)

var ErrBuiltinTeamConflict = errors.New("teamstore: built-in team conflict")

type BuiltinAgentSpec struct {
	ID           string
	Name         string
	Description  string
	IsLeader     bool
	SystemPrompt string
	MCPServers   []string
	SkillIDs     []string
}

type BuiltinTeamSpec struct {
	ID          string
	Name        string
	DisplayName string
	Description string
	Agents      []BuiltinAgentSpec
}

type BuiltinTeamView struct {
	Spec          BuiltinTeamSpec
	Status        BuiltinTeamInstallStatus
	MissingAgents []string
	Conflicts     []string
}

type BuiltinInstallResult struct {
	ID            string                   `json:"id"`
	Status        BuiltinTeamInstallStatus `json:"status"`
	CreatedTeam   bool                     `json:"created_team"`
	CreatedAgents []string                 `json:"created_agents"`
}

var builtinTeams = []BuiltinTeamSpec{
	{
		ID:          "engineering",
		Name:        "engineering",
		DisplayName: "Engineering Team",
		Description: "Engineering group responsible for architecture design, fullstack development, and quality assurance. Explorer discovers, Editor implements, Tester validates.",
		Agents: []BuiltinAgentSpec{
			{
				ID:           "andrej karpathy",
				Name:         "Andrej Karpathy",
				Description:  "Principal Architect responsible for task breakdown, architectural decisions, and technical leadership.",
				IsLeader:     true,
				SystemPrompt: BuiltinLeaderPrompt,
				MCPServers:   []string{"builtin-lsp"},
			},
			{
				ID:           "explorer",
				Name:         "explorer",
				Description:  "Code Explorer responsible for searching the codebase, tracing dependencies, understanding architecture, and reporting structured findings. Read-only — never modifies code.",
				SystemPrompt: BuiltinExplorerPrompt,
				MCPServers:   []string{"builtin-lsp"},
			},
			{
				ID:           "editor",
				Name:         "editor",
				Description:  "Code Editor responsible for precise, surgical code changes following existing patterns. Implements features and fixes bugs with minimal, clean edits.",
				SystemPrompt: BuiltinEditorPrompt,
				MCPServers:   []string{"builtin-lsp"},
			},
			{
				ID:           "tester",
				Name:         "tester",
				Description:  "Code Tester responsible for writing and running tests, measuring coverage, finding regressions, and reporting structured test results.",
				SystemPrompt: BuiltinTesterPrompt,
				MCPServers:   []string{"builtin-lsp"},
			},
		},
	},
}

func BuiltinTeamCatalog() []BuiltinTeamSpec {
	result := make([]BuiltinTeamSpec, len(builtinTeams))
	for i, spec := range builtinTeams {
		result[i] = spec
		result[i].Agents = append([]BuiltinAgentSpec(nil), spec.Agents...)
	}
	return result
}

func BuiltinTeamByID(id string) (BuiltinTeamSpec, bool) {
	for _, spec := range builtinTeams {
		if strings.EqualFold(spec.ID, id) {
			return spec, true
		}
	}
	return BuiltinTeamSpec{}, false
}

func (s *Store) ListBuiltinTeamStatuses(ctx context.Context) ([]BuiltinTeamView, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]BuiltinTeamView, 0, len(builtinTeams))
	for _, spec := range builtinTeams {
		view, err := s.builtinTeamStatusLocked(spec)
		if err != nil {
			return nil, err
		}
		result = append(result, view)
	}
	return result, nil
}

func (s *Store) InstallBuiltinTeams(ctx context.Context, ids []string) ([]BuiltinInstallResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	seen := make(map[string]struct{}, len(ids))
	specs := make([]BuiltinTeamSpec, 0, len(ids))
	for _, id := range ids {
		key := strings.ToLower(strings.TrimSpace(id))
		if key == "" {
			return nil, fmt.Errorf("teamstore: built-in team id is required")
		}
		if _, ok := seen[key]; ok {
			continue
		}
		spec, ok := BuiltinTeamByID(key)
		if !ok {
			return nil, fmt.Errorf("teamstore: unknown built-in team %q", id)
		}
		seen[key] = struct{}{}
		specs = append(specs, spec)
	}
	if len(specs) == 0 {
		return nil, fmt.Errorf("teamstore: at least one built-in team is required")
	}

	for _, spec := range specs {
		view, err := s.builtinTeamStatusLocked(spec)
		if err != nil {
			return nil, err
		}
		if view.Status == BuiltinTeamConflict {
			return nil, fmt.Errorf("%w: %s: %s", ErrBuiltinTeamConflict, spec.ID, strings.Join(view.Conflicts, "; "))
		}
	}

	createdPaths := make([]string, 0)
	rollback := func() {
		for i := len(createdPaths) - 1; i >= 0; i-- {
			_ = os.Remove(createdPaths[i])
		}
	}

	results := make([]BuiltinInstallResult, 0, len(specs))
	for _, spec := range specs {
		result := BuiltinInstallResult{ID: spec.ID, Status: BuiltinTeamInstalled}
		teamPath := getTeamFilePath(s.groupsDir, spec.Name)
		if _, err := os.Stat(teamPath); os.IsNotExist(err) {
			now := time.Now().Format(time.RFC3339)
			team := &Team{
				ID:          spec.ID,
				Name:        spec.Name,
				Description: spec.Description,
				CreatedAt:   now,
				UpdatedAt:   now,
			}
			if err := s.writeTeamFile(teamPath, team); err != nil {
				rollback()
				return nil, fmt.Errorf("teamstore: install built-in team %s: %w", spec.ID, err)
			}
			createdPaths = append(createdPaths, teamPath)
			result.CreatedTeam = true
		}

		for _, agentSpec := range spec.Agents {
			agentPath := getAgentFilePath(s.agentsDir, agentSpec.Name)
			if _, err := os.Stat(agentPath); !os.IsNotExist(err) {
				continue
			}
			now := time.Now().Format(time.RFC3339)
			a := &Agent{
				ID:           agentSpec.ID,
				Name:         agentSpec.Name,
				Description:  agentSpec.Description,
				TeamName:     spec.Name,
				IsLeader:     agentSpec.IsLeader,
				SystemPrompt: agentSpec.SystemPrompt,
				MCPServers:   append([]string(nil), agentSpec.MCPServers...),
				SkillIDs:     append([]string(nil), agentSpec.SkillIDs...),
				CreatedAt:    now,
				UpdatedAt:    now,
			}
			if err := s.writeAgentFile(agentPath, a); err != nil {
				rollback()
				return nil, fmt.Errorf("teamstore: install built-in agent %s: %w", agentSpec.Name, err)
			}
			createdPaths = append(createdPaths, agentPath)
			result.CreatedAgents = append(result.CreatedAgents, agentSpec.Name)
		}
		results = append(results, result)
	}

	return results, nil
}

// EnsureBuiltinTechTeam is retained for callers that explicitly request the legacy team.
func (s *Store) EnsureBuiltinTechTeam(ctx context.Context) error {
	_, err := s.InstallBuiltinTeams(ctx, []string{"engineering"})
	return err
}

func (s *Store) builtinTeamStatusLocked(spec BuiltinTeamSpec) (BuiltinTeamView, error) {
	view := BuiltinTeamView{Spec: spec}
	present := 0
	expected := 1 + len(spec.Agents)

	teamPath := getTeamFilePath(s.groupsDir, spec.Name)
	if info, err := os.Stat(teamPath); err == nil {
		present++
		team, parseErr := parseTeamFile(teamPath, info)
		if parseErr != nil {
			view.Conflicts = append(view.Conflicts, fmt.Sprintf("team %s cannot be parsed", spec.Name))
		} else if !strings.EqualFold(team.Name, spec.Name) || !strings.EqualFold(team.ID, spec.ID) {
			view.Conflicts = append(view.Conflicts, fmt.Sprintf("team %s has incompatible identity", spec.Name))
		}
	} else if !os.IsNotExist(err) {
		return BuiltinTeamView{}, err
	}

	for _, agentSpec := range spec.Agents {
		agentPath := getAgentFilePath(s.agentsDir, agentSpec.Name)
		info, err := os.Stat(agentPath)
		if os.IsNotExist(err) {
			view.MissingAgents = append(view.MissingAgents, agentSpec.Name)
			continue
		}
		if err != nil {
			return BuiltinTeamView{}, err
		}
		present++
		a, parseErr := parseAgentFile(agentPath, info)
		if parseErr != nil {
			view.Conflicts = append(view.Conflicts, fmt.Sprintf("agent %s cannot be parsed", agentSpec.Name))
			continue
		}
		if !strings.EqualFold(a.ID, agentSpec.ID) ||
			!strings.EqualFold(a.Name, agentSpec.Name) ||
			!strings.EqualFold(a.TeamName, spec.Name) ||
			a.IsLeader != agentSpec.IsLeader {
			view.Conflicts = append(view.Conflicts, fmt.Sprintf("agent %s has incompatible identity or team", agentSpec.Name))
		}
	}

	switch {
	case len(view.Conflicts) > 0:
		view.Status = BuiltinTeamConflict
	case present == 0:
		view.Status = BuiltinTeamAvailable
	case present == expected:
		view.Status = BuiltinTeamInstalled
	default:
		view.Status = BuiltinTeamPartial
	}
	return view, nil
}

func (s *Store) isBuiltinTeamLocked(name string) bool {
	for _, spec := range builtinTeams {
		if !strings.EqualFold(spec.Name, name) {
			continue
		}
		view, err := s.builtinTeamStatusLocked(spec)
		return err == nil && (view.Status == BuiltinTeamInstalled || view.Status == BuiltinTeamPartial)
	}
	return false
}

func (s *Store) isBuiltinAgentLocked(name string) bool {
	for _, spec := range builtinTeams {
		for _, agentSpec := range spec.Agents {
			if !strings.EqualFold(agentSpec.Name, name) {
				continue
			}
			path := getAgentFilePath(s.agentsDir, name)
			info, err := os.Stat(path)
			if err != nil {
				return false
			}
			a, err := parseAgentFile(path, info)
			return err == nil &&
				strings.EqualFold(a.Name, agentSpec.Name) &&
				strings.EqualFold(a.TeamName, spec.Name) &&
				a.IsLeader == agentSpec.IsLeader
		}
	}
	return false
}
