# Models and routing

> 中文：[模型与路由](../zh/guides/models-and-routing.md)

I route requests by work nature, not by a difficulty ladder. I currently use
these task types:

| Task type | Typical requests |
| --- | --- |
| general | Conversation, writing, translation, summarization |
| engineering | Code, repositories, debugging, tests, deployment |
| research | Web search, current information, source verification |

## Configure providers and models

I open Settings → Models to add an OpenAI-compatible provider, register its
models, and enable the entries I want to use. I enter provider credentials in
settings or supply them through an environment variable.

Every model I configure has a local ID, provider ID, optional API model name,
context window, generation settings, and thinking settings. I use provider:model
as the route reference format.

## Configure task routes

My model route section maps task types to models:

~~~yaml
model_routes:
  general: deepseek:deepseek-v4-flash-thinking
  engineering: deepseek:deepseek-v4-flash-thinking-max
  research: deepseek:deepseek-v4-flash-thinking-max
  classifier: deepseek:deepseek-v4-flash
  fallback: deepseek:deepseek-v4-flash
~~~

My local fast-track classifier recognizes obvious code, command, traceback, and
research patterns without an extra model call. For ambiguous input, I can use
the configured classifier model. If a selected route is unavailable, I use the
fallback route and the UI records that resolution.

## Check a resolution

I use the active request indicator in Chat to see the resolved task type and model
when available. I use Stats to inspect usage and routing history. If the
resolution is unexpected, I inspect the previous request in the same session:
follow-up prompts retain task context so my conversation does not change
behavior arbitrarily between turns.

## Model setup checklist

- Provider is enabled and has a usable key.
- Model provider_id matches an enabled provider.
- Every route points to an enabled provider:model pair.
- The model context window is large enough for the intended workflow.
- Thinking is enabled only when the provider accepts the selected thinking
  fields.
