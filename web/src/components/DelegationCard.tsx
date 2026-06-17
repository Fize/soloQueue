import { useState } from 'react'
import { Bot, Loader2, CheckCircle2, XCircle, X } from 'lucide-react'
import { useAgentStore } from '@/stores/agentStore'
import { useAgentStream } from '@/hooks/useAgentStream'
import { AgentStreamView } from '@/components/AgentStreamView'
import { cn } from '@/lib/utils'

export function DelegationCard({
  name,
  args,
  done,
  error,
  durationMs,
}: {
  name: string
  args: string
  done: boolean
  error?: string
  durationMs?: number
}) {
  const [modalOpen, setModalOpen] = useState(false)
  const agentsData = useAgentStore((state) => state.agents)

  // Extract agent name
  // Format from tool: delegate_Andrej_Karpathy
  const rawName = name.startsWith('delegate_') ? name.substring(9) : name
  const cleanName = rawName.replace(/_/g, ' ')

  // Match agent in store
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

  // Clickable only if running and we have instanceId
  const isClickable = running && !!instanceId

  return (
    <>
      <div className="my-2">
        <button
          onClick={() => {
            if (isClickable) setModalOpen(true)
          }}
          disabled={!isClickable}
          className={cn(
            'w-full text-left rounded-xl border p-3.5 transition-all duration-200 flex flex-col gap-2.5 relative overflow-hidden',
            isClickable
              ? 'cursor-pointer hover:bg-muted/40 hover:ring-1 hover:ring-violet-500/20 border-violet-500/20 bg-violet-500/5'
              : 'cursor-default border-border/80 bg-card/40'
          )}
        >
          {running && (
            <div className="absolute top-0 left-0 right-0 h-[2px] bg-violet-500 animate-pulse" />
          )}

          <div className="flex items-center justify-between gap-2 w-full">
            <div className="flex items-center gap-2 min-w-0">
              {running ? (
                <Loader2 className="h-3.5 w-3.5 animate-spin text-violet-500 shrink-0" />
              ) : error ? (
                <XCircle className="h-3.5 w-3.5 text-destructive shrink-0" />
              ) : (
                <CheckCircle2 className="h-3.5 w-3.5 text-emerald-500 shrink-0" />
              )}
              <span className="font-semibold text-xs truncate text-foreground">
                Delegated to: {cleanName}
              </span>
            </div>

            <div className="flex items-center gap-1.5 text-[10px] uppercase font-semibold tracking-wider text-muted-foreground/60 select-none">
              {running ? 'Running' : error ? 'Failed' : 'Completed'}
            </div>
          </div>

          {taskText && (
            <div className="text-[11px] text-muted-foreground line-clamp-2 bg-muted/20 rounded p-1.5 font-mono border border-border/10">
              {taskText}
            </div>
          )}

          {durationMs != null && durationMs > 0 && (
            <div className="text-[9px] text-muted-foreground/40 font-mono self-end">
              {(durationMs / 1000).toFixed(1)}s
            </div>
          )}
        </button>
      </div>

      {modalOpen && isClickable && agentStream && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm animate-in fade-in duration-200"
          onClick={() => setModalOpen(false)}
        >
          <div
            className="bg-card border border-border/60 rounded-2xl shadow-2xl w-[90vw] max-w-4xl h-[80vh] flex flex-col overflow-hidden animate-in zoom-in-95 duration-200"
            onClick={(e) => e.stopPropagation()}
          >
            {/* Header */}
            <div className="shrink-0 flex items-center justify-between px-5 py-4 border-b border-border/50 bg-card/50">
              <div className="flex items-center gap-2.5">
                <div className="h-7 w-7 rounded-lg bg-violet-500/10 flex items-center justify-center">
                  <Bot className="h-4 w-4 text-violet-500" />
                </div>
                <div>
                  <h3 className="text-sm font-semibold text-foreground">
                    {cleanName} Event Stream
                  </h3>
                  <p className="text-[10px] text-muted-foreground/60 font-mono">
                    Instance: {instanceId}
                  </p>
                </div>
              </div>
              <button
                onClick={() => setModalOpen(false)}
                className="text-muted-foreground hover:text-foreground p-1.5 rounded-lg hover:bg-muted/50 transition-colors cursor-pointer"
              >
                <X className="h-4 w-4" />
              </button>
            </div>

            {/* Scrollable Event Stream Content */}
            <div className="flex-1 overflow-y-auto p-6 bg-card/20">
              <AgentStreamView state={agentStream} />
            </div>
          </div>
        </div>
      )}
    </>
  )
}
