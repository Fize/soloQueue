import { useEffect, useState } from 'react'
import { useTranslation } from '@/lib/i18n'
import { Button } from '@/components/ui/button'
import { getObservedRequest } from '@/lib/api/observability-api'
import { DispatchProcess } from './DispatchProcess'
import { ExecutionDetails } from './ExecutionDetails'

function DelegationProcessView({ sessionId, requestId, toolCallId, dispatchId }: {
  sessionId: string; requestId?: string; toolCallId: string; dispatchId?: string
}) {
  const { t } = useTranslation()
  const [resolved, setResolved] = useState(dispatchId || '')
  const [loading, setLoading] = useState(!dispatchId && !!requestId)
  const [error, setError] = useState('')
  const [retry, setRetry] = useState(0)
  useEffect(() => {
    const controller = new AbortController()
    if (!dispatchId && requestId) {
      void getObservedRequest(sessionId, requestId, undefined, controller.signal).then((value) => {
        if (controller.signal.aborted) return
        // A tool call ID is stable across history replay; display names are not.
        const matches = value.dispatches.filter((dispatch) => dispatch.tool_call_id === toolCallId)
        if (matches.length === 1) setResolved(matches[0].dispatch_id)
      }).catch((cause) => { if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : t('observability.error')) })
        .finally(() => { if (!controller.signal.aborted) setLoading(false) })
    }
    return () => controller.abort()
  }, [sessionId, requestId, toolCallId, dispatchId, retry, t])
  if (loading) return <p role="status" className="text-xs">{t('observability.loading')}</p>
  if (error) return <div role="alert" className="text-xs text-destructive">{error}<Button size="xs" variant="ghost" onClick={() => setRetry((value) => value + 1)}>{t('observability.retry')}</Button></div>
  if (resolved) return <DispatchProcess key={`${sessionId}:${resolved}`} sessionId={sessionId} dispatchId={resolved} />
  return <div className="space-y-2"><p className="text-xs text-muted-foreground">{t('observability.oldProcess')}</p>{requestId && <ExecutionDetails sessionId={sessionId} requestId={requestId} />}</div>
}

export function DelegationProcess(props: { sessionId: string; requestId?: string; toolCallId: string; dispatchId?: string }) {
  return <DelegationProcessView key={`${props.sessionId}:${props.requestId}:${props.toolCallId}:${props.dispatchId}`} {...props} />
}
