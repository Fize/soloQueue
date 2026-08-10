# Memory and stats

SoloQueue has short-term conversation memory, long-term memory, timeline
events, and usage statistics. They answer different questions and should not
be treated as one undifferentiated history.

## Memory

- Conversation summaries are created when context compaction needs them.
- Long-term memory uses SQLite-backed BM25 search and a knowledge graph.
- Optional vector search can be enabled with an embedding provider; the
  default configuration does not require one.
- Timeline JSONL files preserve append-only session events for replay and
  diagnosis.

Open Settings → Memory to inspect embedding and retention-related settings.
Memory records and logs can contain prompts, file paths, tool arguments, and
responses; treat them as private project data.

## Statistics

Open Stats to inspect token usage, request history, and routing classifications
over a selected time range. The page is for operational feedback, not billing
truth for an external provider.

The read-only memory audit is also available from the CLI:

~~~bash
soloqueue memory audit
~~~

Legacy cleanup is planned before it is applied. Applying it creates a database
backup and writes a manifest:

~~~bash
soloqueue memory cleanup --project-root /absolute/path/to/project
soloqueue memory cleanup --project-root /absolute/path/to/project --apply
~~~

## Keep evidence useful

- Use Stats to identify routing or usage changes, then inspect the matching
  session and timeline.
- Back up SQLite and JSONL logs before changing retention or cleanup settings.
- Do not paste raw memory or timeline files into public bug reports without
  removing secrets and user content.
