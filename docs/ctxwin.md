# Context windows

> 中文：[上下文窗口](zh/ctxwin.md)

I keep the context-window subsystem under internal/memory/ctxwin. I use it to
protect the model context budget while preserving the information most useful
for the next turn.

## Inputs

I combine the system prompt, conversation messages, tool
calls and results, skill instructions, memory references, and the current
environment. System prompts are replayed into a new request but are not
written as user-visible timeline events.

## Compaction

I count tokens with the configured tokenizer and model context window. When I
reach the dual waterline, I let the compactor summarize or remove older
material while keeping the recent turn boundary intact. I filter tool-call
pairs so I do not send an orphaned tool call to an LLM provider.

I may truncate long tool output and oversized JSON values under the tool limits.
I use these limits to protect request size; I do not treat them as a guarantee
that a large result remains semantically complete.

## Operational guidance

- I configure a realistic context_window for every enabled model.
- I prefer concise tool output and explicit task acceptance criteria.
- I treat a compaction summary as working memory, not as a verbatim transcript.
- I use timeline and logs when diagnosing what I actually sent to the provider.

I describe the user-facing memory behavior in
[Memory and stats](guides/memory-and-stats.md).
