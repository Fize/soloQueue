import { useState, useEffect, useCallback } from 'react'
import type { MCPConfig } from '@/types'
import { getMCPConfig, updateMCPConfig } from '@/lib/api'

export function useMCPConfig() {
  const [config, setConfig] = useState<MCPConfig | null>(null)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    try {
      const data = await getMCPConfig()
      setConfig(data)
      setError(null)
    } catch {
      setConfig(null)
    }
  }, [])

  useEffect(() => {
    let cancelled = false
    getMCPConfig()
      .then((data) => {
        if (!cancelled) setConfig(data)
      })
      .catch(() => {
        if (!cancelled) setConfig(null)
      })
    return () => {
      cancelled = true
    }
  }, [])

  const save = useCallback(async (cfg: MCPConfig) => {
    setSaving(true)
    setError(null)
    try {
      const updated = await updateMCPConfig(cfg)
      setConfig(updated)
      return updated
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Failed to save MCP config'
      setError(msg)
      throw err
    } finally {
      setSaving(false)
    }
  }, [])

  return { config, saving, error, save, refresh }
}
