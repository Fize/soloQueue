import { useEffect, useState } from 'react'
import { useTranslation } from '@/lib/i18n'
import { Button } from '@/components/ui/button'
import { getObservedDispatch } from '@/lib/api/observability-api'
import type { ObservedDispatch } from '@/lib/api/observability-api'

function DispatchProcessView({ sessionId, dispatchId, initial }: { sessionId: string; dispatchId: string; initial?: ObservedDispatch }) {
  const { t } = useTranslation()
  const [data, setData] = useState(initial || null)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(true)
  const [refresh, setRefresh] = useState(0)
  useEffect(() => {
    const controller = new AbortController()
    void getObservedDispatch(sessionId, dispatchId, undefined, controller.signal).then((value) => {
      if (!controller.signal.aborted) { setData(value); setError('') }
    }).catch((cause) => { if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : t('observability.error')) })
      .finally(() => { if (!controller.signal.aborted) setBusy(false) })
    return () => controller.abort()
  }, [sessionId, dispatchId, refresh, initial, t])
  async function more() {
    if (!data?.next_cursor || busy) return
    setBusy(true); setError('')
    try {
      const value = await getObservedDispatch(sessionId, dispatchId, data.next_cursor)
      setData((old) => old?.dispatch_id === dispatchId ? { ...value, events: [...old.events, ...value.events.filter((event) => !old.events.some((item) => item.sequence === event.sequence))] } : old)
    } catch (cause) { setError(cause instanceof Error ? cause.message : t('observability.error')) } finally { setBusy(false) }
  }
  const expired = !!data?.details_expired || !!data?.events.some((event) => event.type === 'details_expired')
  const partial = !!data?.details_truncated || !!data?.events.some((event) => event.payload?.truncated === true || event.payload?.recording_status === 'partial')
  return <div className="space-y-3 text-xs">
    {error && <div role="alert">{error}<Button variant="ghost" size="xs" onClick={() => setRefresh((value) => value + 1)}>{t('observability.retry')}</Button></div>}
    {busy && !data && <p role="status">{t('observability.loading')}</p>}
    {data && <>
      <div className="flex flex-wrap items-center gap-2"><strong>{data.executor}</strong><span>{data.status}</span><code className="break-all text-muted-foreground">{data.dispatch_id}</code></div>
      {data.parent_dispatch_id && <p className="break-all text-muted-foreground">↳ {data.parent_dispatch_id}</p>}
      {(expired || partial) && <div role="status" className="space-y-2 rounded border border-border/50 bg-muted/30 p-3"><p>{t(expired ? 'observability.dispatchExpired' : 'observability.partial')}</p><a href="#/settings/storage?category=execution_details" className="text-primary underline underline-offset-2">{t('observability.settings')}</a></div>}
      {!expired && data.events.length === 0 && <p className="text-muted-foreground">{t('observability.noProcess')}</p>}
      {data.events.map((event) => <details key={event.sequence} className="rounded-md border border-border/40 p-2">
        <summary className="cursor-pointer break-words">{event.sequence} · {event.type} <time className="text-muted-foreground">{new Date(event.timestamp).toLocaleTimeString()}</time></summary>
        {event.payload && <pre className="mt-2 whitespace-pre-wrap break-words rounded bg-muted/30 p-2">{JSON.stringify(event.payload, null, 2)}</pre>}
      </details>)}
      {data.next_cursor && <Button variant="outline" size="xs" disabled={busy} onClick={() => void more()}>{busy ? t('observability.loading') : t('observability.loadMore')}</Button>}
      <Button variant="ghost" size="xs" disabled={busy} onClick={() => setRefresh((value) => value + 1)}>{t('observability.refresh')}</Button>
    </>}
  </div>
}

export function DispatchProcess(props: { sessionId: string; dispatchId: string; initial?: ObservedDispatch }) {
  return <DispatchProcessView key={`${props.sessionId}:${props.dispatchId}`} {...props} />
}
