import { useEffect, useRef, useState } from 'react'
import { Activity, ArrowUpRight } from 'lucide-react'
import { useTranslation } from '@/lib/i18n'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from '@/components/ui/dialog'
import { getObservedRequest, formatExecutionTime } from '@/lib/api/observability-api'
import type { ObservedRequest } from '@/lib/api/observability-api'
import { ToolInspector } from './ToolDetails'
import { CallInspector } from './CallDetails'
import { TraceTimeline } from './TraceTimeline'
import { buildTrace, isTraceError, type TraceNode } from './trace'
import { DispatchProcess } from './DispatchProcess'

function ExecutionDetailsView({
  sessionId,
  requestId,
  running = false,
}: {
  sessionId: string
  requestId: string
  running?: boolean
}) {
  const { t } = useTranslation()
  const [data, setData] = useState<ObservedRequest | null>(null)
  const [error, setError] = useState('')
  const [open, setOpen] = useState(false)
  const [refresh, setRefresh] = useState(0)
  const [moreBusy, setMoreBusy] = useState(false)
  const [selectedID, setSelectedID] = useState('request')
  const inspectorRef = useRef<HTMLElement>(null)
  useEffect(() => {
    if (selectedID !== 'request' && window.innerWidth < 1024) {
      inspectorRef.current?.scrollIntoView?.({ block: 'start' })
    }
  }, [selectedID])
  const identity = `${sessionId}\u0000${requestId}`
  const identityRef = useRef(identity)
  const loadedRef = useRef<{ count: number; generation?: string }>({ count: 0 })
  useEffect(() => {
    loadedRef.current = {
      count: (data?.calls.length || 0) + (data?.tools?.length || 0),
      generation: data?.generation,
    }
  }, [data])
  useEffect(() => {
    const controller = new AbortController()
    let timer: ReturnType<typeof setTimeout> | undefined
    identityRef.current = identity
    async function load() {
      try {
        let value = await getObservedRequest(sessionId, requestId, undefined, controller.signal)
        const target =
          open && value.generation === loadedRef.current.generation ? loadedRef.current.count : 0
        const cursors = new Set<string>()
        // Refresh every page already opened, so later tool spans do not retain
        // a stale running state after the first page reports completion.
        while (value.next_cursor && value.calls.length + (value.tools?.length || 0) < target) {
          if (cursors.has(value.next_cursor)) throw new Error(t('observability.error'))
          cursors.add(value.next_cursor)
          const page = await getObservedRequest(
            sessionId,
            requestId,
            value.next_cursor,
            controller.signal
          )
          value = {
            ...value,
            next_cursor: page.next_cursor,
            calls: [...value.calls, ...page.calls],
            tools: [...(value.tools || []), ...(page.tools || [])],
          }
        }
        if (controller.signal.aborted) return
        setData(value)
        setError('')
        if (
          running ||
          value.status === 'running' ||
          value.status === 'queued' ||
          value.calls.some((call) => call.status === 'running') ||
          value.tools?.some((tool) => tool.status === 'running')
        )
          timer = setTimeout(() => void load(), 2000)
      } catch (cause) {
        if (!controller.signal.aborted)
          setError(cause instanceof Error ? cause.message : t('observability.error'))
      }
    }
    void load()
    return () => {
      controller.abort()
      clearTimeout(timer)
      identityRef.current = ''
    }
  }, [sessionId, requestId, identity, running, open, refresh, t])
  async function more() {
    if (!data?.next_cursor || moreBusy) return
    const currentIdentity = identity
    setMoreBusy(true)
    try {
      const value = await getObservedRequest(sessionId, requestId, data.next_cursor)
      if (identityRef.current !== currentIdentity) return
      setData((old) =>
        old
          ? {
              ...value,
              tools: [
                ...(old.tools || []),
                ...(value.tools || []).filter(
                  (tool) => !old.tools?.some((item) => item.call_id === tool.call_id)
                ),
              ],
              calls: [
                ...old.calls,
                ...value.calls.filter(
                  (call) => !old.calls.some((item) => item.call_id === call.call_id)
                ),
              ],
            }
          : value
      )
    } catch (cause) {
      if (identityRef.current === currentIdentity)
        setError(cause instanceof Error ? cause.message : t('observability.error'))
    } finally {
      if (identityRef.current === currentIdentity) setMoreBusy(false)
    }
  }
  const trace = data ? buildTrace(data) : null
  const selected = trace?.nodes.find((node) => node.id === selectedID) || trace?.root
  const statusNotice = data?.metrics_only
    ? t('observability.metricsOnly')
    : data?.recording_status === 'complete'
      ? ''
      : t(`observability.${data?.recording_status || 'unrecorded'}`)
  const durationLabel = data?.duration_source === 'call_span' ? 'callSpan' : 'duration'
  const duration =
    data?.duration_source === 'unknown'
      ? t('observability.unknown')
      : formatExecutionTime(data?.duration_ms || 0)
  const attemptsKnown =
    data?.attempts_reported ?? data?.calls.every((call) => call.attempts_reported)
  const loadedTokens = data?.calls.reduce((sum, call) => sum + call.total_tokens, 0) || 0
  function select(node: TraceNode) {
    setSelectedID(node.id)
  }
  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="mt-2 inline-flex flex-wrap items-center gap-2 rounded-md border border-border/50 bg-muted/20 px-2.5 py-1.5 text-[11px] text-muted-foreground hover:bg-muted/60 hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring"
      >
        <Activity className="size-3.5 text-primary" />
        {data && (data.recording_status !== 'unrecorded' || data.call_count > 0) ? (
          <>
            <span>
              {t(`observability.${durationLabel}`)} {duration}
            </span>
            <span>·</span>
            <span>
              {t('observability.calls')} {data.call_count}
            </span>
            <span>·</span>
            <span>
              {t('observability.subtasks')} {data.dispatches.length}
            </span>
            <span>·</span>
          </>
        ) : null}
        <span className="font-medium">{t('observability.title')}</span>
        <ArrowUpRight className="size-3" />
      </button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent placement="right">
          <DialogHeader className="shrink-0 border-b px-5 pb-4 pt-5 pr-14">
            <DialogTitle className="flex items-center gap-2">
              <Activity className="size-4 text-primary" />
              {t('observability.traceTitle')}
            </DialogTitle>
            <DialogDescription className="truncate font-mono text-[11px]" title={requestId}>
              {requestId}
            </DialogDescription>
          </DialogHeader>
          <div className="shrink-0 space-y-3 border-b bg-muted/15 p-4">
            {data && (
              <>
                <div className="flex flex-wrap items-center justify-between gap-2 text-xs">
                  <span className={isTraceError(data.status) ? 'text-destructive' : 'font-medium'}>
                    {t(
                      `observability.${data.metrics_only ? 'historicalMetrics' : data.status === 'error' ? 'callError' : data.status}`
                    )}
                  </span>
                  <span className="text-muted-foreground">
                    {t('observability.subtasks')} {data.dispatches.length}
                  </span>
                </div>
                <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
                  {[
                    [durationLabel, duration],
                    ['calls', data.call_count],
                    ['attempts', attemptsKnown ? data.attempt_count : t('observability.unknown')],
                    ['loadedTokens', loadedTokens.toLocaleString()],
                  ].map(([label, value]) => (
                    <div
                      key={label}
                      className="rounded-lg border border-border/50 bg-background/60 px-3 py-2"
                    >
                      <div className="text-[10px] text-muted-foreground">
                        {t(`observability.${label}`)}
                      </div>
                      <div className="mt-1 text-base font-semibold tabular-nums">{value}</div>
                    </div>
                  ))}
                </div>
                {statusNotice && <p className="text-xs text-muted-foreground">{statusNotice}</p>}
                {data.duration_source === 'call_span' && !data.timing_complete && (
                  <p className="text-xs text-muted-foreground">
                    {t('observability.timingPartial')}
                  </p>
                )}
                {!attemptsKnown && (
                  <p className="text-xs text-muted-foreground">
                    {t('observability.attemptsUnknown')}
                  </p>
                )}
                {!!data.errors?.length && (
                  <div role="alert" className="whitespace-pre-wrap text-xs text-destructive">
                    {data.errors.join('\n')}
                  </div>
                )}
              </>
            )}
            {error && (
              <div role="alert" className="text-xs text-destructive">
                {t('observability.error')} <span>{error}</span>
                <Button variant="ghost" size="xs" onClick={() => setRefresh((value) => value + 1)}>
                  {t('observability.retry')}
                </Button>
              </div>
            )}
            {!data && !error && (
              <p role="status" className="animate-pulse text-xs text-muted-foreground">
                {t('observability.loading')}
              </p>
            )}
          </div>
          {data && trace && (
            <div className="min-h-0 flex-1 overflow-y-auto lg:overflow-hidden">
              <div className="grid min-h-full lg:h-full lg:grid-cols-[minmax(0,1.15fr)_minmax(0,1fr)]">
                <section
                  className="min-w-0 border-b lg:overflow-y-auto lg:border-b-0 lg:border-r"
                  aria-label={t('observability.trace')}
                >
                  <div className="border-b px-4 py-3">
                    <h3 className="text-xs font-semibold">{t('observability.trace')}</h3>
                    <p className="mt-1 text-[10px] text-muted-foreground">
                      {t('observability.parallelHint')}
                    </p>
                  </div>
                  <TraceTimeline
                    root={trace.root}
                    start={trace.start}
                    end={trace.end}
                    selected={selected?.id || 'request'}
                    onSelect={select}
                  />
                  {data.next_cursor && (
                    <div className="space-y-2 p-3">
                      <p className="text-[10px] text-muted-foreground">
                        {t('observability.moreSpans')}
                      </p>
                      <Button
                        variant="outline"
                        size="xs"
                        disabled={moreBusy}
                        onClick={() => void more()}
                      >
                        {moreBusy ? t('observability.loading') : t('observability.loadMore')}
                      </Button>
                    </div>
                  )}
                </section>
                <section
                  ref={inspectorRef}
                  aria-label={t('observability.nodeDetails')}
                  className="min-w-0 space-y-4 bg-muted/10 p-4 lg:overflow-y-auto"
                >
                  <div>
                    <p className="text-[10px] uppercase tracking-wider text-muted-foreground">
                      {t('observability.nodeDetails')}
                    </p>
                    <h3 className="mt-1 break-words text-sm font-semibold">
                      {selected?.kind === 'request'
                        ? t('observability.mainAgent')
                        : selected?.label}
                    </h3>
                  </div>
                  {selected?.missingParent && (
                    <p className="text-xs text-muted-foreground">
                      {t('observability.parentMissing')}
                    </p>
                  )}
                  {selected?.start === undefined && (
                    <p className="text-xs text-muted-foreground">
                      {t('observability.timingMissing')}
                    </p>
                  )}
                  {selected?.tool ? (
                    <ToolInspector key={selected.id} tool={selected.tool} />
                  ) : selected?.call ? (
                    <CallInspector
                      key={selected.id}
                      call={selected.call}
                      sessionId={sessionId}
                      requestId={requestId}
                    />
                  ) : selected?.dispatch ? (
                    <DispatchProcess
                      key={selected.id}
                      sessionId={sessionId}
                      dispatchId={selected.dispatch.dispatch_id}
                      initial={selected.dispatch}
                    />
                  ) : (
                    <>
                      <p className="text-xs leading-relaxed text-muted-foreground">
                        {t('observability.selectSpan')}
                      </p>
                      <dl className="space-y-3 text-xs">
                        <div>
                          <dt className="text-muted-foreground">{t('observability.started')}</dt>
                          <dd>
                            {trace.root.start === undefined
                              ? '—'
                              : new Date(trace.root.start).toLocaleString()}
                          </dd>
                        </div>
                        <div>
                          <dt className="text-muted-foreground">{t('observability.finished')}</dt>
                          <dd>
                            {data.finished_at ? new Date(data.finished_at).toLocaleString() : '—'}
                          </dd>
                        </div>
                      </dl>
                      {!data.calls.length && (
                        <p className="text-xs text-muted-foreground">
                          {t('observability.noCalls')}
                        </p>
                      )}
                      {!data.dispatches.length && (
                        <p className="text-xs text-muted-foreground">
                          {t('observability.noProcess')}
                        </p>
                      )}
                    </>
                  )}
                </section>
              </div>
            </div>
          )}
          <div className="mt-auto shrink-0 border-t px-5 py-3">
            <a
              href="#/settings/storage?category=execution_details"
              className="text-xs text-primary underline underline-offset-2"
              onClick={() => setOpen(false)}
            >
              {t('observability.settings')}
            </a>
          </div>
        </DialogContent>
      </Dialog>
    </>
  )
}

export function ExecutionDetails(props: {
  sessionId: string
  requestId: string
  running?: boolean
}) {
  return <ExecutionDetailsView key={`${props.sessionId}:${props.requestId}`} {...props} />
}
