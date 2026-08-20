import { useDraggable } from '@dnd-kit/core'
import { AlertCircle, Bot, Loader2, Plus } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useTranslation } from '@/lib/i18n'
import type { AgentResponse } from '@/types'

// ─── Types ──────────────────────────────────────────────────────────────

interface AgentCardProps {
  agent: AgentResponse
  onAdd: () => void
}

// ─── AgentCard (draggable) ──────────────────────────────────────────────

function AgentCard({ agent, onAdd }: AgentCardProps) {
  const { attributes, listeners, setNodeRef, isDragging } = useDraggable({
    id: `agent-palette-${agent.id}`,
    data: { type: 'agent', template: agent.id },
  })

  return (
    <div
      ref={setNodeRef}
      {...listeners}
      {...attributes}
      className={cn(
        'mx-2 rounded-lg border border-border/60 bg-card hover:border-primary/40 px-3 py-2.5 transition-all',
        isDragging && 'opacity-50 shadow-lg ring-2 ring-primary/20'
      )}
    >
      <div
        {...listeners}
        {...attributes}
        className="flex cursor-grab items-center gap-2 active:cursor-grabbing"
        title={agent.description}
      >
        <Bot className="h-3.5 w-3.5 shrink-0 text-primary" />
        <span className="min-w-0 flex-1 truncate text-xs font-medium text-foreground">{agent.name}</span>
        <button
          type="button"
          onPointerDown={(event) => event.stopPropagation()}
          onClick={onAdd}
          className="rounded-md p-1 text-muted-foreground hover:bg-primary/10 hover:text-primary"
          title="Add node"
        >
          <Plus className="h-3.5 w-3.5" />
        </button>
      </div>
      <div className="flex items-center gap-1.5 mt-1">
        <span className="text-[10px] text-muted-foreground truncate">{agent.team_name || agent.description}</span>
        {agent.model && (
          <span className="text-[9px] bg-primary/10 text-primary px-1 py-0.5 rounded font-mono">
            {agent.model}
          </span>
        )}
      </div>
    </div>
  )
}

// ─── AgentPalette ───────────────────────────────────────────────────────

interface AgentPaletteProps {
  agents: AgentResponse[]
  loading: boolean
  error: string | null
  entryNodes: string[]
  onAddAgent: (template: string) => void
  onAutoLayout?: () => void
}

export function AgentPalette({
  agents,
  loading,
  error,
  entryNodes,
  onAddAgent,
  onAutoLayout,
}: AgentPaletteProps) {
  const { t } = useTranslation()

  return (
    <div className="w-52 shrink-0 border-r border-border/40 bg-muted/5 flex flex-col h-full overflow-hidden">
      {/* Agents section */}
      <div className="flex-1 overflow-y-auto">
        <div className="text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono px-3 mt-3 mb-2">
          {t('workflow.agentsPalette')}
        </div>
        <p className="px-3 pb-2 text-[10px] leading-relaxed text-muted-foreground">
          {t('workflow.agentPaletteHint')}
        </p>

        <div className="space-y-1.5">
          {agents.map((agent) => (
            <AgentCard
              key={agent.id}
              agent={agent}
              onAdd={() => onAddAgent(agent.id)}
            />
          ))}
          {loading && (
            <div className="flex items-center gap-2 px-3 py-2 text-[10px] text-muted-foreground">
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
              {t('common.loading')}
            </div>
          )}
          {!loading && error && (
            <div className="mx-2 flex gap-2 rounded-lg border border-rose-500/20 bg-rose-500/5 p-2 text-[10px] text-rose-500">
              <AlertCircle className="h-3.5 w-3.5 shrink-0" />
              {t('workflow.agentsLoadFailed')}
            </div>
          )}
          {!loading && !error && agents.length === 0 && (
            <div className="mx-2 rounded-lg border border-dashed border-border p-3 text-[10px] leading-relaxed text-muted-foreground">
              {t('workflow.noAgentsAvailable')}
            </div>
          )}
        </div>

        {/* Entry nodes section */}
        <div className="text-[10px] font-bold text-muted-foreground uppercase tracking-wider font-mono px-3 mt-4 mb-2">
          {t('workflow.entryNodes')}
        </div>

        <div className="space-y-1 px-2">
          {entryNodes.map((nodeId) => (
            <div
              key={nodeId}
              className="flex items-center gap-1.5 rounded-md bg-accent/5 border border-accent/20 px-2 py-1.5"
            >
              <span className="h-1.5 w-1.5 rounded-full bg-accent shrink-0" />
              <span className="text-[10px] font-mono text-foreground truncate">{nodeId}</span>
            </div>
          ))}
          {entryNodes.length === 0 && (
            <p className="px-1 text-[10px] text-muted-foreground/60">
              {t('workflow.noRuns')}
            </p>
          )}
        </div>
      </div>

      {/* Actions */}
      <div className="shrink-0 border-t border-border/40 p-2 space-y-1.5 bg-muted/10">
        <button
          type="button"
          onClick={onAutoLayout}
          className="flex w-full items-center justify-center gap-1.5 rounded-lg border border-border/60 px-3 py-2 text-[10px] text-muted-foreground hover:text-foreground hover:bg-muted/40 transition-colors font-mono"
        >
          {t('workflow.autoLayout')}
        </button>
      </div>
    </div>
  )
}
