import { useTranslation } from '@/lib/i18n'
import { useRef, useState } from 'react'
import { Loader2, CheckCircle2, XCircle, ExternalLink } from 'lucide-react'
import { useAgentStore } from '@/stores/agentStore'
import { useAgentStream } from '@/hooks/useAgentStream'
import { AgentStreamView } from '@/components/AgentStreamView'
import { MarkdownPreview } from '@/components/ui/markdown-preview'
import { cn } from '@/lib/utils'
import { useChatStore } from '@/stores/chatStore'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from '@/components/ui/dialog'
import { DelegationHistory } from './observability/DelegationHistory'

export function DelegationCard({
  name,
  args,
  callId,
  done,
  result,
  error,
  durationMs,
  agentInstanceId,
  requestId,
  dispatchId,
}: {
  name: string
  args: string
  callId: string
  done: boolean
  result?: string
  error?: string
  durationMs?: number
  agentInstanceId?: string
  requestId?: string
  dispatchId?: string
}) {
  const [modalOpen, setModalOpen] = useState(false)
  const streamScrollRef = useRef<HTMLDivElement>(null)
  const activeSessionId = useChatStore((state) => state.activeSessionId)
  const requestSessionId = useChatStore((state) => requestId ? state.activeRequests[requestId]?.sessionId : undefined)
  const sessionId = requestSessionId || activeSessionId
  const agentsData = useAgentStore((state) => state.agents)

  const { t } = useTranslation()

  const getTargetName = () => {
    const raw = name.startsWith('delegate_') ? name.substring(9) : name
    if (raw !== 'delegate' && raw) {
      return raw
    }
    try {
      const parsed = JSON.parse(args)
      if (parsed.target) return parsed.target
    } catch {
      const match = args.match(/"target"\s*:\s*"([^"]+)"/)
      if (match && match[1]) return match[1]
    }
    return raw || 'agent'
  }

  const rawTarget = getTargetName()
  const cleanName = rawTarget.replace(/_/g, ' ')

  const namePart = rawTarget.toLowerCase().replace(/[\s_-]/g, '')
  const matchedAgent = agentsData?.agents.find(
    (a) => a.name.toLowerCase().replace(/[\s_-]/g, '') === namePart
  )

  const instanceId = agentInstanceId || matchedAgent?.instance_id || null
  const agentStream = useAgentStream(instanceId, requestId)

  const running = !done
  const cancelled = done && error === 'Cancelled by user'

  const getTaskText = () => {
    try {
      const parsed = JSON.parse(args)
      return parsed.task || ''
    } catch {
      return args
    }
  }
  const taskText = getTaskText()

  const isClickable = true // always clickable — user may inspect agent stream at any time

  return (
    <>
      <div className="my-1.5">
        <button
          onClick={() => {
            if (isClickable) setModalOpen(true)
          }}
          disabled={!isClickable}
          className={cn(
            'group relative flex w-full items-center gap-3 rounded-[12px] border p-2.5 text-left transition-all duration-200 ease-out outline-none focus-visible:ring-2 focus-visible:ring-primary/20',
            isClickable ? 'cursor-pointer hover:shadow-sm' : 'cursor-default',
            running 
              ? 'border-signal/20 bg-signal/5 hover:border-signal/30' 
              : error 
                ? 'border-destructive/20 bg-destructive/5 hover:border-destructive/30' 
                : 'border-border/40 bg-card hover:border-border/60 hover:bg-muted/40'
          )}
        >
          {/* Subtle pulse for running state */}
          {running && (
            <div className="absolute inset-0 rounded-[12px] ring-1 ring-inset ring-signal/10 animate-pulse pointer-events-none" />
          )}

          {/* Icon */}
          <div
            className={cn(
              'flex h-7 w-7 shrink-0 items-center justify-center rounded-full border shadow-sm transition-colors',
              running 
                ? 'border-signal/20 bg-background text-signal' 
                : error 
                  ? 'border-destructive/20 bg-destructive/10 text-destructive' 
                  : 'border-success/20 bg-success/10 text-success'
            )}
          >
            {running ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : error ? (
              <XCircle className="h-3.5 w-3.5" />
            ) : (
              <CheckCircle2 className="h-3.5 w-3.5" />
            )}
          </div>

          {/* Content */}
          <div className="flex min-w-0 flex-1 flex-col justify-center gap-0.5">
            <div className="flex items-center gap-2">
              <span className="truncate text-xs font-semibold text-foreground/90">
                {cleanName}
              </span>
              <div className="flex items-center gap-1.5 shrink-0">
                <span
                  className={cn(
                    'text-[10px] font-medium tracking-wide',
                    running
                      ? 'text-signal/80'
                      : error
                        ? 'text-destructive/80'
                        : 'text-success/80'
                  )}
                >
                  {running
                    ? t('common.running')
                    : cancelled
                      ? t('common.cancelled')
                      : error
                        ? t('common.failed')
                        : t('common.done')}
                </span>
                {isClickable && (
                  <ExternalLink className="h-3 w-3 text-muted-foreground/40 opacity-0 transition-opacity group-hover:opacity-100" />
                )}
              </div>
            </div>
            {taskText && (
              <p className="truncate text-[11px] text-muted-foreground/60">{taskText}</p>
            )}
          </div>

          {/* Duration */}
          {durationMs != null && durationMs > 0 && !running && (
            <span className="shrink-0 text-[10px] font-medium tabular-nums text-muted-foreground/40">
              {(durationMs / 1000).toFixed(1)}s
            </span>
          )}
        </button>
      </div>

      <Dialog open={modalOpen} onOpenChange={setModalOpen}>
        <DialogContent className="sm:max-w-4xl max-h-[85vh] flex flex-col overflow-hidden">
          <DialogHeader className="shrink-0 pr-8">
            <DialogTitle>{cleanName}</DialogTitle>
            <DialogDescription className="min-w-0 truncate">
              {(taskText || cleanName).replace(/\s+/g, ' ')}
            </DialogDescription>
          </DialogHeader>
          <div ref={streamScrollRef} className="min-h-0 min-w-0 overflow-y-auto">
            {taskText && <details className="mb-4 rounded-md border border-border/40 text-xs">
              <summary className="cursor-pointer px-3 py-2 text-muted-foreground">{t('observability.taskDescription')}</summary>
              <div tabIndex={0} className="max-h-[35vh] overflow-y-auto whitespace-pre-wrap break-words border-t border-border/40 px-3 py-2 text-muted-foreground">{taskText}</div>
            </details>}
            {agentStream ? <AgentStreamView state={agentStream} scrollContainerRef={streamScrollRef} conversationStyle identity={`${sessionId}:${dispatchId || callId}`} /> : sessionId && (dispatchId || requestId) ?
              <DelegationHistory sessionId={sessionId} requestId={requestId} dispatchId={dispatchId} toolCallId={callId} result={result} error={error} scrollRef={streamScrollRef} /> :
              <div className="space-y-3 text-xs">
                {result && <MarkdownPreview content={result} />}
                {error && <pre role="alert" className="whitespace-pre-wrap break-words text-destructive">{error}</pre>}
                {!result && !error && <p className="text-muted-foreground">{running ? t('observability.running') : t('observability.unrecorded')}</p>}
              </div>}
          </div>
        </DialogContent>
      </Dialog>
    </>
  )
}
