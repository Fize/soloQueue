import { useCallback, useEffect, useState } from 'react'
import { getSessionModel, setSessionModel } from '@/lib/api'
import type { SessionModelSelection, SessionModelState } from '@/types'

/**
 * Session-scoped model selection. The server owns the state; this hook mirrors
 * it so every entry point (Web, QQ, WeChat, WeCom, Telegram) sees one value.
 */
export function useSessionModel(sessionId?: string) {
  const [state, setState] = useState<SessionModelState | null>(null)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setState(null)
    getSessionModel(sessionId)
      .then((next) => {
        if (!cancelled) setState(next)
      })
      .catch(() => {
        if (!cancelled) setState(null)
      })
    return () => {
      cancelled = true
    }
  }, [sessionId])

  const select = useCallback(
    async (selection: SessionModelSelection): Promise<SessionModelState | null> => {
      setSaving(true)
      setError(null)
      try {
        const next = await setSessionModel(sessionId, selection)
        setState(next)
        return next
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err))
        return null
      } finally {
        setSaving(false)
      }
    },
    [sessionId]
  )

  return { state, saving, error, select }
}
