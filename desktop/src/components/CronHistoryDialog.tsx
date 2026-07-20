import { useEffect, useState } from 'react'
import { listCronHistory, getCronHistory } from '@/lib/api'
import type { CronTask, CronExecutionRecord, CronHistoryDetail, TimelineEvent } from '@/types'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Badge } from '@/components/ui/badge'
import { GlassCard } from '@/components/ui/glass-card'
import { useTranslation } from '@/lib/i18n'
import {
  CheckCircle2,
  XCircle,
  AlertTriangle,
  Loader2,
  Brain,
  Wrench,
  ChevronDown,
  ChevronRight,
  Clock,
  Cpu,
  Terminal,
  History,
} from 'lucide-react'
import { toast } from 'sonner'

// ─── Types ──────────────────────────────────────────────────────────────────

interface Props {
  open: boolean
  task: CronTask | null
  onClose: () => void
}

interface ExecutionStep {
  type: 'user' | 'thinking' | 'tool_call' | 'tool_result' | 'assistant'
  content: string
  name?: string
  callId?: string
  args?: string
}

// ─── Helpers ────────────────────────────────────────────────────────────────

const statusIcon = (status: string) => {
  switch (status) {
    case 'success': return <CheckCircle2 className="w-4 h-4 text-emerald-400" />
    case 'failed': return <XCircle className="w-4 h-4 text-red-400" />
    case 'panic': return <AlertTriangle className="w-4 h-4 text-amber-400" />
    default: return <Loader2 className="w-4 h-4 text-muted-foreground animate-spin" />
  }
}

function statusLabel(status: string, t: ReturnType<typeof useTranslation>['t']) {
  switch (status) {
    case 'success': return t('cron.jobSucceeded')
    case 'failed': return t('cron.jobFailed')
    case 'panic': return t('cron.jobPanic')
    default: return status
  }
}

