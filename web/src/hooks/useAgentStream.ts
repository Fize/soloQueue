import { useRuntime } from './useRuntime'
import type { AgentStreamState } from '@/types'

/**
 * Resolve one live agent stream.
 *
 * A delegation's stream is keyed by its own dispatch, so two delegations served
 * by the same agent instance under the same request stay distinguishable. A
 * dispatch that is not streaming right now resolves to null instead of another
 * delegation's output.
 */
export function useAgentStream(
  agentId: string | null,
  requestId?: string,
  dispatchId?: string,
): AgentStreamState | null {
  const runtime = useRuntime()
  if (!agentId || !requestId || !runtime?.agent_streams) return null
  const scoped = Object.values(runtime.agent_streams).filter(
    (stream) => stream.agent_id === agentId && stream.request_id === requestId,
  )
  if (!scoped.length) return null
  if (dispatchId) return scoped.find((stream) => stream.dispatch_id === dispatchId) ?? null
  return scoped.find((stream) => !stream.dispatch_id) ?? scoped[0]
}
