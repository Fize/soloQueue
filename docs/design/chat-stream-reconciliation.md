# Chat Stream Reconciliation Fix

## Verified causes

- Runtime recovery can select any message with the same request ID. History assigns that ID to both the user prompt and the assistant reply, so recovery can overwrite the user prompt as an assistant message and leave a duplicate reply.
- Runtime snapshots derive an L2 logical session from the agent target ID. Real targets use `l2-<session-id>-session`, while the current normalization only prefixes the complete target, producing a key that does not match the UI session.
- `ChatInput` already renders task type and model metadata, but `AssistantPage` and `ChatPage` do not pass the request-scoped values to it. Their working indicators use the same values separately.
- Timeline assistant rows store one model iteration as reasoning, content, and tool-call fields. History currently rebuilds those fields as reasoning, tool calls, then content, although live rendering shows streamed content before the subsequently executed tool calls.
- Worked thinking text is rendered as plain text even though the existing chat Markdown renderer is already used for ordinary content.

## Minimal approach

- Restrict request-owned recovery to assistant messages and keep repeated recovery idempotent.
- Normalize the established real L2 target form at the backend runtime boundary while preserving L1 and already canonical L2 IDs.
- Pass the existing request-scoped task type and model values through both shared `ChatInput` call paths and keep `AgentWorkingIndicator` on the same values.
- Record an optional assistant `segment_order` alongside the existing aggregate fields. New writes record reasoning, streamed content, then tool calls; history honors explicit order and uses that same deterministic order for legacy rows.
- Render worked thinking with the existing streaming Markdown component.

## Compatibility

- Legacy timeline entries keep a deterministic replay using their existing fields; no migration or rewrite is needed.
- Canonical `l2:<id>` runtime keys and L1 aliases retain their current results.
- No dependency, storage migration, API endpoint, or deployment change is introduced.

## Acceptance tests

1. A user prompt and assistant reply sharing one request ID remain two messages; repeated recovery preserves the user prompt and produces one updated assistant reply.
2. `l2-bd1049bb-7977-4575-affe-bc7d665fb32a-session` maps to `l2:bd1049bb-7977-4575-affe-bc7d665fb32a`, while L1 and canonical L2 inputs remain stable.
3. Both L1 and Team pages pass request-scoped task type and model values to the shared input and working indicator.
4. History returns reasoning, content, and tool-call segments in live order and still reads timeline rows without new metadata.
5. Worked thinking renders Markdown emphasis through the existing renderer.
