import { useMemo, useRef } from 'react'
import type { AgentInfo } from '@/types'

export interface InputBadgeInfo {
  /**
   * Model name to display. Returns `""` (sentinel) when the agent is
   * processing but the router hasn't classified the prompt yet — this
   * is intentionally coupled to `taskLevel === ""` (see coupling note
   * on the hook), so the caller can render a single coherent "router
   * is still thinking" state for both chips.
   *
   * Returns `undefined` only when the agent is NOT processing.
   */
  modelName: string | undefined
  /**
   * Task level to display, or `undefined` when the agent is not processing.
   * If the agent is processing but the level is not yet known (the router
   * hasn't classified the prompt and there is no `last_level` to fall back
   * on), the returned string is the empty string `""` — callers can use
   * this to render a placeholder ("…") instead of hiding the chip.
   */
  taskLevel: string | undefined
}

/**
 * Derive the model and task-level badge values for the chat input area.
 *
 * Badges are only shown when the agent is actively processing a task.
 * When idle (just viewing history), the input area is clean — no model
 * or level badge is displayed.
 *
 * The hook implements sticky/flicker-prevention: once a task level or
 * model is known, it is remembered in refs so transient undefined values
 * (e.g. during WebSocket updates) don't cause the badge to flicker.
 *
 * Model/level coupling:
 *   The backend's `EffectiveModelID()` now returns three distinct states
 *   that we mirror on the frontend:
 *     - "" (empty string)         — router hasn't classified yet. UI
 *                                   renders a "…" placeholder chip for
 *                                   the model (in lockstep with the
 *                                   level chip).
 *     - template-pinned model     — agent's Definition has ExplicitModel
 *                                   =true; the template model IS the
 *                                   routed model. UI shows it directly.
 *     - routed model from override — router classified the prompt and
 *                                   set a per-ask ModelID. UI shows it.
 *   The level and model chips share the same shape contract, so when
 *   the router is still classifying, both render as placeholders; when
 *   the level arrives, the model transitions in alongside it. The two
 *   never disagree.
 *
 * Field semantics — both `taskLevel` and `modelName` use the same shape:
 *   - Not processing         → `taskLevel === undefined` (chips hidden)
 *   - Processing, classified → `taskLevel === "L0"`, `modelName === "..."`
 *   - Processing, unclassified → `taskLevel === ""`, `modelName === ""`
 *                                (caller renders "…" placeholder chip
 *                                for both; transitions together once the
 *                                WebSocket `state` push arrives)
 *
 * @param agent        The active agent info, or null.
 * @param isProcessing Whether the agent is actively working on a task.
 * @param deriveModel  Optional function to derive the model name from the
 *                     stable task level, agent, and last known model.
 *                     If omitted, uses the agent's `model_id` directly (L1).
 *                     Note: `deriveModel` is only invoked once the level
 *                     is known — when `taskLevel === ""` the hook short-
 *                     circuits and returns `""` for `modelName` directly.
 */
export function useInputBadges(
  agent: AgentInfo | null,
  isProcessing: boolean,
  deriveModel?: (
    taskLevel: string | undefined,
    agent: AgentInfo | null,
    lastModel: string | undefined,
  ) => string | undefined,
): InputBadgeInfo {
  const lastTaskLevelRef = useRef<string | undefined>(undefined)
  const lastModelRef = useRef<string | undefined>(undefined)

  const stableTaskLevel = useMemo(() => {
    if (!isProcessing) {
      lastTaskLevelRef.current = undefined
      return undefined
    }
    const current = agent?.task_level || agent?.last_level || undefined
    if (current) {
      lastTaskLevelRef.current = current
      return current
    }
    // Sticky: once we've seen a level this session, keep showing it
    // through transient WebSocket gaps.
    if (lastTaskLevelRef.current) {
      return lastTaskLevelRef.current
    }
    // Processing but level not known yet — return empty string sentinel
    // so the caller can render a placeholder chip rather than hiding
    // the slot. This avoids the "chip blinks in late after thinking
    // starts" UX failure mode.
    return ''
  }, [isProcessing, agent?.task_level, agent?.last_level])

  const displayModel = useMemo(() => {
    if (!isProcessing) {
      lastModelRef.current = undefined
      return undefined
    }
    // Coupling: the backend's `EffectiveModelID()` already returns ""
    // when the router hasn't classified the prompt (no template-default
    // fallback leaks through), so `agent.model_id === ""` is a reliable
    // signal that the level is also unclassified. We still gate the
    // model on the level explicitly — if for any reason the two diverge
    // (a transient WebSocket blip, a partial payload, a new agent
    // without an ExplicitModel), the level chip drives the placeholder
    // state and the model chip stays in lockstep. Without this gate, a
    // stale `lastModelRef` value could flicker into view while the
    // level chip is still rendering `…`.
    if (stableTaskLevel === '') {
      return ''
    }
    if (deriveModel) {
      const derived = deriveModel(stableTaskLevel, agent, lastModelRef.current)
      if (derived) lastModelRef.current = derived
      return derived || ''
    }
    // Level is known. Read the routed model from `agent.model_id` —
    // for non-explicit agents this is the override's ModelID; for
    // explicit agents this is the template-pinned model. Either way it
    // is the model the LLM client will actually use.
    //
    // If `agent.model_id` is empty during a transient WebSocket gap
    // (the routed model is briefly absent from the payload), we hold
    // the last known value via `lastModelRef.current` to avoid
    // flickering the chip. This is purely a presentation-layer
    // concern (the underlying model hasn't actually changed) — the
    // ref's value was itself derived from a classified level, so it is
    // not a "fallback" to template config in the unclassified case.
    const agentModel = agent?.model_id
    if (agentModel) {
      lastModelRef.current = agentModel
      return agentModel
    }
    return lastModelRef.current || ''
  }, [isProcessing, stableTaskLevel, agent?.model_id, deriveModel])

  return {
    modelName: displayModel,
    taskLevel: stableTaskLevel,
  }
}
