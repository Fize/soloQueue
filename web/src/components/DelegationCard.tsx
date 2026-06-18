import { useState } from 'react'
import { Bot, Loader2, CheckCircle2, XCircle, X, ExternalLink } from 'lucide-react'
import { useAgentStore } from '@/stores/agentStore'
import { useAgentStream } from '@/hooks/useAgentStream'
import { AgentStreamView } from '@/components/AgentStreamView'
import { cn } from '@/lib/utils'

export function DelegationCard({
  name,
  args,
  callId: _callId,
  done,
  result,
  error,
  durationMs,
}: {
  name: string
  args: string
  callId: string
  done: boolean
  result?: string
  error?: string
  durationMs?: number
}) {
  const [modalOpen, setModalOpen] = useState(false)
  const agentsData = useAgentStore((state) => state.agents)

  const rawName = name.startsWith('delegate_') ? name.substring(9) : name
  const cleanName = rawName.replace(/_/g, ' ')

  const namePart = rawName.toLowerCase().replace(/[\s_-]/g, '')
  const matchedAgent = agentsData?.agents.find(
    (a) => a.name.toLowerCase().replace(/[\s_-]/g, '') === namePart
  )

  const instanceId = matchedAgent?.instance_id || null
  const agentStream = useAgentStream(instanceId)

  const running = !done

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
      <div className="my-2">
        <button
          onClick={() => {
            if (isClickable) setModalOpen(true)
          }}
          disabled={!isClickable}
          className={cn(
            'w-full text-left rounded-xl border overflow-hidden transition-all',
            isClickable
              ? 'cursor-pointer hover:shadow-md hover:shadow-violet-500/5 border-violet-500/30 bg-gradient-to-r from-violet-500/8 via-violet-500/4 to-transparent'
              : 'cursor-default border-border/50 bg-card/20'
          )}
        >
          {/* Accent bar */}
          <div
            className={cn(
              'h-0.5 w-full',
              running
                ? 'bg-gradient-to-r from-violet-500 to-purple-400'
                : error
                  ? 'bg-destructive/60'
                  : 'bg-emerald-500/40'
            )}
          />

          <div className="flex items-center gap-2.5 px-3 py-2">
            {/* Agent icon */}
            <div
              className={cn(
                'h-7 w-7 rounded-lg flex items-center justify-center shrink-0',
                running ? 'bg-violet-500/15' : error ? 'bg-destructive/10' : 'bg-emerald-500/10'
              )}
            >
              {running ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin text-violet-500" />
              ) : error ? (
                <XCircle className="h-3.5 w-3.5 text-destructive" />
              ) : (
                <CheckCircle2 className="h-3.5 w-3.5 text-emerald-500" />
              )}
            </div>

            {/* Content */}
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-1.5">
                <span className="font-semibold text-xs text-foreground/90 truncate">
                  {cleanName}
                </span>
                <span
                  className={cn(
                    'text-[9px] uppercase font-bold tracking-wider px-1.5 py-0.5 rounded-md',
                    running
                      ? 'bg-violet-500/15 text-violet-600'
                      : error
                        ? 'bg-destructive/10 text-destructive'
                        : 'bg-emerald-500/10 text-emerald-600'
                  )}
                >
                  {running ? 'Running' : error ? 'Failed' : 'Done'}
                </span>
                {isClickable && (
                  <ExternalLink className="h-2.5 w-2.5 text-violet-500/40 shrink-0" />
                )}
              </div>
              {taskText && (
                <p className="text-[11px] text-muted-foreground/60 truncate mt-0.5">{taskText}</p>
              )}
            </div>

            {/* Duration */}
            {durationMs != null && durationMs > 0 && !running && (
              <span className="text-[9px] text-muted-foreground/30 font-mono shrink-0">
                {(durationMs / 1000).toFixed(1)}s
              </span>
            )}
          </div>
        </button>
      </div>

      {modalOpen && isClickable && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm animate-in fade-in duration-200"
          onClick={() => setModalOpen(false)}
        >
          <div
            className="bg-card border border-border/60 rounded-2xl shadow-2xl w-[90vw] max-w-4xl h-[80vh] flex flex-col overflow-hidden animate-in zoom-in-95 duration-200"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="shrink-0 flex items-center justify-between px-5 py-4 border-b border-border/50 bg-card/50">
              <div className="flex items-center gap-2.5">
                <div className="h-7 w-7 rounded-lg bg-violet-500/10 flex items-center justify-center">
                  <Bot className="h-4 w-4 text-violet-500" />
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
                        agentStream ? 'bg-emerald-500' : 'bg-muted-foreground/40'
                      )}
                    />
                    <span
                      className={cn(
                        'text-[10px] font-medium',
                        agentStream ? 'text-emerald-600' : 'text-muted-foreground/60'
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
                      <pre className="whitespace-pre-wrap break-all rounded bg-muted/50 p-2 text-xs leading-relaxed max-h-96 overflow-y-auto">
                        {result}
                      </pre>
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
                  <CheckCircle2 className="h-8 w-8 text-emerald-500/60" />
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
