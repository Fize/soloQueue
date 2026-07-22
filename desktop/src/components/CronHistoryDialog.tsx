import { useEffect, useState } from 'react'
import { listCronHistory, getCronHistory } from '@/lib/api'
import type { CronTask, CronExecutionRecord, CronHistoryDetail, TimelineEvent } from '@/types'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Badge } from '@/components/ui/badge'
import { GlassCard } from '@/components/ui/glass-card'
import { useTranslation } from '@/lib/i18n'
import { StreamdownPreview } from '@/components/ui/streamdown-preview'
import { formatToolCallHeader } from '@/lib/utils'
import { tryPrettify } from '@/components/chat/ToolCallSegment'
import {
  CheckCircle2,
  XCircle,
  AlertTriangle,
  Loader2,
  ChevronDown,
  ChevronRight,
  Clock,
  Cpu,
  Terminal,
  History,
  AlertCircle,
  Bot,
} from 'lucide-react'
import { toast } from 'sonner'

// ─── Types ──────────────────────────────────────────────────────────────────

interface Props {
  open: boolean
  task: CronTask | null
  onClose: () => void
}

interface ToolCallStep {
  type: 'tool_call'
  name: string
  callId: string
  args: string
  result?: string
  error?: string
}

interface ThinkingStep {
  type: 'thinking'
  content: string
}

type WorkedStep = ToolCallStep | ThinkingStep

interface ParsedTimeline {
  userContent: string
  workedSteps: WorkedStep[]
  assistantContent: string
}

// ─── Helpers ────────────────────────────────────────────────────────────────

const statusIcon = (status: string) => {
  switch (status) {
    case 'success': return <CheckCircle2 className="w-4 h-4 text-emerald-400" />
    case 'failed':  return <XCircle className="w-4 h-4 text-red-400" />
    case 'panic':   return <AlertTriangle className="w-4 h-4 text-amber-400" />
    default:        return <Loader2 className="w-4 h-4 text-muted-foreground animate-spin" />
  }
}

function statusLabel(status: string, t: ReturnType<typeof useTranslation>['t']) {
  switch (status) {
    case 'success': return t('cron.jobSucceeded')
    case 'failed':  return t('cron.jobFailed')
    case 'panic':   return t('cron.jobPanic')
    default:        return status
  }
}

