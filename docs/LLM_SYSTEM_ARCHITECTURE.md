# LLM System Architecture

## Overview

The LLM system is split into two layers:

- `internal/llm`: provider-agnostic shared protocol and retry primitives
- `internal/llm/deepseek`: the concrete HTTP/SSE transport for the current provider

The agent package depends only on the `LLMClient` contract in [internal/agent/llm.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/llm.go#L61). The runtime currently wires a DeepSeek implementation in `cmd/soloqueue/main.go` through `deepseek.NewClient(...)`.

That gives the LLM subsystem a clean architectural purpose: define the model-facing protocol once, then implement provider-specific transport adapters under subpackages.

## Layer 1: Provider-Agnostic Types

The core shared model lives in [internal/llm/types.go](/Users/xiaobaitu/github.com/soloQueue/internal/llm/types.go).

This package defines the universal protocol objects used across providers:

- tool-calling types: `ToolCall`, `ToolDef`, `FunctionCall`, `FunctionDecl`
- usage accounting: `Usage`
- finish semantics: `FinishReason`
- streaming output: `Event`, `EventType`, `ToolCallDelta`
- structured error envelope: `APIError`

The design choice here is explicit: the package does not own HTTP logic, only data structures and generic helpers.

### Streaming Event Model

The most important shared type is `llm.Event` ([internal/llm/types.go](/Users/xiaobaitu/github.com/soloQueue/internal/llm/types.go#L113)). It is a tagged struct with three modes:

- `EventDelta`
- `EventDone`
- `EventError`

This event stream is the raw provider-facing substrate consumed by the agent loop. The agent layer then converts it into higher-level `AgentEvent`s.

The separation is deliberate:

- `llm.Event` describes transport/provider semantics
- `agent.AgentEvent` describes application execution semantics

## Layer 2: Agent-Facing Request/Response Model

The agent package wraps the lower-level shared types in its own request/response layer in [internal/agent/llm.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/llm.go).

Main types:

- `LLMMessage`
- `LLMRequest`
- `LLMResponse`
- `LLMClient`

### Why A Separate Agent-Layer Contract Exists

`internal/llm` holds transport-neutral shared primitives, but the agent package needs a richer request shape tied to execution behavior:

- model name
- messages and tool declarations
- output formatting flags
- reasoning controls
- include-usage option for stream mode

So `LLMRequest` becomes the internal API between the agent runtime and provider clients.

## DeepSeek Provider Implementation

The only concrete provider in this repo is `internal/llm/deepseek`.

The entrypoint is `Client` in [internal/llm/deepseek/client.go](/Users/xiaobaitu/github.com/soloQueue/internal/llm/deepseek/client.go#L50).

### Construction

`NewClient` accepts a `deepseek.Config` with:

- `BaseURL`
- `APIKey`
- custom headers
- request timeout
- retry policy
- optional injected `http.Client`
- optional logger

This config is built from the global config service in [cmd/soloqueue/main.go](/Users/xiaobaitu/github.com/soloQueue/cmd/soloqueue/main.go#L177).

### Streaming-First Design

The most important architectural choice is in [internal/llm/deepseek/client.go](/Users/xiaobaitu/github.com/soloQueue/internal/llm/deepseek/client.go#L93):

- `ChatStream` is the primary HTTP path
- `Chat` is implemented by consuming `ChatStream`

That means there is only one implementation for:

- HTTP request setup
- retry behavior
- SSE parsing
- chunk-to-event conversion
- transport error handling

This mirrors the agent package's own streaming-first architecture and removes divergence risk between sync and stream paths.

## Transport Pipeline

The DeepSeek transport pipeline is:

1. build a wire-level request from `agent.LLMRequest`
2. submit HTTP POST with retry
3. parse Server-Sent Events from response body
4. convert each provider chunk into one or more `llm.Event`s
5. emit through a channel until done or error

### Request Encoding

Wire request structs live in [internal/llm/deepseek/wire.go](/Users/xiaobaitu/github.com/soloQueue/internal/llm/deepseek/wire.go#L21).

`buildWireRequest(...)` maps agent-layer semantics to DeepSeek's HTTP schema:

- messages
- `stream=true`
- `stream_options.include_usage`
- tool definitions and `tool_choice`
- `response_format`
- `reasoning_effort`
- DeepSeek V4 `thinking` mode

One notable detail is `buildWireMessages(...)` in [internal/llm/deepseek/wire.go](/Users/xiaobaitu/github.com/soloQueue/internal/llm/deepseek/wire.go#L145). It preserves `reasoning_content` on assistant messages and injects a placeholder when thinking mode is enabled but a historical assistant turn lacks reasoning content. That is a provider-compatibility shim driven by DeepSeek validation behavior.

### HTTP And Retry

`ChatStream` calls `doWithRetry(...)` in [internal/llm/deepseek/client.go](/Users/xiaobaitu/github.com/soloQueue/internal/llm/deepseek/client.go#L233).

`doWithRetry` uses the shared helper `llm.RunWithRetryHooks(...)` from [internal/llm/retry.go](/Users/xiaobaitu/github.com/soloQueue/internal/llm/retry.go#L61). Retry policy is provider-configured, but the retry engine is generic.

The generic retry layer handles:

- backoff schedule
- max retry count
- retryability decision callback
- context cancellation during backoff

The provider layer decides what is retryable via `llm.IsRetryableErr(...)` and `APIError.IsRetryable()` in [internal/llm/types.go](/Users/xiaobaitu/github.com/soloQueue/internal/llm/types.go#L181).

This is a clean boundary:

- `internal/llm` owns retry policy mechanics
- provider package owns request execution and error classification

## SSE Parsing And Chunk Conversion

### SSE Reader

Low-level SSE parsing is in [internal/llm/deepseek/sse.go](/Users/xiaobaitu/github.com/soloQueue/internal/llm/deepseek/sse.go#L11).

`sseReader` is intentionally minimal:

- skips comments and empty separators
- only reads `data:` lines
- recognizes `[DONE]`
- ignores unused SSE fields like `event:` and `id:`

That keeps parsing aligned with what the provider actually emits, without building a full generic SSE framework.

### Chunk To Event Expansion

Provider chunk conversion happens in `chunkToEvents(...)` in [internal/llm/deepseek/wire.go](/Users/xiaobaitu/github.com/soloQueue/internal/llm/deepseek/wire.go#L253).

One input chunk may generate multiple `llm.Event`s:

- one event per tool-call delta slot
- one combined content/reasoning delta event
- one done event when `finish_reason` appears
- optional usage merged into the final done event

This stage is architecturally important because it normalizes provider wire quirks into the shared event contract consumed by the agent loop.

## Error Model

The LLM subsystem has two layers of error handling.

### Request-Phase Errors

Errors before the event stream begins are returned directly from `ChatStream`, for example:

- request marshaling failure
- HTTP network failure after retries
- API 4xx/5xx response

That behavior is documented in [internal/llm/deepseek/client.go](/Users/xiaobaitu/github.com/soloQueue/internal/llm/deepseek/client.go#L142).

### Stream-Phase Errors

Errors after a stream has started are emitted as `llm.EventError` and then the channel is closed. That includes:

- SSE parse failures
- malformed JSON chunks
- context cancellation during stream consumption

This split is sensible because the caller needs a synchronous error only if the stream could not be established at all.

### Structured API Errors

HTTP error bodies are decoded into `llm.APIError`, whose retryability is status-code based:

- `429` is retryable
- `5xx` is retryable
- most `4xx` are not

That logic is centralized in [internal/llm/types.go](/Users/xiaobaitu/github.com/soloQueue/internal/llm/types.go#L156), which keeps provider retry policy behavior consistent across clients.

## Runtime Integration With Config

The config system feeds the LLM layer through `cmd/soloqueue/main.go`:

- `cfg.DefaultProvider()` selects the enabled provider record
- provider fields populate `deepseek.Config`
- model selection is resolved separately through `cfg.DefaultModelByRole(...)` and template/model resolver logic

The important architectural split is:

- provider config chooses transport credentials and retry behavior
- model config chooses semantic model identity and reasoning mode

The LLM client itself does not know about role-based model selection. It only receives fully resolved `LLMRequest.Model`, `ThinkingEnabled`, and `ReasoningEffort` from the agent layer.

## FakeLLM For Tests

`FakeLLM` in [internal/agent/llm.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/llm.go#L76) is a test-focused implementation of `LLMClient`.

It supports scripted behavior for:

- plain responses
- tool-calling turns
- streaming content/reasoning/tool deltas
- explicit finish reasons and injected errors

Architecturally, this is useful because agent tests are written against the stable `LLMClient` contract rather than against a mocked HTTP transport.

## Code Layout Summary

The LLM subsystem is split cleanly by concern:

- shared protocol and retry: `internal/llm/types.go`, `internal/llm/retry.go`
- provider transport: `internal/llm/deepseek/client.go`
- provider wire schema: `internal/llm/deepseek/wire.go`
- provider stream framing: `internal/llm/deepseek/sse.go`
- tests: `internal/llm/*_test.go`, `internal/llm/deepseek/*_test.go`

## Architectural Strengths

- Streaming-first implementation removes duplicated provider logic.
- Shared protocol types are provider-agnostic and compact.
- Retry policy is generic and reusable.
- Provider-specific wire translation is isolated in one subpackage.
- Clear distinction between request-establishment errors and in-stream errors.

## Architectural Tradeoffs

- The agent-facing request model lives in `internal/agent`, not `internal/llm`, so provider implementations depend upward on agent types.
- Current provider support is single-provider in practice even though config and protocol are partly generalized.
- Provider-specific quirks like DeepSeek thinking placeholders are encoded in wire translation code rather than abstracted behind a more general compatibility layer.

## Files To Read First

- [internal/llm/types.go](/Users/xiaobaitu/github.com/soloQueue/internal/llm/types.go)
- [internal/llm/retry.go](/Users/xiaobaitu/github.com/soloQueue/internal/llm/retry.go)
- [internal/llm/deepseek/client.go](/Users/xiaobaitu/github.com/soloQueue/internal/llm/deepseek/client.go)
- [internal/llm/deepseek/wire.go](/Users/xiaobaitu/github.com/soloQueue/internal/llm/deepseek/wire.go)
- [internal/llm/deepseek/sse.go](/Users/xiaobaitu/github.com/soloQueue/internal/llm/deepseek/sse.go)
- [internal/agent/llm.go](/Users/xiaobaitu/github.com/soloQueue/internal/agent/llm.go)
