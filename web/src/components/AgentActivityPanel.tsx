import type { SimulationMessage, AgentProgressState } from '@/types'
import { Bot, MessageCircle, MoveRight, Lightbulb, Clock } from 'lucide-react'

interface AgentActivityPanelProps {
  agentId: string
  agentName: string
  agentRole: string
  messages: SimulationMessage[]
  progress: { agent_states?: Record<string, AgentProgressState> } | null
}

export function AgentActivityPanel({
  agentId,
  agentName,
  agentRole,
  messages,
  progress,
}: AgentActivityPanelProps) {
  const agentMessages = messages.filter((m) => m.agent_id === agentId).reverse()
  const agentState = progress?.agent_states?.[agentId]

  return (
    <div className="flex flex-col h-full">
      {/* Agent header */}
      <div className="flex items-center gap-3 p-3 border-b border-border">
        <div className="flex items-center justify-center w-9 h-9 rounded-full bg-primary/10 text-primary text-sm font-bold">
          {agentName.charAt(0)}
        </div>
        <div className="flex-1 min-w-0">
          <h4 className="font-semibold text-sm text-foreground truncate">{agentName}</h4>
          <p className="text-[10px] text-muted-foreground truncate">{agentRole}</p>
        </div>
        {agentState && (
          <span
            className={`flex items-center gap-1 px-2 py-0.5 rounded-full text-[9px] font-mono ${
              agentState.status === 'thinking'
                ? 'bg-yellow-500/10 text-yellow-500'
                : agentState.status === 'spoke'
                  ? 'bg-green-500/10 text-green-500'
                  : 'bg-muted text-muted-foreground'
            }`}
          >
            <span
              className={`w-1.5 h-1.5 rounded-full ${
                agentState.status === 'thinking'
                  ? 'bg-yellow-500 animate-pulse'
                  : agentState.status === 'spoke'
                    ? 'bg-green-500'
                    : 'bg-muted-foreground'
              }`}
            />
            {agentState.status}
          </span>
        )}
      </div>

      {/* Activity timeline */}
      <div className="flex-1 overflow-y-auto p-3 space-y-2">
        {agentMessages.length === 0 && (
          <div className="flex flex-col items-center justify-center h-full text-muted-foreground gap-2">
            <Bot className="h-8 w-8 opacity-30" />
            <span className="text-xs">No activity yet</span>
          </div>
        )}
        {agentMessages.map((msg, i) => {
          const isPrivate = msg.type === 'private_speak' || msg.to !== '*'
          const isMovement = msg.type === 'agent_move'
          const isReflection = msg.type === 'reflection'
          const isQuestion = msg.content.endsWith('?')

          let Icon = MessageCircle
          let label = 'Spoke'
          let iconColor = 'text-blue-500'
          let bgColor = 'bg-blue-500/5'
          let borderColor = 'border-blue-500/20'

          if (isPrivate) {
            Icon = MessageCircle
            label = `Spoke @${msg.to}`
            iconColor = 'text-violet-500'
            bgColor = 'bg-violet-500/5'
            borderColor = 'border-violet-500/20'
          } else if (isMovement) {
            Icon = MoveRight
            label = 'Moved'
            iconColor = 'text-orange-500'
            bgColor = 'bg-orange-500/5'
            borderColor = 'border-orange-500/20'
          } else if (isReflection) {
            Icon = Lightbulb
            label = 'Reflected'
            iconColor = 'text-amber-500'
            bgColor = 'bg-amber-500/5'
            borderColor = 'border-amber-500/20'
          } else if (isQuestion) {
            Icon = MessageCircle
            label = 'Asked'
            iconColor = 'text-cyan-500'
            bgColor = 'bg-cyan-500/5'
            borderColor = 'border-cyan-500/20'
          }

          return (
            <div
              key={`${msg.agent_id}-${msg.seq_num}-${i}`}
              className={`rounded-lg border ${borderColor} ${bgColor} p-2.5 text-xs transition-colors`}
            >
              <div className="flex items-center gap-1.5 mb-1.5">
                <Icon className={`h-3.5 w-3.5 ${iconColor}`} />
                <span className={`font-mono text-[9px] font-semibold ${iconColor}`}>{label}</span>
                {msg.round > 0 && (
                  <span className="text-[8px] text-muted-foreground font-mono ml-auto">
                    <Clock className="h-3 w-3 inline mr-0.5 opacity-50" />#{msg.seq_num}
                  </span>
                )}
              </div>
              <p className="text-foreground/90 leading-relaxed whitespace-pre-wrap break-words">
                {msg.content.length > 400 ? msg.content.slice(0, 400) + '...' : msg.content}
              </p>
              {msg.reasoning && (
                <details className="mt-1.5">
                  <summary className="text-[9px] text-muted-foreground cursor-pointer hover:text-foreground transition-colors">
                    Reasoning
                  </summary>
                  <p className="mt-1 text-[9px] text-muted-foreground/80 italic leading-relaxed border-l-2 border-muted pl-2">
                    {msg.reasoning}
                  </p>
                </details>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}
