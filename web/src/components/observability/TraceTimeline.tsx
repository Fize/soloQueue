import { useId, useState } from 'react'
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
import { formatExecutionTime, formatExecutionTimeCompact } from '@/lib/api/observability-api'
import { cn } from '@/lib/utils'
import { groupParallel, isTraceError, subtaskHue, type TraceNode } from './trace'

export function TraceTimeline({
  root,
  start,
  end,
  selected,
  flash,
  expand,
  onSelect,
}: {
  root: TraceNode
  start?: number
  end?: number
  selected: string
  flash?: Set<string>
  expand?: Set<string>
  onSelect: (node: TraceNode) => void
}) {
  const { t } = useTranslation()
  const baseId = useId()
  // Only the request row is open by default: a turn with several subtasks stays
  // readable until the reader opens the one they care about.
  const [toggled, setToggled] = useState<Record<string, boolean>>({})
  const span = start !== undefined && end !== undefined ? Math.max(1, end - start) : undefined
  const indent = (depth: number) => 28 + Math.min(depth, 6) * 12
  const percent = (from: number, to: number) => {
    const offset =
      start !== undefined && span !== undefined ? Math.max(0, ((from - start) / span) * 100) : 0
    return {
      offset,
      width:
        start !== undefined && span !== undefined
          ? Math.min(100 - offset, Math.max(0.5, ((to - from) / span) * 100))
          : 0,
    }
  }
  const groups = (node: TraceNode) =>
    node.children.length > 0 && node.children.every((child) => child.kind === 'dispatch')
      ? groupParallel(node.children)
      : node.children.map((child) => [child])
  const row = (node: TraceNode, depth: number, group?: TraceNode[], owner?: number) => {
    const failure = isTraceError(node.status)
    const dispatch = node.kind === 'dispatch'
    const Icon = failure
      ? AlertCircle
      : node.kind === 'call'
        ? Sparkles
        : node.kind === 'tool'
          ? Wrench
          : dispatch
            ? GitBranch
            : Circle
    const known =
      start !== undefined &&
      span !== undefined &&
      node.start !== undefined &&
      node.end !== undefined
    const bar = known ? percent(node.start!, node.end!) : { offset: 0, width: 0 }
    const duration = known
      ? formatExecutionTime(node.end! - node.start!)
      : t('observability.timingMissing')
    const label = node.kind === 'request' ? t('observability.mainAgent') : node.label
    const detail = node.dispatch?.task || node.dispatch?.task_name || label
    // Explicit toggle wins; otherwise only the request row, plus any subtask
    // that just received loaded spans, starts open.
    const open = toggled[node.id] ?? (depth === 0 || !!expand?.has(node.id))
    const shadow =
      group &&
      group.length > 1 &&
      group.every((item) => item.start !== undefined && item.end !== undefined)
        ? percent(
            Math.min(...group.map((item) => item.start!)),
            Math.max(...group.map((item) => item.end!))
          )
        : undefined
    const titleId = `${baseId}-${node.id}-title`
    const statusId = `${baseId}-${node.id}-status`
    // Nested calls keep their owning subtask number visible while scrolled.
    const childOwner = dispatch ? node.ordinal : owner
    // A 7% wash, a 55% rail and a 16% chip. Identity only — never a status colour.
    const accentOf = (value?: number) =>
      value === undefined ? undefined : `var(--color-subtask-${subtaskHue(value)})`
    const accent = dispatch ? accentOf(node.ordinal) : undefined
    const ownerAccent = accentOf(owner)
    const wash = (color: string | undefined, amount: number) =>
      color ? `color-mix(in srgb, ${color} ${amount}%, transparent)` : undefined
    return (
      <li key={node.id} className="min-w-0" style={{ backgroundColor: wash(accent, 7) }}>
        <div
          className={cn(
            'relative flex min-w-0 items-stretch border-b border-border/40',
            flash?.has(node.id) && 'ring-1 ring-inset ring-primary/50',
            selected === node.id ? 'bg-primary/12' : 'hover:bg-muted/40'
          )}
        >
          {accent && (
            <span
              aria-hidden
              className="absolute inset-y-0 left-0 w-0.5"
              style={{ backgroundColor: wash(accent, 55) }}
            />
          )}
          {node.children.length > 0 ? (
            <button
              type="button"
              aria-label={`${t(open ? 'observability.collapse' : 'observability.expand')} ${label}`}
              aria-expanded={open}
              className="absolute inset-y-0 z-10 flex w-6 items-center justify-center rounded focus-visible:ring-2 focus-visible:ring-ring"
              style={{ left: Math.min(depth, 6) * 12 }}
              onClick={() => setToggled((old) => ({ ...old, [node.id]: !open }))}
            >
              {open ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
            </button>
          ) : null}
          <button
            type="button"
            aria-pressed={selected === node.id}
            aria-labelledby={`${titleId} ${statusId}`}
            onClick={() => onSelect(node)}
            className="grid min-w-0 flex-1 grid-cols-[minmax(0,1fr)_minmax(72px,32%)] items-center gap-3 rounded px-1 py-3 text-left focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
          >
            <span className="min-w-0 space-y-1" style={{ paddingLeft: indent(depth) }}>
              <span className="flex items-start gap-1.5">
                {node.ordinal !== undefined && (
                  <span
                    className={cn(
                      'mt-0.5 inline-flex h-[18px] min-w-[18px] shrink-0 items-center justify-center rounded-md px-1 text-[10px] font-semibold tabular-nums',
                      !accent && 'bg-muted/60 font-medium text-muted-foreground'
                    )}
                    style={accent ? { color: accent, backgroundColor: wash(accent, 16) } : undefined}
                  >
                    #{node.ordinal}
                  </span>
                )}
                <Icon
                  className={cn(
                    'mt-0.5 size-3.5 shrink-0',
                    failure ? 'text-destructive' : !accent && 'text-muted-foreground'
                  )}
                  style={!failure && accent ? { color: accent } : undefined}
                />
                <span
                  id={titleId}
                  title={detail}
                  className={cn(
                    'min-w-0 break-words text-xs',
                    dispatch ? 'font-semibold text-foreground' : 'font-medium'
                  )}
                >
                  {label}
                </span>
              </span>
              <span className="flex flex-wrap items-center gap-x-2 gap-y-1 pl-5 text-[10px] text-muted-foreground">
                {owner !== undefined && (
                  <span className="font-mono" style={{ color: wash(ownerAccent, 75) }}>
                    #{owner}
                  </span>
                )}
                {node.dispatch && (
                  <span className="rounded bg-muted/60 px-1 font-mono">{node.dispatch.executor}</span>
                )}
                <span id={statusId} className={cn(failure && 'text-destructive')}>
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
                aria-label={`${label}: ${duration}`}
              >
                {shadow && (
                  <span
                    className="absolute inset-y-0 z-0 bg-muted-foreground/25"
                    style={{ left: `${shadow.offset}%`, width: `${shadow.width}%` }}
                  />
                )}
                {known ? (
                  <span
                    className={cn(
                      'absolute inset-y-0 z-1 min-w-px rounded-sm',
                      failure
                        ? 'bg-destructive/80'
                        : !accent && (node.kind === 'call' ? 'bg-primary/70' : 'bg-primary/30')
                    )}
                    style={{
                      left: `${bar.offset}%`,
                      width: `${bar.width}%`,
                      backgroundColor: failure ? undefined : wash(accent, 60),
                    }}
                  />
                ) : (
                  <span className="absolute inset-0 border border-dashed border-muted-foreground/40" />
                )}
              </span>
              <span className="text-right text-[10px] tabular-nums text-muted-foreground">
                {known
                  ? `+${formatExecutionTimeCompact(node.start! - start!)} · ${formatExecutionTimeCompact(node.end! - node.start!)}`
                  : duration}
              </span>
            </span>
          </button>
        </div>
        {open && node.children.length > 0 && (
          <ul>
            {groups(node).map((group) =>
              group.length > 1 ? (
                <li key={`parallel:${group[0].id}`} className="relative min-w-0">
                  <span
                    aria-hidden
                    className="absolute inset-y-0 w-0.5 rounded-full bg-border"
                    style={{ left: indent(depth + 1) - 4 }}
                  />
                  <div
                    className="flex items-center gap-1.5 border-b border-border/40 py-2 text-[10px] text-muted-foreground"
                    style={{ paddingLeft: indent(depth + 1) }}
                  >
                    <GitBranch className="size-3 shrink-0" />
                    <span>
                      {t('observability.parallelGroup', {
                        count: group.length,
                        duration: formatExecutionTime(
                          Math.max(...group.map((item) => item.end!)) -
                            Math.min(...group.map((item) => item.start!))
                        ),
                      })}
                    </span>
                  </div>
                  <ul>{group.map((item) => row(item, depth + 1, group, childOwner))}</ul>
                </li>
              ) : (
                row(group[0], depth + 1, undefined, childOwner)
              )
            )}
          </ul>
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
