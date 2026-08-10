# Timeline and replay

> 中文：[Timeline 与回放](zh/timeline.md)

I keep the timeline subsystem under internal/memory/timeline. I use it to
record append-only JSONL events for sessions, tools, delegation, routing, and
workflow-related activity.

## Invariants

- System prompts are not written to the user timeline.
- Events are append-only; a new event explains a correction or state change.
- Tool-call payloads are repaired before being sent to an LLM provider so an
  orphaned tool call does not produce an invalid request.
- Timeline files can rotate according to session configuration.

## Why it matters

I optimize the live WebSocket stream for the current UI, while I use the
timeline as durable evidence for restart recovery, replay, and diagnosis. It
can contain prompts, paths, tool arguments, and provider output, so I protect
it like source data.

I use [Data, logs, and backup](operations/data-and-backup.md) for retention and
[Memory](memory.md) for the relationship with summaries and long-term recall.
