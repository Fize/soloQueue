import { useMemo, useRef } from 'react'
import type { AgentInfo } from '@/types'

export interface InputBadgeInfo {
  /** Model name to display, or undefined when agent is not processing */
  modelName: string | undefined
  /** Task level to display, or undefined when agent is not processing */
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
 * @param agent        The active agent info, or null.
 * @param isProcessing Whether the agent is actively working on a task.
 * @param deriveModel  Optional function to derive the model name from the
 *                     stable task level, agent, and last known model.
 *                     If omitted, uses the agent's `model_id` directly (L1).
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
    return lastTaskLevelRef.current
  }, [isProcessing, agent?.task_level, agent?.last_level])

  const displayModel = useMemo(() => {
    if (!isProcessing) {
      lastModelRef.current = undefined
      return undefined
    }
    if (deriveModel) {
      const derived = deriveModel(stableTaskLevel, agent, lastModelRef.current)
      if (derived) lastModelRef.current = derived
      return derived
    }
    // Default: use agent's model_id directly
    const agentModel = agent?.model_id
    if (agentModel) {
      lastModelRef.current = agentModel
      return agentModel
    }
    return lastModelRef.current
  }, [isProcessing, stableTaskLevel, agent?.model_id, deriveModel])

  return {
    modelName: displayModel,
    taskLevel: stableTaskLevel,
  }
}
