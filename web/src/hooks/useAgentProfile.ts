import { useState, useEffect } from 'react'
import type { AgentProfile } from '@/types'
import { getAgentProfile } from '@/lib/api'

export function useAgentProfile(agentId: string | null) {
  const [profile, setProfile] = useState<AgentProfile | null>(null)
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (!agentId) {
      setProfile(null)
      return
    }

    setLoading(true)
    getAgentProfile(agentId)
      .then(setProfile)
      .catch(() => setProfile(null))
      .finally(() => setLoading(false))
  }, [agentId])

  return { profile, loading }
}
