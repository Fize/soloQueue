import { useRuntime } from './useRuntime'
import type { AgentStreamState } from '@/types'

export function useAgentStream(agentId: string | null): AgentStreamState | null {
  const runtime = useRuntime()
  if (!agentId || !runtime?.agent_streams) return null
  return runtime.agent_streams[agentId] ?? null
}
