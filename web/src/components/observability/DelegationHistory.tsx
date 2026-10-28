import { useEffect, useState, type RefObject } from 'react'
import { getObservedDispatch, getObservedRequest, type ObservedCall } from '@/lib/api/observability-api'
import { useTranslation } from '@/lib/i18n'
import { Button } from '@/components/ui/button'
import { AgentStreamView } from '@/components/AgentStreamView'
import { MarkdownPreview } from '@/components/ui/markdown-preview'
import type { Segment } from '@/types'

export function DelegationHistory({ sessionId, requestId, dispatchId, toolCallId, result, error, scrollRef }: {
  sessionId: string; requestId?: string; dispatchId?: string; toolCallId: string; result?: string; error?: string; scrollRef: RefObject<HTMLDivElement | null>
}) {
  const { t } = useTranslation()
  const [segments, setSegments] = useState<Segment[]>([])
  const [notice, setNotice] = useState('')
  const [failure, setFailure] = useState('')
  const [loading, setLoading] = useState(true)
  const [retry, setRetry] = useState(0)
  useEffect(() => {
    const controller = new AbortController()
    async function load() {
      setLoading(true); setFailure(''); setNotice(''); setSegments([])
      try {
        let id = dispatchId
        let req = requestId
        if (id && !req) req = (await getObservedDispatch(sessionId, id, undefined, controller.signal)).request_id
        if (!req) { setNotice(t('observability.oldProcess')); return }
        let cursor: string | undefined
        const calls: ObservedCall[] = []
        const seen = new Set<string>()
        do {
          const page = await getObservedRequest(sessionId, req, cursor, controller.signal)
          if (!id) {
            const matches = page.dispatches.filter((item) => item.tool_call_id === toolCallId)
            if (matches.length === 1) id = matches[0].dispatch_id
          }
          calls.push(...page.calls, ...(page.tools || []))
          if (page.recording_status !== 'complete') setNotice(t(`observability.${page.recording_status === 'expired' ? 'dispatchExpired' : page.recording_status === 'unrecorded' ? 'unrecorded' : 'partial'}`))
          cursor = page.next_cursor
          if (cursor && seen.has(cursor)) throw new Error('Repeated history cursor')
          if (cursor) seen.add(cursor)
        } while (cursor)
        if (!id) { setNotice(t('observability.oldProcess')); return }
        const dispatch = await getObservedDispatch(sessionId, id, undefined, controller.signal)
        if (dispatch.details_expired) setNotice(t('observability.dispatchExpired'))
        else if (dispatch.details_truncated) setNotice(t('observability.partial'))
        const output: Segment[] = []
        for (const call of calls.filter((item) => item.dispatch_id === id).sort((a, b) => a.started_at.localeCompare(b.started_at))) {
          if (call.kind === 'tool') output.push({ type: 'tool_call', call_id: call.tool_call_id || call.call_id, name: call.tool_name || '', args: call.arguments || '', result: call.content || '', error: call.error || '', done: call.status !== 'running', duration_ms: call.duration_ms })
          else {
            if (call.reasoning) output.push({ type: 'thinking', text: call.reasoning })
            if (call.content) output.push({ type: 'content', text: call.content })
          }
          if (call.recording_status !== 'complete') setNotice(t('observability.partial'))
        }
        if (!controller.signal.aborted) setSegments(output)
      } catch (e) { if (!controller.signal.aborted) setFailure(e instanceof Error ? e.message : t('observability.error')) }
      finally { if (!controller.signal.aborted) setLoading(false) }
    }
    void load()
    return () => controller.abort()
  }, [sessionId, requestId, dispatchId, toolCallId, retry, t])
  const hasResult = segments.some((segment) => segment.type === 'content' && segment.text === result)
  return <div className="space-y-3">
    {loading && <p role="status">{t('observability.loading')}</p>}
    {failure && <div role="alert">{failure}<Button size="xs" variant="ghost" onClick={() => setRetry((n) => n + 1)}>{t('observability.retry')}</Button></div>}
    {notice && <p role="status" className="text-xs text-muted-foreground">{notice}</p>}
    {segments.length > 0 && <AgentStreamView state={{ agent_id: dispatchId || '', request_id: requestId, processing: false, segments, iteration: 0 }} scrollContainerRef={scrollRef} conversationStyle identity={`${sessionId}:${dispatchId || toolCallId}`} />}
    {result && !hasResult && <MarkdownPreview content={result} />}
    {error && <pre role="alert" className="whitespace-pre-wrap break-words text-destructive">{error}</pre>}
    {!loading && !failure && !notice && !segments.length && !result && !error && <p>{t('observability.unrecorded')}</p>}
  </div>
}
