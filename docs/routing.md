# Task routing

SoloQueue classifies requests by work nature:

| Task type | Meaning |
| --- | --- |
| general | Conversation, writing, translation, summarization |
| engineering | Code, repositories, debugging, tests, deployment |
| research | Search, current information, and source verification |

## Resolution pipeline

~~~text
User prompt
    │
    ▼
Local fast-track patterns
    │ obvious match ───────────────┐
    │ ambiguous                     │
    ▼                              ▼
Configured classifier model    Task type
    │                              │
    └──────────────┬───────────────┘
                   ▼
       provider:model route + fallback
~~~

Fast-track rules recognize common code, command, traceback, path, and research
signals without an extra model call. Ambiguous prompts use the configured
classifier when available. The previous task type remains available to
preserve context for follow-up turns.

The route configuration lives in model_routes in settings.yaml. Route values
use provider:model. If the selected provider or model is unavailable, the
fallback route is used and the resolved state is exposed to the UI and stats.

This taxonomy is deliberately different from a difficulty ladder. A simple
engineering request is still engineering, and a complex conversation is still
general.

See [Models and routing](guides/models-and-routing.md) for configuration and
[Architecture overview](architecture/overview.md) for the runtime boundary.
