import { useState } from 'react'
import { Bot, Loader2, CheckCircle2, XCircle, X } from 'lucide-react'
import { useAgentStore } from '@/stores/agentStore'
import { useAgentStream } from '@/hooks/useAgentStream'
import { AgentStreamView } from '@/components/AgentStreamView'
import { cn } from '@/lib/utils'

export function DelegationCard({
  name,
  args,
  callId: _callId,
  done,
  error,
  durationMs,
}: {
  name: string
  args: string
  callId: string
  done: boolean
  error?: string
  durationMs?: number
}) {
  const [modalOpen, setModalOpen] = useState(false)
  const agentsData = useAgentStore((state) => state.agents)

  const rawName = name.startsWith('delegate_') ? name.substring(9) : name
  const cleanName = rawName.replace(/_/g, ' ')

  const namePart = rawName.toLowerCase().replace(/[\s_]/g, '')
  const matchedAgent = agentsData?.agents.find(
    (a) => a.name.toLowerCase().replace(/[\s_]/g, '') === namePart
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

  const isClickable = running

  return (
    <>
      <div className="my-1.5">
        <button
          onClick={() => {
            if (isClickable) setModalOpen(true)
          }}
          disabled={!isClickable}
          className={cn(
            'w-full text-left rounded-lg border px-2.5 py-2 transition-colors flex items-center gap-2',
            isClickable
              ? 'cursor-pointer hover:bg-muted/40 border-violet-500/20 bg-violet-500/5'
              : 'cursor-default border-border/60 bg-card/30'
          )}
        >
          {running ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin text-violet-500 shrink-0" />
          ) : error ? (
            <XCircle className="h-3.5 w-3.5 text-destructive shrink-0" />
          ) : (
            <CheckCircle2 className="h-3.5 w-3.5 text-emerald-500 shrink-0" />
          )}
          <span className="font-medium text-xs truncate text-foreground/80 min-w-0">
            {cleanName}
          </span>
          {taskText && (
            <span className="text-[11px] text-muted-foreground/50 truncate min-w-0 flex-1">
              &mdash; {taskText}
            </span>
          )}
          <span className="text-[9px] uppercase font-semibold tracking-wider text-muted-foreground/40 shrink-0 ml-auto">
            {running ? 'Running' : error ? 'Failed' : 'Done'}
          </span>
          {durationMs != null && durationMs > 0 && !running && (
            <span className="text-[9px] text-muted-foreground/30 font-mono shrink-0">
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
              {agentStream ? (
                <AgentStreamView state={agentStream} />
              ) : (
                <div className="flex flex-col items-center justify-center h-full gap-3 text-muted-foreground/40">
                  <Bot className="h-8 w-8" />
                  <p className="text-xs">Waiting for agent stream...</p>
                  {taskText && (
                    <p className="text-[11px] max-w-md text-center">{taskText}</p>
                  )}
                </div>
              )}
            </div>
          </div>
        </div>
      )}
    </>
  )
}
