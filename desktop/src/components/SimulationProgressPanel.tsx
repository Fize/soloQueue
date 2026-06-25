import { useEffect, useRef } from 'react'
import {
  Loader2,
  CheckCircle2,
  Clock,
  MessageSquare,
  Zap,
  ArrowLeft,
  MessageCircle,
  MapPin,
  Lightbulb,
  Lock,
  AlertTriangle,
  AlertCircle,
  SkipForward,
  LogOut,
  Skull,
  Pause,
} from 'lucide-react'
import type { SimulationProgress, SimulationMessage } from '@/types'

// ── Message type visual configuration ──────────────────────────────────
const MESSAGE_TYPE_CONFIG: Record<string, {
  icon: React.ElementType
  borderColor: string
  badgeBg: string
  badgeText: string
  label: string
}> = {
  speak: {
    icon: MessageCircle,
    borderColor: 'border-l-blue-500/50',
    badgeBg: 'bg-blue-500/10',
    badgeText: 'text-blue-600 dark:text-blue-400',
    label: '对话',
  },
  private_speak: {
    icon: Lock,
    borderColor: 'border-l-violet-500/50',
    badgeBg: 'bg-violet-500/10',
    badgeText: 'text-violet-600 dark:text-violet-400',
    label: '私语',
  },
  agent_move: {
    icon: MapPin,
    borderColor: 'border-l-amber-500/50',
    badgeBg: 'bg-amber-500/10',
    badgeText: 'text-amber-600 dark:text-amber-400',
    label: '移动',
  },
  reflection: {
    icon: Lightbulb,
    borderColor: 'border-l-emerald-500/50',
    badgeBg: 'bg-emerald-500/10',
    badgeText: 'text-emerald-600 dark:text-emerald-400',
    label: '反思',
  },
  conflict: {
    icon: AlertTriangle,
    borderColor: 'border-l-rose-500/50',
    badgeBg: 'bg-rose-500/10',
    badgeText: 'text-rose-600 dark:text-rose-400',
    label: '冲突',
  },
  rebuttal: {
    icon: AlertCircle,
    borderColor: 'border-l-rose-400/50',
    badgeBg: 'bg-rose-400/10',
    badgeText: 'text-rose-500 dark:text-rose-400',
    label: '反驳',
  },
  question: {
    icon: MessageCircle,
    borderColor: 'border-l-cyan-500/50',
    badgeBg: 'bg-cyan-500/10',
    badgeText: 'text-cyan-600 dark:text-cyan-400',
    label: '提问',
  },
  auto_pass: {
    icon: SkipForward,
    borderColor: 'border-l-gray-400/30 border-dashed',
    badgeBg: 'bg-gray-400/10',
    badgeText: 'text-gray-500 dark:text-gray-400',
    label: '例行',
  },
  agent_exit: {
    icon: LogOut,
    borderColor: 'border-l-gray-500/40',
    badgeBg: 'bg-gray-500/10',
    badgeText: 'text-gray-600 dark:text-gray-400',
    label: '退场',
  },
  agent_death_announcement: {
    icon: Skull,
    borderColor: 'border-l-red-600/50',
    badgeBg: 'bg-red-600/10',
    badgeText: 'text-red-600 dark:text-red-400',
    label: '死亡',
  },
}

function getTypeConfig(type: string) {
  return MESSAGE_TYPE_CONFIG[type] || {
    icon: MessageSquare,
    borderColor: 'border-l-muted-foreground/30',
    badgeBg: 'bg-muted',
    badgeText: 'text-muted-foreground',
    label: type,
  }
}

