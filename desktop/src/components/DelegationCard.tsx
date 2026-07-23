import { useTranslation } from '@/lib/i18n'
import { useState } from 'react'
import { Bot, Loader2, CheckCircle2, XCircle, X, ExternalLink } from 'lucide-react'
import { useAgentStore } from '@/stores/agentStore'
import { useAgentStream } from '@/hooks/useAgentStream'
import { AgentStreamView } from '@/components/AgentStreamView'
import { MarkdownPreview } from '@/components/ui/markdown-preview'
import { cn } from '@/lib/utils'

export function DelegationCard({
  name,
  args,
  callId: _callId,
  done,
  result,
  error,
  durationMs,
  agentInstanceId,
}: {
  name: string
  args: string
  callId: string
  done: boolean
  result?: string
  error?: string
  durationMs?: number
  agentInstanceId?: string
}) {
  const [modalOpen, setModalOpen] = useState(false)
  const agentsData = useAgentStore((state) => state.agents)

  const { t } = useTranslation()

  const rawName = name.startsWith('delegate_') ? name.substring(9) : name
  const cleanName = rawName.replace(/_/g, ' ')

  const namePart = rawName.toLowerCase().replace(/[\s_-]/g, '')
  const matchedAgent = agentsData?.agents.find(
    (a) => a.name.toLowerCase().replace(/[\s_-]/g, '') === namePart
  )

  const instanceId = agentInstanceId || matchedAgent?.instance_id || null
  const agentStream = useAgentStream(instanceId)

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

      {modalOpen && isClickable && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm animate-in fade-in duration-200"
          onClick={() => setModalOpen(false)}
        >
          <div
            className="bg-background border border-border/60 rounded-2xl shadow-2xl w-[90vw] max-w-4xl h-[80vh] flex flex-col overflow-hidden animate-in zoom-in-95 duration-200"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="shrink-0 flex items-center justify-between px-5 py-4 border-b border-border/50 bg-card/50">
              <div className="flex items-center gap-2.5">
                <div className="h-7 w-7 rounded-lg bg-primary/10 flex items-center justify-center">
                  <Bot className="h-4 w-4 text-primary" />
                </div>
                <div>
                  <h3 className="text-sm font-semibold text-foreground">
                    {cleanName} Event Stream
                  </h3>
                  {instanceId && (
                    <p className="text-[10px] text-muted-foreground/60 font-mono">
                      Instance: {instanceId}
                    </p>
                  )}
                  <div className="flex items-center gap-1.5 mt-0.5">
                    <span
                      className={cn(
                        'h-1.5 w-1.5 rounded-full',
                        agentStream ? 'bg-success' : 'bg-muted-foreground/40'
                      )}
                    />
                    <span
                      className={cn(
                        'text-[10px] font-medium',
                        agentStream ? 'text-success' : 'text-muted-foreground/60'
                      )}
                    >
                      {agentStream ? 'Stream live' : 'Stream unavailable'}
                    </span>
                  </div>
                </div>
              </div>
              <button
                onClick={() => setModalOpen(false)}
                className="text-muted-foreground hover:text-foreground p-1.5 rounded-lg hover:bg-muted/50 transition-colors cursor-pointer"
              >
                <X className="h-4 w-4" />
              </button>
            </div>

            <div className="flex-1 overflow-y-auto p-6 bg-card/20">
              {done && (result || error) ? (
                <div className="space-y-4">
                  <div>
                    <h4 className="text-xs font-medium text-foreground/80">
                      {agentStream
                        ? 'Task result'
                        : 'Task completed — agent stream no longer available'}
                    </h4>
                    {taskText && (
                      <p className="text-[11px] text-muted-foreground mt-1">{taskText}</p>
                    )}
                  </div>
                  {result && (
                    <div>
                      <div className="mb-1 text-xs font-medium text-muted-foreground">Result</div>
                      <div className="rounded bg-muted/50 p-3 text-xs leading-relaxed max-h-96 overflow-y-auto border border-border/40">
                        <MarkdownPreview content={result} />
                      </div>
                    </div>
                  )}
                  {error && (
                    <div>
                      <div className="mb-1 text-xs font-medium text-destructive">Error</div>
                      <pre className="whitespace-pre-wrap break-all rounded bg-destructive/10 border border-destructive/20 p-2 text-xs text-destructive">
                        {error}
                      </pre>
                    </div>
                  )}
                </div>
              ) : agentStream ? (
                <AgentStreamView state={agentStream} />
              ) : running ? (
                <div className="flex flex-col items-center justify-center h-full gap-3 text-muted-foreground/40">
                  <Bot className="h-8 w-8" />
                  <p className="text-xs">Waiting for agent stream...</p>
                  {taskText && <p className="text-[11px] max-w-md text-center">{taskText}</p>}
                </div>
              ) : (
                <div className="flex flex-col items-center justify-center h-full gap-3 text-muted-foreground/40">
                  <CheckCircle2 className="h-8 w-8 text-success/60" />
                  <p className="text-xs">Task completed — no result available</p>
                  {taskText && <p className="text-[11px] max-w-md text-center">{taskText}</p>}
                </div>
              )}
            </div>
          </div>
        </div>
      )}
    </>
  )
}
