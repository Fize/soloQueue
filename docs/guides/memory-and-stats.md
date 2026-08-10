# Memory and stats

> 中文：[记忆与统计](../zh/guides/memory-and-stats.md)

I use short-term conversation memory, long-term memory, timeline events, and
usage statistics in SoloQueue. They answer different questions, so I do not
treat them as one undifferentiated history.

## Memory

- I create conversation summaries when context compaction needs them.
- I use SQLite-backed BM25 search and a knowledge graph for long-term memory.
- I can enable vector search with an embedding provider; my default
  configuration does not require one.
- I preserve append-only session events in Timeline JSONL files for replay and
  diagnosis.

I open Settings → Memory to inspect embedding and retention-related settings.
I treat memory records and logs as private project data because they can contain
prompts, file paths, tool arguments, and responses.

## Statistics

I open Stats to inspect token usage, request history, and routing
classifications over a selected time range. I use the page for operational
feedback, not as billing truth for an external provider.

I can also run the read-only memory audit from the CLI:

~~~bash
soloqueue memory audit
~~~

I plan legacy cleanup before applying it. When I apply it, I create a database
backup and write a manifest:

~~~bash
soloqueue memory cleanup --project-root /absolute/path/to/project
soloqueue memory cleanup --project-root /absolute/path/to/project --apply
~~~

## Keep evidence useful

- I use Stats to identify routing or usage changes, then inspect the matching
  session and timeline.
- I back up SQLite and JSONL logs before changing retention or cleanup settings.
- I do not paste raw memory or timeline files into public bug reports without
  removing secrets and user content.
