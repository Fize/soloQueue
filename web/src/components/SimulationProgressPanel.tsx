import { useEffect, useRef } from 'react'
import { Loader2, CheckCircle2, Clock, MessageSquare, Zap, ArrowLeft } from 'lucide-react'
import type { SimulationProgress, SimulationMessage } from '@/types'

interface SimulationProgressPanelProps {
  progress: SimulationProgress
  messages: SimulationMessage[]
  selectedAgentId?: string | null
  onSelectAgent?: (agentId: string | null) => void
}

export function SimulationProgressPanel({
  progress,
  messages,
  selectedAgentId,
  onSelectAgent,
}: SimulationProgressPanelProps) {
  const timelineEndRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    timelineEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages, selectedAgentId])

  const phaseSteps = [
    { key: 'initializing', label: 'Initializing' },
    { key: 'running', label: 'Running' },
    { key: 'generating_report', label: 'Report' },
    { key: 'completed', label: 'Done' },
  ] as const

  const currentPhaseIdx = phaseSteps.findIndex((s) => s.key === progress.phase)

  const formatTime = (seconds: number) => {
    const m = Math.floor(seconds / 60)
    const s = Math.floor(seconds % 60)
    return `${m}m ${s}s`
  }

  const agentList = Object.values(progress.agent_states || {})
  const filteredMessages = selectedAgentId
    ? messages.filter((m) => m.agent_id === selectedAgentId)
    : messages

  return (
    <div className="flex flex-col h-full overflow-hidden">
      <div className="shrink-0 space-y-3 p-4 pb-2 border-b border-border/40">
        <div className="flex items-center gap-4">
          <div className="flex-1 space-y-1">
            <div className="flex items-center justify-between text-xs text-muted-foreground font-mono">
              <span>{progress.current_actions} actions</span>
              <span className="font-semibold text-foreground">
                {progress.progress_percent.toFixed(1)}%
              </span>
            </div>
            <div className="relative h-2 w-full overflow-hidden rounded-full bg-muted">
              <div
                className="h-full rounded-full bg-primary transition-all duration-500 ease-out"
                style={{ width: `${Math.min(progress.progress_percent, 100)}%` }}
              />
            </div>
          </div>

          <div className="flex items-center gap-3 text-[10px] text-muted-foreground font-mono whitespace-nowrap">
            <div className="flex items-center gap-1">
              <Clock className="h-3 w-3" />
              <span>{formatTime(progress.elapsed_seconds)}</span>
            </div>
            {progress.estimated_remaining_seconds > 0 && (
              <div className="flex items-center gap-1">
                <Zap className="h-3 w-3" />
                <span>ETA {formatTime(progress.estimated_remaining_seconds)}</span>
              </div>
            )}
          </div>
        </div>

        <div className="flex items-center gap-1">
          {phaseSteps.map((step, idx) => (
            <div key={step.key} className="flex items-center gap-1 flex-1">
              <div className="flex items-center gap-1.5 min-w-0">
                {idx < currentPhaseIdx ? (
                  <CheckCircle2 className="h-3.5 w-3.5 shrink-0 text-success" />
                ) : idx === currentPhaseIdx ? (
                  <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin text-primary" />
                ) : (
                  <div className="h-3.5 w-3.5 shrink-0 rounded-full border-2 border-muted-foreground/30" />
                )}
                <span
                  className={`text-[10px] font-mono truncate ${
                    idx === currentPhaseIdx
                      ? 'text-primary font-semibold'
                      : idx < currentPhaseIdx
                        ? 'text-muted-foreground'
                        : 'text-muted-foreground/40'
                  }`}
                >
                  {step.label}
                </span>
              </div>
              {idx < phaseSteps.length - 1 && (
                <div
                  className={`flex-1 h-px mx-1 ${
                    idx < currentPhaseIdx ? 'bg-success/50' : 'bg-muted-foreground/20'
                  }`}
                />
              )}
            </div>
          ))}
        </div>
      </div>

      <div className="shrink-0 px-4 py-2 overflow-x-auto border-b border-border/20">
        <div className="flex gap-2 min-w-max pb-1">
          {agentList.map((agent) => {
            const isSelected = selectedAgentId === agent.persona_id
            return (
              <div
                key={agent.persona_id}
                onClick={() => onSelectAgent?.(isSelected ? null : agent.persona_id)}
                className={`flex items-center gap-2 rounded-lg border px-2.5 py-1.5 text-xs min-w-[120px] cursor-pointer transition-all ${
                  isSelected
                    ? 'border-primary ring-1 ring-primary/30 bg-primary/5'
                    : agent.status === 'thinking'
                      ? 'border-primary/40 bg-primary/5 hover:border-primary/60'
                      : agent.status === 'spoke'
                        ? 'border-border bg-card/50 hover:border-border/80'
                        : 'border-border/60 bg-muted/20 hover:border-border/80'
                }`}
              >
                <div className="relative shrink-0">
                  <div
                    className={`flex h-7 w-7 items-center justify-center rounded-full text-[10px] font-bold ${
                      isSelected
                        ? 'bg-primary text-primary-foreground'
                        : 'bg-muted text-muted-foreground'
                    }`}
                  >
                    {agent.name.charAt(0).toUpperCase()}
                  </div>
                  {agent.status === 'thinking' && (
                    <div className="absolute -right-0.5 -top-0.5 h-3 w-3">
                      <div className="absolute inset-0 rounded-full bg-primary animate-ping opacity-40" />
                      <div className="absolute inset-0.5 rounded-full bg-primary" />
                    </div>
                  )}
                </div>
                <div className="min-w-0">
                  <div className="font-semibold text-foreground text-[10px] truncate max-w-[80px]">
                    {agent.name}
                  </div>
                  <div className="flex items-center gap-1 text-[9px] text-muted-foreground font-mono">
                    <MessageSquare className="h-2.5 w-2.5" />
                    <span>{agent.message_count}</span>
                    {agent.status === 'thinking' && (
                      <span className="text-primary/80 ml-1 animate-pulse">thinking...</span>
                    )}
                  </div>
                </div>
              </div>
            )
          })}
        </div>
      </div>

      <div className="flex-1 overflow-y-auto px-4 py-2 space-y-1.5 min-h-0">
        {selectedAgentId && (
          <button
            onClick={() => onSelectAgent?.(null)}
            className="flex items-center gap-1 text-[10px] text-muted-foreground hover:text-foreground font-mono mb-2 transition-colors"
          >
            <ArrowLeft className="h-3 w-3" />
            Back to all messages
          </button>
        )}

        {selectedAgentId && (
          <div className="rounded-lg bg-primary/5 border border-primary/20 px-3 py-2 mb-3 text-xs">
            <span className="font-semibold text-primary">
              {agentList.find((a) => a.persona_id === selectedAgentId)?.name || selectedAgentId}
            </span>
            <span className="text-muted-foreground ml-2">
              — {filteredMessages.length} message{filteredMessages.length !== 1 ? 's' : ''}
            </span>
          </div>
        )}

        {filteredMessages.length === 0 ? (
          <div className="flex h-24 items-center justify-center text-xs text-muted-foreground font-mono">
            <Loader2 className="mr-2 h-3 w-3 animate-spin" />
            {selectedAgentId
              ? 'Waiting for this agent to respond...'
              : 'Waiting for agents to respond...'}
          </div>
        ) : (
          [...filteredMessages].reverse().map((msg, idx) => (
            <div
              key={`${msg.seq_num}-${idx}`}
              className="rounded-lg border border-border/60 bg-card/30 p-3 text-xs"
            >
              {!selectedAgentId && (
                <div className="flex items-center justify-between gap-2 mb-1.5">
                  <div className="flex items-center gap-1.5 min-w-0">
                    <span className="flex h-4 w-4 shrink-0 items-center justify-center rounded-full bg-muted text-[7px] font-bold text-muted-foreground">
                      {msg.agent_name?.charAt(0)?.toUpperCase() || '?'}
                    </span>
                    <span className="font-semibold text-primary truncate">{msg.agent_name}</span>
                    <span
                      className={`shrink-0 rounded px-1 py-0.5 text-[8px] font-mono font-semibold ${
                        msg.type === 'rebuttal'
                          ? 'bg-error/10 text-error'
                          : msg.type === 'question'
                            ? 'bg-info/10 text-info'
                            : 'bg-muted text-muted-foreground'
                      }`}
                    >
                      {msg.type.toUpperCase()}
                    </span>
                  </div>
                  <span className="shrink-0 text-[8px] text-muted-foreground font-mono">
                    R{msg.round}
                  </span>
                </div>
              )}
              <p
                className={`text-foreground/90 leading-relaxed whitespace-pre-wrap ${!selectedAgentId ? 'line-clamp-2' : ''}`}
              >
                {msg.content}
              </p>
              {msg.reasoning && selectedAgentId && (
                <details className="mt-2.5">
                  <summary className="text-[9px] text-muted-foreground cursor-pointer select-none hover:text-foreground font-mono">
                    View Agent Reasoning
                  </summary>
                  <p className="mt-1 text-[9px] text-muted-foreground/80 italic bg-background/50 p-2 rounded border border-border/40 leading-relaxed">
                    {msg.reasoning}
                  </p>
                </details>
              )}
            </div>
          ))
        )}
        <div ref={timelineEndRef} />
      </div>
    </div>
  )
}
