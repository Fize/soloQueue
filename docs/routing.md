# Task routing

> 中文：[任务路由](zh/routing.md)

I classify requests by work nature:

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

I use fast-track rules to recognize common code, command, traceback, path, and
research signals without an extra model call. I use the configured classifier
for ambiguous prompts when it is available. I retain the previous task type to
preserve context for follow-up turns.

I keep the route configuration in model_routes in settings.yaml. Route values
use provider:model. If the selected provider or model is unavailable, the
fallback route is used and the resolved state is exposed to the UI and stats.

I deliberately keep this taxonomy different from a difficulty ladder. A simple
engineering request is still engineering, and a complex conversation is still
general.

I use [Models and routing](guides/models-and-routing.md) for configuration and
[Architecture overview](architecture/overview.md) for the runtime boundary.
