import { useDraggable } from '@dnd-kit/core'
import { Bot } from 'lucide-react'
import { cn } from '@/lib/utils'
import { useTranslation } from '@/lib/i18n'

// ─── Types ──────────────────────────────────────────────────────────────

interface AgentCardProps {
  agentKey: string
  template: string
  model?: string
}

// ─── AgentCard (draggable) ──────────────────────────────────────────────

function AgentCard({ agentKey, template, model }: AgentCardProps) {
  const { attributes, listeners, setNodeRef, isDragging } = useDraggable({
    id: `agent-palette-${agentKey}`,
    data: { type: 'agent', agentKey, template, model },
  })

  return (
    <div
      ref={setNodeRef}
      {...listeners}
      {...attributes}
      className={cn(
        'mx-2 rounded-lg border border-border/60 bg-card hover:border-primary/40 cursor-grab active:cursor-grabbing px-3 py-2.5 transition-all',
        isDragging && 'opacity-50 shadow-lg ring-2 ring-primary/20'
      )}
    >
      <div className="flex items-center gap-2">
        <Bot className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
        <span className="text-xs font-medium text-foreground truncate">{agentKey}</span>
      </div>
      <div className="flex items-center gap-1.5 mt-1">
        <span className="text-[10px] text-muted-foreground truncate">{template}</span>
        {model && (
          <span className="text-[9px] bg-primary/10 text-primary px-1 py-0.5 rounded font-mono">
            {model}
          </span>
        )}
      </div>
    </div>
  )
}

// ─── AgentPalette ───────────────────────────────────────────────────────

interface AgentPaletteProps {
  agents: Record<string, { template: string; model?: string }>
  entryNodes: string[]
  onAddAgent?: () => void
  onAutoLayout?: () => void
}

export function AgentPalette({
  agents,
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

        <div className="space-y-1.5">
          {Object.entries(agents).map(([key, ref]) => (
            <AgentCard
              key={key}
              agentKey={key}
              template={ref.template}
              model={ref.model}
            />
          ))}
          {Object.keys(agents).length === 0 && (
            <p className="px-3 text-[10px] text-muted-foreground/60">
              {t('workflow.noWorkflowsYet')}
            </p>
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
        <button
          type="button"
          onClick={onAddAgent}
          className="flex w-full items-center justify-center gap-1.5 rounded-lg border border-border/60 px-3 py-2 text-[10px] text-muted-foreground hover:text-foreground hover:bg-muted/40 transition-colors font-mono"
        >
          {t('workflow.addAgent')}
        </button>
      </div>
    </div>
  )
}
