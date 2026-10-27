import { useState } from 'react'
import {
  ChevronDown,
  ChevronRight,
  Circle,
  GitBranch,
  Sparkles,
  Wrench,
  AlertCircle,
} from 'lucide-react'
import { useTranslation } from '@/lib/i18n'
import { formatExecutionTime } from '@/lib/api/observability-api'
import { cn } from '@/lib/utils'
import { isTraceError, type TraceNode } from './trace'

export function TraceTimeline({
  root,
  start,
  end,
  selected,
  onSelect,
}: {
  root: TraceNode
  start?: number
  end?: number
  selected: string
  onSelect: (node: TraceNode) => void
}) {
  const { t } = useTranslation()
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set())
  const span = start !== undefined && end !== undefined ? Math.max(1, end - start) : undefined
  const row = (node: TraceNode, depth: number) => {
    const failure = isTraceError(node.status)
    const Icon = failure
      ? AlertCircle
      : node.kind === 'call'
        ? Sparkles
        : node.kind === 'tool'
          ? Wrench
          : node.kind === 'dispatch'
            ? GitBranch
            : Circle
    const known =
      start !== undefined &&
      span !== undefined &&
      node.start !== undefined &&
      node.end !== undefined
    const offset = known ? Math.max(0, ((node.start! - start!) / span!) * 100) : 0
    const width = known
      ? Math.min(100 - offset, Math.max(0.5, ((node.end! - node.start!) / span!) * 100))
      : 0
    const duration = known
      ? formatExecutionTime(node.end! - node.start!)
      : t('observability.timingMissing')
    const title = node.kind === 'request' ? t('observability.mainAgent') : node.label
    const open = !collapsed.has(node.id)
    return (
      <li key={node.id} className="min-w-0">
        <div
          className={cn(
            'relative flex min-w-0 items-stretch border-b border-border/40',
            selected === node.id ? 'bg-primary/8' : 'hover:bg-muted/40'
          )}
        >
          {node.children.length > 0 ? (
            <button
              type="button"
              aria-label={`${t(open ? 'observability.collapse' : 'observability.expand')} ${title}`}
              aria-expanded={open}
              className="absolute inset-y-0 z-10 flex w-7 items-center justify-center rounded focus-visible:ring-2 focus-visible:ring-ring"
              style={{ left: Math.min(depth, 6) * 12 }}
              onClick={() =>
                setCollapsed((old) => {
                  const next = new Set(old)
                  if (open) next.add(node.id)
                  else next.delete(node.id)
                  return next
                })
              }
            >
              {open ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
            </button>
          ) : null}
          <button
            type="button"
            aria-pressed={selected === node.id}
            onClick={() => onSelect(node)}
            className="grid min-w-0 flex-1 grid-cols-[minmax(0,1fr)_minmax(72px,32%)] items-center gap-3 rounded px-1 py-3 text-left focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
          >
            <span
              className="min-w-0 space-y-1"
              style={{ paddingLeft: 28 + Math.min(depth, 6) * 12 }}
            >
              <span className="flex items-start gap-1.5">
                <Icon
                  className={cn(
                    'mt-0.5 size-3.5 shrink-0',
                    failure
                      ? 'text-destructive'
                      : node.kind === 'call'
                        ? 'text-primary'
                        : 'text-muted-foreground'
                  )}
                />
                <span className="min-w-0 break-words text-xs font-medium">{title}</span>
              </span>
              <span className="flex flex-wrap items-center gap-x-2 gap-y-1 pl-5 text-[10px] text-muted-foreground">
                <span className={cn(failure && 'text-destructive')}>
                  {t(
                    `observability.${['error', 'failed', 'panic'].includes(node.status) ? 'callError' : node.status}`
                  )}
                </span>
                {node.tool && <span>{t('observability.toolExecution')}</span>}
                {node.call && (
                  <span>{t(`observability.${node.call.usage_type || 'unknown'}`)}</span>
                )}
                {!!node.call?.retry_count && (
                  <span className="text-amber-600 dark:text-amber-400">
                    {t('observability.retries')} {node.call.retry_count}
                  </span>
                )}
                {node.warning && (
                  <span className="text-amber-600 dark:text-amber-400">
                    {t('observability.recordingNotice')}
                  </span>
                )}
                {node.missingParent && <span>{t('observability.parentMissing')}</span>}
              </span>
            </span>
            <span className="flex min-w-0 flex-col gap-1.5">
              <span
                className="relative h-2.5 overflow-hidden rounded-sm bg-muted/70"
                role="img"
                aria-label={`${title}: ${duration}`}
              >
                {known ? (
                  <span
                    className={cn(
                      'absolute inset-y-0 min-w-px rounded-sm',
                      failure
                        ? 'bg-destructive/80'
                        : node.kind === 'call'
                          ? 'bg-primary/70'
                          : 'bg-primary/30'
                    )}
                    style={{ left: `${offset}%`, width: `${width}%` }}
                  />
                ) : (
                  <span className="absolute inset-0 border border-dashed border-muted-foreground/40" />
                )}
              </span>
              <span className="text-right text-[10px] tabular-nums text-muted-foreground">
                {known ? `+${formatExecutionTime(node.start! - start!)} · ${duration}` : duration}
              </span>
            </span>
          </button>
        </div>
        {open && node.children.length > 0 && (
          <ul>{node.children.map((child) => row(child, depth + 1))}</ul>
        )}
      </li>
    )
  }
  return (
    <div className="min-w-0">
      <div className="flex items-center justify-between border-b px-3 py-2 text-[10px] text-muted-foreground">
        <span>{t('observability.span')}</span>
        <span className="tabular-nums">
          0 s — {span !== undefined ? formatExecutionTime(span) : '—'}
        </span>
      </div>
      <ul aria-label={t('observability.trace')}>{row(root, 0)}</ul>
    </div>
  )
}
