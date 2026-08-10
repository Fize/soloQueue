# Context windows

The context-window subsystem lives under internal/memory/ctxwin. It protects
the model context budget while preserving the information most useful for the
next turn.

## Inputs

The context builder combines the system prompt, conversation messages, tool
calls and results, skill instructions, memory references, and the current
environment. System prompts are replayed into a new request but are not
written as user-visible timeline events.

## Compaction

Token counting uses the configured tokenizer and model context window. When the
dual waterline is reached, the compactor summarizes or removes older material
and keeps the recent turn boundary intact. Tool-call pairs are filtered so an
orphaned tool call is not sent to an LLM provider.

Long tool output and oversized JSON values can be truncated under the tool
limits. These limits protect request size; they are not a guarantee that a
large result will remain semantically complete.

## Operational guidance

- Configure a realistic context_window for every enabled model.
- Prefer concise tool output and explicit task acceptance criteria.
- Treat a compaction summary as working memory, not as a verbatim transcript.
- Use timeline and logs when diagnosing what was actually sent to the provider.

The user-facing memory behavior is described in
[Memory and stats](guides/memory-and-stats.md).