function statusBadge(status: string) {
  const map: Record<string, { label: string; variant: 'default' | 'destructive' | 'secondary' | 'outline' }> = {
    success: { label: '✓', variant: 'default' },
    failed: { label: '✕', variant: 'destructive' },
    panic: { label: '!', variant: 'destructive' },
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

function eventsToSteps(events: TimelineEvent[]): ExecutionStep[] {
  const steps: ExecutionStep[] = []
  for (const ev of events) {
    if (ev.type !== 'message' || !ev.msg) continue
    const m = ev.msg

    if (m.role === 'user') {
      steps.push({ type: 'user', content: m.content })
    } else if (m.role === 'assistant') {
      if (m.tool_calls && m.tool_calls.length > 0) {
        for (const tc of m.tool_calls) {
          steps.push({ type: 'tool_call', content: tc.name, callId: tc.id, name: tc.name, args: tc.arguments })
        }
      }
      if (m.reasoning) steps.push({ type: 'thinking', content: m.reasoning })
      if (m.content) steps.push({ type: 'assistant', content: m.content })
    } else if (m.role === 'tool') {
      steps.push({ type: 'tool_result', content: m.content, name: m.name, callId: m.tool_call_id })
    }
  }
  return steps
}

// ─── Step Renderer ──────────────────────────────────────────────────────────

function StepView({ step, t }: { step: ExecutionStep; t: ReturnType<typeof useTranslation>['t'] }) {
  const [expanded, setExpanded] = useState(false)

  switch (step.type) {
    case 'user':
      return (
        <div className="rounded-lg bg-muted/40 border border-border/40 p-3">
          <div className="flex items-center gap-2 mb-1">
            <Terminal className="w-3.5 h-3.5 text-muted-foreground" />
            <span className="text-[11px] font-medium text-muted-foreground uppercase tracking-wide">{t('cron.instructionLabel')}</span>
          </div>
          <p className="text-sm text-foreground/80 whitespace-pre-wrap">{step.content}</p>
        </div>
      )

    case 'thinking':
      return (
        <div className="rounded-lg border border-amber-500/20 bg-amber-500/5 overflow-hidden">
          <button onClick={() => setExpanded(!expanded)} className="w-full flex items-center gap-2 px-3 py-2 text-left hover:bg-amber-500/10 transition-colors">
            {expanded ? <ChevronDown className="w-3.5 h-3.5 text-amber-400" /> : <ChevronRight className="w-3.5 h-3.5 text-amber-400" />}
            <Brain className="w-3.5 h-3.5 text-amber-400" />
            <span className="text-xs font-medium text-amber-400/80">{t('cron.thinkingLabel')}</span>
          </button>
          {expanded && (
            <div className="px-3 pb-3 pt-0">
              <pre className="text-xs text-amber-300/60 whitespace-pre-wrap font-mono leading-relaxed">{step.content}</pre>
            </div>
          )}
        </div>
      )

    case 'tool_call':
      return (
        <div className="rounded-lg border border-blue-500/20 bg-blue-500/5 overflow-hidden">
          <button onClick={() => setExpanded(!expanded)} className="w-full flex items-center gap-2 px-3 py-2 text-left hover:bg-blue-500/10 transition-colors">
            {expanded ? <ChevronDown className="w-3.5 h-3.5 text-blue-400" /> : <ChevronRight className="w-3.5 h-3.5 text-blue-400" />}
            <Wrench className="w-3.5 h-3.5 text-blue-400" />
            <span className="text-xs font-medium text-blue-400/80">{t('cron.toolCall')} {step.name}</span>
          </button>
          {expanded && (
            <div className="px-3 pb-3 pt-0">
              <pre className="text-xs text-blue-300/60 font-mono mt-1 bg-background/40 rounded p-2 overflow-x-auto max-h-32 overflow-y-auto">{step.args}</pre>
            </div>
          )}
        </div>
      )

    case 'tool_result':
      return (
        <div className="rounded-lg border border-emerald-500/20 bg-emerald-500/5 overflow-hidden">
          <button onClick={() => setExpanded(!expanded)} className="w-full flex items-center gap-2 px-3 py-2 text-left hover:bg-emerald-500/10 transition-colors">
            {expanded ? <ChevronDown className="w-3.5 h-3.5 text-emerald-400" /> : <ChevronRight className="w-3.5 h-3.5 text-emerald-400" />}
            <CheckCircle2 className="w-3.5 h-3.5 text-emerald-400" />
            <span className="text-xs font-medium text-emerald-400/80">{t('cron.resultOf')} {step.name}</span>
          </button>
          {expanded && (
            <div className="px-3 pb-3 pt-0">
              <pre className="text-xs text-emerald-300/60 whitespace-pre-wrap font-mono leading-relaxed max-h-64 overflow-y-auto">{step.content}</pre>
            </div>
          )}
        </div>
      )

    case 'assistant':
      return (
        <div className="rounded-lg bg-background/40 border border-border/30 p-3">
          <p className="text-sm text-foreground/85 whitespace-pre-wrap leading-relaxed">{step.content}</p>
        </div>
      )

    default:
      return null
  }
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

  const steps = detail ? eventsToSteps(detail.events) : []

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

          {/* Right: Execution Detail */}
          <div className="flex-1 overflow-y-auto p-4 min-w-0">
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

            {selectedId && !detailLoading && detail && (
              <div className="max-w-2xl mx-auto space-y-3">
                <GlassCard variant="flat" className="p-3">
                  <div className="flex items-center gap-3 flex-wrap text-xs text-muted-foreground">
                    <span className="flex items-center gap-1">
                      {statusIcon(detail.execution.status)}
                      {statusLabel(detail.execution.status, t)}
                    </span>
                    <span className="flex items-center gap-1">
                      <Clock className="w-3 h-3" />
                      {formatDuration(detail.execution.duration_ms)}
                    </span>
                    {detail.execution.model_id && (
                      <span className="flex items-center gap-1">
                        <Cpu className="w-3 h-3" />
                        {detail.execution.model_id}
                      </span>
                    )}
                    <span>{formatTime(detail.execution.executed_at)}</span>
                  </div>
                  {detail.execution.error_message && (
                    <p className="text-xs text-red-400 mt-2 bg-red-500/10 rounded p-2">{detail.execution.error_message}</p>
                  )}
                </GlassCard>

                {steps.length === 0 && (
                  <p className="text-center text-muted-foreground text-sm py-8">{t('cron.noExecutionDetail')}</p>
                )}
                {steps.map((step, idx) => (
                  <StepView key={`${idx}-${step.type}`} step={step} t={t} />
                ))}
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