function statusBadge(status: string) {
  const map: Record<string, { label: string; variant: 'default' | 'destructive' | 'secondary' | 'outline' }> = {
    success: { label: '✓', variant: 'default' },
    failed:  { label: '✕', variant: 'destructive' },
    panic:   { label: '!', variant: 'destructive' },
  }
  const s = map[status] || { label: status, variant: 'outline' as const }
  return <Badge variant={s.variant} className="text-[10px] px-1 py-0 min-w-[18px] text-center">{s.label}</Badge>
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`
  return `${Math.floor(ms / 60000)}m ${Math.floor((ms % 60000) / 1000)}s`
}

function formatTime(ts: string): string {
  try {
    const d = new Date(ts)
    return d.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit' })
  } catch { return ts }
}

/**
 * Clean up common LLM markdown syntax artifacts:
 * 1. Fix closing backticks stuck directly to headers, rules or tables (e.g. ```--- or ```##)
 * 2. Balance unclosed triple backtick code fences
 */
function normalizeMarkdown(text: string): string {
  if (!text) return ''
  let s = text

  // Fix closing backticks directly attached to horizontal rules, headers, lists, or tables without a newline
  s = s.replace(/(^|\n)```\s*(--{2,}|=={2,}|\*\*{2,}|#{1,6}\s|\|)/g, '$1```\n\n$2')

  // Auto-close unclosed code fence if total count of ``` is odd
  const fenceMatches = s.match(/(^|\n)```/g)
  if (fenceMatches && fenceMatches.length % 2 !== 0) {
    s = s.trimEnd() + '\n```'
  }

  return s
}

/**
 * Parse raw timeline events for a cron execution into structured data:
 * - userContent: prompt / instruction
 * - workedSteps: thinking and tool_call steps (with tool execution results paired)
 * - assistantContent: complete combined assistant markdown output
 */
function parseTimelineEvents(events: TimelineEvent[]): ParsedTimeline {
  let userContent = ''
  const workedSteps: WorkedStep[] = []
  const assistantChunks: string[] = []
  const callMap = new Map<string, ToolCallStep>()

  for (const ev of events) {
    if (ev.type !== 'message' || !ev.msg) continue
    const m = ev.msg

    if (m.role === 'user') {
      if (!userContent && m.content) {
        userContent = m.content
      }
    } else if (m.role === 'assistant') {
      if (m.reasoning && m.reasoning.trim()) {
        workedSteps.push({ type: 'thinking', content: m.reasoning.trim() })
      }
      if (m.tool_calls && m.tool_calls.length > 0) {
        for (const tc of m.tool_calls) {
          const step: ToolCallStep = { type: 'tool_call', name: tc.name, callId: tc.id, args: tc.arguments }
          workedSteps.push(step)
          callMap.set(tc.id, step)
        }
      }
      if (m.content && m.content.trim()) {
        assistantChunks.push(m.content.trim())
      }
    } else if (m.role === 'tool') {
      const tc = m.tool_call_id ? callMap.get(m.tool_call_id) : undefined
      if (tc) {
        tc.result = m.content
      }
    }
  }

  // Deduplicate overlapping or repeated chunks
  const cleanedChunks = assistantChunks.map((c) => c.trim()).filter(Boolean)
  const filteredChunks: string[] = []
  for (let i = 0; i < cleanedChunks.length; i++) {
    const curr = cleanedChunks[i]
    const isSub = cleanedChunks.some(
      (other, j) => j !== i && other.includes(curr) && other.length > curr.length
    )
    if (!isSub) {
      if (filteredChunks.length === 0 || filteredChunks[filteredChunks.length - 1] !== curr) {
        filteredChunks.push(curr)
      }
    }
  }

  let rawCombined = filteredChunks.join('\n\n')

  // Check if rawCombined contains identical repeated block halves (e.g. A + "\n\n" + A)
  const parts = rawCombined.split(/\n{2,}/)
  if (parts.length > 1) {
    const half = Math.floor(parts.length / 2)
    const firstHalf = parts.slice(0, half).join('\n\n')
    const secondHalf = parts.slice(half, half * 2).join('\n\n')
    if (firstHalf && firstHalf === secondHalf) {
      rawCombined = parts.slice(0, half).concat(parts.slice(half * 2)).join('\n\n')
    }
  }

  const assistantContent = normalizeMarkdown(rawCombined)

  return { userContent, workedSteps, assistantContent }
}

// ─── Sub-components ─────────────────────────────────────────────────────────

/** User instruction card */
function UserInstructionCard({ content }: { content: string }) {
  const { t } = useTranslation()
  return (
    <div className="rounded-xl bg-muted/40 border border-border/40 px-4 py-3">
      <div className="flex items-center gap-2 mb-1.5">
        <Terminal className="w-3.5 h-3.5 text-muted-foreground" />
        <span className="text-[11px] font-medium text-muted-foreground uppercase tracking-wide">{t('cron.instructionLabel')}</span>
      </div>
      <p className="text-sm text-foreground/80 whitespace-pre-wrap leading-relaxed">{content}</p>
    </div>
  )
}

/** Tool call row inside worked group */
function ToolCallRow({ step }: { step: ToolCallStep }) {
  const { t } = useTranslation()
  const [expanded, setExpanded] = useState(false)
  const done = step.result !== undefined || step.error !== undefined
  const hasError = !!step.error

  return (
    <div className="text-xs border rounded-xl overflow-hidden w-full border-border/60 bg-muted/20">
      <button
        onClick={() => setExpanded(!expanded)}
        className="flex items-center gap-2 w-full px-3 py-2 transition-colors text-muted-foreground hover:text-foreground text-left"
      >
        {!done ? (
          <Loader2 className="h-3.5 w-3.5 animate-spin text-signal shrink-0" />
        ) : hasError ? (
          <AlertCircle className="h-3.5 w-3.5 text-destructive shrink-0" />
        ) : (
          <div className="h-3.5 w-3.5 rounded-full bg-success/20 flex items-center justify-center shrink-0">
            <div className="h-1.5 w-1.5 rounded-full bg-success" />
          </div>
        )}
        <span className="font-mono text-[11px] truncate flex-1 min-w-0 whitespace-nowrap">
          {formatToolCallHeader(step.name, step.args).replace(/\r?\n/g, ' ')}
        </span>
        <span className="text-[10px] uppercase tracking-wider text-muted-foreground/40 shrink-0">
          {!done ? t('common.running') : hasError ? t('common.failed') : t('common.done')}
        </span>
        {expanded
          ? <ChevronDown className="h-3.5 w-3.5 shrink-0 text-muted-foreground/60" />
          : <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground/60" />
        }
      </button>

      {expanded && (
        <div className="px-3 pb-2 pt-2 space-y-2 border-t border-border/30">
          {step.args && (
            <div>
              <div className="text-[10px] font-semibold uppercase tracking-wider mb-1 text-muted-foreground/50">
                {t('common.arguments')}
              </div>
              <pre className="text-[11px] leading-relaxed whitespace-pre-wrap overflow-x-auto rounded-lg p-2 max-h-[150px] overflow-y-auto font-mono bg-muted/40">
                {tryPrettify(step.args)}
              </pre>
            </div>
          )}
          {(step.result || step.error) && (
            <div>
              <div className="text-[10px] font-semibold uppercase tracking-wider mb-1 text-muted-foreground/50">
                {step.error ? t('common.error') : t('common.result')}
              </div>
              <pre className={`text-[11px] leading-relaxed whitespace-pre-wrap overflow-x-auto rounded-lg p-2 max-h-[250px] overflow-y-auto font-mono ${step.error ? 'bg-destructive/5 text-destructive/90' : 'bg-muted/40'}`}>
                {step.error || step.result}
              </pre>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

/** Worked group — collapsible details for execution steps (thinking & tools) */
function WorkedGroupView({ steps }: { steps: WorkedStep[] }) {
  const [open, setOpen] = useState(false)

  const toolCalls = steps.filter((s) => s.type === 'tool_call') as ToolCallStep[]
  const statsText = toolCalls.length > 0
    ? `(${toolCalls.length} step${toolCalls.length > 1 ? 's' : ''})`
    : `(${steps.length} item${steps.length > 1 ? 's' : ''})`

  return (
    <details className="group/worked" open={open}>
      <summary
        onClick={(e) => { e.preventDefault(); setOpen(!open) }}
        className="flex items-center gap-1.5 text-xs cursor-pointer transition-colors py-1 text-muted-foreground hover:text-foreground/70 select-none list-none"
      >
        <div className="h-2 w-2 rounded-full bg-success/30 shrink-0 flex items-center justify-center">
          <div className="h-1 w-1 rounded-full bg-success" />
        </div>
        <span className="font-medium inline-flex items-center gap-1.5">
          <span>worked</span>
          {statsText && <span className="opacity-60 font-normal">{statsText}</span>}
        </span>
        <ChevronRight className="h-3 w-3 ml-auto group-open/worked:hidden" />
        <ChevronDown className="h-3 w-3 ml-auto hidden group-open/worked:block" />
      </summary>

      <div className="mt-1.5 ml-2.5 pl-3.5 space-y-3 border-l-2 border-muted-foreground/20">
        {steps.map((step, idx) => {
          if (step.type === 'thinking') {
            return (
              <div key={idx} className="text-xs whitespace-pre-wrap leading-relaxed text-muted-foreground/75 font-mono">
                {step.content}
              </div>
            )
          }
          if (step.type === 'tool_call') {
            return (
              <div key={idx} className="my-1.5">
                <ToolCallRow step={step} />
              </div>
            )
          }
          return null
        })}
      </div>
    </details>
  )
}

/** Assistant response card with Markdown rendering */
function AssistantOutputCard({ content }: { content: string }) {
  return (
    <div className="rounded-xl border border-border/40 bg-card/30 p-4 space-y-2">
      <div className="flex items-center gap-2 mb-2 text-xs font-medium text-muted-foreground pb-2 border-b border-border/20">
        <Bot className="w-4 h-4 text-primary/70" />
        <span>Execution Result</span>
      </div>
      <div className="text-sm text-foreground/90 leading-relaxed">
        <StreamdownPreview content={content} />
      </div>
    </div>
  )
}

// ─── Execution List Item ────────────────────────────────────────────────────

function ExecutionItem({
  record,
  selected,
  onClick,
}: {
  record: CronExecutionRecord
  selected: boolean
  onClick: () => void
}) {
  return (
    <button
      onClick={onClick}
      className={`w-full text-left px-3 py-2.5 rounded-lg transition-colors border ${
        selected
          ? 'bg-primary/10 border-primary/30'
          : 'bg-card/40 border-transparent hover:bg-accent/30 hover:border-border/40'
      }`}
    >
      <div className="flex items-center gap-2 mb-1">
        {statusIcon(record.status)}
        <span className="text-xs font-mono text-muted-foreground">{formatTime(record.executed_at)}</span>
        {statusBadge(record.status)}
      </div>
      <div className="flex items-center gap-3 text-[11px] text-muted-foreground">
        <span className="flex items-center gap-1">
          <Clock className="w-3 h-3" />
          {formatDuration(record.duration_ms)}
        </span>
        {record.model_id && (
          <span className="flex items-center gap-1">
            <Cpu className="w-3 h-3" />
            {record.model_id}
          </span>
        )}
      </div>
      {record.result_summary && (
        <p className="text-xs text-foreground/60 mt-1 truncate">{record.result_summary}</p>
      )}
      {record.error_message && (
        <p className="text-xs text-red-400/70 mt-1 truncate">{record.error_message}</p>
      )}
    </button>
  )
}

// ─── Main Component ─────────────────────────────────────────────────────────

export function CronHistoryDialog({ open, task, onClose }: Props) {
  const { t } = useTranslation()
  const [records, setRecords] = useState<CronExecutionRecord[]>([])
  const [loading, setLoading] = useState(false)
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [detail, setDetail] = useState<CronHistoryDetail | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)

  useEffect(() => {
    if (!open || !task) return
    setLoading(true)
    setSelectedId(null)
    setDetail(null)
    listCronHistory(task.id, { limit: 50 })
      .then((data) => {
        setRecords(data)
        if (data.length > 0) {
          setSelectedId(data[0].id)
          loadDetail(task.id, data[0].id)
        }
      })
      .catch((err) => toast.error(`${t('cron.loadHistoryFailed')}: ${err.message}`))
      .finally(() => setLoading(false))
  }, [open, task?.id])

  function loadDetail(taskId: string, execId: string) {
    setDetailLoading(true)
    getCronHistory(taskId, execId)
      .then(setDetail)
      .catch((err) => toast.error(`${t('cron.loadDetailFailed')}: ${err.message}`))
      .finally(() => setDetailLoading(false))
  }

  function handleSelect(record: CronExecutionRecord) {
    if (!task) return
    setSelectedId(record.id)
    loadDetail(task.id, record.id)
  }

  if (!task) return null

  const parsed = detail ? parseTimelineEvents(detail.events) : null

  return (
    <Dialog open={open} onOpenChange={(open) => { if (!open) onClose() }}>
      <DialogContent className="max-w-5xl h-[80vh] flex flex-col p-0 gap-0" showCloseButton>
        <DialogHeader className="px-5 py-4 border-b border-border/50 shrink-0">
          <DialogTitle className="flex items-center gap-2 text-base">
            <History className="w-4 h-4 text-muted-foreground" />
            {t('cron.executionHistory')}
            <span className="text-muted-foreground font-normal text-sm">· {task.title}</span>
          </DialogTitle>
        </DialogHeader>

        <div className="flex flex-1 min-h-0 overflow-hidden">
          {/* Left: Execution List */}
          <div className="w-64 shrink-0 border-r border-border/50 overflow-y-auto p-3 space-y-1.5">
            {loading && (
              <div className="space-y-2">
                {[1, 2, 3].map((i) => (
                  <div key={i} className="h-16 rounded-lg bg-muted/30 animate-pulse" />
                ))}
              </div>
            )}
            {!loading && records.length === 0 && (
              <div className="text-center py-8 text-muted-foreground text-sm">
                <Clock className="w-6 h-6 mx-auto mb-2 opacity-40" />
                {t('cron.noExecutionsYet')}
              </div>
            )}
            {!loading && records.map((r) => (
              <ExecutionItem key={r.id} record={r} selected={selectedId === r.id} onClick={() => handleSelect(r)} />
            ))}
          </div>

          {/* Right: Execution Detail Panel */}
          <div className="flex-1 overflow-y-auto p-5 min-w-0">
            {!selectedId && (
              <div className="flex items-center justify-center h-full text-muted-foreground text-sm">
                {t('cron.selectExecutionHint')}
              </div>
            )}

            {selectedId && detailLoading && (
              <div className="flex items-center justify-center h-full">
                <Loader2 className="w-6 h-6 animate-spin text-muted-foreground" />
              </div>
            )}

            {selectedId && !detailLoading && detail && parsed && (
              <div className="max-w-3xl mx-auto space-y-4">
                {/* Meta GlassCard */}
                <GlassCard variant="flat" className="p-3.5">
                  <div className="flex items-center gap-3 flex-wrap text-xs text-muted-foreground">
                    <span className="flex items-center gap-1 font-medium text-foreground">
                      {statusIcon(detail.execution.status)}
                      {statusLabel(detail.execution.status, t)}
                    </span>
                    <span className="flex items-center gap-1">
                      <Clock className="w-3.5 h-3.5 text-muted-foreground/70" />
                      {formatDuration(detail.execution.duration_ms)}
                    </span>
                    {detail.execution.model_id && (
                      <span className="flex items-center gap-1 font-mono">
                        <Cpu className="w-3.5 h-3.5 text-muted-foreground/70" />
                        {detail.execution.model_id}
                      </span>
                    )}
                    <span className="font-mono text-muted-foreground/80">{formatTime(detail.execution.executed_at)}</span>
                  </div>
                  {detail.execution.error_message && (
                    <p className="text-xs text-red-400 mt-2 bg-red-500/10 border border-red-500/20 rounded-lg p-2.5 font-mono">{detail.execution.error_message}</p>
                  )}
                </GlassCard>

                {/* Task prompt / instruction */}
                {parsed.userContent && (
                  <UserInstructionCard content={parsed.userContent} />
                )}

                {/* Worked steps group (thinking + tool calls) */}
                {parsed.workedSteps.length > 0 && (
                  <WorkedGroupView steps={parsed.workedSteps} />
                )}

                {/* Final Assistant Markdown Output */}
                {parsed.assistantContent ? (
                  <AssistantOutputCard content={parsed.assistantContent} />
                ) : (
                  !parsed.workedSteps.length && !parsed.userContent && (
                    <p className="text-center text-muted-foreground text-sm py-8">{t('cron.noExecutionDetail')}</p>
                  )
                )}
              </div>
            )}

            {selectedId && !detailLoading && !detail && (
              <div className="flex items-center justify-center h-full text-muted-foreground text-sm">
                {t('cron.noExecutionDetail')}
              </div>
            )}
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
