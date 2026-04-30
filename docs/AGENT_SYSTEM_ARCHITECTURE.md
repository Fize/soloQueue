# Agent System Architecture

## Overview

`internal/agent` is the execution core of SoloQueue. It turns an LLM client, a tool registry, and optional skill/delegation capabilities into a long-lived actor-like runtime that accepts jobs through a mailbox and emits typed stream events.

At a high level, the agent layer is responsible for:

- lifecycle management of long-lived agents
- serial job execution through mailbox semantics
- the LLM tool-use loop
- tool execution, confirmation, timeout, and delegation orchestration
- constructing specialized L2/L3 agents from templates

The main runtime wires this layer in `cmd/soloqueue/main.go`: the runtime stack builds a shared LLM client and `agent.Registry`, then uses `agent.NewDefaultFactory(...)` to instantiate template-based agents and session-scoped L1 agents ([main.go](../cmd/soloqueue/main.go) is the entrypoint; relevant construction is around `buildRuntimeStack` and `sessionBuilder.Build`).

## Core Model

The foundational static model is `Definition` in [internal/agent/types.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/types.go#L23). It captures immutable agent attributes for one runtime instance:

- identity: `ID`, `Name`
- role/kind: `Role`, `Kind`
- model parameters: `ModelID`, `Temperature`, `MaxTokens`
- reasoning flags: `ThinkingEnabled`, `ReasoningEffort`
- loop limits: `MaxIterations`, `ContextWindow`
- override policy: `ExplicitModel`

`Agent` itself, defined in [internal/agent/agent.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/agent.go#L41), mixes immutable configuration with runtime state:

- immutable dependencies: `LLM`, logger, tool registry, skill registry
- execution controls: `parallelTools`, per-tool timeouts, confirmation store
- runtime lifecycle fields: `ctx`, `cancel`, `mailbox`, `done`
- async delegation state: `asyncTurns`
- priority execution path: `priorityMailbox`
- per-ask model override: `modelOverride`

This split matters: configuration is injected once through functional options, while run-state is rebuilt on every `Start()`.

## Lifecycle And Actor Model

The package explicitly models an agent as a long-lived actor. The lifecycle is documented in [internal/agent/agent.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/agent.go#L29):

`NewAgent -> Start -> Ask/Submit -> Stop`, with restart allowed after stop.

### Start/Stop

`Start` in [internal/agent/lifecycle.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/lifecycle.go#L14) allocates a fresh runtime context, resets state, initializes either a plain mailbox or a priority mailbox, and launches the main run loop goroutine.

`Stop` in [internal/agent/lifecycle.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/lifecycle.go#L58) cancels the runtime context, waits for the run loop to exit, and supports timeout-bounded shutdown.

Two run loops exist:

- `run` in [internal/agent/run.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/run.go#L17) for a single FIFO mailbox
- `runWithPriorityMailbox` in [internal/agent/run.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/run.go#L50) for L1-style agents that must prioritize delegation completions over normal asks

Both loops preserve the actor invariant: one job executes at a time per agent.

### Jobs, Ask, And Submit

The internal unit of work is `job`, a closure type declared in [internal/agent/agent.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/agent.go#L17). Public APIs convert higher-level operations into mailbox jobs:

- `Ask` and `AskStream` for prompt-driven LLM execution
- `AskWithHistory` and `AskStreamWithHistory` for ContextWindow-backed sessions
- `Submit` for arbitrary custom work

The mailbox-facing APIs live in [internal/agent/ask.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/ask.go#L12). Key design points:

- submission is concurrent-safe
- execution is serialized inside the run goroutine
- caller cancellation and agent shutdown both propagate through merged contexts
- the streaming API is primary; blocking APIs are wrappers over event accumulation

This is the first major architectural decision in the package: streaming is the canonical execution path, not a side path.

## Event Architecture

Agent output is represented as a sealed event stream defined in [internal/agent/events.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/events.go#L12). The package uses concrete event structs instead of untyped maps or interface-heavy payloads.

Important event families:

- LLM output: `ContentDeltaEvent`, `ReasoningDeltaEvent`, `DoneEvent`, `ErrorEvent`
- tool protocol: `ToolCallDeltaEvent`, `ToolExecStartEvent`, `ToolExecDoneEvent`
- loop accounting: `IterationDoneEvent`
- human approval: `ToolNeedsConfirmEvent`
- async delegation control: `DelegationStartedEvent`, `DelegationCompletedEvent`

Every event also implements the shared `iface.AgentEvent` and `iface.EventConsumer` contracts, which is how `tools` can consume child-agent streams without importing concrete agent types.

This event model is the contract boundary between:

- agent and session/TUI/server layers
- parent and child agents during delegation
- agent and tools package during confirm forwarding

## LLM Execution Loop

The core streaming/tool loop is `streamLoop` in [internal/agent/stream.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/stream.go#L158).

Its loop shape is:

1. build messages for the current iteration
2. call `LLM.ChatStream`
3. accumulate output/tool-call deltas
4. emit stream events to the caller
5. if no tool calls: emit `DoneEvent`
6. otherwise execute tools and feed results back into the next iteration

### Strategy-Based Unification

The package has two main execution modes:

- simple in-memory ask flow
- ContextWindow-backed session flow

Instead of duplicating three almost-identical loops, `streamLoop` delegates differences to `streamStrategy` ([internal/agent/stream.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/stream.go#L128)). There are two concrete strategies:

- `simpleStrategy` for `AskStream`
- `historyStrategy` for `AskStreamWithHistory`

`historyStrategy` adds session-grade behavior:

- hard overflow checks against model context window
- prompt token calibration back into the `ContextWindow`
- pushing assistant/tool messages into conversation state
- async delegation yielding via `DelegationStartedEvent`

This keeps the package architecturally coherent: the LLM tool loop is one mechanism with pluggable storage semantics.

## Tool Execution Architecture

### Tool Registration

Agents receive tools through `WithTools(...)` in [internal/agent/agent.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/agent.go#L128). Internally this populates a `tools.ToolRegistry`.

At request time, `streamLoop` converts the registry to `[]llm.ToolDef` and exposes it to the provider.

### Synchronous Tool Path

The plain tool executor is `execTools` plus `execToolStream` in [internal/agent/stream.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/stream.go#L541) and [internal/agent/stream.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/stream.go#L578).

`execToolStream` handles the full execution contract for one tool call:

- tool lookup
- start/done event emission
- confirmation gating for `tools.Confirmable`
- per-tool timeout wrapping
- event-relay context injection for delegated child streams
- result normalization back into `tool` role content

Errors are intentionally not fatal to the whole ask. The architecture treats tool failure as model-visible business feedback, not as a system-level abort.

### Parallel Tool Calls

If `WithParallelTools(true)` is enabled, one LLM iteration with multiple tool calls uses `errgroup` parallelism in [internal/agent/stream.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/stream.go#L557).

Important invariant:

- event ordering across different tool calls is nondeterministic
- result ordering fed back to the LLM is still aligned with the original `tool_calls` sequence

This is achieved by writing each result into a fixed index of the `results` slice.

### Confirmation Pipeline

The confirmation state machine is centered around `pendingConfirm` slots in [internal/agent/agent.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/agent.go#L65) and used from `execToolStream`.

The flow is:

1. a `Confirmable` tool indicates confirmation is needed
2. the agent emits `ToolNeedsConfirmEvent`
3. the tool call blocks on a per-call confirmation slot
4. UI or server code calls `Agent.Confirm(callID, choice)`
5. execution resumes with potentially rewritten args

The same mechanism is reused across delegation layers through confirm forwarders injected into tool execution context.

## Async Delegation Architecture

Async delegation is the most specialized part of the agent package. It is implemented in [internal/agent/async_turn.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/async_turn.go#L35).

### Why It Exists

L1 session agents need to delegate tasks to L2 leaders without monopolizing the session mailbox for the full child execution time. That requires yielding the current turn, letting the user continue interacting, and resuming the tool loop later.

### Main Structures

- `delegatedTask`: one async tool call tracker
- `asyncTurnState`: one iteration-wide aggregation object holding all tool call results for that iteration

### Execution Flow

`execToolsWithAsync` performs a two-phase orchestration:

1. scan tool calls and detect which tools implement `tools.AsyncTool`
2. build a complete `asyncTurnState` before any goroutine starts
3. register the turn in `a.asyncTurns`
4. launch async actions
5. launch watchers that wait for completion

When the LLM iteration finishes, `historyStrategy.postIteration` checks whether `a.asyncTurns[iter]` exists ([internal/agent/stream.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/stream.go#L379)). If yes, it emits `DelegationStartedEvent` and returns `yield=true`, leaving the output channel open.

After all async tasks finish, `watchDelegatedTask` triggers `resumeTurn` through a high-priority mailbox submission ([internal/agent/async_turn.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/async_turn.go#L279)). `resumeTurn` then:

1. removes the async turn registration
2. injects all tool results into the `ContextWindow`
3. emits `DelegationCompletedEvent`
4. continues the LLM loop from the next iteration

This is effectively continuation-passing over a mailbox actor.

## Factory, Templates, And Hierarchical Agents

Template-driven construction lives in [internal/agent/factory.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/factory.go#L18).

### AgentTemplate

`AgentTemplate` captures markdown-defined agents loaded from `~/.soloqueue/agents/*.md`.

It contains:

- identity and description
- prompt body
- explicit model binding
- leader/worker role flags
- group membership and MCP server declarations

### DefaultFactory Responsibilities

`DefaultFactory.Create` assembles a complete running agent instance:

1. build final prompt for L2 or L3
2. resolve model parameters from config
3. build built-in tools from `tools.Config`
4. inject `delegate_*` tools for leaders
5. load and register skills
6. optionally register a `Skill` tool with fork support
7. create the agent
8. create a `ContextWindow`
9. register the agent in the registry
10. start it

The factory is therefore not only an object builder. It is also the integration layer between config, tools, skills, registry, and prompt systems.

### L2/L3 Prompt Specialization

The same file also defines prompt synthesis rules:

- `buildL2SystemPrompt(...)` produces a supervisor prompt with user prompt, team context, available workers, and enforced framework rules
- `buildL3SystemPrompt(...)` produces worker prompts with stricter execution constraints

This means architecture is split between code and prompt contracts. Agent roles are not defined only by Go structs; they are also defined by generated system prompt composition.

## Registry And Supervision

### Registry

`Registry` in [internal/agent/registry.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/registry.go#L18) is the authoritative in-process `id -> *Agent` map.

It is intentionally narrow in scope:

- owns lookup and listing
- supports batch start/stop/shutdown
- does not hide lifecycle side effects behind registration

It also implements `iface.AgentLocator`, allowing the `tools.DelegateTool` to resolve target agents without importing concrete agent types.

### Supervisor

`Supervisor` in [internal/agent/supervisor.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/supervisor.go#L15) manages child-agent lifecycle for L2 leaders.

Its job is not scheduling. Scheduling is still handled by the tool loop. Supervisor only handles:

- spawning L3 workers through the factory
- tracking them in `children`
- reaping them on completion or shutdown

This separation is useful: delegation semantics stay in the tool/agent loop, while child lifetime ownership stays in supervisor.

## Session Integration Boundary

The agent layer does not own conversation lifetime. That belongs to `internal/session`. But it exposes the mechanisms session depends on:

- `AskStreamWithHistory(...)` for ContextWindow-backed execution
- `DelegationStartedEvent` / `DelegationCompletedEvent` to coordinate asynchronous turns
- `LocatableAdapter` to convert concrete agent streams into the shared `iface.Locatable` contract ([internal/agent/registry.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/registry.go#L207))

That boundary is important architecturally:

- `agent` owns execution
- `session` owns conversation ordering and multi-turn context mutation
- `tui` and `server` consume only the event protocol

## Code Layout Summary

`internal/agent` is organized by concern rather than by type:

- lifecycle and runtime: `agent.go`, `lifecycle.go`, `run.go`, `types.go`, `errors.go`
- ask/submission APIs: `ask.go`
- stream/tool loop: `stream.go`, `llm.go`
- delegation and continuation: `async_turn.go`
- confirmation: `confirm.go`
- template/factory/supervision: `factory.go`, `registry.go`, `supervisor.go`
- tests grouped by feature: `*_test.go`

That structure is consistent with the architecture: the package is a runtime subsystem, not a pure data model package.

## Architectural Strengths

- Strong actor semantics simplify concurrency reasoning: one agent, one active job.
- Streaming-first design avoids duplicate blocking vs streaming implementations.
- Event typing is explicit and stable across package boundaries.
- Async delegation is continuation-based rather than thread-blocking.
- Factory centralizes multi-system assembly instead of spreading wiring across callers.

## Architectural Tradeoffs

- The package is a high-responsibility module: lifecycle, execution loop, tool orchestration, delegation, factory logic, and prompt assembly all live here.
- Prompt synthesis for L2/L3 is code-coupled to runtime creation, which is practical but mixes behavioral policy with construction logic.
- Async delegation relies on shared conventions between `agent`, `session`, `tools`, and `iface`, so regressions tend to be cross-package rather than local.

## Files To Read First

- [internal/agent/agent.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/agent.go)
- [internal/agent/ask.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/ask.go)
- [internal/agent/stream.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/stream.go)
- [internal/agent/async_turn.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/async_turn.go)
- [internal/agent/factory.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/factory.go)
- [internal/agent/registry.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/registry.go)
- [internal/agent/supervisor.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/supervisor.go)