function formatRound(round: number): string {
  if (round === 0) return '初始化'
  return `第 ${round} 轮`
}

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
    { key: 'initializing', label: '环境准备' },
    { key: 'generating_plans', label: '生成计划' },
    { key: 'building_prompts', label: '构建提示' },
    { key: 'running', label: '运行中' },
    { key: 'generating_report', label: '生成报告' },
    { key: 'completed', label: '完成' },
  ] as const

  // Map any backend phase to the closest step index
  const phaseOrder = phaseSteps.map(s => s.key)
  function resolveStepIndex(phase: string): number {
    const exact = phaseOrder.indexOf(phase as any)
    if (exact !== -1) return exact
    // Unknown/fallback phases → treat as progress within running
    if (phase === 'failed' || phase === 'paused') {
      return phaseOrder.indexOf('running')
    }
    return 0
  }
  const currentPhaseIdx = resolveStepIndex(progress.phase)

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
          [...filteredMessages].reverse().map((msg, idx) => {
            const cfg = getTypeConfig(msg.type)
            const Icon = cfg.icon
            return (
              <div
                key={`${msg.seq_num}-${idx}`}
                className={`rounded-lg border border-border/60 bg-card/30 p-2.5 text-xs ${cfg.borderColor} border-l-[3px] transition-colors hover:bg-card/50`}
              >
                {!selectedAgentId && (
                  <div className="flex items-center justify-between gap-2 mb-1">
                    <div className="flex items-center gap-1.5 min-w-0">
                      <span className="flex h-4 w-4 shrink-0 items-center justify-center rounded-full bg-muted text-[7px] font-bold text-muted-foreground">
                        {msg.agent_name?.charAt(0)?.toUpperCase() || '?'}
                      </span>
                      <span className="font-semibold text-foreground truncate text-[11px]">
                        {msg.agent_name}
                      </span>
                      <span
                        className={`inline-flex items-center gap-0.5 rounded px-1 py-0.5 text-[8px] font-semibold font-mono leading-none ${cfg.badgeBg} ${cfg.badgeText}`}
                      >
                        <Icon className="h-2.5 w-2.5" />
                        {cfg.label}
                      </span>
                    </div>
                    {msg.round > 0 && (
                      <span className="shrink-0 text-[8px] text-muted-foreground/60 font-mono">
                        {formatRound(msg.round)}
                      </span>
                    )}
                  </div>
                )}
                <p
                  className={`text-foreground/85 leading-relaxed whitespace-pre-wrap ${!selectedAgentId ? 'line-clamp-2' : ''}`}
                >
                  {msg.content}
                </p>
                {msg.reasoning && selectedAgentId && (
                  <details className="mt-2 group">
                    <summary className="text-[8px] text-muted-foreground/50 cursor-pointer select-none hover:text-foreground font-mono tracking-wide flex items-center gap-1">
                      <span className="inline-block w-0 h-0 border-l-4 border-l-transparent border-t-4 border-t-current border-r-4 border-r-transparent group-open:rotate-90 transition-transform" />
                      LLM 推理过程
                    </summary>
                    <p className="mt-1 text-[8px] text-muted-foreground/70 italic bg-background/40 p-2 rounded border border-border/30 leading-relaxed whitespace-pre-wrap">
                      {msg.reasoning}
                    </p>
                  </details>
                )}
              </div>
            )
          })
        )}
        {/* End-state banner */}
        {progress?.phase && ['completed', 'failed', 'paused'].includes(progress.phase) && (
          <div
            className={`rounded-lg border p-2.5 text-[10px] ${
              progress.phase === 'completed'
                ? 'border-success/30 bg-success/5 text-success-foreground'
                : progress.phase === 'failed'
                  ? 'border-destructive/30 bg-destructive/5 text-destructive-foreground'
                  : 'border-amber-500/30 bg-amber-500/5 text-amber-700 dark:text-amber-300'
            }`}
          >
            <div className="flex items-center gap-1.5">
              {progress.phase === 'completed' ? (
                <CheckCircle2 className="h-3.5 w-3.5" />
              ) : progress.phase === 'failed' ? (
                <AlertCircle className="h-3.5 w-3.5" />
              ) : (
                <Pause className="h-3.5 w-3.5" />
              )}
              <span className="font-semibold">
                {progress.phase === 'completed'
                  ? '仿真已完成'
                  : progress.phase === 'failed'
                    ? '仿真失败'
                    : '仿真已暂停'}
              </span>
            </div>
            {progress.phase === 'completed' && progress.elapsed_seconds > 0 && (
              <div className="mt-1 text-[8px] text-muted-foreground/60 font-mono">
                耗时 {formatTime(progress.elapsed_seconds)}，共 {progress.current_actions} 次动作
              </div>
            )}
          </div>
        )}
        <div ref={timelineEndRef} />
      </div>
    </div>
  )
}
