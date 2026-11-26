import { useEffect, useState } from 'react'
import { getObservedRequest, type ObservedDispatch } from '@/lib/api/observability-api'

export interface DelegationIdentity {
  dispatchId?: string
  instanceId: string | null
}

type TurnDispatches = Map<string, ObservedDispatch>

const turnCache = new Map<string, Promise<TurnDispatches>>()

function loadTurnDispatches(sessionId: string, requestId: string): Promise<TurnDispatches> {
  const key = `${sessionId}\u0000${requestId}`
  const cached = turnCache.get(key)
  if (cached) return cached
  const pending = getObservedRequest(sessionId, requestId)
    .then((page) => {
      const dispatches: TurnDispatches = new Map()
      for (const dispatch of page.dispatches ?? []) {
        if (dispatch.tool_call_id) dispatches.set(dispatch.tool_call_id, dispatch)
      }
      return dispatches
    })
    .catch(() => {
      turnCache.delete(key)
      return new Map() as TurnDispatches
    })
  turnCache.set(key, pending)
  return pending
}

/**
 * Resolve which delegation one tool call belongs to.
 *
 * The transcript carries the identity for current turns; older turns fall back
 * to the dispatch records of their request. Agent names are deliberately not
 * used — the same agent can be delegated twice within one turn.
 */
export function useDelegationIdentity({
  sessionId,
  requestId,
  callId,
  dispatchId,
  agentInstanceId,
}: {
  sessionId?: string | null
  requestId?: string
  callId: string
  dispatchId?: string
  agentInstanceId?: string
}): DelegationIdentity {
  const lookupKey = sessionId && requestId && callId ? `${sessionId}\u0000${requestId}\u0000${callId}` : ''
  const [resolved, setResolved] = useState<{ key: string; identity: DelegationIdentity } | null>(null)
  const known = !!agentInstanceId

  useEffect(() => {
    if (!lookupKey || known || !sessionId || !requestId) return
    let active = true
    void loadTurnDispatches(sessionId, requestId).then((dispatches) => {
      if (!active) return
      const match = dispatches.get(callId)
      setResolved({
        key: lookupKey,
        identity: match
          ? { dispatchId: match.dispatch_id, instanceId: match.executor_instance_id || null }
          : { instanceId: null },
      })
    })
    return () => {
      active = false
    }
  }, [lookupKey, known, sessionId, requestId, callId])

  if (known) return { dispatchId, instanceId: agentInstanceId ?? null }
  if (resolved?.key === lookupKey) return { dispatchId: resolved.identity.dispatchId ?? dispatchId, instanceId: resolved.identity.instanceId }
  return { dispatchId, instanceId: null }
}
