import { useRuntime } from './useRuntime'
import type { AgentStreamState } from '@/types'

export function useAgentStream(agentId: string | null, requestId?: string): AgentStreamState | null {
  const runtime = useRuntime()
  if (!agentId || !requestId || !runtime?.agent_streams) return null
  return Object.values(runtime.agent_streams).find(
    (stream) => stream.agent_id === agentId && stream.request_id === requestId,
  ) ?? null
}
