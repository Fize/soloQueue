# Models and routing

SoloQueue routes requests by work nature, not by a difficulty ladder. The
current task types are:

| Task type | Typical requests |
| --- | --- |
| general | Conversation, writing, translation, summarization |
| engineering | Code, repositories, debugging, tests, deployment |
| research | Web search, current information, source verification |

## Configure providers and models

Open Settings → Models to add an OpenAI-compatible provider, register its
models, and enable the entries you want to use. Provider credentials can be
entered in settings or supplied through an environment variable.

Each model has a local ID, provider ID, optional API model name, context window,
generation settings, and thinking settings. The route reference format is
provider:model.

## Configure task routes

The model route section maps task types to models:

~~~yaml
model_routes:
  general: deepseek:deepseek-v4-flash-thinking
  engineering: deepseek:deepseek-v4-flash-thinking-max
  research: deepseek:deepseek-v4-flash-thinking-max
  classifier: deepseek:deepseek-v4-flash
  fallback: deepseek:deepseek-v4-flash
~~~

The local fast-track classifier recognizes obvious code, command, traceback,
and research patterns without an extra model call. Ambiguous input can use the
configured classifier model. If a selected route is unavailable, the fallback
route is used and the UI records that resolution.

## Check a resolution

The active request indicator in Chat shows the resolved task type and model
when available. Usage and routing history are available under Stats. If the
resolution is unexpected, inspect the previous request in the same session:
follow-up prompts retain task context so a conversation does not change
behavior arbitrarily between turns.

## Model setup checklist

- Provider is enabled and has a usable key.
- Model provider_id matches an enabled provider.
- Every route points to an enabled provider:model pair.
- The model context window is large enough for the intended workflow.
- Thinking is enabled only when the provider accepts the selected thinking
  fields.
