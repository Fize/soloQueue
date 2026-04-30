# Skill System Architecture

## Overview

The skill system in `internal/skill` adds a second abstraction layer above raw tools. A tool is a low-level executable primitive. A skill is a reusable task recipe with instructions, optional preprocessing, and an execution mode.

The package comment in [internal/skill/skill.go](/Users/xiaobaitu/github.com/soloQueue/internal/skill/skill.go#L1) defines the intent clearly:

- `Skill` is an immutable definition, not an interface
- `SkillTool` exposes skills to the LLM through function calling
- skills support `inline` and `fork` execution modes
- skills can restrict their allowed tools and preprocess content

Architecturally, the skill layer bridges three worlds:

- markdown-defined reusable procedures
- LLM-visible function-calling surface
- optional child-agent execution sandboxes

## Core Data Model

The canonical data object is `Skill` in [internal/skill/skill.go](/Users/xiaobaitu/github.com/soloQueue/internal/skill/skill.go#L36).

Important fields:

- `ID`: unique stable skill identifier
- `Description`: short discovery text for users and LLMs
- `Instructions`: full skill content
- `AllowedTools`: optional whitelist patterns
- `DisableModelInvocation`: hide from automatic model selection
- `UserInvocable`: control slash-command visibility
- `Context`: execution mode, especially `fork`
- `Agent`: metadata for forked execution profile
- `Category`, `FilePath`, `Dir`: provenance information

The design is intentionally data-oriented. A skill is not behavior itself. Behavior is applied later by `SkillTool` and preprocess/fork helpers.

## Registry Layer

`SkillRegistry` in [internal/skill/skill.go](/Users/xiaobaitu/github.com/soloQueue/internal/skill/skill.go#L143) is a simple concurrent-safe `id -> *Skill` map.

Its job is deliberately narrow:

- registration
- duplicate prevention
- lookup by ID
- stable ordered snapshot for description generation

Unlike the tool registry, it does not produce provider-facing specs directly. Skills become visible to the LLM only through the single `SkillTool` adapter.

That is a key architectural decision: the model does not see each skill as its own function. It sees one dispatch tool named `Skill`.

## SkillTool As The Execution Adapter

`SkillTool` in [internal/skill/skill_tool.go](/Users/xiaobaitu/github.com/soloQueue/internal/skill/skill_tool.go#L12) is the adapter that turns the registry into a callable tool.

### LLM Surface

The tool exposes one schema:

- `skill`: the skill name
- `args`: optional string arguments

The tool description is generated dynamically from registry contents in [internal/skill/skill_tool.go](/Users/xiaobaitu/github.com/soloQueue/internal/skill/skill_tool.go#L43). This is important because the LLM only has one function name, so the description must enumerate discoverable skills.

### Execution Flow

`SkillTool.Execute(...)` performs four steps:

1. parse tool arguments
2. resolve the `Skill` from the registry
3. preprocess the instruction content
4. dispatch by execution mode

Dispatch has two branches:

- `inline`: return transformed instruction content to the parent agent
- `fork`: execute in a child agent if a spawn function is available

This means skills are not directly side-effecting by default. Inline mode is more like prompt injection as a tool result; the model then continues the task using ordinary tools.

## Execution Modes

### Inline Mode

Inline mode is the default branch in [internal/skill/skill_tool.go](/Users/xiaobaitu/github.com/soloQueue/internal/skill/skill_tool.go#L99).

In this mode, the skill system returns the fully preprocessed instruction body as the tool result. The same agent then continues execution in the next tool-use iteration.

Use this when:

- the skill is primarily instruction reuse
- isolation is unnecessary
- the caller should retain full context and full toolset

### Fork Mode

Fork mode is implemented through `ExecuteFork(...)` in [internal/skill/fork.go](/Users/xiaobaitu/github.com/soloQueue/internal/skill/fork.go#L25).

The parent does not create child agents directly. Instead, `SkillTool` depends on an injected `SkillForkSpawnFn`, which is supplied by the factory/runtime layer.

Fork execution flow:

1. spawn a temporary child agent using preprocessed skill content as system prompt
2. send either `args` or a default prompt to the child
3. consume the child stream and accumulate content deltas
4. cleanup the child agent

This is architecturally clean because `internal/skill` depends only on `iface.Locatable`, not on concrete agent construction.

## Preprocessing Pipeline

The skill preprocessor is defined in [internal/skill/preprocess.go](/Users/xiaobaitu/github.com/soloQueue/internal/skill/preprocess.go#L15).

`PreprocessContent(...)` applies three ordered transformations:

1. `$ARGUMENTS` substitution
2. shell expansion for ``!`command` ``
3. file reference expansion for `@path`

### Why Order Matters

The pipeline order is part of the architecture, not just implementation detail:

- arguments are substituted before command execution so commands can use them
- shell output is resolved before file inclusion so included references see the latest expanded text

### Shell Expansion

`expandShellCommands(...)` runs shell commands with a timeout and optional working directory. Failures collapse to an empty string rather than aborting skill execution.

That design favors best-effort template expansion over strict failure semantics.

### File Inclusion

`expandFileRefs(...)` resolves relative paths against `skill.Dir` and inlines file contents. Failures are rendered as textual placeholders.

This makes skills partly self-documenting and composable with colocated files.

## SKILL.md Loading

External user skills are loaded from markdown files through [internal/skill/skill_md.go](/Users/xiaobaitu/github.com/soloQueue/internal/skill/skill_md.go#L12).

### File Format

Each skill may define YAML frontmatter plus markdown body. Parsed fields include:

- `name`
- `description`
- `allowed-tools`
- `disable-model-invocation`
- `user-invocable`
- `context`
- `agent`

### Loading Model

The loader scans directories for `SKILL.md` files and converts them into `Skill` structs. There are two levels:

- `LoadSkillsFromDir(...)` for one root directory
- `LoadSkillsFromDirs(...)` for layered scopes with override precedence: `plugin -> user -> project`

The multi-directory override model is significant architecturally because it gives the system a controlled customization stack without changing runtime code.

## Allowed-Tools Filtering

Tool restriction for forked skills is implemented in [internal/skill/fork.go](/Users/xiaobaitu/github.com/soloQueue/internal/skill/fork.go#L63).

`FilterTools(...)` takes a full tool list and a whitelist pattern set, then keeps only matching tools.

Supported patterns are intentionally lightweight:

- exact tool names
- names with argument-like suffix syntax such as `Bash(git:*)`
- MCP-style prefixes like `mcp__server`

Current enforcement is coarse-grained by tool name. Path-level or command-prefix constraints are not deeply enforced in the filter itself.

Architecturally, this means `AllowedTools` is currently a capability-reduction hint for the spawned child agent, not a full policy engine.

## Runtime Integration

The skill system is integrated in two places.

### Factory-Created Agents

`DefaultFactory.Create(...)` in [internal/agent/factory.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/factory.go#L143) loads skills from the configured skill directory, registers them into a `SkillRegistry`, and appends a `SkillTool` if at least one skill exists.

For fork mode, the factory injects a `forkSpawn` function that:

- creates a temporary child agent
- optionally filters tools by `AllowedTools`
- starts the child
- returns a cleanup function

### Session L1 Agents

The session builder in [cmd/soloqueue/main.go](/Users/xiaobaitu/github.com/soloQueue/cmd/soloqueue/main.go#L426) also registers a `SkillTool` for the session agent and provides a similar fork-spawn closure.

So skills are available both in template-created agents and in session-scoped user-facing agents.

## Architectural Role Of Skills Relative To Tools

The best way to understand the package is:

- tools are primitive capabilities
- skills are reusable procedures or execution recipes built on top of those capabilities

Examples of what skills add that tools do not:

- reusable instruction bundles
- content templating with runtime expansion
- tool-scope reduction in forked execution
- alternate isolated execution mode

This is why the package depends on `tools`, but not vice versa.

## Code Layout Summary

The skill package is structured around four responsibilities:

- core types and registry: `skill.go`
- LLM-facing adapter: `skill_tool.go`
- isolated execution and filtering: `fork.go`
- markdown loading and frontmatter parsing: `skill_md.go`
- instruction preprocessing: `preprocess.go`
- tests by feature: `*_test.go`

## Architectural Strengths

- Strong separation between immutable skill definition and execution adapter.
- `SkillTool` gives one stable LLM surface regardless of skill count.
- Fork mode avoids direct package coupling to concrete agent types.
- Markdown loading with scope precedence supports user/project overrides cleanly.
- Preprocessing allows skills to act as executable templates rather than static prompt snippets.

## Architectural Tradeoffs

- Dynamic description generation can become noisy as skill count grows.
- `AllowedTools` filtering is capability narrowing, not full policy enforcement.
- Preprocessing runs shell commands and file reads during skill expansion, which is powerful but adds hidden execution behavior inside what looks like content loading.
- Fork mode currently accumulates only content deltas from the child and ignores richer event semantics.

## Files To Read First

- [internal/skill/skill.go](/Users/xiaobaitu/github.com/soloQueue/internal/skill/skill.go)
- [internal/skill/skill_tool.go](/Users/xiaobaitu/github.com/soloQueue/internal/skill/skill_tool.go)
- [internal/skill/fork.go](/Users/xiaobaitu/github.com/soloQueue/internal/skill/fork.go)
- [internal/skill/skill_md.go](/Users/xiaobaitu/github.com/soloQueue/internal/skill/skill_md.go)
- [internal/skill/preprocess.go](/Users/xiaobaitu/github.com/soloQueue/internal/skill/preprocess.go)
