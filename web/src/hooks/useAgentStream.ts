import { useRuntime } from './useRuntime'
import type { AgentStreamState } from '@/types'

export function useAgentStream(agentId: string | null, requestId?: string): AgentStreamState | null {
  const runtime = useRuntime()
  if (!agentId || !runtime?.agent_streams) return null
  if (requestId) {
    return Object.values(runtime.agent_streams).find(
      (stream) => stream.agent_id === agentId && stream.request_id === requestId,
    ) ?? null
  }
  return runtime.agent_streams[agentId] ?? Object.values(runtime.agent_streams).find(
    (stream) => stream.agent_id === agentId,
  ) ?? null
}
