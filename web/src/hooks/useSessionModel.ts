import { useCallback, useEffect, useState } from 'react'
import { getSessionModel, setSessionModel } from '@/lib/api'
import { isDraftSessionId, useChatStore } from '@/stores/chatStore'
import type {
  SessionModelSelection,
  SessionModelState,
  SessionModelView,
} from '@/types'

/** The view a composer reports for a selection the server has not seen yet. */
function unstartedStateFor(selection: SessionModelSelection | null): SessionModelState {
  if (!selection) return { selection: null, effective: null, source: 'default' }
  const view: SessionModelView = { ...selection }
  return { selection: view, effective: { ...view }, source: 'user' }
}

function cleared(selection: SessionModelSelection): boolean {
  return !selection.provider_id || !selection.model_id
}

/**
 * Session-scoped model selection. The server owns the state; this hook mirrors
 * it so every entry point (Web, QQ, WeChat, WeCom, Telegram) sees one value.
 *
 * A composer that has no server session yet — the welcome screen or a
 * placeholder new chat — has nothing to write to. Its choice stays in the store
 * (on the placeholder row, or as the pending choice of the welcome screen) and
 * the first message applies it when it creates the real session.
 */
export function useSessionModel(sessionId?: string) {
  const draft = isDraftSessionId(sessionId)
  const unstarted = !sessionId || draft
  const draftModel = useChatStore((s) =>
    draft && sessionId ? s.drafts.find((d) => d.id === sessionId)?.model : undefined
  )
  const pendingModel = useChatStore((s) => s.pendingModel)
  const setDraftModel = useChatStore((s) => s.setDraftModel)
  const setPendingModel = useChatStore((s) => s.setPendingModel)
  const [state, setState] = useState<SessionModelState | null>(null)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    // Nothing to ask: there is no session endpoint for an unstarted chat, and a
    // request would 404 so the trigger would fall back to "Auto" as if nothing
    // had been chosen.
    if (unstarted) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setState(null)
      return
    }
    let cancelled = false
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
  }, [sessionId, unstarted])

  const select = useCallback(
    async (selection: SessionModelSelection): Promise<SessionModelState | null> => {
      if (unstarted) {
        const next = cleared(selection) ? null : selection
        // The placeholder owns the choice until its session exists; the welcome
        // screen keeps one pending choice for the chat it is about to create.
        if (draft && sessionId) setDraftModel(sessionId, next)
        else setPendingModel(next)
        return unstartedStateFor(next)
      }
      setSaving(true)
      setError(null)
      try {
        const next = await setSessionModel(sessionId, selection)
        setState(next)
        return next
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err))
        // Roll the UI back to what the server actually holds.
        try {
          setState(await getSessionModel(sessionId))
        } catch {
          // Keep the last known server state when the refetch also fails.
        }
        return null
      } finally {
        setSaving(false)
      }
    },
    [draft, sessionId, setDraftModel, setPendingModel, unstarted]
  )

  const chosen = draft ? draftModel : pendingModel
  return {
    state: unstarted ? (chosen ? unstartedStateFor(chosen) : null) : state,
    saving: unstarted ? false : saving,
    error,
    select,
  }
}
