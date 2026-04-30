# Tool System Architecture

## Overview

The tool system in `internal/tools` provides the executable primitive layer for the entire runtime. Every tool maps directly to one LLM function-calling entry.

The package comment in [internal/tools/tool.go](/Users/xiaobaitu/github.com/soloQueue/internal/tools/tool.go#L1) captures the design:

- `Tool` is the minimal callable unit
- `Confirmable` adds user-approval semantics
- `AsyncTool` adds async delegation intent
- `ToolRegistry` manages tool lookup and provider-facing spec generation

Architecturally, tools are the base execution substrate under skills and agents.

## Core Contracts

### Tool

The main interface is `Tool` in [internal/tools/tool.go](/Users/xiaobaitu/github.com/soloQueue/internal/tools/tool.go#L25).

A tool must provide:

- `Name()`
- `Description()`
- `Parameters()` as JSON Schema
- `Execute(ctx, args)`

Important behavioral contract from the comments:

- tool metadata is effectively immutable after registration
- `Execute` must be concurrency-safe because multiple agents may share the same tool instance
- tools should respect context cancellation, but the agent layer can add outer timeouts

### Confirmable

`Confirmable` in [internal/tools/tool.go](/Users/xiaobaitu/github.com/soloQueue/internal/tools/tool.go#L56) is a behavioral extension for dangerous operations.

It lets the tool declare:

- whether confirmation is needed
- the prompt/options shown to the user
- whether session-wide whitelisting is supported
- how to mutate args after a user choice

This keeps confirmation policy close to the tool implementation rather than pushing it into UI code or ad hoc command lists.

### AsyncTool

`AsyncTool` in [internal/tools/tool.go](/Users/xiaobaitu/github.com/soloQueue/internal/tools/tool.go#L117) is not an execution interface in the usual sense. It returns an `AsyncAction` intent object.

That is a major design choice: async tools do not start goroutines themselves. They only declare:

- target child agent
- prompt to send
- timeout to use

The agent framework then owns scheduling, lifecycle, bookkeeping, and continuation.

## Shared Configuration Model

`tools.Config` in [internal/tools/config.go](/Users/xiaobaitu/github.com/soloQueue/internal/tools/config.go#L28) is the shared runtime configuration for all built-in tools.

It centralizes limits and policies for:

- filesystem sandboxing
- read/write size limits
- HTTP fetch restrictions
- shell restrictions and timeout
- web search timeout

This is important architecturally because individual tool implementations stay small and focused; cross-cutting policy is injected once through config.

### Tool Construction

The canonical constructor is `Build(cfg)` in [internal/tools/config.go](/Users/xiaobaitu/github.com/soloQueue/internal/tools/config.go#L104). It returns the built-in tool set in a stable order.

The main runtime converts global config to `tools.Config` through [internal/config/tools_convert.go](/Users/xiaobaitu/github.com/soloQueue/internal/config/tools_convert.go#L9), then passes the resulting tools into agents.

So the dependency chain is:

`config.Settings -> config.ToolsConfig -> tools.Config -> tools.Build -> agent.WithTools`

## Registry Layer

`ToolRegistry` in [internal/tools/registry.go](/Users/xiaobaitu/github.com/soloQueue/internal/tools/registry.go#L24) is a concurrent-safe name-based registry.

Its responsibilities are:

- reject nil or duplicate tools
- look up tools by name
- generate stable `[]llm.ToolDef` snapshots via `Specs()`

The most important architectural role of the registry is provider exposure. It is the source of truth from which the agent constructs `LLMRequest.Tools`.

That keeps tool execution lookup and provider declaration consistent by design.

## Built-In Tool Families

The current built-in tools returned by `Build(cfg)` are:

- file tools: read, grep, glob, write, replace, multi-replace, multi-write
- network tools: `WebFetch`, `WebSearch`
- command tool: shell execution

The package uses a flat file layout: one tool per file plus tests. This is explicitly documented in [internal/tools/config.go](/Users/xiaobaitu/github.com/soloQueue/internal/tools/config.go#L3).

### Example: WebFetch

`httpFetchTool` in [internal/tools/http_fetch.go](/Users/xiaobaitu/github.com/soloQueue/internal/tools/http_fetch.go#L16) is a representative example of the package style.

It combines:

- schema-based input parsing
- SSRF restrictions
- host allow-list support
- response-size truncation
- `http.Client` timeout
- optional confirmation support via `Confirmable`

This is typical of tool design in the repo: tool-local execution logic plus shared agent-level orchestration for timeouts and confirmation.

## Delegation Tool As A Special Tool

`DelegateTool` in [internal/tools/delegate.go](/Users/xiaobaitu/github.com/soloQueue/internal/tools/delegate.go#L42) is the most architecturally significant tool because it turns tool calling into agent-to-agent orchestration.

### Two Operating Modes

One struct supports both:

- synchronous L2 -> L3 delegation through `Execute`
- asynchronous L1 -> L2 delegation through `ExecuteAsync`

The mode is determined by how the runtime wires the tool:

- `Locator` means resolve an already-running target agent
- `SpawnFn` means dynamically obtain the target and enable async mode

### Synchronous Delegation

`Execute(...)` in [internal/tools/delegate.go](/Users/xiaobaitu/github.com/soloQueue/internal/tools/delegate.go#L116) does the following:

1. parse `task`
2. resolve or spawn a target agent
3. apply a delegation timeout
4. forward model overrides if present
5. call child `AskStream`
6. relay child confirm events upward if the parent injected relay context
7. accumulate content/error and return the final result

This makes delegation look like just another tool call to the parent LLM loop, which is a strong architectural simplification.

### Asynchronous Delegation

`ExecuteAsync(...)` does not run the task. It produces an `AsyncAction` that the agent framework consumes later.

This is why delegation belongs in the tools package but async scheduling belongs in the agent package.

## Context-Based Relay Hooks

`DelegateTool` also defines execution-context helpers in [internal/tools/delegate.go](/Users/xiaobaitu/github.com/soloQueue/internal/tools/delegate.go#L355):

- `WithToolEventChannel`
- `ToolEventChannelFromCtx`
- `WithConfirmForwarder`
- `ConfirmForwarderFromCtx`

These hooks let the agent inject parent-owned relay channels and confirmation forwarding closures into tool execution contexts.

This is the main cross-package integration mechanism between `agent`, `tools`, and `iface`.

## Fallback Wrapping

`FallbackTool` and `WithFallbackPrefix(...)` in [internal/tools/tool.go](/Users/xiaobaitu/github.com/soloQueue/internal/tools/tool.go#L134) are small but architecturally meaningful.

They let L1 agents expose ordinary tools while labeling them as fallback-only when delegation tools are also available.

This is not enforcement. It is prompt-shaping through tool descriptions. It reflects a wider architectural pattern in the repo: some policy is expressed through runtime rules, some through prompts, and some through tool metadata.

## Integration With Agent Runtime

The tools package does not manage scheduling. It relies on the agent runtime for:

- registry exposure to the provider
- execution ordering
- per-tool timeout wrapping
- confirmation gating
- event emission around tool execution
- async orchestration for `AsyncTool`

This split is visible in `agent.execToolStream(...)` and `agent.execToolsWithAsync(...)`, but the tools package remains independent of mailbox logic.

That boundary is clean:

- tools own capability semantics
- agent owns orchestration semantics

## Code Layout Summary

The package is organized as a flat capability library:

- contracts: `tool.go`, `registry.go`, `errors.go`, `config.go`
- delegation/context hooks: `delegate.go`
- file and text tools: `file_read.go`, `grep.go`, `glob.go`, `write_file.go`, `replace.go`, `multi_*`
- network tools: `http_fetch.go`, `web_search.go`
- shell tool: `shell_exec.go`
- shared helpers/sandbox: `helpers.go`, `sandbox.go`

## Architectural Strengths

- Clear minimal interface for model-callable capabilities.
- Shared config keeps safety and limits centralized.
- Registry guarantees provider spec generation matches runtime lookup.
- Delegation fits naturally into the tool abstraction without special-casing the provider interface.
- Async behavior is expressed declaratively through `AsyncAction`.

## Architectural Tradeoffs

- Some policy lives in metadata/prompt shaping rather than hard enforcement, such as fallback-only labeling.
- `DelegateTool` carries a large amount of orchestration logic for what is nominally “just a tool”.
- Tool package is intentionally flat; as tool count grows, discoverability may degrade.
- Network/security-sensitive tools such as `WebFetch` still need careful per-tool audits because shared config is not sufficient by itself.

## Files To Read First

- [internal/tools/tool.go](/Users/xiaobaitu/github.com/soloQueue/internal/tools/tool.go)
- [internal/tools/config.go](/Users/xiaobaitu/github.com/soloQueue/internal/tools/config.go)
- [internal/tools/registry.go](/Users/xiaobaitu/github.com/soloQueue/internal/tools/registry.go)
- [internal/tools/delegate.go](/Users/xiaobaitu/github.com/soloQueue/internal/tools/delegate.go)
- [internal/tools/http_fetch.go](/Users/xiaobaitu/github.com/soloQueue/internal/tools/http_fetch.go)
