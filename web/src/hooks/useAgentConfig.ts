import { useState, useEffect, useCallback } from 'react'
import type { AgentConfig } from '@/types'
import { getAgentConfig } from '@/lib/api'

export function useAgentConfig(agentId: string | null) {
  const [config, setConfig] = useState<AgentConfig | null>(null)
  const [loading, setLoading] = useState(false)

  const fetch = useCallback(() => {
    if (!agentId) {
      setConfig(null)
      return
    }
    setLoading(true)
    getAgentConfig(agentId)
      .then(setConfig)
      .catch(() => setConfig(null))
      .finally(() => setLoading(false))
  }, [agentId])

  useEffect(() => {
    fetch()
  }, [fetch])

  return { config, loading, refetch: fetch }
}
